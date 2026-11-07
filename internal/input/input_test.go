package input

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"unicode/utf16"
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

// bom is U+FEFF, which Go source cannot contain as is.
var bom = string(rune(0xFEFF))

// utf16Bytes encodes s as UTF-16 with a byte order mark.
func utf16Bytes(s string, order binary.AppendByteOrder) string {
	b := order.AppendUint16(nil, 0xFEFF)
	for _, u := range utf16.Encode([]rune(s)) {
		b = order.AppendUint16(b, u)
	}
	return string(b)
}

func TestReadUTF16(t *testing.T) {
	japanese := map[string]any{"名前": "太郎", "emoji": "😀"}
	tests := []struct {
		name  string
		input string
		want  []any
	}{
		{"little endian", utf16Bytes(`{"名前":"太郎","emoji":"😀"}`, binary.LittleEndian), []any{japanese}},
		{"big endian", utf16Bytes(`{"名前":"太郎","emoji":"😀"}`, binary.BigEndian), []any{japanese}},
		// What Windows PowerShell 5.1 writes with ">".
		{"crlf separated", utf16Bytes("1\r\n2\r\n", binary.LittleEndian), []any{json.Number("1"), json.Number("2")}},
		{"bom only", "\xFF\xFE", []any{}},
		{"empty after the bom", utf16Bytes("", binary.BigEndian), []any{}},
		{"more boms in the text", utf16Bytes(bom+bom+"1", binary.LittleEndian), []any{json.Number("1")}},
		{"after a utf-8 bom", "\xEF\xBB\xBF" + utf16Bytes("1", binary.LittleEndian), []any{json.Number("1")}},
		{"unpaired surrogate", "\xFF\xFE" + "\x22\x00" + "\x00\xD8" + "\x22\x00", []any{"�"}},
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

func TestReadUTF16Errors(t *testing.T) {
	t.Run("odd number of bytes", func(t *testing.T) {
		_, err := Read(strings.NewReader(utf16Bytes("1", binary.LittleEndian) + "\x00"))
		if !errors.Is(err, ErrOddUTF16) {
			t.Errorf("got error %v, want ErrOddUTF16", err)
		}
	})
	t.Run("invalid JSON", func(t *testing.T) {
		// The offset is the same as for the UTF-8 input.
		_, utf8Err := Read(strings.NewReader(`{"名":}`))
		_, err := Read(strings.NewReader(utf16Bytes(`{"名":}`, binary.LittleEndian)))
		if err == nil || utf8Err == nil || err.Error() != utf8Err.Error() {
			t.Errorf("got error %v, want %v", err, utf8Err)
		}
	})
	t.Run("read error", func(t *testing.T) {
		wantErr := errors.New("disk failure")
		_, err := Read(io.MultiReader(strings.NewReader("\xFF\xFE1\x00"), iotest.ErrReader(wantErr)))
		if !errors.Is(err, wantErr) {
			t.Errorf("got error %v, want %v", err, wantErr)
		}
	})
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
