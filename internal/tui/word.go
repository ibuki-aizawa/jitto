package tui

import "unicode"

// The word keys (Ctrl+W, Alt+D, Ctrl+Left, Ctrl+Right and their aliases) treat a
// run of key characters and a run of symbols as separate words, like Vim does.
// textinput splits words only at spaces, but jq expressions rarely have any, so
// Ctrl+W would delete .items[0].name as a whole instead of leaving .items[0].
// for the key candidates.

type charClass int

const (
	classSpace charClass = iota
	// classWord covers the characters of keys: letters, digits, marks and "_".
	classWord
	// classSymbol covers everything else, such as . [ ] | and quotes.
	classSymbol
)

func classOf(r rune) charClass {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
		return classWord
	}
	return classSymbol
}

// wordStart returns the start of the word before pos: it skips spaces, then the
// run of characters of the same class.
func wordStart(s []rune, pos int) int {
	i := pos
	for i > 0 && classOf(s[i-1]) == classSpace {
		i--
	}
	if i == 0 {
		return 0
	}
	class := classOf(s[i-1])
	for i > 0 && classOf(s[i-1]) == class {
		i--
	}
	return i
}

// wordEnd returns the end of the word after pos: it skips spaces, then the run of
// characters of the same class.
func wordEnd(s []rune, pos int) int {
	i := pos
	for i < len(s) && classOf(s[i]) == classSpace {
		i++
	}
	if i == len(s) {
		return i
	}
	class := classOf(s[i])
	for i < len(s) && classOf(s[i]) == class {
		i++
	}
	return i
}
