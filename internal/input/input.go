// Package input reads JSON documents from a file or stream.
package input

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrUTF16 is returned when the input starts with a UTF-16 byte order mark.
// Windows PowerShell 5.1 writes such files when redirecting with ">".
var ErrUTF16 = errors.New("input is encoded as UTF-16; convert it to UTF-8")

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Read decodes every JSON document in r, in order. Whitespace-separated
// documents such as NDJSON yield one element each, and empty input yields none.
// Numbers are decoded as json.Number so that large integers keep their precision.
func Read(r io.Reader) ([]any, error) {
	br := bufio.NewReader(r)
	if err := skipBOM(br); err != nil {
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

// skipBOM discards a leading UTF-8 byte order mark and rejects UTF-16 input.
func skipBOM(br *bufio.Reader) error {
	head, err := br.Peek(len(utf8BOM))
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	switch {
	case bytes.Equal(head, utf8BOM):
		_, err := br.Discard(len(utf8BOM))
		return err
	case bytes.HasPrefix(head, []byte{0xFF, 0xFE}), bytes.HasPrefix(head, []byte{0xFE, 0xFF}):
		return ErrUTF16
	}
	return nil
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
