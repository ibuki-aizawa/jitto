// Package input reads JSON documents from a file or stream.
package input

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

// ErrOddUTF16 is returned when UTF-16 input ends in the middle of a code unit.
var ErrOddUTF16 = errors.New("UTF-16 input has an odd number of bytes")

// ErrInvalidUTF8 is the notice Read gives when input read as UTF-8 is not
// valid UTF-8. The documents are still usable: the invalid bytes in strings are
// U+FFFD. Shift_JIS read without --encoding is the usual cause.
//
// It is also wrapped in the error when such input is not valid JSON either:
// Shift_JIS text can contain 0x5C, a backslash in ASCII, which escapes the
// closing quote of a string.
var ErrInvalidUTF8 = errors.New("input is not valid UTF-8; for Shift_JIS, use --encoding sjis")

var (
	utf8BOM    = []byte{0xEF, 0xBB, 0xBF}
	utf16LEBOM = []byte{0xFF, 0xFE}
	utf16BEBOM = []byte{0xFE, 0xFF}
)

// Encoding is the character encoding of input that has no byte order mark.
type Encoding int

const (
	// UTF8 is the default.
	UTF8 Encoding = iota
	// ShiftJIS is Shift_JIS as Windows uses it, also known as code page 932 and
	// Windows-31J.
	ShiftJIS
)

// encodingNames maps the names ParseEncoding accepts, in lower case.
var encodingNames = map[string]Encoding{
	"utf-8":       UTF8,
	"utf8":        UTF8,
	"shift_jis":   ShiftJIS,
	"shift-jis":   ShiftJIS,
	"sjis":        ShiftJIS,
	"cp932":       ShiftJIS,
	"windows-31j": ShiftJIS,
}

// ParseEncoding returns the encoding with the given name, ignoring case.
func ParseEncoding(name string) (Encoding, error) {
	if enc, ok := encodingNames[strings.ToLower(name)]; ok {
		return enc, nil
	}
	return 0, fmt.Errorf("unknown encoding %q (use utf-8 or shift_jis)", name)
}

// Read decodes every JSON document in r, in order. Whitespace-separated
// documents such as NDJSON yield one element each, and empty input yields none.
// Numbers are decoded as json.Number so that large integers keep their precision.
//
// The input is in enc, unless it starts with a UTF-16 byte order mark, as the
// files that Windows PowerShell 5.1 writes with ">" do. UTF-8 byte order marks at
// the start are discarded whatever enc is. The byte offsets in errors count bytes
// of the input converted to UTF-8.
//
// notice is ErrInvalidUTF8 if the input is read as UTF-8 and is not valid UTF-8,
// and nil otherwise. It does not make the documents unusable, unlike err. If
// the input is not valid JSON, err wraps ErrInvalidUTF8 when invalid UTF-8 came
// before the error.
func Read(r io.Reader, enc Encoding) (docs []any, notice, err error) {
	src, check, err := toUTF8(bufio.NewReader(r), enc)
	if err != nil {
		return nil, nil, err
	}

	dec := json.NewDecoder(src)
	dec.UseNumber()

	docs = []any{}
	for {
		var doc any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			err = describe(err, len(docs)+1)
			if check != nil && check.invalid {
				err = fmt.Errorf("%w (%w)", err, ErrInvalidUTF8)
			}
			return nil, nil, err
		}
		docs = append(docs, doc)
	}
	if check != nil && check.invalid {
		notice = ErrInvalidUTF8
	}
	return docs, notice, nil
}

// ReadFile decodes every JSON document in the named file. See Read.
func ReadFile(path string, enc Encoding) (docs []any, notice, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	docs, notice, err = Read(f, enc)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return docs, notice, nil
}

// toUTF8 returns the input as UTF-8 without byte order marks. It converts UTF-16
// input, which it recognizes by its byte order mark, and input in enc. If the
// input is read as UTF-8, it also returns the checker that sees it go by.
func toUTF8(br *bufio.Reader, enc Encoding) (io.Reader, *utf8Checker, error) {
	if err := skipBOM(br); err != nil {
		return nil, nil, err
	}
	head, err := br.Peek(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, err
	}
	var order binary.ByteOrder
	switch {
	case bytes.Equal(head, utf16LEBOM):
		order = binary.LittleEndian
	case bytes.Equal(head, utf16BEBOM):
		order = binary.BigEndian
	case enc == ShiftJIS:
		// A UTF-8 byte order mark does not count here: Windows PowerShell 5.1 adds
		// one to the Shift_JIS text it pipes when the console uses the UTF-8 code
		// page. JSON starts with ASCII, so Shift_JIS input does not start with
		// these bytes otherwise. UTF-16 marks still count: 0xFE and 0xFF are not
		// used in Shift_JIS.
		//
		// Invalid bytes become U+FFFD, as invalid UTF-8 does when it is decoded.
		return transform.NewReader(br, japanese.ShiftJIS.NewDecoder()), nil, nil
	default:
		check := &utf8Checker{r: br}
		return check, check, nil
	}

	// The documents end up in memory anyway, so convert the input as a whole.
	b, err := io.ReadAll(br)
	if err != nil {
		return nil, nil, err
	}
	s, err := decodeUTF16(b[2:], order)
	if err != nil {
		return nil, nil, err
	}
	// The text itself can start with more byte order marks.
	out := bufio.NewReader(strings.NewReader(s))
	if err := skipBOM(out); err != nil {
		return nil, nil, err
	}
	return out, nil, nil
}

// utf8Checker passes r through and notes whether it is valid UTF-8. A valid
// U+FFFD in the input does not count as invalid.
type utf8Checker struct {
	r io.Reader
	// pending holds the start of a character that the last read cut off.
	pending []byte
	invalid bool
}

func (c *utf8Checker) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if !c.invalid {
		c.check(p[:n])
		if errors.Is(err, io.EOF) && len(c.pending) > 0 {
			// The input ends in the middle of a character.
			c.invalid = true
		}
	}
	return n, err
}

func (c *utf8Checker) check(b []byte) {
	// Finish the character the last read cut off.
	for len(c.pending) > 0 && len(b) > 0 {
		c.pending = append(c.pending, b[0])
		b = b[1:]
		if utf8.FullRune(c.pending) {
			if r, size := utf8.DecodeRune(c.pending); r == utf8.RuneError && size == 1 {
				c.invalid = true
				return
			}
			c.pending = c.pending[:0]
		}
	}
	if len(b) == 0 {
		return
	}

	// Keep a character that this read cuts off for the next one.
	end := len(b)
	for k := 1; k <= utf8.UTFMax-1 && k <= len(b); k++ {
		if utf8.RuneStart(b[len(b)-k]) {
			if !utf8.FullRune(b[len(b)-k:]) {
				end = len(b) - k
			}
			break
		}
	}
	if !utf8.Valid(b[:end]) {
		c.invalid = true
		return
	}
	c.pending = append(c.pending, b[end:]...)
}

// decodeUTF16 converts b to UTF-8. An unpaired surrogate becomes U+FFFD, as
// invalid UTF-8 does when it is decoded.
func decodeUTF16(b []byte, order binary.ByteOrder) (string, error) {
	if len(b)%2 != 0 {
		return "", ErrOddUTF16
	}
	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = order.Uint16(b[2*i:])
	}
	return string(utf16.Decode(units)), nil
}

// skipBOM discards the UTF-8 byte order marks at the start. There can be more
// than one: Windows PowerShell 5.1 writes two to a pipe when $OutputEncoding is
// [Text.Encoding]::UTF8 and the console uses the UTF-8 code page.
func skipBOM(br *bufio.Reader) error {
	for {
		head, err := br.Peek(len(utf8BOM))
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if !bytes.Equal(head, utf8BOM) {
			return nil
		}
		if _, err := br.Discard(len(utf8BOM)); err != nil {
			return err
		}
	}
}

// describe adds the document number and, for syntax errors, the byte offset.
// Failures of the underlying reader are not JSON problems and are reported as is.
func describe(err error, doc int) error {
	var syntax *json.SyntaxError
	switch {
	case errors.As(err, &syntax):
		return fmt.Errorf("invalid JSON in document %d at byte %d: %w", doc, syntax.Offset, err)
	case errors.Is(err, io.ErrUnexpectedEOF):
		return fmt.Errorf("invalid JSON in document %d: %w", doc, err)
	}
	return fmt.Errorf("read input: %w", err)
}
