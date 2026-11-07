package eval

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/itchyny/gojq"
)

func completeDoc() map[string]any {
	return map[string]any{
		"name":     "太郎",
		"Nickname": "taro",
		"名前":       "太郎",
		"a-b":      json.Number("1"),
		"if":       json.Number("1"),
		"items": []any{
			map[string]any{"id": json.Number("1"), "title": "a"},
			map[string]any{"id": json.Number("2"), "tag": "x"},
		},
	}
}

func TestComplete(t *testing.T) {
	tests := []struct {
		name string
		text string
		docs []any
		want []string
	}{
		{"top level, sorted ignoring case", ".", []any{completeDoc()}, []string{`"a-b"`, "if", "items", "name", "Nickname", `"名前"`}},
		{"prefix ignores case", ".n", []any{completeDoc()}, []string{"name", "Nickname"}},
		{"upper case prefix", ".N", []any{completeDoc()}, []string{"name", "Nickname"}},
		{"quoted key matches its ASCII prefix", ".a", []any{completeDoc()}, []string{`"a-b"`}},
		{"exact key", ".name", []any{completeDoc()}, []string{"name"}},
		{"after an index", ".items[0].", []any{completeDoc()}, []string{"id", "title"}},
		{"keys of all elements", ".items[].", []any{completeDoc()}, []string{"id", "tag", "title"}},
		{"after a pipe", ".items[] | .t", []any{completeDoc()}, []string{"tag", "title"}},
		{"inside map", ".items | map(.t", []any{completeDoc()}, []string{"tag", "title"}},
		{"nested calls", ".items | map(select(.i", []any{completeDoc()}, []string{"id"}},
		{"inside an object", "{x: .items[0].t", []any{completeDoc()}, []string{"title"}},
		{"inside an array", "[.items[0].t", []any{completeDoc()}, []string{"title"}},
		{"inside string interpolation", `"\(.items[0].t`, []any{completeDoc()}, []string{"title"}},
		{"closed brackets before", `(.items) | .[0].t`, []any{completeDoc()}, []string{"title"}},
		{"operator precedence", ".name + .items[0].t", []any{completeDoc()}, []string{"title"}},
		{"variable", ".items[0] as $x | $x.t", []any{completeDoc()}, []string{"title"}},
		{"parenthesized term", "(.items[0]).t", []any{completeDoc()}, []string{"title"}},
		{"each document", ".", []any{map[string]any{"b": nil}, map[string]any{"a": nil}}, []string{"a", "b"}},
		{"error in one document", ".k.", []any{"s", map[string]any{"k": map[string]any{"a": nil}}}, []string{"a"}},
		{"stops after many inputs", "repeat({a: 1}) | .", []any{nil}, []string{"a"}},

		{"empty text", "", []any{completeDoc()}, nil},
		{"no dot", ".items | length", []any{completeDoc()}, nil},
		{"trailing space", ". ", []any{completeDoc()}, nil},
		{"string value", ".name.", []any{completeDoc()}, nil},
		{"array value", ".items.", []any{completeDoc()}, nil},
		{"no match", ".zzz", []any{completeDoc()}, nil},
		{"number", ".1", []any{completeDoc()}, nil},
		{"recursive descent", "..", []any{completeDoc()}, nil},
		{"inside a string", `"a .n`, []any{completeDoc()}, nil},
		{"unmatched bracket", ".)", []any{completeDoc()}, nil},
		{"unparsable", "if . then .n", []any{completeDoc()}, nil},
		{"no documents", ".", []any{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Complete(context.Background(), tt.text, tt.docs)
			if !reflect.DeepEqual(got.Candidates, tt.want) {
				t.Errorf("got %q, want %q", got.Candidates, tt.want)
			}
		})
	}
}

func TestCompletePartial(t *testing.T) {
	text := ".items[0].ti"
	got := Complete(context.Background(), text, []any{completeDoc()})
	if got.Start != len(".items[0].") || got.Partial != "ti" {
		t.Errorf("got Start %d, Partial %q", got.Start, got.Partial)
	}
	if text[:got.Start]+got.Partial != text {
		t.Errorf("Partial %q does not end %q at %d", got.Partial, text, got.Start)
	}
}

// TestCompleteCandidatesParse checks that inserting any candidate gives a valid
// expression, including keys that are keywords or need quotes.
func TestCompleteCandidatesParse(t *testing.T) {
	// Each key maps to itself, so the test can tell which key a candidate reads.
	doc := map[string]any{}
	for _, k := range []string{
		"if", "and", "end", "__loc__", "_x", "x1",
		"1x", "a b", `q"uote`, "back\\slash", "<&>", "tab\t", "名前",
	} {
		doc[k] = k
	}
	got := Complete(context.Background(), ".", []any{doc})
	if len(got.Candidates) != len(doc) {
		t.Fatalf("got %d candidates, want %d: %q", len(got.Candidates), len(doc), got.Candidates)
	}
	seen := map[string]bool{}
	for _, c := range got.Candidates {
		query, err := gojq.Parse("." + c)
		if err != nil {
			t.Errorf("%q: %v", c, err)
			continue
		}
		code, err := gojq.Compile(query)
		if err != nil {
			t.Errorf("%q: %v", c, err)
			continue
		}
		v, _ := code.Run(doc).Next()
		k, _ := v.(string)
		if _, ok := doc[k]; !ok {
			t.Errorf("%q: got %v, want a key of the document", c, v)
		}
		seen[k] = true
	}
	if len(seen) != len(doc) {
		t.Errorf("candidates read %d distinct keys, want %d", len(seen), len(doc))
	}
}

func TestCompleteTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := Complete(ctx, "until(false; .) | .", []any{map[string]any{"a": nil}})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v", elapsed)
	}
	if got.Candidates != nil {
		t.Errorf("got %q, want none", got.Candidates)
	}
}

func TestCompleteDoesNotModifyDocs(t *testing.T) {
	docs := []any{completeDoc()}
	Complete(context.Background(), ".items[].", docs)
	if !reflect.DeepEqual(docs, []any{completeDoc()}) {
		t.Errorf("docs changed: %v", docs)
	}
}
