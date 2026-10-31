package domain

import (
	"errors"
	"fmt"
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
