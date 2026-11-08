package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// MaxCombiningMarksPerBase is the most combining marks (Unicode category M) in a row that user-chosen text may
// have. Real text needs a few at most, such as a Vietnamese vowel with two accents or an emoji with a variation
// selector and a keycap; long stacks only serve to draw over the surrounding text.
const MaxCombiningMarksPerBase = 4

var (
	// errDisallowedCharacter is wrapped by CheckTextCharacters for a character outside the allowed categories.
	errDisallowedCharacter = errors.New(
		"must contain only letters, marks, numbers, punctuation, symbols and spaces")
	// errTooManyCombiningMarks is wrapped by CheckTextCharacters for a run of too many combining marks.
	errTooManyCombiningMarks = errors.New("must not stack so many combining marks on one character")
)

// CheckTextCharacters returns an error if s, which must be valid UTF-8, has a character that user-chosen text
// such as names may not contain. Only letters, marks, numbers, punctuation, symbols and space separators are
// allowed, so control and format characters (such as U+202E, which reverses the text after it), line and
// paragraph separators, and private-use and unassigned code points are rejected. At most
// MaxCombiningMarksPerBase combining marks may follow one another.
func CheckTextCharacters(s string) error {
	marks := 0
	for _, r := range s {
		if !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Zs) {
			return fmt.Errorf("%w, not %U", errDisallowedCharacter, r)
		}

		if !unicode.Is(unicode.M, r) {
			marks = 0
			continue
		}
		marks++
		if marks > MaxCombiningMarksPerBase {
			return fmt.Errorf("%w (at most %d)", errTooManyCombiningMarks, MaxCombiningMarksPerBase)
		}
	}

	return nil
}

// IsBlankLetter reports whether r renders blank, or not at all, while counting as a letter or a symbol, so that a
// name holding it looks like the name without it: the Hangul fillers (U+115F, U+1160, U+3164, U+FFA0), the
// braille blank (U+2800) and the other default-ignorable code points.
func IsBlankLetter(r rune) bool {
	switch r {
	case 0x115f, 0x1160, 0x3164, 0xffa0, 0x2800:
		return true
	}

	return unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r)
}

// IsControlCharacter reports whether r is a character imported text must not hold, as it can rewrite how a
// terminal or a page shows the text after it: a C0 or C1 control other than tab and line feed (NUL, ESC), or a
// bidirectional embedding, override or isolate (U+202A–U+202E, U+2066–U+2069).
func IsControlCharacter(r rune) bool {
	switch {
	case r == '\t' || r == '\n':
		return false
	case r < 0x20 || r >= 0x7f && r < 0xa0:
		return true
	}

	return r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069
}

// WithoutControlCharacters is s without its control characters (IsControlCharacter).
func WithoutControlCharacters(s string) string {
	return strings.Map(func(r rune) rune {
		if IsControlCharacter(r) {
			return -1
		}

		return r
	}, s)
}
