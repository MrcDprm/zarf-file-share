package main

import (
	"context"
	"crypto/rand"
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
)

const (
	maxFile          = 1024 * 1024 * 1024 // 1024 MB, the public server's limit
	passwordAttempts = 3
)

var expiries = map[string]int64{"1h": 3600, "1d": 86400, "7d": 7 * 86400}

func serverFlag(fs *flag.FlagSet) *string {
	def := os.Getenv("ZARF_SERVER")
	if def == "" {
		def = defaultServer
	}
	return fs.String("server", def, "")
}

// send encrypts a file, uploads the envelope in parts and prints the link. The key goes
// only into the link's "#" part, which never reaches the server.
func (a *app) send(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	withPassword := fs.Bool("p", false, "")
	expires := fs.String("expires", "1d", "")
	downloads := fs.Int("downloads", 1, "")
	server := serverFlag(fs)
	if err := fs.Parse(reorder(args, "p")); err != nil || fs.NArg() != 1 {
		return usageError{"usage: zarf send [-p] [--expires 1h|1d|7d] [--downloads 1|5|10|20] FILE"}
	}
	expiresIn, ok := expiries[*expires]
	if !ok {
		return usageError{"--expires must be 1h, 1d or 7d"}
	}
	if !slices.Contains([]int{1, 5, 10, 20}, *downloads) {
		return usageError{"--downloads must be 1, 5, 10 or 20"}
	}
	c, err := newClient(*server, a.progress)
	if err != nil {
		return err
	}

	in, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("folders are not supported yet; pack it into a .zip first")
	}
	if info.Size() > maxFile {
		return errors.New("the file is too large: the limit is 1024 MB")
	}

	fileKey := make([]byte, 32)
	if _, err := rand.Read(fileKey); err != nil {
		return err
	}
	p := envelope.Params{Mode: envelope.ModeLink}
	ikm := envelope.LinkKey(fileKey)
	if *withPassword {
		pw, err := a.newPassword()
		if err != nil {
			return err
		}
		p.Mode, p.Argon = envelope.ModeLinkPassword, envelope.DefaultArgon()
		if _, err := rand.Read(p.Argon.Salt[:]); err != nil {
			return err
		}
		ikm = envelope.PasswordKey(fileKey, pw, p.Argon)
	}

	// Encrypt into a private temporary file first: the server needs the exact size up front,
	// and parts can then be sent (and sent again) in any order.
	tmp, err := os.CreateTemp("", "zarf-send-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	name := filepath.Base(fs.Arg(0))
	meta := envelope.Metadata{Files: []envelope.File{{Name: name, Size: info.Size(), Type: mimeType(name)}}}
	if err := writeEnvelope(tmp, in, p, ikm, meta); err != nil {
		return err
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	dlAuth, err := envelope.DownloadAuth(fileKey)
	if err != nil {
		return err
	}
	ctx := context.Background()
	var up struct {
		ID          string    `json:"id"`
		UploadToken string    `json:"upload_token"`
		DeleteToken string    `json:"delete_token"`
		PartSize    int64     `json:"part_size"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	err = c.retry(ctx, func() error {
		return c.call(ctx, "POST", "/uploads", "", map[string]any{
			"size": size, "expires_in": expiresIn, "max_downloads": *downloads, "dl_auth_hash": b64.EncodeToString(sha256Sum(dlAuth)),
		}, &up)
	})
	if err != nil {
		return err
	}
	if up.PartSize <= 0 {
		return errors.New("the server sent a bad part size")
	}

	parts := (size + up.PartSize - 1) / up.PartSize
	bar := c.newProgress("Uploading", size)
	sendPart := func(n int64) error {
		buf := make([]byte, min(up.PartSize, size-n*up.PartSize))
		if _, err := tmp.ReadAt(buf, n*up.PartSize); err != nil {
			return err
		}
		return c.retry(ctx, func() error {
			return c.call(ctx, "PUT", fmt.Sprintf("/uploads/%s/parts/%d", up.ID, n), up.UploadToken, buf, nil)
		})
	}
	for n := int64(0); n < parts; n++ {
		if err := sendPart(n); err != nil {
			return fmt.Errorf("upload stopped at part %d of %d: %w", n+1, parts, err)
		}
		bar.add(min(up.PartSize, size-n*up.PartSize))
	}
	// If the server missed a part, it names it; send those again once.
	var missing struct {
		Received []int64 `json:"received"`
	}
	err = c.call(ctx, "POST", "/uploads/"+up.ID+"/complete", up.UploadToken, nil, nil)
	var api *apiError
	if errors.As(err, &api) && api.Code == "missing_parts" {
		if err := c.call(ctx, "GET", "/uploads/"+up.ID, up.UploadToken, nil, &missing); err != nil {
			return err
		}
		have := map[int64]bool{}
		for _, n := range missing.Received {
			have[n] = true
		}
		for n := int64(0); n < parts; n++ {
			if !have[n] {
				if err := sendPart(n); err != nil {
					return err
				}
			}
		}
		err = c.retry(ctx, func() error { return c.call(ctx, "POST", "/uploads/"+up.ID+"/complete", up.UploadToken, nil, nil) })
	}
	if err != nil {
		return err
	}

	link := c.base.String() + "/d/" + up.ID + "#" + b64.EncodeToString(fileKey)
	fmt.Fprintf(a.out, "Link: %s\n", link)
	if *withPassword {
		fmt.Fprintln(a.out, "The link alone does not open it: share the password another way.")
	}
	fmt.Fprintf(a.out, "It can be downloaded %d time(s) until %s.\n", *downloads, up.ExpiresAt.Local().Format("2 Jan 2006 15:04"))
	fmt.Fprintf(a.out, "To delete it sooner: zarf delete %s %s\n", c.base.String()+"/d/"+up.ID, up.DeleteToken)
	return nil
}

// parseLink splits https://host/d/<id>#<key> into the server, the id and the key.
func parseLink(link string) (server, id string, key []byte, err error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Host == "" {
		return "", "", nil, errors.New("that is not a Zarf link")
	}
	rest, ok := strings.CutPrefix(u.Path, "/d/")
	if !ok || rest == "" || strings.Contains(rest, "/") {
		return "", "", nil, errors.New("that is not a Zarf link")
	}
	if u.Fragment != "" {
		if key, err = parseKey(u.Fragment); err != nil {
			return "", "", nil, err
		}
	}
	return u.Scheme + "://" + u.Host, rest, key, nil
}

// receive downloads, decrypts and saves a file from a link.
func (a *app) receive(args []string) error {
	fs := flag.NewFlagSet("receive", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("d", ".", "")
	if err := fs.Parse(reorder(args)); err != nil || fs.NArg() != 1 {
		return usageError{"usage: zarf receive LINK [-d DIR]"}
	}
	server, id, fileKey, err := parseLink(fs.Arg(0))
	if err != nil {
		return err
	}
	if fileKey == nil {
		return errors.New("the link has no key (the part after #); copy the whole link")
	}
	c, err := newClient(server, a.progress)
	if err != nil {
		return err
	}
	ctx := context.Background()
	dlAuth, err := envelope.DownloadAuth(fileKey)
	if err != nil {
		return err
	}
	var start struct {
		Ticket string `json:"ticket"`
		Size   int64  `json:"size"`
	}
	if err := c.call(ctx, "POST", "/files/"+id+"/download", "", map[string]string{"dl_auth": b64.EncodeToString(dlAuth)}, &start); err != nil {
		return err
	}
	body := &resumingBody{ctx: ctx, c: c, path: "/files/" + id + "/content", ticket: start.Ticket, size: start.Size, progress: c.newProgress("Downloading", start.Size)}
	defer body.Close()

	r, err := envelope.Open(body, func(h *envelope.Header) ([]byte, error) {
		switch h.Mode {
		case envelope.ModeLink:
			return envelope.LinkKey(fileKey), nil
		case envelope.ModeLinkPassword:
			// The download is already spent, so a typo must not waste it: ask again.
			for i := 0; i < passwordAttempts; i++ {
				pw, err := a.secret("Password: ")
				if err != nil {
					return nil, err
				}
				if ikm := envelope.PasswordKey(fileKey, pw, h.Argon); h.KeyMatches(ikm) {
					return ikm, nil
				}
				fmt.Fprintln(a.errOut, "Wrong password.")
			}
			return nil, envelope.ErrWrongKey
		}
		return nil, errors.New("this file was not sent with a link")
	})
	if err != nil {
		return err
	}
	name, err := saveVerified(*dir, r.Metadata.Files[0].Name, r)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Received: %s\n", filepath.Join(*dir, name))
	return nil
}

// remove deletes a sent file before it expires, with the delete code from "send".
func (a *app) remove(args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil || fs.NArg() != 2 {
		return usageError{"usage: zarf delete LINK DELETE-CODE"}
	}
	server, id, _, err := parseLink(fs.Arg(0))
	if err != nil {
		return err
	}
	c, err := newClient(server, nil)
	if err != nil {
		return err
	}
	if err := c.call(context.Background(), "DELETE", "/files/"+id, strings.TrimSpace(fs.Arg(1)), nil, nil); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "Deleted.")
	return nil
}
