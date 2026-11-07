// Package eval runs jq expressions against decoded JSON documents and formats
// the results.
package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/itchyny/gojq"
)

// maxDepth is the deepest value that can be formatted. It matches the limit of
// json.Indent. It also protects gojq.Marshal, which is recursive: on a value
// nested millions of levels deep it overflows the goroutine stack, a fatal error
// that recover cannot catch.
const maxDepth = 10000

// ErrTooDeep is returned when a result is nested deeper than can be formatted.
var ErrTooDeep = fmt.Errorf("result is nested too deeply (limit %d)", maxDepth)

// Result is the formatted output of an expression.
type Result struct {
	// Output holds the results as indented JSON, each followed by a newline.
	Output string
	// Truncated reports that evaluation stopped early because of maxBytes.
	Truncated bool
}

// Eval runs expr against each document in docs, in order. An empty expression
// means ".".
//
// If maxBytes is positive, evaluation stops once the output exceeds it, and the
// output is cut to at most maxBytes at a line or, failing that, character
// boundary. A truncated result may hide a later error, so it does not prove that
// the expression succeeds on all of its input.
//
// On any error the result is empty: partial output is never returned. If ctx ends
// first, the error wraps ctx.Err(). Eval does not modify docs and may be called
// concurrently with the same docs.
func Eval(ctx context.Context, expr string, docs []any, maxBytes int) (Result, error) {
	code, err := compile(expr)
	if err != nil {
		return Result{}, err
	}

	var out bytes.Buffer
	for _, doc := range docs {
		iter := code.RunWithContext(ctx, doc)
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if err, ok := v.(error); ok {
				return Result{}, err
			}
			if err := appendValue(&out, v); err != nil {
				return Result{}, err
			}
			if maxBytes > 0 && out.Len() > maxBytes {
				return Result{Output: truncate(out.Bytes(), maxBytes), Truncated: true}, nil
			}
		}
	}
	return Result{Output: out.String()}, nil
}

func compile(expr string) (*gojq.Code, error) {
	if strings.TrimSpace(expr) == "" {
		expr = "."
	}
	query, err := gojq.Parse(expr)
	if err != nil {
		return nil, withKeyHint(expr, err)
	}
	return gojq.Compile(query, gojq.WithEnvironLoader(os.Environ))
}

// withKeyHint suggests quoting when a parse error comes from an object key written
// like .テスト, because jq only allows ASCII in unquoted keys.
func withKeyHint(expr string, err error) error {
	var parseErr *gojq.ParseError
	if !errors.As(err, &parseErr) {
		return err
	}
	key, ok := unquotedKey(expr, parseErr.Offset-len(parseErr.Token))
	if !ok {
		return err
	}
	return fmt.Errorf(`%w (try ."%s")`, err, key)
}

// unquotedKey returns the object key around offset, the position of the first
// character the parser rejected. It reports false unless that character is a
// non-ASCII letter in a key that follows a single dot.
func unquotedKey(expr string, offset int) (string, bool) {
	if offset <= 0 || offset >= len(expr) {
		return "", false
	}
	if r, _ := utf8.DecodeRuneInString(expr[offset:]); r < utf8.RuneSelf || !isKeyRune(r) {
		return "", false
	}

	// The ASCII start of a key like .Aテスト was accepted before the parser failed.
	start := offset
	for start > 0 && expr[start-1] < utf8.RuneSelf && isKeyRune(rune(expr[start-1])) {
		start--
	}
	if start == 0 || expr[start-1] != '.' || (start >= 2 && expr[start-2] == '.') {
		return "", false
	}

	end := offset
	for end < len(expr) {
		r, size := utf8.DecodeRuneInString(expr[end:])
		if !isKeyRune(r) {
			break
		}
		end += size
	}
	return expr[start:end], true
}

func isKeyRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r)
}

func appendValue(out *bytes.Buffer, v any) error {
	if exceedsDepth(v, maxDepth) {
		return ErrTooDeep
	}
	compact, err := gojq.Marshal(v)
	if err != nil {
		return err
	}
	if err := json.Indent(out, compact, "", "  "); err != nil {
		return fmt.Errorf("format result: %w", err)
	}
	out.WriteByte('\n')
	return nil
}

// exceedsDepth reports whether v nests deeper than limit levels of arrays and
// objects. Its own recursion stops at limit, so it is safe on any value.
func exceedsDepth(v any, limit int) bool {
	switch v := v.(type) {
	case []any:
		if limit == 0 {
			return true
		}
		for _, x := range v {
			if exceedsDepth(x, limit-1) {
				return true
			}
		}
	case map[string]any:
		if limit == 0 {
			return true
		}
		for _, x := range v {
			if exceedsDepth(x, limit-1) {
				return true
			}
		}
	}
	return false
}

// truncate returns at most maxBytes of b. It prefers to end after a complete line
// and otherwise avoids splitting a UTF-8 sequence.
func truncate(b []byte, maxBytes int) string {
	if i := bytes.LastIndexByte(b[:maxBytes], '\n'); i >= 0 {
		return string(b[:i+1])
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(b[cut]) {
		cut--
	}
	return string(b[:cut])
}
