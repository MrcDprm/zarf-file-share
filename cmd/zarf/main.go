// Command zarf sends and receives end-to-end encrypted files, through a Zarf server or as
// local encrypted files.
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
  zarf send [-p] [--expires 1h|1d|7d] [--downloads 1|5|10|20] FILE
                                       upload FILE (1024 MB at most) and print its link
  zarf receive LINK [-d DIR]           download and decrypt a file from a link
  zarf delete LINK DELETE-CODE         delete a sent file before it expires

  zarf send --to REQUEST-LINK FILE     upload FILE for the person who sent you a request link
  zarf request new [--note TEXT] [--expires 1h|1d|7d] [--uploads 1|5|10|20]
                                       ask someone for files; prints a request link
  zarf request list                    your requests and what has arrived
  zarf request get ID [-d DIR]         download and open what arrived
  zarf request close ID                close a request and delete what arrived

  zarf send --code FILE                send to someone right now: prints a short code
  zarf receive CODE [-d DIR] [-y]      receive with a code, like 7-apple-tower

  zarf encrypt [-p] [-o OUT] FILE      encrypt FILE into FILE.zarf locally and print its key
  zarf decrypt [--key KEY] [-d DIR] FILE.zarf
  zarf version

Options:
  -p             also protect the file with a password (asked twice, never shown)
  --expires      how long the link works (default 1d)
  --downloads    how many times it can be downloaded (default 1)
  --server URL   the Zarf server (default https://zarf.miracdeprem.com, or $ZARF_SERVER)
  -d DIR         folder for received files; existing files are never overwritten
  -y             accept a file sent with a code without asking
  -o OUT         where to write the encrypted file
  --key KEY      the key, or the whole link; asked for when left out
`

// app holds what a command talks to, so tests can run commands without a terminal.
type app struct {
	in        *bufio.Reader
	out       io.Writer
	errOut    io.Writer
	secret    func(prompt string) (string, error) // reads a key or password without showing it
	progress  io.Writer                           // progress lines; nil when not a terminal
	alone     func() bool                         // opened by double-clicking (Windows)
	configDir func() (string, error)              // where requests' keys are kept
}

func main() {
	a := &app{in: bufio.NewReader(os.Stdin), out: os.Stdout, errOut: os.Stderr, progress: terminalProgress()}
	a.secret = a.terminalSecret
	a.configDir = os.UserConfigDir
	a.alone = startedOnItsOwn
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		// No command: a short start, and when double-clicked, a window that waits for Enter.
		fmt.Fprint(a.out, welcome)
		if a.alone != nil && a.alone() {
			fmt.Fprint(a.out, doubleClicked)
			_, _ = a.in.ReadString('\n')
		}
		return 0
	}
	var err error
	switch args[0] {
	case "send":
		err = a.send(args[1:])
	case "receive":
		err = a.receive(args[1:])
	case "request":
		err = a.request(args[1:])
	case "delete":
		err = a.remove(args[1:])
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

// welcome is what "zarf" with no command shows, in English and Turkish: the few commands
// people need first, instead of the full usage.
const welcome = `Zarf: end-to-end encrypted file sharing / uçtan uca şifreli dosya paylaşımı

  Send a file / Dosya gönder                   zarf send rapor.pdf
  Receive a file / Dosya al                    zarf receive LINK
  Send with a code / Kodla gönder              zarf send --code rapor.pdf
  Receive with a code / Kodla al               zarf receive 7-elma-kule
  Ask someone for files / Birinden dosya iste  zarf request new --note "vergi levhası"
  Every command / Bütün komutlar               zarf help

Without the command line / Komut satırı olmadan: https://zarf.miracdeprem.com
`

// doubleClicked follows the welcome when the tool was opened from File Explorer: its window
// closes when the tool ends, so it says where to type the commands.
const doubleClicked = `
This window is not a terminal. Open "Zarf" from the Start menu (after installing it),
or type powershell in the address bar of this folder and run:  .\\zarf.exe
Bu pencere bir terminal değil. Kurduysan Başlat menüsünden "Zarf"ı aç, kurmadıysan
bu klasörün adres çubuğuna powershell yazıp şunu çalıştır:  .\\zarf.exe

Press Enter to close. / Kapatmak için Enter'a bas.
`

// Prompts for secrets say that typing stays invisible, so nobody thinks the keyboard stopped working.
const (
	promptPassword      = "Password (not shown as you type): "
	promptPasswordAgain = "Password again (not shown): "
	promptKey           = "Key or link (not shown as you type): "
)

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// terminalSecret hides what is typed when stdin is a terminal; when input comes from a
// pipe (scripts), it reads one line.
func (a *app) terminalSecret(prompt string) (string, error) {
	fmt.Fprint(a.errOut, prompt)
	if secret, ok, err := readConsoleSecret(); ok {
		fmt.Fprintln(a.errOut)
		return secret, err
	}
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
