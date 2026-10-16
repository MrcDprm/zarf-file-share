package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MrcDprm/zarf-file-share/internal/envelope"
	"github.com/MrcDprm/zarf-file-share/internal/live"
	"github.com/MrcDprm/zarf-file-share/internal/words"
	"github.com/coder/websocket"
)

// openSource opens a file to send and checks it fits.
func openSource(path string) (*os.File, os.FileInfo, error) {
	in, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := in.Stat()
	if err == nil && info.IsDir() {
		err = errors.New("folders are not supported; pack it into a .zip first")
	} else if err == nil && info.Size() > maxFile {
		err = errors.New("the file is too large: the limit is 1024 MB")
	}
	if err != nil {
		in.Close()
		return nil, nil, err
	}
	return in, info, nil
}

// encryptToTemp seals a file into a private temporary file: the server needs the exact
// size up front. The caller removes it.
func encryptToTemp(in *os.File, info os.FileInfo, p envelope.Params, ikm []byte) (*os.File, int64, error) {
	tmp, err := os.CreateTemp("", "zarf-send-*")
	if err != nil {
		return nil, 0, err
	}
	name := filepath.Base(in.Name())
	meta := envelope.Metadata{Files: []envelope.File{{Name: name, Size: info.Size(), Type: mimeType(name)}}}
	if err := writeEnvelope(tmp, in, p, ikm, meta); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, 0, err
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, 0, err
	}
	return tmp, size, nil
}

// --- request mode: answering a request ---

// parseRequestLink splits https://host/r/<rid>#<public key>.
func parseRequestLink(link string) (server, rid string, pub []byte, err error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Host == "" {
		return "", "", nil, errors.New("that is not a Zarf request link")
	}
	rest, ok := strings.CutPrefix(u.Path, "/r/")
	if !ok || rest == "" || strings.Contains(rest, "/") {
		return "", "", nil, errors.New("that is not a Zarf request link")
	}
	pub, err = b64.DecodeString(u.Fragment)
	if err != nil || len(pub) != 32 {
		return "", "", nil, errors.New("the link has no key (the part after #); copy the whole link")
	}
	return u.Scheme + "://" + u.Host, rest, pub, nil
}

type requestInfo struct {
	Label       string        `json:"label"`
	ExpiresAt   time.Time     `json:"expires_at"`
	UploadsLeft int           `json:"uploads_left"`
	Files       []requestFile `json:"files"`
}

type requestFile struct {
	ID            string    `json:"id"`
	Size          int64     `json:"size"`
	CreatedAt     time.Time `json:"created_at"`
	DownloadsLeft int       `json:"downloads_left"`
}

func checkWords(pub []byte) string {
	return words.Fingerprint(pub, "en") + " (Turkish: " + words.Fingerprint(pub, "tr") + ")"
}

// sendToRequest encrypts a file for the person who made the request: with a one-time key of
// this run and their public key, so only they can open it (not even this tool, afterwards).
func (a *app) sendToRequest(link, path string) error {
	server, rid, pub, err := parseRequestLink(link)
	if err != nil {
		return err
	}
	c, err := newClient(server, a.progress)
	if err != nil {
		return err
	}
	ctx := context.Background()
	var info requestInfo
	if err := c.call(ctx, "GET", "/requests/"+rid, "", nil, &info); err != nil {
		return requestProblem(err)
	}
	if info.UploadsLeft == 0 {
		return errors.New("this request is full; ask for a new request link")
	}
	if info.Label != "" {
		fmt.Fprintf(a.out, "Asked for: %s\n", info.Label)
	}
	fmt.Fprintf(a.out, "Check words: %s\n", checkWords(pub))

	reqPub, err := ecdh.X25519().NewPublicKey(pub)
	if err != nil {
		return errors.New("the link's key is not valid")
	}
	eph, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	ikm, err := envelope.RequestKey(eph, reqPub, eph.PublicKey().Bytes(), pub)
	if err != nil {
		return err
	}
	p := envelope.Params{Mode: envelope.ModeRequest}
	copy(p.EphPub[:], eph.PublicKey().Bytes())

	in, info2, err := openSource(path)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, size, err := encryptToTemp(in, info2, p, ikm)
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	var up upload
	if err := c.retry(ctx, func() error {
		return c.call(ctx, "POST", "/requests/"+rid+"/uploads", "", map[string]any{"size": size}, &up)
	}); err != nil {
		return requestProblem(err)
	}
	if err := c.uploadParts(ctx, tmp, size, up); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Sent: %s. Only the person who asked can open it.\n", filepath.Base(path))
	return nil
}

func requestProblem(err error) error {
	var api *apiError
	if errors.As(err, &api) {
		switch {
		case api.Code == "request_full":
			return errors.New("this request is full; ask for a new request link")
		case api.Status == 410:
			return errors.New("this request expired or was closed")
		case api.Status == 404:
			return errors.New("there is no request at this link")
		}
	}
	return err
}

// --- request mode: the requester's side ---

// savedRequest is what this computer keeps of a request it made. The private key opens what
// arrives, so the file is readable by its owner only.
type savedRequest struct {
	Server     string    `json:"server"`
	RID        string    `json:"rid"`
	OwnerToken string    `json:"owner_token"`
	PrivateKey string    `json:"private_key"`
	Label      string    `json:"label"`
	ExpiresAt  time.Time `json:"expires_at"`
	Fetched    []string  `json:"fetched"`
}

func (a *app) requestsDir() (string, error) {
	base, err := a.configDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "zarf", "requests")
	return dir, os.MkdirAll(dir, 0o700)
}

func (a *app) saveRequest(r *savedRequest) error {
	dir, err := a.requestsDir()
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	tmp := filepath.Join(dir, "."+r.RID+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, r.RID+".json"))
}

func (a *app) loadRequests() ([]*savedRequest, error) {
	dir, err := a.requestsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []*savedRequest
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		var r savedRequest
		if err != nil || json.Unmarshal(b, &r) != nil || r.RID == "" {
			continue // a damaged file is skipped, never trusted
		}
		out = append(out, &r)
	}
	slices.SortFunc(out, func(x, y *savedRequest) int { return y.ExpiresAt.Compare(x.ExpiresAt) })
	return out, nil
}

// findRequest accepts the id, the start of it, or the whole link.
func (a *app) findRequest(ref string) (*savedRequest, error) {
	if _, rid, _, err := parseRequestLink(ref); err == nil {
		ref = rid
	}
	all, err := a.loadRequests()
	if err != nil {
		return nil, err
	}
	var hits []*savedRequest
	for _, r := range all {
		if strings.HasPrefix(r.RID, ref) && ref != "" {
			hits = append(hits, r)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return nil, fmt.Errorf("no request %q on this computer; see zarf request list", ref)
	}
	return nil, fmt.Errorf("%q matches several requests; give more of the id", ref)
}

func (a *app) request(args []string) error {
	if len(args) == 0 {
		return usageError{requestUsage}
	}
	switch args[0] {
	case "new":
		return a.requestNew(args[1:])
	case "list":
		return a.requestList()
	case "get":
		return a.requestGet(args[1:])
	case "close":
		return a.requestClose(args[1:])
	}
	return usageError{requestUsage}
}

const requestUsage = `usage:
  zarf request new [--note TEXT] [--expires 1h|1d|7d] [--uploads 1|5|10|20]
  zarf request list
  zarf request get ID [-d DIR]
  zarf request close ID`

func (a *app) requestNew(args []string) error {
	fs := flag.NewFlagSet("request new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	note := fs.String("note", "", "")
	expires := fs.String("expires", "7d", "")
	uploads := fs.Int("uploads", 5, "")
	server := serverFlag(fs)
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usageError{requestUsage}
	}
	expiresIn, ok := expiries[*expires]
	if !ok {
		return usageError{"--expires must be 1h, 1d or 7d"}
	}
	if !slices.Contains([]int{1, 5, 10, 20}, *uploads) {
		return usageError{"--uploads must be 1, 5, 10 or 20"}
	}
	c, err := newClient(*server, nil)
	if err != nil {
		return err
	}
	if _, err := a.requestsDir(); err != nil {
		return fmt.Errorf("cannot keep the request's key on this computer: %w", err)
	}
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	var res struct {
		RID        string    `json:"rid"`
		OwnerToken string    `json:"owner_token"`
		ExpiresAt  time.Time `json:"expires_at"`
	}
	ctx := context.Background()
	if err := c.call(ctx, "POST", "/requests", "", map[string]any{"label": *note, "expires_in": expiresIn, "max_uploads": *uploads}, &res); err != nil {
		return err
	}
	r := &savedRequest{Server: c.base.String(), RID: res.RID, OwnerToken: res.OwnerToken, PrivateKey: b64.EncodeToString(priv.Bytes()), Label: *note, ExpiresAt: res.ExpiresAt}
	if err := a.saveRequest(r); err != nil {
		_ = c.call(ctx, "DELETE", "/requests/"+res.RID, res.OwnerToken, nil, nil) // a key we cannot keep would open nothing
		return fmt.Errorf("cannot keep the request's key on this computer: %w", err)
	}
	pub := priv.PublicKey().Bytes()
	fmt.Fprintf(a.out, "Request link: %s/r/%s#%s\n", r.Server, r.RID, b64.EncodeToString(pub))
	fmt.Fprintf(a.out, "Check words: %s\n", checkWords(pub))
	fmt.Fprintf(a.out, "Up to %d file(s) can be uploaded until %s.\n", *uploads, r.ExpiresAt.Local().Format("2 Jan 2006 15:04"))
	fmt.Fprintf(a.out, "Fetch what arrives with: zarf request get %s\n", r.RID[:8])
	return nil
}

func (a *app) requestList() error {
	all, err := a.loadRequests()
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintln(a.out, "No requests on this computer. Create one with: zarf request new")
		return nil
	}
	ctx := context.Background()
	for _, r := range all {
		label := r.Label
		if label == "" {
			label = "(no note)"
		}
		status := "expired or closed"
		if c, err := newClient(r.Server, nil); err == nil {
			var info requestInfo
			var api *apiError
			if err := c.call(ctx, "GET", "/requests/"+r.RID, r.OwnerToken, nil, &info); err == nil {
				status = fmt.Sprintf("%d arrived, %d new, open until %s", len(info.Files), countNew(info.Files, r.Fetched), info.ExpiresAt.Local().Format("2 Jan 15:04"))
			} else if !errors.As(err, &api) {
				status = "server unreachable"
			}
		}
		fmt.Fprintf(a.out, "%s  %s  %s\n", r.RID[:8], label, status)
	}
	return nil
}

func countNew(files []requestFile, fetched []string) int {
	n := 0
	for _, f := range files {
		if !slices.Contains(fetched, f.ID) {
			n++
		}
	}
	return n
}

// requestGet downloads and opens every upload not fetched before.
func (a *app) requestGet(args []string) error {
	fs := flag.NewFlagSet("request get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("d", ".", "")
	if err := fs.Parse(reorder(args)); err != nil || fs.NArg() != 1 {
		return usageError{requestUsage}
	}
	r, err := a.findRequest(fs.Arg(0))
	if err != nil {
		return err
	}
	rawKey, err := b64.DecodeString(r.PrivateKey)
	if err != nil {
		return errors.New("the saved request is damaged")
	}
	priv, err := ecdh.X25519().NewPrivateKey(rawKey)
	if err != nil {
		return errors.New("the saved request is damaged")
	}
	pub := priv.PublicKey().Bytes()
	c, err := newClient(r.Server, a.progress)
	if err != nil {
		return err
	}
	ctx := context.Background()
	var info requestInfo
	if err := c.call(ctx, "GET", "/requests/"+r.RID, r.OwnerToken, nil, &info); err != nil {
		return requestProblem(err)
	}
	got := 0
	for _, f := range info.Files {
		if slices.Contains(r.Fetched, f.ID) {
			continue
		}
		var start struct {
			Ticket string `json:"ticket"`
			Size   int64  `json:"size"`
		}
		if err := c.call(ctx, "POST", "/requests/"+r.RID+"/files/"+f.ID+"/download", r.OwnerToken, nil, &start); err != nil {
			return err
		}
		body := &resumingBody{ctx: ctx, c: c, path: "/files/" + f.ID + "/content", ticket: start.Ticket, size: start.Size, progress: c.newProgress("Downloading", start.Size)}
		env, err := envelope.Open(body, func(h *envelope.Header) ([]byte, error) {
			if h.Mode != envelope.ModeRequest {
				return nil, errors.New("this file was not sent to a request")
			}
			eph, err := ecdh.X25519().NewPublicKey(h.EphPub[:])
			if err != nil {
				return nil, envelope.ErrBadPublicKey
			}
			return envelope.RequestKey(priv, eph, h.EphPub[:], pub)
		})
		if err != nil {
			body.Close()
			return err
		}
		name, err := saveVerified(*dir, env.Metadata.Files[0].Name, env)
		body.Close()
		if err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Received: %s\n", filepath.Join(*dir, name))
		r.Fetched = append(r.Fetched, f.ID)
		if err := a.saveRequest(r); err != nil {
			return err
		}
		got++
	}
	if got == 0 {
		fmt.Fprintln(a.out, "Nothing new.")
	}
	return nil
}

func (a *app) requestClose(args []string) error {
	if len(args) != 1 {
		return usageError{requestUsage}
	}
	r, err := a.findRequest(args[0])
	if err != nil {
		return err
	}
	c, err := newClient(r.Server, nil)
	if err != nil {
		return err
	}
	err = c.call(context.Background(), "DELETE", "/requests/"+r.RID, r.OwnerToken, nil, nil)
	var api *apiError
	if err != nil && !(errors.As(err, &api) && (api.Status == 404 || api.Status == 410)) {
		return err
	}
	dir, _ := a.requestsDir()
	if err := os.Remove(filepath.Join(dir, r.RID+".json")); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "Closed; what arrived is deleted from the server.")
	return nil
}

// --- live mode ---

func (c *client) socketURL(path string) string {
	u := *c.base
	u.Scheme = map[string]string{"https": "wss", "http": "ws"}[u.Scheme]
	return u.String() + "/api/v1" + path
}

func (c *client) dialLive(ctx context.Context, channel int) (*websocket.Conn, error) {
	ws, _, err := websocket.Dial(ctx, c.socketURL(fmt.Sprintf("/live/%d", channel)), &websocket.DialOptions{HTTPClient: c.http})
	if err != nil {
		return nil, errors.New("could not connect to the server")
	}
	ws.SetReadLimit(1<<20 + 4096)
	return ws, nil
}

const liveTimeout = 2 * time.Hour

func (a *app) sendLive(server, path string) error {
	in, info, err := openSource(path)
	if err != nil {
		return err
	}
	defer in.Close()
	c, err := newClient(server, a.progress)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveTimeout)
	defer cancel()
	var ch struct {
		Channel int    `json:"channel"`
		Token   string `json:"token"`
	}
	if err := c.call(ctx, "POST", "/live", "", nil, &ch); err != nil {
		return err
	}
	r := make([]byte, 2)
	if _, err := rand.Read(r); err != nil {
		return err
	}
	code := words.Code{Channel: ch.Channel, Positions: [2]int{int(r[0]), int(r[1])}}
	ws, err := c.dialLive(ctx, ch.Channel)
	if err != nil {
		return err
	}
	defer ws.CloseNow()
	fmt.Fprintf(a.out, "Code: %s   (in Turkish: %s)\n", code.Format("en"), code.Format("tr"))
	fmt.Fprintln(a.out, "Tell the recipient the code. Waiting for them; keep this running.")

	ikm, err := live.Handshake(ctx, ws, "send", code, ch.Token)
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	bar := c.newProgress("Sending", info.Size())
	err = live.Send(ctx, ws, ikm, in, name, mimeType(name), info.Size(),
		func() { fmt.Fprintln(a.out, "Code checked. Waiting for the recipient to accept...") },
		bar.add)
	if err != nil {
		return err
	}
	ws.Close(websocket.StatusNormalClosure, "")
	fmt.Fprintf(a.out, "Delivered: %s\n", name)
	return nil
}

func (a *app) receiveLive(server string, code words.Code, dir string, yes bool) error {
	c, err := newClient(server, a.progress)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveTimeout)
	defer cancel()
	ws, err := c.dialLive(ctx, code.Channel)
	if err != nil {
		return err
	}
	defer ws.CloseNow()
	ikm, err := live.Handshake(ctx, ws, "receive", code, "")
	if err != nil {
		return err
	}
	in, err := live.Receive(ctx, ws, ikm)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Incoming: %s (%s)\n", in.File.Name, mb(in.File.Size))
	if !yes {
		fmt.Fprint(a.out, "Accept? [y/N] ")
		answer, _ := a.in.ReadString('\n')
		if ans := strings.ToLower(strings.TrimSpace(answer)); ans != "y" && ans != "yes" && ans != "e" && ans != "evet" {
			_ = in.Decline()
			ws.Close(websocket.StatusNormalClosure, "")
			fmt.Fprintln(a.out, "Declined.")
			return nil
		}
	}
	if err := in.Accept(); err != nil {
		return live.DamagedIfEnvelope(err)
	}
	bar := c.newProgress("Receiving", in.File.Size)
	name, err := saveVerified(dir, in.File.Name, &counter{r: in.Reader, bar: bar})
	if err != nil {
		return live.DamagedIfEnvelope(err)
	}
	if err := in.Finish(); err != nil {
		return err
	}
	ws.Close(websocket.StatusNormalClosure, "")
	fmt.Fprintf(a.out, "Received: %s\n", filepath.Join(dir, name))
	return nil
}

type counter struct {
	r   io.Reader
	bar *progress
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.bar.add(int64(n))
	return n, err
}
