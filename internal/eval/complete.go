package eval

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/itchyny/gojq"
)

const (
	// placeholder stands in for the key being typed, so that the text parses.
	placeholder = "__jitto_placeholder__"
	// captureFunc replaces the placeholder and records the keys of its input.
	captureFunc = "__jitto_capture__"
	// valuePlaceholder stands in for the value being typed after == or !=, so
	// that the text parses.
	valuePlaceholder = "__jitto_value__"

	// maxCaptures is the number of input values Complete inspects at most.
	maxCaptures = 10000
	// maxCandidates is the number of distinct keys or values Complete collects at
	// most.
	maxCandidates = 1000
)

// errEnough stops the evaluation once enough keys are collected.
var errEnough = errors.New("enough candidates")

// Completion lists the object keys that can complete the key being typed, or
// the values that can complete the value being typed after == or !=.
type Completion struct {
	// Start is the byte offset of the key being typed, right after its dot, or of
	// the value being typed, at its opening quote if it is a string.
	Start int
	// Partial is the part of the key or the value typed so far, text[Start:].
	Partial string
	// Candidates are the keys or the values that match Partial, sorted ignoring
	// case.
	Candidates []Candidate

	// text is the text the candidates were computed for.
	text string
	// value reports whether the candidates are values rather than keys.
	value bool
}

// Candidate is one key or value that can complete the one being typed.
type Candidate struct {
	// Key is the object key. For a value, it is the same as Text.
	Key string
	// Text replaces the partial key or value. For a key, it is Key if Key is an
	// identifier, and Key quoted otherwise, as in ."名前". For a value, it is the
	// value in JSON, as in "shipped" or 42.
	Text string
}

// Narrow returns the candidates that apply to text, the expression up to the
// cursor, while it continues the key or the value c completes: text must be the
// text c was computed for, followed by more characters of the key or the value.
// The partial key or value then still starts at c.Start. Narrow leaves out a
// candidate that the partial key or value already equals, because inserting it
// would change nothing.
//
// Narrow returns nil for any other text, because c says nothing about it. It lets
// the caller update the candidates on every keystroke and run Complete only after
// a pause.
func (c Completion) Narrow(text string) []Candidate {
	if len(c.Candidates) == 0 || !strings.HasPrefix(text, c.text) {
		return nil
	}
	if c.value {
		if start, ok := partialValueStart(text); !ok || start != c.Start {
			return nil
		}
	} else if !isIdentBytes(text[len(c.text):]) {
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
// When text ends with a value being typed after == or !=, such as
// `.status == "sh` or "select(.id != ", Complete returns instead the strings,
// numbers, booleans and nulls that the left side of the comparison yields there.
//
// The Candidates are empty if text does not end with a key or a value, the
// expression up to there cannot be parsed, or nothing matching reaches the
// position before ctx ends. Complete does not modify docs and may be called
// concurrently with the same docs.
func Complete(ctx context.Context, text string, docs []any) Completion {
	if start, ok := partialValueStart(text); ok {
		return completeValue(ctx, text, start, docs)
	}
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
	c := &collector{prefix: strings.ToLower(partial), found: map[string]struct{}{}, add: (*collector).addKeys}
	if !c.collect(ctx, query, docs) {
		return Completion{}
	}
	return Completion{Start: start, Partial: partial, Candidates: c.candidates(keyText), text: text}
}

// completeValue completes the value that starts at start in text, after == or !=.
func completeValue(ctx context.Context, text string, start int, docs []any) Completion {
	closers, ok := closingBrackets(text[:start])
	if !ok {
		return Completion{}
	}
	query, err := gojq.Parse(text[:start] + valuePlaceholder + closers)
	if err != nil {
		return Completion{}
	}
	if !walk(reflect.ValueOf(query), rewriteComparison) {
		return Completion{}
	}

	partial := text[start:]
	c := &collector{prefix: strings.ToLower(partial), found: map[string]struct{}{}, add: (*collector).addValue}
	if !c.collect(ctx, query, docs) {
		return Completion{}
	}
	identity := func(s string) string { return s }
	return Completion{Start: start, Partial: partial, Candidates: c.candidates(identity), text: text, value: true}
}

// partialValueStart returns the offset of the value being typed at the end of
// text, right after == or != and any spaces. The value is a string that is not
// closed yet, as in "sh, or a run of characters that can start a number, true,
// false or null, possibly empty. It reports false if text does not end so.
func partialValueStart(text string) (int, bool) {
	_, start, inString, ok := scanBrackets(text)
	if !ok {
		return 0, false
	}
	if inString {
		if strings.Contains(text[start:], `\(`) {
			// The string has an interpolation, which a value cannot replace.
			return 0, false
		}
	} else {
		start = len(text)
		for start > 0 && isLiteralByte(text[start-1]) {
			start--
		}
		if start < len(text) && !isLiteralStart(text[start]) {
			// Such as .key or _x, which is not a value.
			return 0, false
		}
	}
	before := strings.TrimRight(text[:start], " \t")
	if !strings.HasSuffix(before, "==") && !strings.HasSuffix(before, "!=") {
		return 0, false
	}
	return start, true
}

func isLiteralByte(c byte) bool {
	return isIdentByte(c) || c == '.' || c == '+' || c == '-'
}

func isLiteralStart(c byte) bool {
	return isDigit(c) || c == '-' || 'a' <= c && c <= 'z'
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
	open, _, inString, ok := scanBrackets(text)
	if !ok || inString {
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

// interpolation marks a \( inside a string in the brackets scanBrackets returns.
const interpolation = 'i'

// scanBrackets returns the brackets and string interpolations that text leaves
// open, in the order they were opened, and whether text ends inside a string,
// with the offset of its opening quote. It reports ok false if text has an
// unmatched closing bracket.
func scanBrackets(text string) (open []byte, stringStart int, inString, ok bool) {
	// starts holds the opening quotes of the strings around the open
	// interpolations.
	var starts []int
	for i := 0; i < len(text); i++ {
		c := text[i]
		if inString {
			switch {
			case c == '\\' && i+1 < len(text) && text[i+1] == '(':
				open = append(open, interpolation)
				starts = append(starts, stringStart)
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
			stringStart = i
		case '(', '[', '{':
			open = append(open, c)
		case ')', ']', '}':
			if len(open) == 0 {
				return nil, 0, false, false
			}
			top := open[len(open)-1]
			open = open[:len(open)-1]
			switch {
			case top == interpolation && c == ')':
				inString = true
				stringStart = starts[len(starts)-1]
				starts = starts[:len(starts)-1]
			case top == '(' && c == ')', top == '[' && c == ']', top == '{' && c == '}':
			default:
				return nil, 0, false, false
			}
		}
	}
	return open, stringStart, inString, true
}

// replacePlaceholder finds the term that indexes the placeholder and rewrites it
// to pass the indexed value to captureFunc: X.placeholder becomes (X | capture)
// and a lone .placeholder becomes capture. It reports whether it found the term.
func replacePlaceholder(v reflect.Value) bool {
	return walk(v, func(p any) bool {
		t, ok := p.(*gojq.Term)
		return ok && rewriteTerm(t)
	})
}

// walk calls f with each pointer in the tree of v, parents first, until f reports
// true. It reports whether f did.
func walk(v reflect.Value, f func(any) bool) bool {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return false
		}
		if f(v.Interface()) {
			return true
		}
		return walk(v.Elem(), f)
	case reflect.Struct:
		for i := range v.NumField() {
			if fv := v.Field(i); fv.CanInterface() && walk(fv, f) {
				return true
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			if walk(v.Index(i), f) {
				return true
			}
		}
	}
	return false
}

// rewriteComparison rewrites X == valuePlaceholder, or X != valuePlaceholder, to
// pass the values of X to captureFunc: it becomes X | capture. It reports whether
// p is such a comparison.
func rewriteComparison(p any) bool {
	q, ok := p.(*gojq.Query)
	if !ok || q.Op != gojq.OpEq && q.Op != gojq.OpNe || q.Right == nil {
		return false
	}
	t := q.Right.Term
	if t == nil || t.Type != gojq.TermTypeFunc || t.Func.Name != valuePlaceholder || len(t.Func.Args) != 0 || len(t.SuffixList) != 0 {
		return false
	}
	q.Op = gojq.OpPipe
	q.Right = &gojq.Query{Term: &gojq.Term{Type: gojq.TermTypeFunc, Func: &gojq.Func{Name: captureFunc}}}
	return true
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

// collector gathers the keys or the values that reach captureFunc during one
// evaluation.
type collector struct {
	// prefix is the lowercased partial key or value.
	prefix string
	// found holds the keys, or the values in JSON.
	found    map[string]struct{}
	captures int
	// add records what v offers: addKeys or addValue.
	add func(c *collector, v any)
}

// addKeys records the matching keys of v if it is an object.
func (c *collector) addKeys(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for k := range m {
		if len(c.found) >= maxCandidates {
			break
		}
		if strings.HasPrefix(strings.ToLower(k), c.prefix) {
			c.found[k] = struct{}{}
		}
	}
}

// addValue records v in JSON if it is a string, a number, a boolean or null and
// matches the prefix.
func (c *collector) addValue(v any) {
	text, ok := scalarText(v)
	if ok && strings.HasPrefix(strings.ToLower(text), c.prefix) {
		c.found[text] = struct{}{}
	}
}

// scalarText returns v in JSON, which is also how jq writes it, if v is a string,
// a number, a boolean or null.
func scalarText(v any) (string, bool) {
	switch v := v.(type) {
	case nil:
		return "null", true
	case bool:
		return strconv.FormatBool(v), true
	case string:
		return jsonString(v), true
	case int, float64, *big.Int, json.Number:
		b, err := json.Marshal(v)
		if err != nil {
			// NaN and infinities have no JSON form.
			return "", false
		}
		return string(b), true
	}
	return "", false
}

// collect compiles query, whose captureFunc records what reaches it, and runs it
// against docs. It reports false if the query does not compile.
func (c *collector) collect(ctx context.Context, query *gojq.Query, docs []any) bool {
	code, err := gojq.Compile(query,
		gojq.WithEnvironLoader(os.Environ),
		gojq.WithIterFunction(captureFunc, 0, 0, c.capture))
	if err != nil {
		return false
	}
	c.run(ctx, code, docs)
	return true
}

// capture records what v offers. It emits nothing, so the rest of the expression
// does not run for v.
func (c *collector) capture(v any, _ []any) gojq.Iter {
	c.captures++
	if len(c.found) < maxCandidates {
		c.add(c, v)
	}
	if c.captures >= maxCaptures || len(c.found) >= maxCandidates {
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

// candidates returns what was found, sorted ignoring case, with text giving the
// Text of each.
func (c *collector) candidates(text func(string) string) []Candidate {
	if len(c.found) == 0 {
		return nil
	}
	keys := make([]string, 0, len(c.found))
	for k := range c.found {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
	})
	cands := make([]Candidate, len(keys))
	for i, k := range keys {
		cands[i] = Candidate{Key: k, Text: text(k)}
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
