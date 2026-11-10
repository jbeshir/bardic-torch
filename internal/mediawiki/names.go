package mediawiki

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// nameToken is a run of letters and digits, folded. text[start:end] is the run; offsets[k] is the offset in text
// of the character folded[k] comes from.
type nameToken struct {
	folded  string
	start   int
	end     int
	offsets []int
	ends    []int
}

// tokenizeNames splits text into folded tokens of letters, digits and combining marks. With joinApostrophes, an
// apostrophe between two letters joins them into one token ("O'Brien" is "obrien"); otherwise it separates them.
func tokenizeNames(text string, joinApostrophes bool) []nameToken {
	var out []nameToken
	var cur nameToken
	var folded strings.Builder
	inToken := false
	endToken := func() {
		if inToken {
			cur.folded = folded.String()
			out = append(out, cur)
			folded.Reset()
		}
		inToken = false
	}
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case isNameRune(r, inToken):
			if !inToken {
				cur = nameToken{start: i}
				inToken = true
			}
			f := foldRune(r)
			cur = cur.with(f, i, size)
			folded.WriteString(f)
		case joinApostrophes && isApostrophe(r) && inToken && startsWord(text[i+size:]):
		default:
			endToken()
		}
		i += size
	}
	endToken()

	return out
}

// isNameRune reports whether r belongs to a name token: a letter or digit, or a combining mark inside one.
func isNameRune(r rune, inToken bool) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) && inToken
}

// with is the token extended by the character at text[at:at+size], whose folded form is f: its offsets and end.
func (t nameToken) with(f string, at, size int) nameToken {
	for range len(f) {
		t.offsets = append(t.offsets, at)
		t.ends = append(t.ends, at+size)
	}
	t.end = at + size

	return t
}

func startsWord(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)

	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func foldRune(r rune) string {
	if r < utf8.RuneSelf {
		return string(unicode.ToLower(r))
	}

	return domain.CollationKey(string(r))
}

// isCapitalised reports whether word starts with an upper-case letter.
func isCapitalised(word string) bool {
	r, _ := utf8.DecodeRuneInString(word)

	return unicode.IsUpper(r)
}

// Word is a word of a text as the lyric n-gram scan reads it: a run of letters, digits and combining marks, folded
// to lower case without accents (Folded), at text[Start:End]. Apostrophes separate words.
type Word struct {
	Folded string
	Start  int
	End    int
}

// Words returns the words of text, in order, as the lyric n-gram scan reads them. The scrubber and its
// denylist test must agree on what a word is.
func Words(text string) []Word {
	tokens := tokenizeNames(text, false)
	out := make([]Word, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, Word{Folded: t.folded, Start: t.start, End: t.end})
	}

	return out
}
