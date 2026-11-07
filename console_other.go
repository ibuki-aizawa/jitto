//go:build !windows

package main

// useConsoleInput does nothing here: when standard input is not a terminal, Bubble
// Tea reads keys from /dev/tty on its own.
func useConsoleInput() error {
	return nil
}
