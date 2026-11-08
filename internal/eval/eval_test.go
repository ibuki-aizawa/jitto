package eval

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/itchyny/gojq"
)

func obj() map[string]any {
	return map[string]any{
		"name":  "太郎",
		"big":   json.Number("12345678901234567890"),
		"float": json.Number("1.0"),
		"items": []any{json.Number("1"), json.Number("2")},
	}
}

func TestEval(t *testing.T) {
	tests := []struct {
		name string
		expr string
		docs []any
		want string
	}{
		{"field", ".name", []any{obj()}, "\"太郎\"\n"},
		{"empty expression means identity", "", []any{"x"}, "\"x\"\n"},
		{"blank expression means identity", " \t", []any{"x"}, "\"x\"\n"},
		{"indents like jq", ".", []any{map[string]any{"a": []any{json.Number("1")}}}, "{\n  \"a\": [\n    1\n  ]\n}\n"},
		{"empty containers stay compact", "[], {}", []any{nil}, "[]\n{}\n"},
		{"keys are sorted", "{b:1,a:2}", []any{nil}, "{\n  \"a\": 2,\n  \"b\": 1\n}\n"},
		{"multiple outputs", ".items[]", []any{obj()}, "1\n2\n"},
		{"each document", ".", []any{"a", "b"}, "\"a\"\n\"b\"\n"},
		{"no documents", ".", []any{}, ""},
		{"no output", "empty", []any{"x"}, ""},
		{"large integer keeps precision", ".big", []any{obj()}, "12345678901234567890\n"},
		{"large integer arithmetic", ".big + 1", []any{obj()}, "12345678901234567891\n"},
		{"number keeps its form", ".float", []any{obj()}, "1.0\n"},
		{"html is not escaped", `"<&>"`, []any{nil}, "\"<&>\"\n"},
		{"nan is null", "nan", []any{nil}, "null\n"},
		{"env is available", "env | type", []any{nil}, "\"object\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(context.Background(), tt.expr, tt.docs, Options{})
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got.Output != tt.want {
				t.Errorf("got %q, want %q", got.Output, tt.want)
			}
			if got.Truncated {
				t.Error("unexpectedly truncated")
			}
		})
	}
}

func TestEvalRaw(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
		raw  []Lines
	}{
		{"string without quotes", `"a"`, "a\n", []Lines{{0, 1}}},
		{"escapes are decoded", `"tab\there\nnext"`, "tab\there\nnext\n", []Lines{{0, 2}}},
		{"empty string is an empty line", `""`, "\n", []Lines{{0, 1}}},
		{"trailing newline adds a line", `"a\n"`, "a\n\n", []Lines{{0, 2}}},
		{"quotes inside stay", `"say \"hi\""`, "say \"hi\"\n", []Lines{{0, 1}}},
		{"other values stay JSON", `1, null, [1], {a: "x"}`, "1\nnull\n[\n  1\n]\n{\n  \"a\": \"x\"\n}\n", nil},
		{"strings inside values stay JSON", `["a"]`, "[\n  \"a\"\n]\n", nil},
		{"mixed", `{a: 1}, "x\ny", 2, "z"`, "{\n  \"a\": 1\n}\nx\ny\n2\nz\n", []Lines{{3, 5}, {6, 7}}},
		{"no output", "empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(context.Background(), tt.expr, []any{nil}, Options{Raw: true})
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got.Output != tt.want {
				t.Errorf("output %q, want %q", got.Output, tt.want)
			}
			if !reflect.DeepEqual(got.Raw, tt.raw) {
				t.Errorf("raw lines %v, want %v", got.Raw, tt.raw)
			}
		})
	}

	t.Run("off by default", func(t *testing.T) {
		got, err := Eval(context.Background(), `"a"`, []any{nil}, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Output != "\"a\"\n" || got.Raw != nil {
			t.Errorf("got %+v", got)
		}
	})
}

func TestEvalRawTruncated(t *testing.T) {
	tests := []struct {
		name     string
		maxBytes int
		want     string
		raw      []Lines
	}{
		// "ab\ncd\n" + "1\n" + "efgh\n" is 13 bytes.
		{"cut after a raw string", 7, "ab\ncd\n", []Lines{{0, 2}}},
		{"cut inside a raw string", 4, "ab\n", []Lines{{0, 1}}},
		{"cut inside a raw line", 2, "ab", []Lines{{0, 1}}},
		// The cut prefers the end of a line, so the last string is dropped.
		{"cut inside the last string", 12, "ab\ncd\n1\n", []Lines{{0, 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(context.Background(), `"ab\ncd", 1, "efgh"`, []any{nil}, Options{Raw: true, MaxBytes: tt.maxBytes})
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if !got.Truncated {
				t.Fatal("not truncated")
			}
			if got.Output != tt.want {
				t.Errorf("output %q, want %q", got.Output, tt.want)
			}
			if !reflect.DeepEqual(got.Raw, tt.raw) {
				t.Errorf("raw lines %v, want %v", got.Raw, tt.raw)
			}
		})
	}
}

func TestResultLines(t *testing.T) {
	type line struct {
		text string
		raw  bool
	}
	tests := []struct {
		name string
		res  Result
		want []line
	}{
		{"empty", Result{}, nil},
		{"one empty line", Result{Output: "\n", Raw: []Lines{{0, 1}}}, []line{{"", true}}},
		{"ranges", Result{Output: "0\n1\n2\n3\n4\n5\n6\n", Raw: []Lines{{1, 3}, {5, 6}}}, []line{
			{"0", false}, {"1", true}, {"2", true}, {"3", false}, {"4", false}, {"5", true}, {"6", false},
		}},
		{"cut last line", Result{Output: "a\nb", Raw: []Lines{{1, 2}}}, []line{{"a", false}, {"b", true}}},
		{"empty lines", Result{Output: "a\n\n\n", Raw: []Lines{{0, 3}}}, []line{{"a", true}, {"", true}, {"", true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []line
			for text, raw := range tt.res.Lines() {
				got = append(got, line{text, raw})
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvalErrors(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{"parse error", ".[", "unexpected EOF"},
		{"unknown function", "foo", "function not defined: foo/0"},
		{"unknown variable", "$x", "variable not defined: $x"},
		{"input is not available", "input", "not allowed"},
		{"runtime error", `.name | keys`, "keys cannot be applied to"},
		{"error()", `error("boom")`, "boom"},
		{"halt", "halt", "halt"},
		{"partial output is discarded", `1, error("boom")`, "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(context.Background(), tt.expr, []any{obj()}, Options{})
			if err == nil {
				t.Fatalf("got %+v, want error", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
			if !reflect.DeepEqual(got, Result{}) {
				t.Errorf("got non-empty result %+v alongside error", got)
			}
		})
	}
}

func TestEvalSuggestsQuotingNonASCIIKeys(t *testing.T) {
	const noHint = ""
	tests := []struct {
		name string
		expr string
		want string
	}{
		{"katakana", ".テスト", `."テスト"`},
		{"kanji", ".名前", `."名前"`},
		{"long vowel mark", ".テストキー", `."テストキー"`},
		{"after a field", ".a.テスト", `."テスト"`},
		{"after a pipe", ".a | .テスト", `."テスト"`},
		{"followed by an index", ".テスト[0]", `."テスト"`},
		{"followed by a pipe", ".テスト|.b", `."テスト"`},
		{"followed by a full-width space", ".テスト　", `."テスト"`},
		{"ascii then japanese", ".aテスト", `."aテスト"`},
		{"japanese then ascii", ".テストa_1", `."テストa_1"`},
		{"no dot", "テスト", noHint},
		{"space after the dot", ". テスト", noHint},
		{"space before the key", ".a テスト", noHint},
		{"recursive descent", "..テスト", noHint},
		{"full-width space", ".　", noHint},
		{"emoji", ".😀", noHint},
		{"ascii syntax error", ".[", noHint},
		{"unterminated string", `."テスト`, noHint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Eval(context.Background(), tt.expr, []any{nil}, Options{})
			if err == nil {
				t.Fatal("got no error")
			}
			var parseErr *gojq.ParseError
			if !errors.As(err, &parseErr) {
				t.Errorf("error %v does not wrap a parse error", err)
			}
			if tt.want == noHint {
				if strings.Contains(err.Error(), "(try") {
					t.Errorf("unexpected hint: %v", err)
				}
				return
			}
			if !strings.HasSuffix(err.Error(), "(try "+tt.want+")") {
				t.Errorf("error %q does not suggest %s", err, tt.want)
			}
			// The suggestion has to work.
			if _, err := Eval(context.Background(), tt.want, []any{map[string]any{}}, Options{}); err != nil {
				t.Errorf("suggestion %s fails: %v", tt.want, err)
			}
		})
	}
}

func TestEvalErrorInLaterDocumentDiscardsOutput(t *testing.T) {
	got, err := Eval(context.Background(), ".a", []any{map[string]any{"a": "x"}, "not an object"}, Options{})
	if err == nil {
		t.Fatalf("got %+v, want error", got)
	}
	if !reflect.DeepEqual(got, Result{}) {
		t.Errorf("got non-empty result %+v alongside error", got)
	}
}

func TestEvalTruncation(t *testing.T) {
	tests := []struct {
		name      string
		expr      string
		maxBytes  int
		want      string
		truncated bool
	}{
		{"cuts at a line", "1, 2, 3", 3, "1\n", true},
		{"exactly at the limit", "1, 2", 4, "1\n2\n", false},
		{"under the limit", "1", 100, "1\n", false},
		{"no newline: keeps characters whole", `"日本語"`, 5, `"日`, true},
		{"stops an infinite generator", "range(infinite)", 4, "0\n1\n", true},
		{"stops at a line within one value", "[1, 2]", 6, "[\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Eval(context.Background(), tt.expr, []any{nil}, Options{MaxBytes: tt.maxBytes})
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}
			if got.Output != tt.want || got.Truncated != tt.truncated {
				t.Errorf("got %+v, want output %q truncated %v", got, tt.want, tt.truncated)
			}
			if !utf8.ValidString(got.Output) {
				t.Errorf("output %q is not valid UTF-8", got.Output)
			}
			if len(got.Output) > tt.maxBytes {
				t.Errorf("output is %d bytes, over the limit of %d", len(got.Output), tt.maxBytes)
			}
		})
	}
}

func TestEvalCancellation(t *testing.T) {
	runaway := map[string]string{
		"collects forever": "[range(infinite)]",
		"loops forever":    "def f: f; f",
	}
	for name, expr := range runaway {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			done := make(chan error, 1)
			go func() {
				_, err := Eval(ctx, expr, []any{nil}, Options{})
				done <- err
			}()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("got error %v, want context.DeadlineExceeded", err)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("Eval did not stop after its context ended")
			}
		})
	}

	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := Eval(ctx, ".", []any{nil}, Options{}); !errors.Is(err, context.Canceled) {
			t.Errorf("got error %v, want context.Canceled", err)
		}
	})
}

func TestEvalDepthLimit(t *testing.T) {
	deep := func(n int) any {
		var v any
		for range n {
			v = []any{v}
		}
		return v
	}

	t.Run("at the limit", func(t *testing.T) {
		if _, err := Eval(context.Background(), ".", []any{deep(maxDepth)}, Options{}); err != nil {
			t.Errorf("Eval: %v", err)
		}
	})
	for name, v := range map[string]any{
		"over the limit": deep(maxDepth + 1),
		"far beyond":     deep(1_000_000),
		"inside objects": map[string]any{"a": deep(maxDepth)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Eval(context.Background(), ".", []any{v}, Options{}); !errors.Is(err, ErrTooDeep) {
				t.Errorf("got error %v, want ErrTooDeep", err)
			}
		})
	}
	t.Run("built by the expression", func(t *testing.T) {
		_, err := Eval(context.Background(), "reduce range(1000000) as $i (null; [.])", []any{nil}, Options{})
		if !errors.Is(err, ErrTooDeep) {
			t.Errorf("got error %v, want ErrTooDeep", err)
		}
	})
}

func TestEvalDoesNotModifyDocuments(t *testing.T) {
	docs := []any{obj()}
	for _, expr := range []string{`.name = "x"`, ".items[0] = 9", `del(.big)`, ".items |= map(. + 1)", "to_entries"} {
		if _, err := Eval(context.Background(), expr, docs, Options{}); err != nil {
			t.Fatalf("Eval(%q): %v", expr, err)
		}
	}
	if want := []any{obj()}; !reflect.DeepEqual(docs, want) {
		t.Errorf("documents changed: got %#v, want %#v", docs, want)
	}
}

func TestEvalConcurrentlyOnSameDocuments(t *testing.T) {
	docs := []any{obj()}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				got, err := Eval(context.Background(), ".big", docs, Options{})
				if err != nil || got.Output != "12345678901234567890\n" {
					t.Errorf("got %+v, %v", got, err)
					return
				}
			}
		})
	}
	wg.Wait()
}
