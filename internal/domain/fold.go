package domain

import (
	"strings"
	"unicode"

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

// Apostrophes are the apostrophe and quote variants names are written with: a separator or a joiner in a name
// (NameMatcher), removed by FoldName.
const Apostrophes = "'’‘ʼ`"

// IsApostrophe reports whether r is one of Apostrophes.
func IsApostrophe(r rune) bool {
	return strings.ContainsRune(Apostrophes, r)
}

func withoutApostrophes(s string) string {
	return strings.Map(func(r rune) rune {
		if IsApostrophe(r) {
			return -1
		}

		return r
	}, s)
}

// IsSpace reports whether r is white space as JavaScript's \s and String.prototype.trim define it, which the
// wiki research scripts, and so the name folding shared with them, rely on.
func IsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}

	return r >= 0x2000 && r <= 0x200a
}

// CollapseSpaces replaces every run of white space (IsSpace) with one space, and trims it.
func CollapseSpaces(s string) string {
	return strings.Join(strings.FieldsFunc(s, IsSpace), " ")
}

// FoldName is the key person names are matched by (never displayed): letters folded (FoldLetters), apostrophes
// and their variants removed, lower case, white space collapsed.
func FoldName(s string) string {
	return CollapseSpaces(strings.ToLower(withoutApostrophes(FoldLetters(s))))
}

// acuteAccent is the spacing acute accent, which is written as an apostrophe ("Wren´s"). FoldLetters would
// decompose it to a space and a combining mark, so DuplicateKey removes it first.
const acuteAccent = "´"

// DuplicateKey is the key names and titles are compared by when looking for a duplicate of a new entity: the
// name in NFC, without spacing acute accents, folded (FoldName). "The Wren's Song", "the wrens song" and
// "The Wren´s Song" have one key, and so do "Łukasz" and "Lukasz".
func DuplicateKey(s string) string {
	return FoldName(strings.ReplaceAll(norm.NFC.String(s), acuteAccent, ""))
}

// DisplayNameKey is the key display names are unique by: DuplicateKey without any character that is not a
// letter or a digit, so that "Mor.wen", "Mor wen" and "MORWEN" are one name.
func DisplayNameKey(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}

		return -1
	}, DuplicateKey(name))
}
