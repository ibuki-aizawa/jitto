package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// useConsoleInput makes the console the process's standard input, so keys can be
// read after standard input was consumed as data.
//
// tea.WithInputTTY cannot do this on Windows. When standard input is not a console,
// Bubble Tea v1 opens CONIN$ itself, and closing that file on exit blocks until the
// pending read returns, so the program hangs until the next key press. With the
// console as real standard input, Bubble Tea reads through ReadConsoleInput instead
// and shuts down normally.
func useConsoleInput() error {
	conin, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := windows.SetStdHandle(windows.STD_INPUT_HANDLE, windows.Handle(conin.Fd())); err != nil {
		conin.Close()
		return err
	}
	os.Stdin = conin
	return nil
}
