package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// sortArticles are the leading articles SortTitle skips, followed by a space.
var sortArticles = []string{"the ", "a ", "an "}

// SortTitle returns the sort key of a piece title or collection name: s without leading spaces and punctuation,
// and then without one leading "The", "A" or "An" word (in any case) if more follows it. Case is kept. A title of
// nothing but spaces and punctuation is its own sort key.
func SortTitle(s string) string {
	rest := strings.TrimLeftFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	if rest == "" {
		return s
	}
	for _, article := range sortArticles {
		if len(rest) > len(article) && strings.EqualFold(rest[:len(article)], article) {
			if after := strings.TrimLeftFunc(rest[len(article):], unicode.IsSpace); after != "" {
				return after
			}
		}
	}

	return rest
}

// SortLetter returns the letter of the A–Z index a sort key sorts under: its first character with letters folded
// (FoldLetters) and upper-cased if that is a letter from A to Z, and LetterOther otherwise.
func SortLetter(key string) Letter {
	first, _ := utf8.DecodeRuneInString(strings.ToUpper(FoldLetters(key)))
	if first >= 'A' && first <= 'Z' {
		return Letter(string(first))
	}

	return LetterOther
}
