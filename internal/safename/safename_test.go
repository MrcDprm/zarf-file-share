package safename

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"rapor.pdf":                     "rapor.pdf",
		"Müşteri Sözleşmesi (son).docx": "Müşteri Sözleşmesi (son).docx",
		"../../etc/passwd":              "passwd",
		`..\..\Windows\win.ini`:         "win.ini",
		"/abs/path/file.txt":            "file.txt",
		"..":                            fallback,
		".bashrc":                       "bashrc", // never a hidden file
		"":                              fallback,
		"   ":                           fallback,
		"CON.txt":                       "_CON.txt",
		"lpt1":                          "_lpt1",
		"a<b>c:d\"e|f?g*h.txt":          "a_b_c_d_e_f_g_h.txt",
		"fatura\u202Etxt.exe":           "faturatxt.exe", // a right-to-left override hid the real extension
		"satir\nsonu\t.txt":             "satirsonu.txt",
		"sonunda nokta. . .":            "sonunda nokta",
		"gizli\u200Bkarakter.png":       "gizlikarakter.png",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLongNamesKeepTheirExtension(t *testing.T) {
	got := Clean(strings.Repeat("ş", 300) + ".pdf")
	if len(got) > maxBytes || !strings.HasSuffix(got, ".pdf") || !strings.HasPrefix(got, "ş") {
		t.Fatalf("got %d bytes: %q", len(got), got)
	}
}

func TestCreateNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rapor.pdf"), []byte("eski"), 0o600); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for i := 0; i < 2; i++ {
		f, name, err := Create(dir, "../rapor.pdf")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		names = append(names, name)
	}
	if names[0] != "rapor (1).pdf" || names[1] != "rapor (2).pdf" {
		t.Fatalf("got %v", names)
	}
	old, _ := os.ReadFile(filepath.Join(dir, "rapor.pdf"))
	if string(old) != "eski" {
		t.Fatal("the existing file was changed")
	}
}
