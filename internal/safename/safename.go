// Package safename turns a file name received from someone else into one that is safe to
// create: never a path, never a device name, never hidden, and never over an existing file.
package safename

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	fallback = "zarf-dosya"
	maxBytes = 200 // well under the 255-byte limit of common file systems, room left for " (99)"
)

// Windows refuses these names with any extension ("CON.txt" too).
var reserved = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}

func init() {
	for i := 1; i <= 9; i++ {
		reserved["COM"+strconv.Itoa(i)] = true
		reserved["LPT"+strconv.Itoa(i)] = true
	}
}

// Clean keeps only the last component of name and removes everything that could make it
// a path, a hidden file, a device or unreadable. It never returns an empty string.
func Clean(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == utf8.RuneError:
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r): // includes direction overrides
		case strings.ContainsRune(`<>:"|?*`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	name = strings.TrimLeft(b.String(), ". ")
	name = strings.TrimRight(name, ". ") // Windows drops trailing dots and spaces itself
	if name == "" {
		return fallback
	}
	stem, ext := split(name)
	if reserved[strings.ToUpper(stem)] {
		stem = "_" + stem
	}
	return shorten(stem, ext)
}

// split separates a short extension ("rapor", ".pdf"); a long tail is not treated as one.
func split(name string) (string, string) {
	ext := filepath.Ext(name)
	if len(ext) > 16 || ext == name {
		return name, ""
	}
	return strings.TrimSuffix(name, ext), ext
}

// shorten cuts the stem (never the extension) to fit maxBytes, on a character boundary.
func shorten(stem, ext string) string {
	for len(stem)+len(ext) > maxBytes && stem != "" {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	if stem == "" {
		stem = fallback
	}
	return stem + ext
}

// Create makes a new file named after name inside dir, adding " (1)", " (2)"... rather than
// touching a file that already exists. It returns the file and the name it got.
func Create(dir, name string) (*os.File, string, error) {
	stem, ext := split(Clean(name))
	for i := 0; i < 1000; i++ {
		candidate := stem + ext
		if i > 0 {
			candidate = stem + " (" + strconv.Itoa(i) + ")" + ext
		}
		f, err := os.OpenFile(filepath.Join(dir, candidate), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return f, candidate, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("safename: too many files with this name")
}
