package server

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Live mode (PROTOCOL.md section 5): the server pairs two peers on a numbered channel and
// passes their messages along. It never sees the code, so it cannot read what passes.

const (
	liveJoinTimeout = 30 * time.Second // the first message after connecting must come soon
	liveMaxMessage  = 1<<20 + 4096     // one 1 MiB binary message, or a small JSON one
	liveMaxChannels = 10000
)

// Close codes the pages turn into plain words.
const (
	closeUnknownChannel websocket.StatusCode = 4404 // no such channel, or it already ended
	closeChannelTaken   websocket.StatusCode = 4409 // both places are taken
	closeBadJoin        websocket.StatusCode = 4400 // the first message was not a valid join
	closePeerLeft       websocket.StatusCode = 4410 // the other side left: the channel is gone
	closeExpired        websocket.StatusCode = 4408 // nobody joined in time
	closeChannelLimit   websocket.StatusCode = 4413 // too long, or too many bytes
)

type liveChannel struct {
	number    int
	tokenHash []byte

	mu       sync.Mutex
	sender   *websocket.Conn
	receiver *websocket.Conn
	relayed  int64

	ctx    context.Context // ends when the channel ends
	cancel context.CancelFunc
	ready  chan struct{} // closed when both peers are in and have the session id
	once   sync.Once
	reason websocket.StatusCode
}

type liveHub struct {
	mu       sync.Mutex
	channels map[int]*liveChannel
}

func (s *Server) liveRoutes() {
	s.live = &liveHub{channels: map[int]*liveChannel{}}
	s.mux.HandleFunc("POST /api/v1/live", s.createLive)
	s.mux.HandleFunc("GET /api/v1/live/{channel}", s.joinLive)
}

func (s *Server) createLive(w http.ResponseWriter, r *http.Request) {
	if !s.liveLimit.allow(s.clientAddress(r)) {
		writeError(w, http.StatusTooManyRequests, "too_many_requests")
		return
	}
	token := randomToken(32)
	ch, ok := s.live.open(hashToken(token), s.cfg.LiveSession)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "busy")
		return
	}
	// A channel nobody joins in time is closed, so its number is free again.
	time.AfterFunc(s.cfg.LiveWait, func() {
		select {
		case <-ch.ready:
		default:
			s.live.end(ch, closeExpired)
		}
	})
	writeJSON(w, http.StatusCreated, map[string]any{"channel": ch.number, "token": b64.EncodeToString(token)})
}

// open picks a free channel number, short while few are in use.
func (h *liveHub) open(tokenHash []byte, session time.Duration) (*liveChannel, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.channels) >= liveMaxChannels {
		return nil, false
	}
	upper := 1000
	if len(h.channels) > 500 {
		upper = 100000
	}
	for {
		n := 1 + rand.IntN(upper-1)
		if _, used := h.channels[n]; !used {
			ctx, cancel := context.WithTimeout(context.Background(), session)
			ch := &liveChannel{number: n, tokenHash: tokenHash, ctx: ctx, cancel: cancel, ready: make(chan struct{})}
			h.channels[n] = ch
			context.AfterFunc(ctx, func() { h.end(ch, closeChannelLimit) })
			return ch, true
		}
	}
}

func (h *liveHub) get(n int) *liveChannel {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.channels[n]
}

// end closes a channel for good: both connections go, and its number is free again.
func (h *liveHub) end(ch *liveChannel, reason websocket.StatusCode) {
	ch.once.Do(func() {
		h.mu.Lock()
		if h.channels[ch.number] == ch {
			delete(h.channels, ch.number)
		}
		h.mu.Unlock()
		ch.mu.Lock()
		ch.reason = reason
		conns := []*websocket.Conn{ch.sender, ch.receiver}
		ch.mu.Unlock()
		// Close first, so each side learns why; cancelling a read would cut the connection
		// without a reason. The closing handshakes run side by side.
		go func() {
			var wg sync.WaitGroup
			for _, c := range conns {
				if c != nil {
					wg.Go(func() { c.Close(reason, "") })
				}
			}
			wg.Wait()
			ch.cancel()
		}()
	})
}

type liveJoin struct {
	T     string `json:"t"`
	Role  string `json:"role"`
	Token string `json:"token,omitempty"`
}

func (s *Server) joinLive(w http.ResponseWriter, r *http.Request) {
	// The browser checks nothing else for WebSockets: Accept refuses pages from other sites.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(liveMaxMessage)
	n, err := strconv.Atoi(r.PathValue("channel"))
	ch := s.live.get(n)
	if err != nil || ch == nil {
		c.Close(closeUnknownChannel, "")
		return
	}

	joinCtx, cancel := context.WithTimeout(r.Context(), liveJoinTimeout)
	typ, data, err := c.Read(joinCtx)
	cancel()
	var join liveJoin
	if err != nil || typ != websocket.MessageText || json.Unmarshal(data, &join) != nil || join.T != "join" {
		c.Close(closeBadJoin, "")
		return
	}

	ch.mu.Lock()
	var peer *websocket.Conn
	switch {
	case ch.reason != 0:
		ch.mu.Unlock()
		c.Close(closeUnknownChannel, "")
		return
	case join.Role == "send" && ch.sender == nil && validToken(join.Token, ch.tokenHash):
		ch.sender, peer = c, ch.receiver
	case join.Role == "receive" && ch.receiver == nil:
		ch.receiver, peer = c, ch.sender
	case join.Role == "send" && ch.sender == nil:
		ch.mu.Unlock()
		c.Close(closeBadJoin, "")
		return
	default:
		ch.mu.Unlock()
		c.Close(closeChannelTaken, "")
		return
	}
	ch.mu.Unlock()

	if peer != nil {
		// The second one in starts the session: both get the same fresh session id.
		start, _ := json.Marshal(map[string]string{"t": "start", "sid": b64.EncodeToString(randomToken(16))})
		if peer.Write(ch.ctx, websocket.MessageText, start) != nil || c.Write(ch.ctx, websocket.MessageText, start) != nil {
			s.live.end(ch, closePeerLeft)
			return
		}
		close(ch.ready)
	}

	select {
	case <-ch.ready:
	case <-ch.ctx.Done():
		return
	}
	ch.mu.Lock()
	peer = ch.sender
	if peer == c {
		peer = ch.receiver
	}
	ch.mu.Unlock()
	s.relay(ch, c, peer)
}

// relay passes every message from one peer to the other, until either leaves or a limit is hit.
func (s *Server) relay(ch *liveChannel, from, to *websocket.Conn) {
	reason := closePeerLeft
	defer func() { s.live.end(ch, reason) }()
	for {
		typ, data, err := from.Read(ch.ctx)
		if err != nil {
			return
		}
		ch.mu.Lock()
		ch.relayed += int64(len(data))
		over := ch.relayed > s.cfg.LiveMaxBytes
		ch.mu.Unlock()
		if over {
			reason = closeChannelLimit
			return
		}
		if err := to.Write(ch.ctx, typ, data); err != nil {
			return
		}
	}
}

func validToken(token string, hash []byte) bool {
	t, err := b64.DecodeString(token)
	return err == nil && tokenMatches(t, hash)
}
