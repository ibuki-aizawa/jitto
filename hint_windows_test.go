package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/ibuki-aizawa/jitto/internal/input"
)

func TestWithInputHint(t *testing.T) {
	display := "\r\nStatusCode        : 200\r\nStatusDescription : OK\r\n"
	_, _, err := input.Read(strings.NewReader(display), input.UTF8)
	got := withInputHint(err)
	if !strings.HasPrefix(got.Error(), err.Error()+"\njitto: this is how PowerShell displays an object") {
		t.Errorf("withInputHint(%q) = %q, want the hint after the error", err, got)
	}
	if !strings.Contains(got.Error(), "(Invoke-WebRequest URL).Content | jitto") {
		t.Errorf("withInputHint(%q) = %q, want an example", err, got)
	}
	if !errors.Is(got, input.ErrPowerShellDisplay) {
		t.Errorf("withInputHint(%q) does not wrap the error", err)
	}

	_, _, err = input.Read(strings.NewReader(`{"a": 1,}`), input.UTF8)
	if got := withInputHint(err); got != err {
		t.Errorf("withInputHint(%q) = %q, want it unchanged", err, got)
	}
}
