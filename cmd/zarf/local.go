package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/MrcDprm/zarf-file-share/internal/envelope"
	"github.com/MrcDprm/zarf-file-share/internal/safename"
)

const minPassword = 8

var b64 = base64.RawURLEncoding

func (a *app) encrypt(args []string) error {
	fs := flag.NewFlagSet("encrypt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	withPassword := fs.Bool("p", false, "")
	out := fs.String("o", "", "")
	if err := fs.Parse(reorder(fs, args)); err != nil || fs.NArg() != 1 {
		return usageError{"usage: zarf encrypt [-p] [-o OUT] FILE"}
	}
	path := fs.Arg(0)
	in, err := os.Open(path)
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

	name := filepath.Base(path)
	meta := envelope.Metadata{Files: []envelope.File{{Name: name, Size: info.Size(), Type: mimeType(name)}}}
	target := *out
	if target == "" {
		target = path + ".zarf"
	}
	// O_EXCL: never overwrite an existing file with ciphertext.
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if err := writeEnvelope(f, in, p, ikm, meta); err != nil {
		f.Close()
		os.Remove(target)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(target)
		return err
	}

	key := b64.EncodeToString(fileKey)
	fmt.Fprintf(a.out, "Encrypted: %s\nKey: %s\n", target, key)
	if *withPassword {
		fmt.Fprintln(a.out, "The key alone does not open it: the password is needed too.")
	}
	fmt.Fprintf(a.out, "To open:  zarf decrypt --key %s %s\n", key, filepath.Base(target))
	return nil
}

func writeEnvelope(dst io.Writer, src io.Reader, p envelope.Params, ikm []byte, meta envelope.Metadata) error {
	w, err := envelope.NewWriter(dst, p, ikm, meta)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, src); err != nil {
		return err
	}
	return w.Close()
}

func (a *app) newPassword() (string, error) {
	pw, err := a.secret(promptPassword)
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(pw) < minPassword {
		return "", fmt.Errorf("the password needs at least %d characters", minPassword)
	}
	again, err := a.secret(promptPasswordAgain)
	if err != nil {
		return "", err
	}
	if again != pw {
		return "", errors.New("the passwords do not match")
	}
	return pw, nil
}

func (a *app) decrypt(args []string) error {
	fs := flag.NewFlagSet("decrypt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	keyFlag := fs.String("key", "", "")
	dir := fs.String("d", ".", "")
	if err := fs.Parse(reorder(fs, args)); err != nil || fs.NArg() != 1 {
		return usageError{"usage: zarf decrypt [--key KEY] [-d DIR] FILE.zarf"}
	}
	in, err := os.Open(fs.Arg(0))
	if err != nil {
		return err
	}
	defer in.Close()

	keyText := *keyFlag
	if keyText == "" {
		if keyText, err = a.secret(promptKey); err != nil {
			return err
		}
	}
	fileKey, err := parseKey(keyText)
	if err != nil {
		return err
	}
	r, err := envelope.Open(in, func(h *envelope.Header) ([]byte, error) {
		if h.Mode == envelope.ModeLinkPassword {
			pw, err := a.secret(promptPassword)
			if err != nil {
				return nil, err
			}
			return envelope.PasswordKey(fileKey, pw, h.Argon), nil
		}
		if h.Mode != envelope.ModeLink {
			return nil, errors.New("this file was not made with a link key")
		}
		return envelope.LinkKey(fileKey), nil
	})
	if err != nil {
		return err
	}

	name, err := saveVerified(*dir, r.Metadata.Files[0].Name, r)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Decrypted: %s\n", filepath.Join(*dir, name))
	return nil
}

// saveVerified writes into a hidden temporary file and gives it its real name only after
// the last chunk verified, so a damaged or cut file never appears as a finished one.
func saveVerified(dir, name string, r io.Reader) (string, error) {
	tmp, err := os.CreateTemp(dir, ".zarf-partial-*")
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmp.Name())
		return "", errors.Join(copyErr, closeErr)
	}
	// Reserve a name that is free (never an existing file), then move the verified data onto it.
	f, final, err := safename.Create(dir, name)
	if err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	f.Close()
	if err := os.Rename(tmp.Name(), filepath.Join(dir, final)); err != nil {
		os.Remove(tmp.Name())
		os.Remove(filepath.Join(dir, final))
		return "", err
	}
	return final, nil
}

// parseKey accepts the bare key or a whole link (the key is what follows "#").
func parseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '#'); i >= 0 {
		s = s[i+1:]
	}
	k, err := b64.DecodeString(s)
	if err != nil || len(k) != 32 {
		return nil, errors.New("that is not a Zarf key")
	}
	return k, nil
}

func mimeType(name string) string {
	t := mime.TypeByExtension(filepath.Ext(name))
	if i := strings.IndexByte(t, ';'); i >= 0 {
		t = t[:i]
	}
	return t
}

// reorder moves flags before the file name, so "zarf decrypt FILE --key K" works too
// (the flag package stops at the first argument that is not a flag). Only flags defined
// in fs are moved; anything else that starts with "-" is an argument, because request
// ids, codes and file names can start with "-" too. Boolean flags take no value. The
// "--" between the two parts stops the flag package from reading such arguments as flags.
func reorder(fs *flag.FlagSet, args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		f := lookupFlag(fs, args[i])
		if f == nil {
			rest = append(rest, args[i])
			continue
		}
		flags = append(flags, args[i])
		if !isBoolFlag(f) && !strings.Contains(args[i], "=") {
			if i+1 == len(args) {
				return args // value missing: let the flag package report it, not take "--"
			}
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(append(flags, "--"), rest...)
}

// lookupFlag returns the flag that arg names ("-d", "--key", "--key=K"), or nil.
func lookupFlag(fs *flag.FlagSet, arg string) *flag.Flag {
	if !strings.HasPrefix(arg, "-") {
		return nil
	}
	name := strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-")
	name, _, _ = strings.Cut(name, "=")
	if name == "" {
		return nil
	}
	return fs.Lookup(name)
}

func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}
