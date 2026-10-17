//go:build !windows

package main

// readConsoleSecret is only needed on Windows; elsewhere the terminal already gives UTF-8.
func readConsoleSecret() (string, bool, error) { return "", false, nil }

// startedOnItsOwn matters only on Windows, where double-clicking opens a console that closes
// at once; elsewhere the tool is started from a terminal.
func startedOnItsOwn() bool { return false }
