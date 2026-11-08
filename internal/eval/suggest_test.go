package eval

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// parseDocs decodes JSON documents as the input package does.
func parseDocs(t *testing.T, text string) []any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var docs []any
	for dec.More() {
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		docs = append(docs, v)
	}
	return docs
}

const ordersJSON = `{
  "store": {"name": "Northwind", "opened": "2019-04-01T09:00:00Z"},
  "orders": [
    {"id": 1001, "customer_id": 1, "status": "shipped", "total": 42.5, "paid": true,  "items": [{"title": "Go", "price": 34.5}]},
    {"id": 1002, "customer_id": 2, "status": "pending", "total": 18,   "paid": false, "items": []},
    {"id": 1003, "customer_id": 1, "status": "shipped", "total": 61.25, "paid": true, "items": []},
    {"id": 1004, "customer_id": 3, "status": "shipped", "total": 9.99, "paid": true,  "items": []}
  ],
  "tags": ["b", "a", "b"],
  "totals": [3, 1, 2]
}`

func examplesOf(got []Suggestion) []string {
	var out []string
	for _, s := range got {
		out = append(out, s.Example)
	}
	return out
}

func TestSuggest(t *testing.T) {
	docs := parseDocs(t, ordersJSON)
	tests := []struct {
		expr string
		want []string
	}{
		{".orders", []string{
			"map(.id)",
			`map(select(.status == "shipped"))`,
			"map(select(.total > 18))",
			"map(select(.paid))",
			"sort_by(.total)",
			"group_by(.status) | map({status: .[0].status, count: length})",
			"map(.status) | unique",
			"map({id, status})",
			"first",
			"length",
		}},
		{".orders[]", []string{
			".items",
			`select(.status == "shipped")`,
			"select(.total > 18)",
			"select(.paid)",
			"{id, status}",
			"keys",
			"to_entries",
			"del(.items)",
			"length",
		}},
		{".", []string{".orders", ".store", ".tags", "keys", "to_entries"}},
		{".tags", []string{
			"unique",
			"group_by(.) | map({value: .[0], count: length})",
			`join(", ")`,
			"sort",
			"length",
		}},
		{".totals", []string{"add", "add / length", "max", "min", "sort", "length"}},
		{".orders[].total", []string{"add", "add / length", "max", "length", "tostring"}},
		{".orders[].status", []string{"unique", "length", "length"}},
		{".store.opened", []string{"fromdateiso8601", "ascii_downcase", "length"}},
		{".orders[0].id", []string{"tostring"}},
		{".orders[] | .id, .status", []string{"type", "length"}},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got := examplesOf(Suggest(context.Background(), tt.expr, docs))
			if !slices.Equal(got, tt.want) {
				t.Errorf("Suggest(%q) =\n  %q\nwant\n  %q", tt.expr, got, tt.want)
			}
		})
	}
}

func TestSuggestText(t *testing.T) {
	docs := parseDocs(t, ordersJSON)
	tests := []struct {
		expr string
		want string // the Text of the "length" example
	}{
		{"", "length"},
		{".", "length"},
		{" .tags ", ".tags | length"},
		{".tags |", ".tags | length"},
		{".tags|", ".tags| length"},
		{"|", "length"},
		{".orders[].status", "[.orders[].status] | length"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			docs := docs
			if base, _ := splitPipe(tt.expr); base == "" || base == "." {
				// The length of the document itself.
				docs = parseDocs(t, `["a", "b"]`)
			}
			assertText(t, Suggest(context.Background(), tt.expr, docs), tt.want)
		})
	}
}

func assertText(t *testing.T, got []Suggestion, want string) {
	t.Helper()
	for _, s := range got {
		if s.Example == "length" && s.Text == want {
			return
		}
	}
	t.Errorf("no length example with text %q in %+v", want, got)
}

func TestSuggestQuotedKeys(t *testing.T) {
	docs := parseDocs(t, `[
		{"名前": "太郎", "区分": "A", "a-b": 1},
		{"名前": "花子", "区分": "B", "a-b": 2},
		{"名前": "次郎", "区分": "A", "a-b": 3},
		{"名前": "三郎", "区分": "A", "a-b": 4},
		{"名前": "桜", "区分": "B", "a-b": 5}
	]`)
	got := examplesOf(Suggest(context.Background(), ".", docs))
	for _, want := range []string{
		`map(."名前")`,
		`map(select(."区分" == "A"))`,
		`map(select(."a-b" > 3))`,
		`group_by(."区分") | map({"区分": .[0]."区分", count: length})`,
		`map({"名前": ."名前", "区分": ."区分"})`,
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestSuggestChecksExamples(t *testing.T) {
	// Sorted by name already, so sort_by(.name) changes nothing.
	docs := parseDocs(t, `[{"name": "a", "x": {}}, {"name": "b", "x": {}}]`)
	got := Suggest(context.Background(), ".", docs)
	if ex := examplesOf(got); slices.Contains(ex, "sort_by(.name)") {
		t.Errorf("got %q, want no sort_by(.name)", ex)
	}
	for _, s := range got {
		res, err := Eval(context.Background(), s.Text, docs, Options{})
		if err != nil || res.Output == "" {
			t.Errorf("%q: %v, output %q", s.Text, err, res.Output)
		}
		if s.Description == "" {
			t.Errorf("%q has no description", s.Text)
		}
	}
}

func TestSuggestNothing(t *testing.T) {
	docs := parseDocs(t, ordersJSON)
	for _, expr := range []string{
		".orders[",        // does not parse
		".store.name.x",   // fails
		"empty",           // yields nothing
		".missing",        // null
		".orders[0].paid", // a boolean
	} {
		if got := Suggest(context.Background(), expr, docs); got != nil {
			t.Errorf("Suggest(%q) = %+v, want nil", expr, got)
		}
	}
}

func TestSuggestTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := Suggest(ctx, "until(false; .)", []any{map[string]any{"a": nil}})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v", elapsed)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

func TestSuggestDoesNotModifyDocs(t *testing.T) {
	docs := parseDocs(t, ordersJSON)
	Suggest(context.Background(), ".orders", docs)
	if !reflect.DeepEqual(docs, parseDocs(t, ordersJSON)) {
		t.Errorf("docs changed: %v", docs)
	}
}
