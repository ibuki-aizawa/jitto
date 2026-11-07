package input

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func TestRead(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []any
	}{
		{"object", `{"a":"b"}`, []any{map[string]any{"a": "b"}}},
		{"scalar", `"x"`, []any{"x"}},
		{"null", `null`, []any{nil}},
		{"ndjson", "{\"a\":\"b\"}\n{\"c\":\"d\"}\n", []any{map[string]any{"a": "b"}, map[string]any{"c": "d"}}},
		{"crlf separated", "true\r\nfalse\r\n", []any{true, false}},
		{"space separated", `true false`, []any{true, false}},
		{"empty", "", []any{}},
		{"whitespace only", " \r\n\t", []any{}},
		{"short input", "1", []any{json.Number("1")}},
		{"utf-8 bom", "\xEF\xBB\xBF" + `"x"`, []any{"x"}},
		{"utf-8 bom only", "\xEF\xBB\xBF", []any{}},
		// Windows PowerShell 5.1 with $OutputEncoding = [Text.Encoding]::UTF8.
		{"two utf-8 boms", "\xEF\xBB\xBF\xEF\xBB\xBF" + `{"名前":"太郎"}`, []any{map[string]any{"名前": "太郎"}}},
		{"three utf-8 boms", "\xEF\xBB\xBF\xEF\xBB\xBF\xEF\xBB\xBF1", []any{json.Number("1")}},
		{"two utf-8 boms only", "\xEF\xBB\xBF\xEF\xBB\xBF", []any{}},
		{"japanese", `{"名前":"太郎"}`, []any{map[string]any{"名前": "太郎"}}},
		{"large integer keeps precision", `12345678901234567890`, []any{json.Number("12345678901234567890")}},
		{"number keeps its form", `1.0`, []any{json.Number("1.0")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Read(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestReadInvalidJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"syntax error", `{"a":}`, "invalid JSON in document 1 at byte 6"},
		{"truncated", `{"a":`, "invalid JSON in document 1"},
		{"second document", `true {`, "invalid JSON in document 2"},
		{"trailing garbage", `true xyz`, "invalid JSON in document 2 at byte"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs, err := Read(strings.NewReader(tt.input))
			if err == nil {
				t.Fatalf("got %#v, want error", docs)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestReadUTF16(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"little endian", "\xFF\xFE1\x00"},
		{"big endian", "\xFE\xFF\x001"},
		{"bom only", "\xFF\xFE"},
		{"after a utf-8 bom", "\xEF\xBB\xBF\xFF\xFE1\x00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Read(strings.NewReader(tt.input)); !errors.Is(err, ErrUTF16) {
				t.Errorf("got error %v, want ErrUTF16", err)
			}
		})
	}
}

func TestReadPropagatesReaderError(t *testing.T) {
	wantErr := errors.New("boom")

	t.Run("before any data", func(t *testing.T) {
		if _, err := Read(iotest.ErrReader(wantErr)); !errors.Is(err, wantErr) {
			t.Errorf("got error %v, want %v", err, wantErr)
		}
	})
	t.Run("after a document", func(t *testing.T) {
		r := io.MultiReader(strings.NewReader(`"long enough to pass the BOM check" `), iotest.ErrReader(wantErr))
		_, err := Read(r)
		if !errors.Is(err, wantErr) {
			t.Errorf("got error %v, want %v", err, wantErr)
		}
		if err != nil && strings.Contains(err.Error(), "invalid JSON") {
			t.Errorf("reader failure reported as invalid JSON: %v", err)
		}
	})
}

func TestReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(path, []byte(`{"a":"b"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := []any{map[string]any{"a": "b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestReadFileErrors(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file", func(t *testing.T) {
		if _, err := ReadFile(filepath.Join(dir, "missing.json")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("got error %v, want os.ErrNotExist", err)
		}
	})
	t.Run("error names the file", func(t *testing.T) {
		path := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(path, []byte(`{`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := ReadFile(path)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("error %v does not name %s", err, path)
		}
	})
}
