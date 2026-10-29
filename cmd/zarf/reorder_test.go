package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"slices"
	"testing"
)

func testFlags() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("p", false, "")
	fs.String("d", ".", "")
	fs.String("key", "", "")
	return fs
}

// Request ids, delete codes and file names are base64url or free text, so one in 64
// starts with "-". Such an argument must stay an argument, not turn into a flag.
func TestReorderKeepsDashArgumentsInPlace(t *testing.T) {
	for name, c := range map[string]struct {
		args []string
		arg  string
		d    string
		p    bool
	}{
		"flag after the file":    {[]string{"FILE", "-d", "out"}, "FILE", "out", false},
		"id starting with -":     {[]string{"-Ab3xYz"}, "-Ab3xYz", ".", false},
		"id with flag after":     {[]string{"-Ab3xYz", "-d", "out"}, "-Ab3xYz", "out", false},
		"id with flag before":    {[]string{"-d", "out", "-Ab3xYz"}, "-Ab3xYz", "out", false},
		"bool flag takes no arg": {[]string{"-p", "-x.pdf"}, "-x.pdf", ".", true},
		"long form with =":       {[]string{"--d=out", "-k"}, "-k", "out", false},
		"after --":               {[]string{"--", "-d"}, "-d", ".", false},
	} {
		fs := testFlags()
		if err := fs.Parse(reorder(fs, c.args)); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		d := fs.Lookup("d").Value.String()
		p := fs.Lookup("p").Value.String() == "true"
		if !slices.Equal(fs.Args(), []string{c.arg}) || d != c.d || p != c.p {
			t.Errorf("%s: args %q, d %q, p %v", name, fs.Args(), d, p)
		}
	}
}

func TestReorderLeavesAMissingValueToTheFlagPackage(t *testing.T) {
	fs := testFlags()
	fs.Parse(reorder(fs, []string{"FILE", "-d"}))
	if fs.Lookup("d").Value.String() == "--" {
		t.Error(`-d took the "--" separator as its value`)
	}
}

// Given as a relative name, as people type it, "-not.txt" must not be read as a flag.
func TestAFileNameStartingWithADash(t *testing.T) {
	t.Chdir(t.TempDir())
	content := []byte("tire ile başlayan dosya")
	os.WriteFile("-not.txt", content, 0o600)
	a, out, errOut := testApp(t)
	if code := a.run([]string{"encrypt", "-not.txt"}); code != 0 {
		t.Fatalf("encrypt: %s", errOut)
	}
	m := keyLine.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("no key printed: %s", out)
	}
	os.Remove("-not.txt")
	a, _, errOut = testApp(t)
	if code := a.run([]string{"decrypt", "-not.txt.zarf", "--key", m[1]}); code != 0 {
		t.Fatalf("decrypt: %s", errOut)
	}
	got, err := os.ReadFile("-not.txt")
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("decrypted file: %v", err)
	}
}
