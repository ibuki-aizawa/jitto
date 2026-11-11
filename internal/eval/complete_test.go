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
			if texts := candidateTexts(got.Candidates); !reflect.DeepEqual(texts, tt.want) {
				t.Errorf("got %q, want %q", texts, tt.want)
			}
		})
	}
}

func candidateTexts(cands []Candidate) []string {
	var texts []string
	for _, c := range cands {
		texts = append(texts, c.Text)
	}
	return texts
}

func TestCompleteCandidateKeepsKey(t *testing.T) {
	got := Complete(context.Background(), ".", []any{map[string]any{"名前": nil, "id": nil}})
	want := []Candidate{{Key: "id", Text: "id"}, {Key: "名前", Text: `"名前"`}}
	if !reflect.DeepEqual(got.Candidates, want) {
		t.Errorf("got %+v, want %+v", got.Candidates, want)
	}
}

func TestNarrow(t *testing.T) {
	docs := []any{map[string]any{"name": nil, "Nickname": nil, "id": nil, "名前": nil, "n-1": nil}}
	c := Complete(context.Background(), ".items | .", []any{map[string]any{"items": docs[0]}})
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"same text", ".items | .", []string{"id", `"n-1"`, "name", "Nickname", `"名前"`}},
		{"more of the key", ".items | .n", []string{`"n-1"`, "name", "Nickname"}},
		{"ignores case", ".items | .NI", []string{"Nickname"}},
		{"leaves out the exact key", ".items | .name", nil},
		{"no match", ".items | .x", nil},
		{"past the key", ".items | .name ", nil},
		{"other text", ".other | .", nil},
		{"shorter text", ".items | ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := candidateTexts(c.Narrow(tt.text)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNarrowWithoutCandidates(t *testing.T) {
	if got := (Completion{}).Narrow(""); got != nil {
		t.Errorf("got %+v", got)
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
	for _, c := range candidateTexts(got.Candidates) {
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

func valueDoc() map[string]any {
	return map[string]any{
		"orders": []any{
			map[string]any{"id": json.Number("1"), "status": "shipped", "paid": true, "note": nil},
			map[string]any{"id": json.Number("2"), "status": "pending", "paid": false},
			map[string]any{"id": json.Number("10"), "status": "Shipped", "paid": true, "items": []any{"x"}},
			map[string]any{"id": json.Number("1.5"), "status": "配送済み", "tags": map[string]any{"a": "b"}},
		},
	}
}

func TestCompleteValue(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"after ==", ".orders[].status == ", []string{`"pending"`, `"Shipped"`, `"shipped"`, `"配送済み"`}},
		{"after !=", ".orders[].status != ", []string{`"pending"`, `"Shipped"`, `"shipped"`, `"配送済み"`}},
		{"no space", ".orders[].status==", []string{`"pending"`, `"Shipped"`, `"shipped"`, `"配送済み"`}},
		{"opening quote", `.orders[].status == "`, []string{`"pending"`, `"Shipped"`, `"shipped"`, `"配送済み"`}},
		{"prefix ignores case", `.orders[].status == "S`, []string{`"Shipped"`, `"shipped"`}},
		{"non-ASCII prefix", `.orders[].status == "配`, []string{`"配送済み"`}},
		{"inside select", `.orders | map(select(.status == "p`, []string{`"pending"`}},
		{"after a pipe", `.orders[] | .status == "p`, []string{`"pending"`}},
		{"numbers", ".orders[].id == ", []string{"1", "1.5", "10", "2"}},
		{"number prefix", ".orders[].id == 1", []string{"1", "1.5", "10"}},
		{"booleans, and null where the key is missing", ".orders[].paid == ", []string{"false", "null", "true"}},
		{"boolean prefix", ".orders[].paid == t", []string{"true"}},
		{"null", ".orders[0].note == n", []string{"null"}},
		{"missing key is null", ".orders[].note == ", []string{"null"}},
		{"no arrays or objects", ".orders[].items == ", []string{"null"}},
		{"left side with +", `.orders[].id + 1 == 1`, []string{"11"}},
		{"after and", `.orders[] | .paid == true and .status == "`, []string{`"Shipped"`, `"shipped"`}},
		{"inside string interpolation", `"\(.orders[0].status == "s`, []string{`"shipped"`}},

		{"no comparison", ".orders[].status ", nil},
		{"assignment", `.orders[].status = "`, nil},
		{"update", `.orders[].status |= "`, nil},
		{"less than", ".orders[].id < ", nil},
		{"closed string", `.orders[].status == "pending"`, nil},
		{"no match", `.orders[].status == "z`, nil},
		{"interpolation in the value", `.orders[].status == "\(.x) `, nil},
		{"unparsable", `if .orders[].status == "`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Complete(context.Background(), tt.text, []any{valueDoc()})
			if texts := candidateTexts(got.Candidates); !reflect.DeepEqual(texts, tt.want) {
				t.Errorf("got %q, want %q", texts, tt.want)
			}
		})
	}
}

func TestCompleteValueKeepsKeyCompletion(t *testing.T) {
	// A key on the right side of == is still completed as a key.
	got := Complete(context.Background(), ".orders[0].id == .orders[0].st", []any{valueDoc()})
	if texts := candidateTexts(got.Candidates); !reflect.DeepEqual(texts, []string{"status"}) {
		t.Errorf("got %q", texts)
	}
}

func TestCompleteValuePartial(t *testing.T) {
	text := `.orders[].status == "sh`
	got := Complete(context.Background(), text, []any{valueDoc()})
	if got.Start != len(`.orders[].status == `) || got.Partial != `"sh` {
		t.Errorf("got Start %d, Partial %q", got.Start, got.Partial)
	}
	want := []Candidate{{Key: `"Shipped"`, Text: `"Shipped"`}, {Key: `"shipped"`, Text: `"shipped"`}}
	if !reflect.DeepEqual(got.Candidates, want) {
		t.Errorf("got %+v, want %+v", got.Candidates, want)
	}
}

func TestNarrowValue(t *testing.T) {
	c := Complete(context.Background(), `.orders[].status == "`, []any{valueDoc()})
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"same text", `.orders[].status == "`, []string{`"pending"`, `"Shipped"`, `"shipped"`, `"配送済み"`}},
		{"more of the value", `.orders[].status == "sh`, []string{`"Shipped"`, `"shipped"`}},
		{"the closing quote is still to come", `.orders[].status == "pending`, []string{`"pending"`}},
		{"closing quote", `.orders[].status == "pending"`, nil},
		{"space in the string", `.orders[].status == "s `, nil},
		{"other text", `.other == "`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := candidateTexts(c.Narrow(tt.text)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCompleteValueCandidatesParse checks that inserting any value gives a valid
// expression that is true for that value.
func TestCompleteValueCandidatesParse(t *testing.T) {
	values := []any{"a b", `q"uote`, "back\\slash", "<&>", "tab\t", "名前", json.Number("12345678901234567890"), json.Number("-0.5"), json.Number("1e3"), true, nil}
	docs := make([]any, len(values))
	for i, v := range values {
		docs[i] = map[string]any{"v": v}
	}
	got := Complete(context.Background(), ".v == ", docs)
	if len(got.Candidates) != len(values) {
		t.Fatalf("got %d candidates, want %d: %q", len(got.Candidates), len(values), candidateTexts(got.Candidates))
	}
	for _, c := range candidateTexts(got.Candidates) {
		query, err := gojq.Parse("[.[] | select(.v == " + c + ")] | length")
		if err != nil {
			t.Errorf("%q: %v", c, err)
			continue
		}
		code, err := gojq.Compile(query)
		if err != nil {
			t.Errorf("%q: %v", c, err)
			continue
		}
		if v, _ := code.Run(docs).Next(); v != 1 {
			t.Errorf("%q matches %v values, want 1", c, v)
		}
	}
}
