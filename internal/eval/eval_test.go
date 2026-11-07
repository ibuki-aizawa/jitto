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
			got, err := Eval(context.Background(), tt.expr, tt.docs, 0)
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
			got, err := Eval(context.Background(), tt.expr, []any{obj()}, 0)
			if err == nil {
				t.Fatalf("got %+v, want error", got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
			if got != (Result{}) {
				t.Errorf("got non-empty result %+v alongside error", got)
			}
		})
	}
}

func TestEvalErrorInLaterDocumentDiscardsOutput(t *testing.T) {
	got, err := Eval(context.Background(), ".a", []any{map[string]any{"a": "x"}, "not an object"}, 0)
	if err == nil {
		t.Fatalf("got %+v, want error", got)
	}
	if got != (Result{}) {
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
			got, err := Eval(context.Background(), tt.expr, []any{nil}, tt.maxBytes)
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
				_, err := Eval(ctx, expr, []any{nil}, 0)
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
		if _, err := Eval(ctx, ".", []any{nil}, 0); !errors.Is(err, context.Canceled) {
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
		if _, err := Eval(context.Background(), ".", []any{deep(maxDepth)}, 0); err != nil {
			t.Errorf("Eval: %v", err)
		}
	})
	for name, v := range map[string]any{
		"over the limit": deep(maxDepth + 1),
		"far beyond":     deep(1_000_000),
		"inside objects": map[string]any{"a": deep(maxDepth)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Eval(context.Background(), ".", []any{v}, 0); !errors.Is(err, ErrTooDeep) {
				t.Errorf("got error %v, want ErrTooDeep", err)
			}
		})
	}
	t.Run("built by the expression", func(t *testing.T) {
		_, err := Eval(context.Background(), "reduce range(1000000) as $i (null; [.])", []any{nil}, 0)
		if !errors.Is(err, ErrTooDeep) {
			t.Errorf("got error %v, want ErrTooDeep", err)
		}
	})
}

func TestEvalDoesNotModifyDocuments(t *testing.T) {
	docs := []any{obj()}
	for _, expr := range []string{`.name = "x"`, ".items[0] = 9", `del(.big)`, ".items |= map(. + 1)", "to_entries"} {
		if _, err := Eval(context.Background(), expr, docs, 0); err != nil {
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
				got, err := Eval(context.Background(), ".big", docs, 0)
				if err != nil || got.Output != "12345678901234567890\n" {
					t.Errorf("got %+v, %v", got, err)
					return
				}
			}
		})
	}
	wg.Wait()
}
