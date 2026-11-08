package eval

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/itchyny/gojq"
)

const (
	// placeholder stands in for the key being typed, so that the text parses.
	placeholder = "__jitto_placeholder__"
	// captureFunc replaces the placeholder and records the keys of its input.
	captureFunc = "__jitto_capture__"

	// maxCaptures is the number of input values Complete inspects at most.
	maxCaptures = 10000
	// maxCandidates is the number of distinct keys Complete collects at most.
	maxCandidates = 1000
)

// errEnough stops the evaluation once enough keys are collected.
var errEnough = errors.New("enough candidates")

// Completion lists the object keys that can complete the key being typed.
type Completion struct {
	// Start is the byte offset of the key being typed, right after its dot.
	Start int
	// Partial is the part of the key typed so far, text[Start:].
	Partial string
	// Candidates are the keys that match Partial, sorted ignoring case.
	Candidates []Candidate

	// text is the text the candidates were computed for.
	text string
}

// Candidate is one key that can complete the key being typed.
type Candidate struct {
	// Key is the object key.
	Key string
	// Text replaces the partial key. It is Key if Key is an identifier, and Key
	// quoted otherwise, as in ."名前".
	Text string
}

// Narrow returns the candidates that apply to text, the expression up to the
// cursor, while it continues the key c completes: text must be the text c was
// computed for, followed by more characters of the key. The partial key then
// still starts at c.Start. Narrow leaves out a candidate that the partial key
// already equals, because inserting it would change nothing.
//
// Narrow returns nil for any other text, because c says nothing about it. It lets
// the caller update the candidates on every keystroke and run Complete only after
// a pause.
func (c Completion) Narrow(text string) []Candidate {
	if len(c.Candidates) == 0 || !strings.HasPrefix(text, c.text) || !isIdentBytes(text[len(c.text):]) {
		return nil
	}
	partial := text[c.Start:]
	prefix := strings.ToLower(partial)
	var out []Candidate
	for _, cand := range c.Candidates {
		if cand.Text != partial && strings.HasPrefix(strings.ToLower(cand.Key), prefix) {
			out = append(out, cand)
		}
	}
	return out
}

// Complete returns the keys that can follow text, the expression up to the
// cursor, when text ends with a key being typed, such as ".", ".items[0].na" or
// "map(.na". The keys come from the values that actually reach that position when
// the expression runs against docs, and match Partial as a prefix, ignoring case.
//
// The Candidates are empty if text does not end with a key, the expression up to
// there cannot be parsed, or no object reaches the position before ctx ends.
// Complete does not modify docs and may be called concurrently with the same docs.
func Complete(ctx context.Context, text string, docs []any) Completion {
	start, ok := partialKeyStart(text)
	if !ok {
		return Completion{}
	}
	closers, ok := closingBrackets(text[:start])
	if !ok {
		return Completion{}
	}
	query, err := gojq.Parse(text[:start] + placeholder + closers)
	if err != nil {
		return Completion{}
	}
	if !replacePlaceholder(reflect.ValueOf(query)) {
		return Completion{}
	}

	partial := text[start:]
	c := &collector{prefix: strings.ToLower(partial), keys: map[string]struct{}{}}
	code, err := gojq.Compile(query,
		gojq.WithEnvironLoader(os.Environ),
		gojq.WithIterFunction(captureFunc, 0, 0, c.capture))
	if err != nil {
		return Completion{}
	}
	c.run(ctx, code, docs)

	return Completion{Start: start, Partial: partial, Candidates: c.candidates(), text: text}
}

// partialKeyStart returns the offset of the identifier characters at the end of
// text. It reports false unless they follow a single dot and could start a key.
func partialKeyStart(text string) (int, bool) {
	start := len(text)
	for start > 0 && isIdentByte(text[start-1]) {
		start--
	}
	if start < len(text) && isDigit(text[start]) {
		// .1 is a number.
		return 0, false
	}
	if start == 0 || text[start-1] != '.' {
		return 0, false
	}
	if start >= 2 && text[start-2] == '.' {
		// .. is the recursive descent.
		return 0, false
	}
	return start, true
}

// closingBrackets returns the brackets and string interpolations that text leaves
// open, closed in order. It reports false if text ends inside a string or has an
// unmatched closing bracket.
func closingBrackets(text string) (string, bool) {
	// interpolation marks a \( inside a string.
	const interpolation = 'i'
	var open []byte
	inString := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if inString {
			switch {
			case c == '\\' && i+1 < len(text) && text[i+1] == '(':
				open = append(open, interpolation)
				inString = false
				i++
			case c == '\\':
				i++
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '(', '[', '{':
			open = append(open, c)
		case ')', ']', '}':
			if len(open) == 0 {
				return "", false
			}
			top := open[len(open)-1]
			open = open[:len(open)-1]
			switch {
			case top == interpolation && c == ')':
				inString = true
			case top == '(' && c == ')', top == '[' && c == ']', top == '{' && c == '}':
			default:
				return "", false
			}
		}
	}
	if inString {
		return "", false
	}

	var s strings.Builder
	for _, c := range slices.Backward(open) {
		switch c {
		case interpolation:
			s.WriteString(`)"`)
		case '(':
			s.WriteByte(')')
		case '[':
			s.WriteByte(']')
		case '{':
			s.WriteByte('}')
		}
	}
	return s.String(), true
}

// replacePlaceholder finds the term that indexes the placeholder and rewrites it
// to pass the indexed value to captureFunc: X.placeholder becomes (X | capture)
// and a lone .placeholder becomes capture. It reports whether it found the term.
func replacePlaceholder(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return false
		}
		if t, ok := v.Interface().(*gojq.Term); ok && rewriteTerm(t) {
			return true
		}
		return replacePlaceholder(v.Elem())
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanInterface() && replacePlaceholder(f) {
				return true
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			if replacePlaceholder(v.Index(i)) {
				return true
			}
		}
	}
	return false
}

func rewriteTerm(t *gojq.Term) bool {
	capture := &gojq.Term{Type: gojq.TermTypeFunc, Func: &gojq.Func{Name: captureFunc}}
	if t.Type == gojq.TermTypeIndex && isPlaceholder(t.Index) && len(t.SuffixList) == 0 {
		*t = *capture
		return true
	}
	n := len(t.SuffixList)
	if n == 0 || !isPlaceholder(t.SuffixList[n-1].Index) {
		return false
	}
	base := *t
	base.SuffixList = t.SuffixList[:n-1]
	*t = gojq.Term{
		Type: gojq.TermTypeQuery,
		Query: &gojq.Query{
			Left:  &gojq.Query{Term: &base},
			Op:    gojq.OpPipe,
			Right: &gojq.Query{Term: capture},
		},
	}
	return true
}

func isPlaceholder(index *gojq.Index) bool {
	return index != nil && index.Name == placeholder
}

// collector gathers the keys that reach captureFunc during one evaluation.
type collector struct {
	// prefix is the lowercased partial key.
	prefix   string
	keys     map[string]struct{}
	captures int
}

// capture records the matching keys of v. It emits nothing, so the rest of the
// expression does not run for v.
func (c *collector) capture(v any, _ []any) gojq.Iter {
	c.captures++
	if m, ok := v.(map[string]any); ok {
		for k := range m {
			if len(c.keys) >= maxCandidates {
				break
			}
			if strings.HasPrefix(strings.ToLower(k), c.prefix) {
				c.keys[k] = struct{}{}
			}
		}
	}
	if c.captures >= maxCaptures || len(c.keys) >= maxCandidates {
		return gojq.NewIter(errEnough)
	}
	return gojq.NewIter[any]()
}

// run evaluates code against each document. An error in one document, such as
// indexing a string, does not stop the others, because their keys are still valid.
func (c *collector) run(ctx context.Context, code *gojq.Code, docs []any) {
	for _, doc := range docs {
		iter := code.RunWithContext(ctx, doc)
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if err, ok := v.(error); ok {
				if errors.Is(err, errEnough) || ctx.Err() != nil {
					return
				}
				break
			}
		}
	}
}

func (c *collector) candidates() []Candidate {
	if len(c.keys) == 0 {
		return nil
	}
	keys := make([]string, 0, len(c.keys))
	for k := range c.keys {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
	})
	cands := make([]Candidate, len(keys))
	for i, k := range keys {
		cands[i] = Candidate{Key: k, Text: keyText(k)}
	}
	return cands
}

// keyText returns k as it can follow a dot: as is if it is an identifier, and
// quoted otherwise.
func keyText(k string) string {
	if isIdent(k) {
		return k
	}
	return jsonString(k)
}

// jsonString returns s as a JSON string, which is also a jq string literal.
func jsonString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Encoding a string cannot fail.
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func isIdent(s string) bool {
	return s != "" && !isDigit(s[0]) && isIdentBytes(s)
}

func isIdentBytes(s string) bool {
	for i := range len(s) {
		if !isIdentByte(s[i]) {
			return false
		}
	}
	return true
}

func isIdentByte(c byte) bool {
	return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || isDigit(c)
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}
