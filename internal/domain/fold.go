package domain

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// letterFolds transliterates the Latin letters that compatibility decomposition leaves whole, as they have no
// combining-mark form, into the ASCII letters they are written as.
var letterFolds = strings.NewReplacer(
	"ł", "l", "Ł", "L",
	"ø", "o", "Ø", "O",
	"đ", "d", "Đ", "D",
	"ð", "d", "Ð", "D",
	"æ", "ae", "Æ", "Ae",
	"œ", "oe", "Œ", "Oe",
	"ß", "ss", "ẞ", "Ss",
	"þ", "th", "Þ", "Th",
)

// FoldLetters returns s in compatibility decomposition (NFKD, so that a ligature such as "ﬁ" is "fi"), without
// combining marks and with the letters NFKD leaves whole transliterated: ł→l, ø→o, đ and ð→d, æ→ae, œ→oe, ß→ss
// and þ→th, a capital becoming a capital and then lower-case letters ("Ælfric" is "Aelfric"). Case is otherwise
// kept. It is the one letter folding of slugs, sort letters and name matching, so that "Astrið" is "Astrid" in
// each.
func FoldLetters(s string) string {
	decomposed := strings.Map(func(r rune) rune {
		if isMark(r) {
			return -1
		}
		return r
	}, norm.NFKD.String(s))

	return letterFolds.Replace(decomposed)
}
