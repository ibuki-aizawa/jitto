package input

import (
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// ErrPowerShellDisplay is wrapped in the error that Read returns when the input
// is not JSON but looks like PowerShell's display of an object. That is what a
// native command gets from "Invoke-WebRequest URL | jitto": PowerShell formats
// objects as text for the screen before it pipes them, and the formatting cuts
// long values short. Wrapping it does not change the error message.
var ErrPowerShellDisplay = errors.New("input is PowerShell's display of an object, not JSON")

// displayError marks err as ErrPowerShellDisplay and keeps its message.
type displayError struct{ err error }

func (e displayError) Error() string        { return e.err.Error() }
func (e displayError) Unwrap() error        { return e.err }
func (e displayError) Is(target error) bool { return target == ErrPowerShellDisplay }

// displayHead is how much of the input LooksLikePowerShellDisplay gets.
const displayHead = 4096

// LooksLikePowerShellDisplay reports whether text, the start of the input, is a
// list of properties as Windows PowerShell and PowerShell display an object:
//
//	StatusCode        : 200
//	StatusDescription : OK
//	Content           : {"id":1,"name":"jitto", ...
//	                    "more":...
//
// The lines end in CRLF, every colon is in the same column, and a value that does
// not fit continues on lines indented to the value. Only the first object, up to
// a blank line, is examined, and it must have at least two properties.
func LooksLikePowerShellDisplay(text []byte) bool {
	s := string(text)
	if !strings.Contains(s, "\r\n") {
		return false
	}
	lines := strings.Split(strings.TrimLeft(s, "\r\n"), "\r\n")
	// The last line may be cut off, or empty after the final CRLF.
	lines = lines[:len(lines)-1]

	// PowerShell lines up the colons by the width of the names, and Windows
	// PowerShell by their length in characters. The two differ for wide characters
	// such as Japanese, so keep track of both.
	var byWidth, byLength column
	props := 0
	for _, line := range lines {
		if line == "" {
			break
		}
		if width, length, ok := propertyColon(line); ok {
			byWidth.property(width)
			byLength.property(length)
			props++
			continue
		}
		if props == 0 {
			return false
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		byWidth.continuation(indent)
		byLength.continuation(indent)
		if byWidth.mismatch && byLength.mismatch {
			return false
		}
	}
	return props >= 2 && !(byWidth.mismatch && byLength.mismatch)
}

// column checks that the colons of the properties are in one column, and that
// continuation lines are indented to the values after them.
type column struct {
	col      int
	set      bool
	mismatch bool
}

func (c *column) property(col int) {
	if c.set && col != c.col {
		c.mismatch = true
	}
	c.col, c.set = col, true
}

func (c *column) continuation(indent int) {
	if indent < c.col+2 {
		c.mismatch = true
	}
}

// propertyColon returns the column of the colon in a property line such as
// "StatusCode        : 200" or "Forms             : ", counted in display width
// and in characters. ok is false if line is not a property line.
func propertyColon(line string) (width, length int, ok bool) {
	name, rest, found := strings.Cut(line, " ")
	if !found || name == "" || strings.Contains(name, ":") {
		return 0, 0, false
	}
	value := strings.TrimLeft(rest, " ")
	if !strings.HasPrefix(value, ":") || (len(value) > 1 && value[1] != ' ') {
		return 0, 0, false
	}
	before := line[:len(line)-len(value)]
	return runewidth.StringWidth(before), utf8.RuneCountInString(before), true
}

// headRecorder passes r through and keeps the first displayHead bytes.
type headRecorder struct {
	r    io.Reader
	head []byte
}

func (h *headRecorder) Read(p []byte) (int, error) {
	n, err := h.r.Read(p)
	if room := displayHead - len(h.head); room > 0 {
		h.head = append(h.head, p[:min(n, room)]...)
	}
	return n, err
}

// isDisplay reports whether the input, which is not valid JSON, looks like
// PowerShell's display of an object. If the input starts with a letter, as a
// property name does, it reads more of the input first: the decoder stops at the
// first character, and a pipe can deliver the text a line at a time.
func (h *headRecorder) isDisplay() bool {
	start := strings.TrimLeft(string(h.head), " \t\r\n")
	if start == "" || strings.ContainsRune(`{["-0123456789`, rune(start[0])) {
		return false
	}
	// The input is not JSON anyway, so a failure here changes nothing.
	_, _ = io.CopyN(io.Discard, h, int64(displayHead-len(h.head)))
	return LooksLikePowerShellDisplay(h.head)
}
