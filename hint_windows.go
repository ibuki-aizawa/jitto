package main

import (
	"errors"
	"fmt"

	"github.com/ibuki-aizawa/jitto/internal/input"
)

// powerShellHint shows how to pipe JSON from PowerShell, for input that is
// PowerShell's display of an object instead.
const powerShellHint = `this is how PowerShell displays an object, not JSON. Pass the JSON text instead, for example:
  (Invoke-WebRequest URL).Content | jitto
  Invoke-RestMethod URL | ConvertTo-Json -Depth 100 | jitto`

// withInputHint adds a hint to an error from reading the input, when there is one.
// Only the Windows build gives the hint, to stay quiet elsewhere.
func withInputHint(err error) error {
	if errors.Is(err, input.ErrPowerShellDisplay) {
		return fmt.Errorf("%w\njitto: %s", err, powerShellHint)
	}
	return err
}
