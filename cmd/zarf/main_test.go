package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// testApp answers secret prompts from a list, in order, and records what was printed.
func testApp(t *testing.T, secrets ...string) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	a := &app{in: bufio.NewReader(strings.NewReader("")), out: &out, errOut: &errOut}
	a.secret = func(string) (string, error) {
		if len(secrets) == 0 {
			t.Fatal("asked for a secret nobody planned")
		}
		s := secrets[0]
		secrets = secrets[1:]
		return s, nil
	}
	return a, &out, &errOut
}

var keyLine = regexp.MustCompile(`Key: (\S+)`)

func encryptFile(t *testing.T, dir, name string, content []byte, secrets ...string) (string, string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"encrypt", path}
	if len(secrets) > 0 {
		args = append(args, "-p") // after the file name: flags work in any order
	}
	a, out, errOut := testApp(t, secrets...)
	if code := a.run(args); code != 0 {
		t.Fatalf("encrypt failed (%d): %s", code, errOut)
	}
	m := keyLine.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("no key printed: %s", out)
	}
	return path + ".zarf", m[1]
}

func TestEncryptThenDecrypt(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	content := bytes.Repeat([]byte("Zarf "), 50000) // several chunks
	enc, key := encryptFile(t, src, "Sözleşme.pdf", content)

	a, out, errOut := testApp(t)
	if code := a.run([]string{"decrypt", enc, "--key", key, "-d", dst}); code != 0 {
		t.Fatalf("decrypt failed: %s", errOut)
	}
	got, err := os.ReadFile(filepath.Join(dst, "Sözleşme.pdf"))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("content differs (%v); output: %s", err, out)
	}
}

func TestTheWholeLinkWorksAsAKey(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	enc, key := encryptFile(t, src, "a.txt", []byte("merhaba"))
	a, _, errOut := testApp(t, "https://zarf.miracdeprem.com/d/abc#"+key) // asked, since --key is left out
	if code := a.run([]string{"decrypt", "-d", dst, enc}); code != 0 {
		t.Fatalf("decrypt failed: %s", errOut)
	}
}

func TestPasswordProtectedFiles(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	enc, key := encryptFile(t, src, "gizli.txt", []byte("gizli"), "uzun bir şifre", "uzun bir şifre")

	a, _, errOut := testApp(t, "yanlış şifre!")
	if code := a.run([]string{"decrypt", "--key", key, "-d", dst, enc}); code != 1 || !strings.Contains(errOut.String(), "wrong key or password") {
		t.Fatalf("wrong password: code %d, %s", code, errOut)
	}
	a, _, errOut = testApp(t, "uzun bir şifre")
	if code := a.run([]string{"decrypt", "--key", key, "-d", dst, enc}); code != 0 {
		t.Fatalf("right password: %s", errOut)
	}
}

func TestWeakOrMistypedPasswordsAreRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	os.WriteFile(path, []byte("x"), 0o600)
	for name, secrets := range map[string][]string{
		"too short":    {"kisa"},
		"not the same": {"uzun bir şifre", "uzun bir sifre"},
	} {
		a, _, errOut := testApp(t, secrets...)
		if code := a.run([]string{"encrypt", "-p", path}); code == 0 {
			t.Fatalf("%s: accepted", name)
		}
		if _, err := os.Stat(path + ".zarf"); err == nil {
			t.Fatalf("%s: left a file behind (%s)", name, errOut)
		}
	}
}

func TestADamagedFileLeavesNothingBehind(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	enc, key := encryptFile(t, src, "rapor.pdf", bytes.Repeat([]byte("x"), 200000))
	b, _ := os.ReadFile(enc)
	b[len(b)-100] ^= 1 // damage the last chunk: the earlier chunks still verify
	os.WriteFile(enc, b, 0o600)

	a, _, errOut := testApp(t)
	if code := a.run([]string{"decrypt", "--key", key, "-d", dst, enc}); code != 1 {
		t.Fatalf("damaged file accepted: %s", errOut)
	}
	if entries, _ := os.ReadDir(dst); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestDecryptingTwiceNeverOverwrites(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	enc, key := encryptFile(t, src, "rapor.pdf", []byte("v1"))
	for i := 0; i < 2; i++ {
		a, _, errOut := testApp(t)
		if code := a.run([]string{"decrypt", "--key", key, "-d", dst, enc}); code != 0 {
			t.Fatal(errOut)
		}
	}
	for _, name := range []string{"rapor.pdf", "rapor (1).pdf"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Fatalf("%s missing", name)
		}
	}
}

func TestEncryptNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	os.WriteFile(path, []byte("x"), 0o600)
	os.WriteFile(path+".zarf", []byte("old"), 0o600)
	a, _, _ := testApp(t)
	if code := a.run([]string{"encrypt", path}); code == 0 {
		t.Fatal("overwrote an existing file")
	}
	if b, _ := os.ReadFile(path + ".zarf"); string(b) != "old" {
		t.Fatal("existing file changed")
	}
}

func TestBadInputsGiveClearErrors(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct {
		args []string
		code int
		msg  string
	}{
		"no command":      {nil, 2, "Usage"},
		"unknown command": {[]string{"gonder"}, 2, "unknown command"},
		"not a key":       {[]string{"decrypt", "--key", "abc", filepath.Join(dir, "x")}, 1, ""},
		"a folder":        {[]string{"encrypt", dir}, 1, "folders are not supported"},
	} {
		a, _, errOut := testApp(t)
		code := a.run(c.args)
		if code != c.code || !strings.Contains(errOut.String(), c.msg) {
			t.Errorf("%s: code %d, %q", name, code, errOut)
		}
	}
	if _, err := parseKey("abc"); err == nil {
		t.Error("a short key was accepted")
	}
}

// Double-clicked on Windows, the tool explains where it runs and waits instead of vanishing.
func TestDoubleClickExplainsAndWaits(t *testing.T) {
	a, out, _ := testApp(t)
	a.in = bufio.NewReader(strings.NewReader("\n"))
	a.alone = func() bool { return true }
	if code := a.run(nil); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	for _, want := range []string{"command line tool", "komut satırı aracıdır", `.\zarf.exe help`, "Enter"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// From a terminal, no arguments still print the usage and fail.
	b, _, errOut := testApp(t)
	if code := b.run(nil); code != 2 || !strings.Contains(errOut.String(), "Usage:") {
		t.Errorf("from a terminal: %d %s", code, errOut)
	}
}
