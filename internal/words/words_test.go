package words

import (
	"os"
	"regexp"
	"testing"
)

// The browser and the command line must read every code the same way.
func TestSameListAsTheWebPage(t *testing.T) {
	src, err := os.ReadFile("../../web/src/lib/words.ts")
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`\["([a-z]+)", "([a-z]+)"\]`).FindAllStringSubmatch(string(src), -1)
	if len(found) != len(pairs) {
		t.Fatalf("web list has %d pairs, Go %d", len(found), len(pairs))
	}
	for i, m := range found {
		if m[1] != pairs[i][0] || m[2] != pairs[i][1] {
			t.Errorf("pair %d: web %s/%s, Go %s/%s", i, m[1], m[2], pairs[i][0], pairs[i][1])
		}
	}
}

func TestCodes(t *testing.T) {
	c := Code{Channel: 7, Positions: [2]int{0, 138}}
	if c.Format("tr") != "7-elma-kule" || c.Format("en") != "7-apple-tower" {
		t.Fatalf("%s %s", c.Format("tr"), c.Format("en"))
	}
	for _, typed := range []string{"7-elma-kule", "7 APPLE tower", "7.elma.tower"} {
		if got, err := Parse(typed); err != nil || got != c {
			t.Errorf("%q: %v %v", typed, got, err)
		}
	}
	for _, bad := range []string{"7-elma", "x-elma-kule", "0-elma-kule", "7-elma-kulee"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if fp := Fingerprint(make([]byte, 32), "en"); len(regexp.MustCompile(`\S+`).FindAllString(fp, -1)) != 5 {
		t.Errorf("fingerprint %q", fp)
	}
}
