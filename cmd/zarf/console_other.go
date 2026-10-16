//go:build !windows

package main

// readConsoleSecret is only needed on Windows; elsewhere the terminal already gives UTF-8.
func readConsoleSecret() (string, bool, error) { return "", false, nil }
