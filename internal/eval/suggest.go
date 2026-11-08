package eval

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/itchyny/gojq"
)

const (
	// maxSuggestions is the number of examples Suggest returns at most.
	maxSuggestions = 10
	// maxSamples is the number of values Suggest inspects at most: results of
	// the expression, and elements of the arrays among them.
	maxSamples = 10000
	// maxDistinct is the number of distinct strings counted for each key.
	maxDistinct = 100
	// maxCategories is the number of distinct values a key may have to be a
	// category, for select and group_by.
	maxCategories = 20
	// maxValueLen is the longest string, in bytes, that Suggest writes into an
	// example.
	maxValueLen = 40
)

// Suggestion is an example of what can follow an expression.
type Suggestion struct {
	// Text is the whole expression to use, such as ".items | map(.name)".
	Text string
	// Example is what Suggest adds to the expression, such as "map(.name)".
	Example string
	// Description says what the example gives, in a few words.
	Description string
}

// example is a suggestion before it is joined to the expression.
type example struct {
	text        string
	description string
	// collect puts the expression in an array first, as in [EXPR] | add, for an
	// expression that yields several values.
	collect bool
}

// Suggest returns examples of what can follow expr, made from the values expr
// yields for docs: their types, keys and values. An empty expression means ".".
// Each example is checked against docs, and one that fails, yields nothing or
// changes nothing is left out.
//
// expr may end with a pipe, as in ".items |", to ask for what comes after it.
// Suggest returns nil if expr fails or yields nothing, or if ctx ends before the
// values are examined. If ctx ends while the examples are checked, it returns the
// ones checked so far. It does not modify docs and may be called concurrently
// with the same docs.
func Suggest(ctx context.Context, expr string, docs []any) []Suggestion {
	base, prefix := splitPipe(expr)
	code, err := compile(base)
	if err != nil {
		return nil
	}
	values, stream, ok := sample(ctx, code, docs)
	if !ok || len(values) == 0 {
		return nil
	}
	var out []Suggestion
	for _, ex := range examples(values, stream) {
		if len(out) == maxSuggestions || ctx.Err() != nil {
			break
		}
		text := prefix + ex.text
		if ex.collect {
			text = "[" + base + "] | " + ex.text
		}
		code, err := compile(text)
		if err != nil {
			continue
		}
		// Comparing the values is much faster than formatting them.
		after, _, ok := sample(ctx, code, docs)
		if !ok || len(after) == 0 || reflect.DeepEqual(after, values) {
			continue
		}
		out = append(out, Suggestion{Text: text, Example: ex.text, Description: ex.description})
	}
	return out
}

// splitPipe returns the expression to examine and the text that goes before an
// example: nothing for an empty expression or ".", the expression followed by
// " | ", or for an expression that already ends with a pipe, followed by " ".
func splitPipe(expr string) (base, prefix string) {
	s := strings.TrimSpace(expr)
	if rest, ok := strings.CutSuffix(s, "|"); ok {
		if rest = strings.TrimSpace(rest); rest == "" {
			return "", ""
		}
		return rest, s + " "
	}
	if s == "" || s == "." {
		return s, ""
	}
	return s, s + " | "
}

// sample returns the values code yields for docs, up to maxSamples, and whether
// a document yields more than one, which makes them a stream rather than one
// value per document. It reports false if code fails or ctx ends.
func sample(ctx context.Context, code *gojq.Code, docs []any) (values []any, stream, ok bool) {
	for _, doc := range docs {
		iter := code.RunWithContext(ctx, doc)
		n := 0
		for len(values) < maxSamples {
			v, more := iter.Next()
			if !more {
				break
			}
			if _, isErr := v.(error); isErr {
				return nil, false, false
			}
			values = append(values, v)
			n++
		}
		stream = stream || n > 1
		if len(values) == maxSamples {
			break
		}
	}
	return values, stream, ctx.Err() == nil
}

// examples returns the examples for values, best first.
func examples(values []any, stream bool) []example {
	switch kindOf(values) {
	case kindObject:
		return objectExamples(newStats(values), stream)
	case kindArray:
		var elems []any
		for _, v := range values {
			for _, e := range v.([]any) {
				if len(elems) == maxSamples {
					break
				}
				elems = append(elems, e)
			}
		}
		return arrayExamples(elems)
	case kindString:
		return stringExamples(values, stream)
	case kindNumber:
		return numberExamples(values, stream)
	case kindMixed:
		out := []example{{text: "type", description: "the type of each result"}}
		if stream {
			out = append(out, example{text: "length", description: "the number of results", collect: true})
		}
		return out
	}
	return nil
}

type kind int

const (
	kindMixed kind = iota
	kindObject
	kindArray
	kindString
	kindNumber
	kindOther // null and booleans
)

// kindOf returns the kind all values share, or kindMixed.
func kindOf(values []any) kind {
	k := kindMixed
	for i, v := range values {
		var vk kind
		switch v.(type) {
		case map[string]any:
			vk = kindObject
		case []any:
			vk = kindArray
		case string:
			vk = kindString
		case int, float64, *big.Int, json.Number:
			vk = kindNumber
		default:
			vk = kindOther
		}
		if i > 0 && vk != k {
			return kindMixed
		}
		k = vk
	}
	return k
}

func arrayExamples(elems []any) []example {
	switch kindOf(elems) {
	case kindObject:
		return objectArrayExamples(newStats(elems))
	case kindString:
		return []example{
			{text: "unique", description: "the distinct values"},
			{text: "group_by(.) | map({value: .[0], count: length})", description: "how often each value appears"},
			{text: `join(", ")`, description: "the values joined in one string"},
			{text: "sort", description: "the values in order"},
			{text: "length", description: "the number of values"},
		}
	case kindNumber:
		return []example{
			{text: "add", description: "the total"},
			{text: "add / length", description: "the average"},
			{text: "max", description: "the largest value"},
			{text: "min", description: "the smallest value"},
			{text: "sort", description: "the values in order"},
			{text: "length", description: "the number of values"},
		}
	case kindArray:
		return []example{
			{text: "flatten", description: "the items of the inner arrays in one array"},
			{text: "map(length)", description: "the length of each array"},
			{text: "length", description: "the number of arrays"},
		}
	}
	if len(elems) == 0 {
		return nil
	}
	return []example{
		{text: "map(type)", description: "the type of each item"},
		{text: "first", description: "the first item"},
		{text: "length", description: "the number of items"},
	}
}

func objectArrayExamples(s *stats) []example {
	var out []example
	if k, ok := s.label(); ok {
		out = append(out, example{text: "map(" + field(k) + ")", description: "the " + k + " of each item"})
	}
	if c, ok := s.category(); ok {
		out = append(out, example{
			text:        "map(select(" + field(c.key) + " == " + jsonString(c.top) + "))",
			description: fmt.Sprintf("the items whose %s is %s", c.key, jsonString(c.top)),
		})
	}
	if n, ok := s.number(); ok {
		out = append(out, example{
			text:        "map(select(" + field(n.key) + " > " + n.median + "))",
			description: fmt.Sprintf("the items whose %s is over %s", n.key, n.median),
		})
	}
	if b, ok := s.boolean(); ok {
		out = append(out, example{text: "map(select(" + field(b) + "))", description: "the items where " + b + " is true"})
	}
	if k, ok := s.sortKey(); ok {
		out = append(out, example{text: "sort_by(" + field(k) + ")", description: "the items in order of " + k})
	}
	if c, ok := s.category(); ok {
		out = append(out,
			example{
				text:        "group_by(" + field(c.key) + ") | map({" + keyText(c.key) + ": .[0]" + field(c.key) + ", count: length})",
				description: "the number of items for each " + c.key,
			},
			example{text: "map(" + field(c.key) + ") | unique", description: "the distinct values of " + c.key},
		)
	}
	if pick, ok := s.pick(); ok {
		out = append(out, example{text: "map(" + pick + ")", description: "only " + s.pickNames() + " of each item"})
	}
	return append(out,
		example{text: "first", description: "the first item"},
		example{text: "length", description: "the number of items"},
	)
}

func objectExamples(s *stats, stream bool) []example {
	var out []example
	for _, k := range s.nested(3) {
		out = append(out, example{text: field(k), description: "the value of " + k})
	}
	if stream {
		if c, ok := s.category(); ok {
			out = append(out, example{
				text:        "select(" + field(c.key) + " == " + jsonString(c.top) + ")",
				description: fmt.Sprintf("the ones whose %s is %s", c.key, jsonString(c.top)),
			})
		}
		if n, ok := s.number(); ok {
			out = append(out, example{
				text:        "select(" + field(n.key) + " > " + n.median + ")",
				description: fmt.Sprintf("the ones whose %s is over %s", n.key, n.median),
			})
		}
		if b, ok := s.boolean(); ok {
			out = append(out, example{text: "select(" + field(b) + ")", description: "the ones where " + b + " is true"})
		}
	}
	if pick, ok := s.pick(); ok {
		out = append(out, example{text: pick, description: "only " + s.pickNames()})
	}
	out = append(out,
		example{text: "keys", description: "the keys"},
		example{text: "to_entries", description: "the key-value pairs"},
	)
	// One nested value among scalars is often bulky detail to leave out.
	if k, ok := s.onlyNested(); ok && len(s.fields) > 1 {
		out = append(out, example{text: "del(" + field(k) + ")", description: "everything but " + k})
	}
	if stream {
		out = append(out, example{text: "length", description: "the number of results", collect: true})
	}
	return out
}

func stringExamples(values []any, stream bool) []example {
	var out []example
	s := values[0].(string)
	if strings.Contains(s, ",") {
		out = append(out, example{text: `split(",")`, description: "the parts between commas"})
	}
	if t := strings.TrimSpace(s); (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Valid([]byte(t)) {
		out = append(out, example{text: "fromjson", description: "the JSON in the string"})
	}
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		out = append(out, example{text: "fromdateiso8601", description: "the time in seconds since 1970"})
	}
	if strings.ToLower(s) != s {
		out = append(out, example{text: "ascii_downcase", description: "in lower case"})
	}
	if stream {
		out = append(out,
			example{text: "unique", description: "the distinct values", collect: true},
			example{text: "length", description: "the number of results", collect: true},
		)
	}
	return append(out, example{text: "length", description: "the number of characters"})
}

func numberExamples(values []any, stream bool) []example {
	var out []example
	if stream {
		out = append(out,
			example{text: "add", description: "the total", collect: true},
			example{text: "add / length", description: "the average", collect: true},
			example{text: "max", description: "the largest value", collect: true},
			example{text: "length", description: "the number of results", collect: true},
		)
	}
	if f := toFloat(values[0]); f >= 1e9 && f < 1e10 && f == float64(int64(f)) {
		out = append(out, example{text: "todate", description: "as a date, read as seconds since 1970"})
	}
	return append(out, example{text: "tostring", description: "as a string"})
}

// stats summarizes the keys of a sample of objects.
type stats struct {
	objects int
	fields  []*fieldStats
}

type fieldStats struct {
	key     string
	count   int
	strings map[string]int
	// stringTotal counts the strings, including those past maxDistinct.
	stringTotal int
	// many reports that the key has more than maxDistinct strings.
	many    bool
	numbers []any
	bools   int
	nested  int // objects and arrays
}

func newStats(objects []any) *stats {
	s := &stats{objects: len(objects)}
	byKey := map[string]*fieldStats{}
	for _, o := range objects {
		for k, v := range o.(map[string]any) {
			f := byKey[k]
			if f == nil {
				f = &fieldStats{key: k, strings: map[string]int{}}
				byKey[k] = f
				s.fields = append(s.fields, f)
			}
			f.count++
			switch v := v.(type) {
			case string:
				f.stringTotal++
				if _, seen := f.strings[v]; seen || len(f.strings) < maxDistinct {
					f.strings[v]++
				} else {
					f.many = true
				}
			case int, float64, *big.Int, json.Number:
				f.numbers = append(f.numbers, v)
			case bool:
				f.bools++
			case map[string]any, []any:
				f.nested++
			}
		}
	}
	slices.SortFunc(s.fields, func(a, b *fieldStats) int {
		return cmp.Or(
			cmp.Compare(b.count, a.count),
			cmp.Compare(labelRank(a.key), labelRank(b.key)),
			cmp.Compare(strings.ToLower(a.key), strings.ToLower(b.key)),
			cmp.Compare(a.key, b.key))
	})
	return s
}

// labelNames are keys that usually name an item, best first.
var labelNames = []string{"name", "title", "label", "key", "id"}

func labelRank(key string) int {
	if i := slices.Index(labelNames, strings.ToLower(key)); i >= 0 {
		return i
	}
	return len(labelNames)
}

// common reports whether f is in at least half of the objects.
func (s *stats) common(f *fieldStats) bool {
	return 2*f.count >= s.objects
}

// mostly reports whether n of the values of f are of one type.
func (f *fieldStats) mostly(n int) bool {
	return 5*n >= 4*f.count
}

// label returns the key that best names each object: a common key with strings,
// or failing that, numbers. A category comes last, because "map(.KEY) | unique"
// covers it.
func (s *stats) label() (string, bool) {
	c, hasCategory := s.category()
	for _, f := range s.fields {
		if s.common(f) && f.mostly(f.stringTotal) && !(hasCategory && f.key == c.key) {
			return f.key, true
		}
	}
	for _, f := range s.fields {
		if s.common(f) && f.mostly(len(f.numbers)) {
			return f.key, true
		}
	}
	if hasCategory {
		return c.key, true
	}
	return "", false
}

// scalar reports whether the values of f are mostly strings, numbers or booleans.
func (f *fieldStats) scalar() bool {
	return f.mostly(f.stringTotal + len(f.numbers) + f.bools)
}

type categoryKey struct {
	key string
	top string // the most common value
}

// category returns a common key with a few distinct strings, and its most
// common value.
func (s *stats) category() (categoryKey, bool) {
	for _, f := range s.fields {
		n := len(f.strings)
		if !s.common(f) || f.many || n < 2 || n > maxCategories || 2*n > f.count || !f.mostly(f.stringTotal) {
			continue
		}
		var top string
		best := 0
		for v, c := range f.strings {
			if c > best || c == best && v < top {
				top, best = v, c
			}
		}
		if len(top) > maxValueLen || strings.ContainsAny(top, "\n\r\t") {
			continue
		}
		return categoryKey{f.key, top}, true
	}
	return categoryKey{}, false
}

type numberKey struct {
	key    string
	median string // as JSON
}

// number returns a common key with numbers, and the median of its values. Keys
// that look like identifiers do not count.
func (s *stats) number() (numberKey, bool) {
	for _, f := range s.fields {
		if s.common(f) && len(f.numbers) >= 2 && f.mostly(len(f.numbers)) && !isIDKey(f.key) {
			return numberKey{f.key, median(f.numbers)}, true
		}
	}
	return numberKey{}, false
}

// boolean returns a common key with booleans.
func (s *stats) boolean() (string, bool) {
	for _, f := range s.fields {
		if s.common(f) && f.mostly(f.bools) {
			return f.key, true
		}
	}
	return "", false
}

// sortKey returns the key to sort by: the number key, or else the label.
func (s *stats) sortKey() (string, bool) {
	if n, ok := s.number(); ok && !isIDKey(n.key) {
		return n.key, true
	}
	return s.label()
}

// nested returns up to n keys whose values are objects or arrays, or if there are
// none, the first n keys.
func (s *stats) nested(n int) []string {
	var keys []string
	for _, f := range s.fields {
		if f.nested > 0 && len(keys) < n {
			keys = append(keys, f.key)
		}
	}
	if len(keys) > 0 {
		return keys
	}
	for _, f := range s.fields[:min(n, len(s.fields))] {
		keys = append(keys, f.key)
	}
	return keys
}

// picked returns the two keys to keep in an object with only some of them.
// They are common keys with scalar values: the label, and then a category, a
// number, or the next key.
func (s *stats) picked() []string {
	var keys []string
	add := func(k string) {
		if len(keys) < 2 && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	if k, ok := s.label(); ok {
		add(k)
	}
	if c, ok := s.category(); ok {
		add(c.key)
	}
	if n, ok := s.number(); ok {
		add(n.key)
	}
	for _, f := range s.fields {
		if s.common(f) && f.scalar() {
			add(f.key)
		}
	}
	return keys
}

// pick returns an object construction that keeps two of the keys, such as
// {id, name}, if the objects have more than two.
func (s *stats) pick() (string, bool) {
	keys := s.picked()
	if len(keys) < 2 || len(s.fields) <= 2 {
		return "", false
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		if isIdent(k) {
			parts[i] = k
		} else {
			parts[i] = keyText(k) + ": " + field(k)
		}
	}
	return "{" + strings.Join(parts, ", ") + "}", true
}

func (s *stats) pickNames() string {
	return strings.Join(s.picked(), " and ")
}

// isIDKey reports whether key names an identifier, such as id or user_id, whose
// numbers are not worth comparing.
func isIDKey(key string) bool {
	k := strings.ToLower(key)
	return k == "id" || strings.HasSuffix(k, "_id") || strings.HasSuffix(key, "Id") || strings.HasSuffix(key, "ID")
}

// median returns the middle one of numbers, as JSON. It is one of the values, so
// that an example compares with a number from the data.
func median(numbers []any) string {
	sorted := slices.Clone(numbers)
	slices.SortStableFunc(sorted, func(a, b any) int { return cmp.Compare(toFloat(a), toFloat(b)) })
	m := sorted[(len(sorted)-1)/2]
	if n, ok := m.(json.Number); ok {
		return n.String()
	}
	b, err := gojq.Marshal(m)
	if err != nil {
		return "0"
	}
	return string(b)
}

func toFloat(v any) float64 {
	switch v := v.(type) {
	case int:
		return float64(v)
	case float64:
		return v
	case *big.Int:
		f, _ := new(big.Float).SetInt(v).Float64()
		return f
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}

// field returns the term that indexes key, such as .name or ."名前".
func field(key string) string {
	return "." + keyText(key)
}

// onlyNested returns the key whose values are objects or arrays, if exactly one
// key has them.
func (s *stats) onlyNested() (string, bool) {
	var key string
	for _, f := range s.fields {
		if f.nested > 0 {
			if key != "" {
				return "", false
			}
			key = f.key
		}
	}
	return key, key != ""
}
