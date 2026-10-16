//go:build windows

package main

import (
	"os"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// readConsoleSecret reads a line from the Windows console without showing it, as UTF-16 through
// ReadConsoleW. Reading bytes instead (as term.ReadPassword does) gives them in the console's code
// page, so a password with "ş" or "ğ" would not match the same password typed in a browser.
// ok is false when stdin is not a console.
func readConsoleSecret() (secret string, ok bool, err error) {
	h := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil {
		return "", false, nil
	}
	quiet := (mode &^ windows.ENABLE_ECHO_INPUT) | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT
	if err := windows.SetConsoleMode(h, quiet); err != nil {
		return "", true, err
	}
	defer windows.SetConsoleMode(h, mode)

	var line []uint16
	buf := make([]uint16, 256)
	for {
		var n uint32
		if err := windows.ReadConsole(h, &buf[0], uint32(len(buf)), &n, nil); err != nil {
			return "", true, err
		}
		line = append(line, buf[:n]...)
		if n == 0 || strings.ContainsRune(string(utf16.Decode(buf[:n])), '\n') {
			break
		}
	}
	return strings.TrimRight(string(utf16.Decode(line)), "\r\n"), true, nil
}
