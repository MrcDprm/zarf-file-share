// Package live is the command line side of live mode (PROTOCOL.md section 5.2). It speaks
// the same messages as web/src/lib/live.ts; the command line always takes the relay path.
package live

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/MrcDprm/zarf-file-share/internal/cpace"
	"github.com/MrcDprm/zarf-file-share/internal/envelope"
	"github.com/MrcDprm/zarf-file-share/internal/words"
	"github.com/coder/websocket"
)

var b64 = base64.RawURLEncoding

// Error is a failure the command can explain in plain words.
type Error struct{ Problem string }

func (e *Error) Error() string {
	switch e.Problem {
	case "unknown_channel":
		return "this code is invalid or already used; ask the sender for a new one"
	case "taken":
		return "someone else already connected with this code; ask the sender for a new one"
	case "peer_left":
		return "the other side left; the connection is closed"
	case "limit":
		return "the connection went over its time or size limit"
	case "expired":
		return "nobody connected and the code expired; get a new code"
	case "wrong_code":
		return "the code doesn't match; the connection closed for safety, so a new code is needed"
	case "declined":
		return "the recipient declined the file"
	case "damaged":
		return "the file was changed or damaged on the way; nothing was saved"
	}
	return "the connection broke: " + e.Problem
}

func fail(p string) error { return &Error{p} }

var closeProblems = map[websocket.StatusCode]string{4404: "unknown_channel", 4400: "unknown_channel", 4409: "taken", 4410: "peer_left", 4413: "limit", 4408: "expired"}

// Conn is the part of a WebSocket the protocol uses.
type Conn interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
}

func readErr(err error) error {
	if p, ok := closeProblems[websocket.CloseStatus(err)]; ok {
		return fail(p)
	}
	return fail(err.Error())
}

func sendJSON(ctx context.Context, c Conn, m map[string]any) error {
	b, _ := json.Marshal(m)
	return c.Write(ctx, websocket.MessageText, b)
}

// expect reads the next message, which must be JSON of one of the given types.
func expect(ctx context.Context, c Conn, types ...string) (map[string]any, error) {
	typ, data, err := c.Read(ctx)
	if err != nil {
		return nil, readErr(err)
	}
	var m map[string]any
	if typ != websocket.MessageText || json.Unmarshal(data, &m) != nil {
		return nil, fail("damaged")
	}
	for _, t := range types {
		if m["t"] == t {
			return m, nil
		}
	}
	return nil, fail("damaged")
}

func field(m map[string]any, k string) []byte {
	s, _ := m[k].(string)
	b, err := b64.DecodeString(s)
	if err != nil {
		return nil
	}
	return b
}

// Handshake joins the channel, runs CPace and key confirmation, and agrees on the relay
// path. It returns the envelope's input key.
func Handshake(ctx context.Context, c Conn, role string, code words.Code, token string) ([]byte, error) {
	join := map[string]any{"t": "join", "role": role}
	if role == "send" {
		join["token"] = token
	}
	if err := sendJSON(ctx, c, join); err != nil {
		return nil, fail("network")
	}
	start, err := expect(ctx, c, "start")
	if err != nil {
		return nil, err
	}
	sid := field(start, "sid")
	if len(sid) != 16 {
		return nil, fail("damaged")
	}

	prs := append([]byte("zarf v1 live"), byte(code.Positions[0]>>8), byte(code.Positions[0]), byte(code.Positions[1]>>8), byte(code.Positions[1]))
	ci := cpace.LVCat([]byte("zarf v1 live"), []byte(strconv.Itoa(code.Channel)))
	me, err := cpace.NewShare(prs, ci, sid, nil)
	if err != nil {
		return nil, err
	}
	if err := sendJSON(ctx, c, map[string]any{"t": "pake", "y": b64.EncodeToString(me.Y)}); err != nil {
		return nil, fail("network")
	}
	pm, err := expect(ctx, c, "pake")
	if err != nil {
		return nil, err
	}
	peerY := field(pm, "y")
	ya, yb := me.Y, peerY
	if role == "receive" {
		ya, yb = peerY, me.Y
	}
	isk, err := me.ISK(peerY, sid, ya, []byte("send"), yb, []byte("receive"))
	if err != nil {
		return nil, fail("wrong_code")
	}
	confirmKey, err := hkdf.Key(sha256.New, isk, sid, "zarf v1 confirm", 32)
	if err != nil {
		return nil, err
	}
	mac := func(who string) []byte {
		h := hmac.New(sha256.New, confirmKey)
		h.Write([]byte(who))
		h.Write(ya)
		h.Write(yb)
		return h.Sum(nil)
	}
	other := "receive"
	if role == "receive" {
		other = "send"
	}
	if err := sendJSON(ctx, c, map[string]any{"t": "confirm", "mac": b64.EncodeToString(mac(role))}); err != nil {
		return nil, fail("network")
	}
	cm, err := expect(ctx, c, "confirm")
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(field(cm, "mac"), mac(other)) {
		return nil, fail("wrong_code")
	}
	ikm, err := hkdf.Key(sha256.New, isk, sid, "zarf v1 live content", 32)
	if err != nil {
		return nil, err
	}

	// The path: this tool cannot open WebRTC connections, so it is always the relay.
	if err := sendJSON(ctx, c, map[string]any{"t": "hello", "direct": false}); err != nil {
		return nil, fail("network")
	}
	if _, err := expect(ctx, c, "hello"); err != nil {
		return nil, err
	}
	if role == "send" {
		if err := sendJSON(ctx, c, map[string]any{"t": "path", "via": "relay"}); err != nil {
			return nil, fail("network")
		}
	} else if p, err := expect(ctx, c, "path"); err != nil {
		return nil, err
	} else if p["via"] != "relay" {
		return nil, fail("damaged")
	}
	return ikm, nil
}

// messageWriter sends each write as one binary message (the envelope writer writes one
// sealed chunk at a time, far below the relay's 1 MiB per message).
type messageWriter struct {
	ctx context.Context
	c   Conn
}

func (w *messageWriter) Write(p []byte) (int, error) {
	if err := w.c.Write(w.ctx, websocket.MessageBinary, p); err != nil {
		return 0, fail("peer_left")
	}
	return len(p), nil
}

// switchWriter holds the envelope's head until the receiver accepts, then streams.
type switchWriter struct{ w io.Writer }

func (s *switchWriter) Write(p []byte) (int, error) { return s.w.Write(p) }

// Send offers the file, waits for the answer, and sends it. waiting is called while the
// receiver decides; progress counts plaintext bytes.
func Send(ctx context.Context, c Conn, ikm []byte, src io.Reader, name, mime string, size int64, waiting func(), progress func(n int64)) error {
	var head bytes.Buffer
	sw := &switchWriter{w: &head}
	w, err := envelope.NewWriter(sw, envelope.Params{Mode: envelope.ModeLive}, ikm, envelope.Metadata{Files: []envelope.File{{Name: name, Size: size, Type: mime}}})
	if err != nil {
		return err
	}
	if err := c.Write(ctx, websocket.MessageBinary, head.Bytes()); err != nil {
		return fail("peer_left")
	}
	waiting()
	answer, err := expect(ctx, c, "accept", "decline")
	if err != nil {
		return err
	}
	if answer["t"] == "decline" {
		return fail("declined")
	}
	sw.w = &messageWriter{ctx: ctx, c: c}
	if _, err := io.Copy(w, &counting{r: src, add: progress}); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	if err := sendJSON(ctx, c, map[string]any{"t": "done"}); err != nil {
		return fail("peer_left")
	}
	_, err = expect(ctx, c, "received")
	return err
}

type counting struct {
	r   io.Reader
	add func(int64)
}

func (c *counting) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.add != nil {
		c.add(int64(n))
	}
	return n, err
}

// messageReader turns the binary messages into a stream that ends at {"t":"done"}.
type messageReader struct {
	ctx  context.Context
	c    Conn
	buf  []byte
	done bool
}

func (r *messageReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		if r.done {
			return 0, io.EOF
		}
		typ, data, err := r.c.Read(r.ctx)
		if err != nil {
			return 0, readErr(err)
		}
		if typ == websocket.MessageText {
			var m map[string]any
			if json.Unmarshal(data, &m) != nil || m["t"] != "done" {
				return 0, fail("damaged")
			}
			r.done = true
			continue
		}
		r.buf = data
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

// Incoming is an offered file. Read the content from Reader after Accept; it ends with
// io.EOF only after the last chunk verified. Then call Finish.
type Incoming struct {
	File   envelope.File
	Reader *envelope.Reader
	ctx    context.Context
	c      Conn
}

// Receive reads the offer: the file's name and size, before any content is sent.
func Receive(ctx context.Context, c Conn, ikm []byte) (*Incoming, error) {
	mr := &messageReader{ctx: ctx, c: c}
	r, err := envelope.Open(mr, func(h *envelope.Header) ([]byte, error) {
		if h.Mode != envelope.ModeLive {
			return nil, errors.New("not a live envelope")
		}
		return ikm, nil
	})
	if err != nil {
		var e *Error
		if errors.As(err, &e) {
			return nil, err
		}
		return nil, fail("damaged")
	}
	if len(r.Metadata.Files) != 1 {
		return nil, fail("damaged")
	}
	return &Incoming{File: r.Metadata.Files[0], Reader: r, ctx: ctx, c: c}, nil
}

func (in *Incoming) Accept() error { return sendJSON(in.ctx, in.c, map[string]any{"t": "accept"}) }

func (in *Incoming) Decline() error { return sendJSON(in.ctx, in.c, map[string]any{"t": "decline"}) }

// Finish tells the sender that everything arrived and verified.
func (in *Incoming) Finish() error { return sendJSON(in.ctx, in.c, map[string]any{"t": "received"}) }

// DamagedIfEnvelope turns envelope errors (a changed or cut stream) into a plain "damaged".
func DamagedIfEnvelope(err error) error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return err
	}
	for _, env := range []error{envelope.ErrAuth, envelope.ErrTruncated, envelope.ErrTrailing, envelope.ErrWrongKey} {
		if errors.Is(err, env) {
			return fail("damaged")
		}
	}
	return fmt.Errorf("receiving: %w", err)
}
