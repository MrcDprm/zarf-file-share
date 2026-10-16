// Command zarf sends and receives end-to-end encrypted files. For now it encrypts and
// decrypts files locally; sending through a server comes next.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// version is set at build time: go build -ldflags "-X main.version=1.0.0"
var version = "dev"

const usage = `zarf: end-to-end encrypted file sharing

Usage:
  zarf encrypt [-p] [-o OUT] FILE      encrypt FILE into FILE.zarf and print its key
  zarf decrypt [--key KEY] [-d DIR] FILE.zarf
                                       decrypt into DIR (default: the current folder)
  zarf version

Options:
  -p             also protect the file with a password (asked twice, never shown)
  -o OUT         where to write the encrypted file
  --key KEY      the key, or the whole link; asked for when left out
  -d DIR         folder for the decrypted file; existing files are never overwritten
`

// app holds what a command talks to, so tests can run commands without a terminal.
type app struct {
	in     *bufio.Reader
	out    io.Writer
	errOut io.Writer
	secret func(prompt string) (string, error) // reads a key or password without showing it
}

func main() {
	a := &app{in: bufio.NewReader(os.Stdin), out: os.Stdout, errOut: os.Stderr}
	a.secret = a.terminalSecret
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.errOut, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "encrypt":
		err = a.encrypt(args[1:])
	case "decrypt":
		err = a.decrypt(args[1:])
	case "version", "--version":
		fmt.Fprintln(a.out, "zarf", version)
	case "help", "-h", "--help":
		fmt.Fprint(a.out, usage)
	default:
		fmt.Fprintf(a.errOut, "zarf: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(a.errOut, "zarf:", err)
		var u usageError
		if errors.As(err, &u) {
			return 2
		}
		return 1
	}
	return 0
}

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// terminalSecret hides what is typed when stdin is a terminal; when input comes from a
// pipe (scripts), it reads one line.
func (a *app) terminalSecret(prompt string) (string, error) {
	fmt.Fprint(a.errOut, prompt)
	if fd := int(os.Stdin.Fd()); term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(a.errOut)
		return string(b), err
	}
	line, err := a.in.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
