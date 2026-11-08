// Package highlight splits lines of indented JSON into spans by the kind of
// value they hold, so that they can be colored like jq does.
package highlight

import "strings"

// Kind is the kind of text in a span.
type Kind int

const (
	// Plain is whitespace, commas, colons and anything else not listed below.
	Plain Kind = iota
	// Key is an object key, with its quotes.
	Key
	// String is a string value, with its quotes.
	String
	// Number is a number.
	Number
	// Bool is true or false.
	Bool
	// Null is null.
	Null
	// Bracket is one of {}[].
	Bracket
)

// Span is a run of text of one kind.
type Span struct {
	Kind Kind
	Text string
}

// Line splits one line of JSON into spans. Concatenating the spans' Text gives
// back line.
//
// It expects a line of the output of json.Indent, where a string never spans
// lines, but it accepts any text: a line cut in the middle of a string, as a
// truncated result can be, ends in a String (or Key) span, and unknown words
// are Plain.
func Line(line string) []Span {
	var spans []Span
	add := func(k Kind, text string) {
		if text == "" {
			return
		}
		if n := len(spans); n > 0 && spans[n-1].Kind == k && k == Plain {
			spans[n-1].Text += text
			return
		}
		spans = append(spans, Span{k, text})
	}

	for i := 0; i < len(line); {
		switch c := line[i]; {
		case c == '"':
			end := stringEnd(line, i)
			kind := String
			if followedByColon(line, end) {
				kind = Key
			}
			add(kind, line[i:end])
			i = end
		case c == '{' || c == '}' || c == '[' || c == ']':
			add(Bracket, line[i:i+1])
			i++
		case c == '-' || isDigit(c):
			end := i + 1
			for end < len(line) && isNumberByte(line[end]) {
				end++
			}
			add(Number, line[i:end])
			i = end
		case isLetter(c):
			end := i + 1
			for end < len(line) && isLetter(line[end]) {
				end++
			}
			switch word := line[i:end]; word {
			case "true", "false":
				add(Bool, word)
			case "null":
				add(Null, word)
			default:
				add(Plain, word)
			}
			i = end
		default:
			add(Plain, line[i:i+1])
			i++
		}
	}
	return spans
}

// The colors of jq 1.7, except null, which uses bright black ("90") as jq 1.8
// does: the "1;30" of jq 1.7 is hard to read on a dark background. Numbers and
// booleans keep the default color.
var colors = map[Kind]string{
	Key:     "\x1b[34;1m",
	String:  "\x1b[32m",
	Null:    "\x1b[90m",
	Bracket: "\x1b[1m",
}

const reset = "\x1b[0m"

// Color adds ANSI colors to text, indented JSON of one or more lines, like jq
// does. Each colored span ends with a reset, so a line can be cut anywhere
// between spans without leaking its color.
func Color(text string) string {
	var b strings.Builder
	b.Grow(len(text) + len(text)/2)
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, s := range Line(line) {
			if c, ok := colors[s.Kind]; ok {
				b.WriteString(c)
				b.WriteString(s.Text)
				b.WriteString(reset)
			} else {
				b.WriteString(s.Text)
			}
		}
	}
	return b.String()
}

// stringEnd returns the index just past the string that starts with the quote at
// start, or len(line) if the string is not closed.
func stringEnd(line string, start int) int {
	for i := start + 1; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(line)
}

func followedByColon(line string, i int) bool {
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i < len(line) && line[i] == ':'
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

func isNumberByte(c byte) bool {
	return isDigit(c) || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-'
}
