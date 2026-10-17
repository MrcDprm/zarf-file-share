package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func liveServer(t *testing.T, change func(*Config)) (*Server, *httptest.Server) {
	t.Helper()
	s, _ := newTestServer(t, func(c *Config) {
		c.Now = time.Now // channels use real timers
		if change != nil {
			change(c)
		}
	})
	hs := httptest.NewServer(s)
	t.Cleanup(hs.Close)
	return s, hs
}

func openChannel(t *testing.T, s *Server) (int, string) {
	t.Helper()
	res, out := do(t, s, call{method: "POST", path: "/api/v1/live"})
	if res.StatusCode != 201 {
		t.Fatalf("create channel: %d %v", res.StatusCode, out)
	}
	return int(out["channel"].(float64)), out["token"].(string)
}

func dial(t *testing.T, hs *httptest.Server, channel int, join map[string]string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, strings.Replace(hs.URL, "http", "ws", 1)+fmt.Sprintf("/api/v1/live/%d", channel), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(liveMaxMessage)
	t.Cleanup(func() { c.CloseNow() })
	if join != nil {
		b, _ := json.Marshal(join)
		if err := c.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func read(t *testing.T, c *websocket.Conn) (websocket.MessageType, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	return c.Read(ctx)
}

func closedWith(t *testing.T, c *websocket.Conn, want websocket.StatusCode) {
	t.Helper()
	for {
		_, _, err := read(t, c)
		if err != nil {
			if got := websocket.CloseStatus(err); got != want {
				t.Errorf("closed with %d (%v), want %d", got, err, want)
			}
			return
		}
	}
}

func TestLiveRelay(t *testing.T) {
	s, hs := liveServer(t, nil)
	n, token := openChannel(t, s)
	if n < 1 || n > 999 {
		t.Errorf("channel number %d is not short", n)
	}
	sender := dial(t, hs, n, map[string]string{"t": "join", "role": "send", "token": token})
	receiver := dial(t, hs, n, map[string]string{"t": "join", "role": "receive"})

	var sids []string
	for _, c := range []*websocket.Conn{sender, receiver} {
		_, msg, err := read(t, c)
		var start map[string]string
		if err != nil || json.Unmarshal(msg, &start) != nil || start["t"] != "start" {
			t.Fatalf("start: %v %s", err, msg)
		}
		sids = append(sids, start["sid"])
	}
	if sids[0] != sids[1] || len(sids[0]) != 22 {
		t.Fatalf("session ids %v", sids)
	}

	ctx := t.Context()
	// Text and binary messages pass unchanged, both ways.
	_ = sender.Write(ctx, websocket.MessageText, []byte(`{"t":"pake","y":"abc"}`))
	if typ, msg, _ := read(t, receiver); typ != websocket.MessageText || string(msg) != `{"t":"pake","y":"abc"}` {
		t.Errorf("text: %v %s", typ, msg)
	}
	big := envelopeBytes(1 << 20)
	_ = sender.Write(ctx, websocket.MessageBinary, big)
	if typ, msg, _ := read(t, receiver); typ != websocket.MessageBinary || string(msg) != string(big) {
		t.Errorf("binary: %v, %d bytes", typ, len(msg))
	}
	_ = receiver.Write(ctx, websocket.MessageText, []byte(`{"t":"accept"}`))
	if _, msg, _ := read(t, sender); string(msg) != `{"t":"accept"}` {
		t.Errorf("back: %s", msg)
	}

	// One side leaves: the other is told, and the channel is gone for good.
	receiver.Close(websocket.StatusNormalClosure, "")
	closedWith(t, sender, closePeerLeft)
	late := dial(t, hs, n, map[string]string{"t": "join", "role": "receive"})
	closedWith(t, late, closeUnknownChannel)
}

func TestLiveJoining(t *testing.T) {
	s, hs := liveServer(t, nil)
	closedWith(t, dial(t, hs, 5, map[string]string{"t": "join", "role": "receive"}), closeUnknownChannel)

	n, token := openChannel(t, s)
	// Only the creator's token takes the sender's place.
	closedWith(t, dial(t, hs, n, map[string]string{"t": "join", "role": "send", "token": b64.EncodeToString(randomToken(32))}), closeBadJoin)
	closedWith(t, dial(t, hs, n, map[string]string{"t": "hello"}), closeBadJoin)

	dial(t, hs, n, map[string]string{"t": "join", "role": "send", "token": token})
	dial(t, hs, n, map[string]string{"t": "join", "role": "receive"})
	// A third person, for instance someone trying codes, finds the places taken.
	closedWith(t, dial(t, hs, n, map[string]string{"t": "join", "role": "receive"}), closeChannelTaken)
}

func TestLiveLimits(t *testing.T) {
	s, hs := liveServer(t, func(c *Config) {
		c.LiveWait = 200 * time.Millisecond
		c.LiveMaxBytes = 3000
	})
	// Nobody comes: the channel closes and the sender is told.
	n, token := openChannel(t, s)
	sender := dial(t, hs, n, map[string]string{"t": "join", "role": "send", "token": token})
	closedWith(t, sender, closeExpired)

	// Too many bytes through one channel.
	n, token = openChannel(t, s)
	sender = dial(t, hs, n, map[string]string{"t": "join", "role": "send", "token": token})
	receiver := dial(t, hs, n, map[string]string{"t": "join", "role": "receive"})
	read(t, sender)
	read(t, receiver)
	for i := 0; i < 4; i++ {
		_ = sender.Write(t.Context(), websocket.MessageBinary, make([]byte, 1000))
	}
	closedWith(t, receiver, closeChannelLimit)

	// Walking through channel numbers is cut short.
	for i := 0; i < 60; i++ {
		dial(t, hs, 999, map[string]string{"t": "join", "role": "receive"}).CloseNow()
	}
	closedWith(t, dial(t, hs, 999, map[string]string{"t": "join", "role": "receive"}), closeTooManyJoins)

	// A page from another site cannot open a channel.
	n, _ = openChannel(t, s)
	_, res, err := websocket.Dial(t.Context(), strings.Replace(hs.URL, "http", "ws", 1)+fmt.Sprintf("/api/v1/live/%d", n),
		&websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example"}}})
	if err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site WebSocket: %v %v", err, res)
	}
}

// The real server has a read timeout for ordinary requests; a live session outlives it.
func TestLiveOutlivesTheReadTimeout(t *testing.T) {
	s, _ := newTestServer(t, func(c *Config) { c.Now = time.Now })
	hs := httptest.NewUnstartedServer(s)
	hs.Config.ReadTimeout = 500 * time.Millisecond
	hs.Start()
	t.Cleanup(hs.Close)

	n, token := openChannel(t, s)
	sender := dial(t, hs, n, map[string]string{"t": "join", "role": "send", "token": token})
	receiver := dial(t, hs, n, map[string]string{"t": "join", "role": "receive"})
	read(t, sender)
	read(t, receiver)
	time.Sleep(time.Second) // longer than the read timeout
	if err := sender.Write(t.Context(), websocket.MessageText, []byte(`{"t":"pake"}`)); err != nil {
		t.Fatal(err)
	}
	if _, msg, err := read(t, receiver); err != nil || string(msg) != `{"t":"pake"}` {
		t.Fatalf("after the read timeout: %v %s", err, msg)
	}
}
