package live

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrcDprm/zarf-file-share/internal/server"
	"github.com/MrcDprm/zarf-file-share/internal/words"
	"github.com/coder/websocket"
)

func relay(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := server.New(server.DefaultConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	hs := httptest.NewServer(s)
	t.Cleanup(hs.Close)
	return hs
}

func channel(t *testing.T, hs *httptest.Server) (int, string) {
	t.Helper()
	res, err := http.Post(hs.URL+"/api/v1/live", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Channel int    `json:"channel"`
		Token   string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Channel, out.Token
}

func connect(t *testing.T, ctx context.Context, hs *httptest.Server, n int) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, strings.Replace(hs.URL, "http", "ws", 1)+fmt.Sprintf("/api/v1/live/%d", n), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(1<<20 + 4096)
	t.Cleanup(func() { c.CloseNow() })
	return c
}

type result struct {
	sent error
	got  []byte
	recv error
}

func transfer(t *testing.T, content []byte, receiverPositions [2]int, accept bool) result {
	t.Helper()
	hs := relay(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	n, token := channel(t, hs)
	code := words.Code{Channel: n, Positions: [2]int{3, 200}}
	theirs := words.Code{Channel: n, Positions: receiverPositions}

	sentc := make(chan error, 1)
	go func() {
		c := connect(t, ctx, hs, n)
		ikm, err := Handshake(ctx, c, "send", code, token)
		if err == nil {
			err = Send(ctx, c, ikm, bytes.NewReader(content), "rapor ğüş.pdf", "application/pdf", int64(len(content)), func() {}, nil)
		}
		if err != nil {
			c.Close(websocket.StatusNormalClosure, "")
		}
		sentc <- err
	}()

	var r result
	c := connect(t, ctx, hs, n)
	ikm, err := Handshake(ctx, c, "receive", theirs, "")
	if err == nil {
		var in *Incoming
		if in, err = Receive(ctx, c, ikm); err == nil {
			if in.File.Name != "rapor ğüş.pdf" || in.File.Size != int64(len(content)) {
				t.Errorf("offer %+v", in.File)
			}
			if !accept {
				err = in.Decline()
			} else if err = in.Accept(); err == nil {
				if r.got, err = io.ReadAll(in.Reader); err == nil {
					err = in.Finish()
				}
			}
		}
	}
	if err != nil {
		c.Close(websocket.StatusNormalClosure, "")
	}
	r.recv = DamagedIfEnvelope(err)
	r.sent = <-sentc
	return r
}

func problem(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Problem
	}
	return fmt.Sprint(err)
}

func TestTheSameCodeMovesTheFile(t *testing.T) {
	content := bytes.Repeat([]byte("zarf "), 300_000) // 1.5 MB: many chunks
	r := transfer(t, content, [2]int{3, 200}, true)
	if r.sent != nil || r.recv != nil || !bytes.Equal(r.got, content) {
		t.Fatalf("sent %v, received %v, %d bytes", r.sent, r.recv, len(r.got))
	}
}

func TestAWrongCodeStopsBothSides(t *testing.T) {
	r := transfer(t, []byte("gizli"), [2]int{3, 201}, true)
	if problem(r.sent) != "wrong_code" || problem(r.recv) != "wrong_code" {
		t.Fatalf("sent %v, received %v", r.sent, r.recv)
	}
}

func TestTheReceiverCanDecline(t *testing.T) {
	r := transfer(t, []byte("istenmeyen"), [2]int{3, 200}, false)
	if problem(r.sent) != "declined" || r.recv != nil {
		t.Fatalf("sent %v, received %v", r.sent, r.recv)
	}
}
