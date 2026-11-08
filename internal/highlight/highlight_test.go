package highlight

import (
	"reflect"
	"strings"
	"testing"
)

func TestLine(t *testing.T) {
	tests := []struct {
		line string
		want []Span
	}{
		{"", nil},
		{"{", []Span{{Bracket, "{"}}},
		{"  ],", []Span{{Plain, "  "}, {Bracket, "]"}, {Plain, ","}}},
		{"[]", []Span{{Bracket, "["}, {Bracket, "]"}}},
		{`  "name": "Alice",`, []Span{
			{Plain, "  "}, {Key, `"name"`}, {Plain, ": "}, {String, `"Alice"`}, {Plain, ","},
		}},
		{`  "items": [`, []Span{
			{Plain, "  "}, {Key, `"items"`}, {Plain, ": "}, {Bracket, "["},
		}},
		{`  "a": {}`, []Span{
			{Plain, "  "}, {Key, `"a"`}, {Plain, ": "}, {Bracket, "{"}, {Bracket, "}"},
		}},
		{`    -1.5e+10,`, []Span{{Plain, "    "}, {Number, "-1.5e+10"}, {Plain, ","}}},
		{`  "ok": true,`, []Span{
			{Plain, "  "}, {Key, `"ok"`}, {Plain, ": "}, {Bool, "true"}, {Plain, ","},
		}},
		{"false", []Span{{Bool, "false"}}},
		{"  null", []Span{{Plain, "  "}, {Null, "null"}}},
		// A string value that looks like a key is still a value.
		{`  "a:b"`, []Span{{Plain, "  "}, {String, `"a:b"`}}},
		// Escaped quotes and colons inside strings.
		{`"say \"hi\": now": "x\\"`, []Span{
			{Key, `"say \"hi\": now"`}, {Plain, ": "}, {String, `"x\\"`},
		}},
		{`"名前": "太郎"`, []Span{{Key, `"名前"`}, {Plain, ": "}, {String, `"太郎"`}}},
		// A line cut inside a string, as a truncated result can be.
		{`  "title": "The Go Prog`, []Span{
			{Plain, "  "}, {Key, `"title"`}, {Plain, ": "}, {String, `"The Go Prog`},
		}},
		{`  "unclosed key`, []Span{{Plain, "  "}, {String, `"unclosed key`}}},
		// Unknown words are plain.
		{"nan undefined", []Span{{Plain, "nan undefined"}}},
	}
	for _, tt := range tests {
		got := Line(tt.line)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Line(%q) =\n  %v\nwant\n  %v", tt.line, got, tt.want)
		}
	}
}

func TestColor(t *testing.T) {
	in := "{\n  \"a\": \"x\",\n  \"n\": 1,\n  \"b\": true,\n  \"z\": null\n}\n"
	want := "\x1b[1m{\x1b[0m\n" +
		"  \x1b[34;1m\"a\"\x1b[0m: \x1b[32m\"x\"\x1b[0m,\n" +
		"  \x1b[34;1m\"n\"\x1b[0m: 1,\n" +
		"  \x1b[34;1m\"b\"\x1b[0m: true,\n" +
		"  \x1b[34;1m\"z\"\x1b[0m: \x1b[90mnull\x1b[0m\n" +
		"\x1b[1m}\x1b[0m\n"
	if got := Color(in); got != want {
		t.Errorf("Color(%q) =\n  %q\nwant\n  %q", in, got, want)
	}
	if got := Color(""); got != "" {
		t.Errorf("Color(\"\") = %q", got)
	}
}

func TestLineKeepsText(t *testing.T) {
	for _, line := range []string{
		`  "a": [1, "two", null, {"b": false}],`,
		`"\u00e9\"\\": -0.5`,
		"\t\"tab\"\t:\tnull",
		`"`,
		`\`,
	} {
		var b strings.Builder
		for _, s := range Line(line) {
			b.WriteString(s.Text)
		}
		if b.String() != line {
			t.Errorf("spans of %q join to %q", line, b.String())
		}
	}
}
