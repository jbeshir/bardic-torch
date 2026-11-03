package mediawiki

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// tokenScanner matches the inline tokens of a text (links, quotes, tags, separators) at a position, for scans that
// move left to right. A regexp alternation with an unbounded "up to the next ]" part costs O(n) per match once an
// unclosed "[http://" precedes the match, so a scan over many matches is O(n²); the scanner remembers the next
// "]" and the next end of a URL instead, so a whole scan stays linear.
type tokenScanner struct {
	s string
	// closing is the first "]" at or after closeFrom (len(s) if none).
	closing, closeFrom int
	// urlEnd is the first "]" or white space at or after urlFrom (len(s) if none).
	urlEnd, urlFrom int
}

func newTokenScanner(s string) *tokenScanner {
	return &tokenScanner{s: s, closing: -1, closeFrom: -1, urlEnd: -1, urlFrom: -1}
}

// nextClose is the offset of the first "]" at or after from, or len(s). Calls with offsets that do not decrease
// search each byte once.
func (sc *tokenScanner) nextClose(from int) int {
	if from < sc.closeFrom || from > sc.closing {
		sc.closing = len(sc.s)
		if k := strings.IndexByte(sc.s[from:], ']'); k >= 0 {
			sc.closing = from + k
		}
	}
	sc.closeFrom = from

	return sc.closing
}

// nextURLEnd is the offset of the first "]" or white space at or after from, or len(s).
func (sc *tokenScanner) nextURLEnd(from int) int {
	if from < sc.urlFrom || from > sc.urlEnd {
		sc.urlEnd = len(sc.s)
		for k := from; k < len(sc.s); {
			r, size := utf8.DecodeRuneInString(sc.s[k:])
			if r == ']' || domain.IsSpace(r) {
				sc.urlEnd = k

				break
			}
			k += size
		}
	}
	sc.urlFrom = from

	return sc.urlEnd
}

// wikilinkAt returns the end of the [[…]] (no brackets inside) starting at i.
func (sc *tokenScanner) wikilinkAt(i int) (int, bool) {
	if !strings.HasPrefix(sc.s[i:], "[[") {
		return 0, false
	}
	k := strings.IndexAny(sc.s[i+2:], "[]")
	if k < 0 || !strings.HasPrefix(sc.s[i+2+k:], "]]") {
		return 0, false
	}

	return i + 2 + k + 2, true
}

// schemeRelativeAt returns the offset after "[//", "[http://" or "[https://" (any case) at i.
func (sc *tokenScanner) schemeRelativeAt(i int) (int, bool) {
	if i >= len(sc.s) || sc.s[i] != '[' {
		return 0, false
	}
	for _, prefix := range []string{"//", "http://", "https://"} {
		if n, ok := prefixFold(sc.s[i+1:], prefix); ok {
			return i + 1 + n, true
		}
	}

	return 0, false
}

// valueLinkAt returns the end of the [//…] or [http(s)://…] that ParseValue treats as one item: everything up to
// the first "]".
func (sc *tokenScanner) valueLinkAt(i int) (int, bool) {
	q, ok := sc.schemeRelativeAt(i)
	if !ok {
		return 0, false
	}
	if c := sc.nextClose(q); c < len(sc.s) {
		return c + 1, true
	}

	return 0, false
}

// externalLinkAt returns the end of the bracketed external link starting at i: "[", a URL ("//", "http://",
// "https://" or "www." and at least one character that is neither "]" nor white space), optionally white space and
// text, and "]".
func (sc *tokenScanner) externalLinkAt(i int) (int, bool) {
	q, ok := sc.schemeRelativeAt(i)
	if !ok {
		n, www := 0, false
		if i < len(sc.s) && sc.s[i] == '[' {
			n, www = prefixFold(sc.s[i+1:], "www.")
		}
		if !www {
			return 0, false
		}
		q = i + 1 + n
	}
	r := sc.nextURLEnd(q)
	switch {
	case r == q || r == len(sc.s):
		return 0, false
	case sc.s[r] == ']':
		return r + 1, true
	}
	if c := sc.nextClose(r); c < len(sc.s) {
		return c + 1, true
	}

	return 0, false
}

// lineBreakAt returns the end of the <br>, <br/> or <br /> (any case, any white space) starting at i.
func (sc *tokenScanner) lineBreakAt(i int) (int, bool) {
	n, ok := prefixFold(sc.s[i:], "<br")
	if !ok {
		return 0, false
	}
	k := i + n
	for k < len(sc.s) {
		r, size := utf8.DecodeRuneInString(sc.s[k:])
		if !domain.IsSpace(r) {
			break
		}
		k += size
	}
	if k < len(sc.s) && sc.s[k] == '/' {
		k++
	}
	if k < len(sc.s) && sc.s[k] == '>' {
		return k + 1, true
	}

	return 0, false
}

// tagAt returns the end of the HTML tag starting at i: "<", optionally "/", a letter a–z (any case) and anything
// but "<" and ">" up to ">".
func (sc *tokenScanner) tagAt(i int) (int, bool) {
	if !strings.HasPrefix(sc.s[i:], "<") {
		return 0, false
	}
	k := i + 1
	if k < len(sc.s) && sc.s[k] == '/' {
		k++
	}
	r, size := utf8.DecodeRuneInString(sc.s[k:])
	if size == 0 || !foldsToASCIILetter(r) {
		return 0, false
	}
	k += size
	if e := strings.IndexAny(sc.s[k:], "<>"); e >= 0 && sc.s[k+e] == '>' {
		return k + e + 1, true
	}

	return 0, false
}

// wordAt returns the end of word (ASCII, any case) at i when it stands alone: no ASCII letter, digit or "_" on
// either side, as a regexp's \b requires.
func (sc *tokenScanner) wordAt(i int, word string) (int, bool) {
	if i > 0 && isASCIIWord(sc.s[i-1]) {
		return 0, false
	}
	n, ok := prefixFold(sc.s[i:], word)
	if !ok || i+n < len(sc.s) && isASCIIWord(sc.s[i+n]) {
		return 0, false
	}

	return i + n, true
}

// prefixFold returns the length in s of prefix, if s starts with it under Unicode simple case folding (as a
// regexp's (?i) matches; "ſ" matches "s").
func prefixFold(s, prefix string) (int, bool) {
	n := 0
	for _, want := range prefix {
		got, size := utf8.DecodeRuneInString(s[n:])
		if size == 0 || !equalFoldRune(got, want) {
			return 0, false
		}
		n += size
	}

	return n, true
}

func equalFoldRune(a, b rune) bool {
	for f := a; ; {
		if f == b {
			return true
		}
		if f = unicode.SimpleFold(f); f == a {
			return false
		}
	}
}

// foldsToASCIILetter reports whether r is a–z or A–Z under simple case folding, as a regexp's (?i)[a-z] matches.
func foldsToASCIILetter(r rune) bool {
	for f := r; ; {
		if f >= 'a' && f <= 'z' || f >= 'A' && f <= 'Z' {
			return true
		}
		if f = unicode.SimpleFold(f); f == r {
			return false
		}
	}
}
