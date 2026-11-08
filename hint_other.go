//go:build !windows

package main

// withInputHint returns err as it is. The hint for PowerShell's display of an
// object is given only on Windows; see hint_windows.go.
func withInputHint(err error) error {
	return err
}
