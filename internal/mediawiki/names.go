package mediawiki

import (
	"unicode"
	"unicode/utf8"
)

// isCapitalised reports whether word starts with an upper-case letter.
func isCapitalised(word string) bool {
	r, _ := utf8.DecodeRuneInString(word)

	return unicode.IsUpper(r)
}
