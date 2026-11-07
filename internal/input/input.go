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
)

// ErrOddUTF16 is returned when UTF-16 input ends in the middle of a code unit.
var ErrOddUTF16 = errors.New("UTF-16 input has an odd number of bytes")

var (
	utf8BOM    = []byte{0xEF, 0xBB, 0xBF}
	utf16LEBOM = []byte{0xFF, 0xFE}
	utf16BEBOM = []byte{0xFE, 0xFF}
)

// Read decodes every JSON document in r, in order. Whitespace-separated
// documents such as NDJSON yield one element each, and empty input yields none.
// Numbers are decoded as json.Number so that large integers keep their precision.
//
// The input is UTF-8, or UTF-16 if it starts with a UTF-16 byte order mark, as
// the files that Windows PowerShell 5.1 writes with ">" do. For UTF-16 input,
// the byte offsets in errors count bytes of the input converted to UTF-8.
func Read(r io.Reader) ([]any, error) {
	br, err := toUTF8(bufio.NewReader(r))
	if err != nil {
		return nil, err
	}

	dec := json.NewDecoder(br)
	dec.UseNumber()

	docs := []any{}
	for {
		var doc any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, describe(err, len(docs)+1)
		}
		docs = append(docs, doc)
	}
}

// ReadFile decodes every JSON document in the named file. See Read.
func ReadFile(path string) ([]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	docs, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return docs, nil
}

// toUTF8 returns the input as UTF-8 without byte order marks. It converts UTF-16
// input, which it recognizes by its byte order mark.
func toUTF8(br *bufio.Reader) (*bufio.Reader, error) {
	if err := skipBOM(br); err != nil {
		return nil, err
	}
	head, err := br.Peek(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var order binary.ByteOrder
	switch {
	case bytes.Equal(head, utf16LEBOM):
		order = binary.LittleEndian
	case bytes.Equal(head, utf16BEBOM):
		order = binary.BigEndian
	default:
		return br, nil
	}

	// The documents end up in memory anyway, so convert the input as a whole.
	b, err := io.ReadAll(br)
	if err != nil {
		return nil, err
	}
	s, err := decodeUTF16(b[2:], order)
	if err != nil {
		return nil, err
	}
	// The text itself can start with more byte order marks.
	out := bufio.NewReader(strings.NewReader(s))
	if err := skipBOM(out); err != nil {
		return nil, err
	}
	return out, nil
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
