package mediawiki

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// MinConcatenatedSubstring is the shortest concatenated name form ("wrenplaceholder") NameMatcher finds inside a
// longer word (a host name or handle); a shorter one must be the whole word.
const MinConcatenatedSubstring = 8

// NameMatcher finds person names in text however they are written: in any case, with accents or not, with
// apostrophes or not, with any punctuation between the words ("Placeholder, Wren" is not a match, but
// "wren_placeholder", "Wren-Placeholder" and "wren.placeholder" are), as one concatenated word, and, for a name of
// three or more words, as its first and last word. It is shared by the scrubber and the importer, so that both
// find the same occurrences.
type NameMatcher struct {
	byFirst map[string][]namePhrase
	concat  []namePhrase
}

type namePhrase struct {
	name   int
	tokens []string
}

// NameMatch is one occurrence of a name or phrase: text[Start:End] is the one numbered Name, counting the
// matcher's names and then its phrases.
type NameMatch struct {
	Name  int
	Start int
	End   int
}

// NewNameMatcher returns a matcher for names, found in every form NameMatcher describes, and for phrases, which may
// not be names ("Sample Words Here"), found only as their words in order: in any case, with accents or not and with
// any punctuation between the words, but not concatenated or by their first and last word, which for a phrase
// of common words would be found everywhere. Names and phrases with no letters or digits are never found.
func NewNameMatcher(names, phrases []string) NameMatcher {
	m := NameMatcher{byFirst: map[string][]namePhrase{}}
	for k, name := range names {
		split, joined := nameTokens(name)
		forms := [][]string{split, joined}
		for _, f := range [][]string{split, joined} {
			if len(f) >= 3 {
				forms = append(forms, []string{f[0], f[len(f)-1]})
			}
		}
		addForms(&m, k, forms, true)
	}
	for k, phrase := range phrases {
		split, joined := nameTokens(phrase)
		addForms(&m, len(names)+k, [][]string{split, joined}, false)
	}

	return m
}

// addForms adds to m the forms of the name or phrase numbered k, and, if concatenated, their concatenations.
func addForms(m *NameMatcher, k int, forms [][]string, concatenated bool) {
	seen := map[string]bool{}
	for _, f := range forms {
		if len(f) == 0 || seen[strings.Join(f, " ")] {
			continue
		}
		seen[strings.Join(f, " ")] = true
		m.byFirst[f[0]] = append(m.byFirst[f[0]], namePhrase{name: k, tokens: f})
		if concatenated && len(f) >= 2 && !seen[strings.Join(f, "")] {
			seen[strings.Join(f, "")] = true
			m.concat = append(m.concat, namePhrase{name: k, tokens: []string{strings.Join(f, "")}})
		}
	}
}

// nameTokens returns the folded words of name with apostrophes as separators and with apostrophes joining.
func nameTokens(name string) (split, joined []string) {
	for _, t := range tokenizeNames(name, false) {
		split = append(split, t.folded)
	}
	for _, t := range tokenizeNames(name, true) {
		joined = append(joined, t.folded)
	}

	return split, joined
}

// Find returns the occurrences of the matcher's names in text, sorted by position and not overlapping; of
// overlapping occurrences the one starting first, and then the longest, is kept.
func (m NameMatcher) Find(text string) []NameMatch {
	var found []NameMatch
	split, joined := tokenizeNames(text, false), tokenizeNames(text, true)
	for _, tokens := range [][]nameToken{split, joined} {
		for i, t := range tokens {
			for _, p := range m.byFirst[t.folded] {
				if end, ok := matchPhrase(tokens, i, p.tokens); ok {
					found = append(found, NameMatch{Name: p.name, Start: t.start, End: end})
				}
			}
		}
	}
	for _, t := range joined {
		for _, p := range m.concat {
			found = append(found, concatMatches(t, p)...)
		}
	}

	return nonOverlapping(found)
}

func matchPhrase(tokens []nameToken, i int, phrase []string) (int, bool) {
	if i+len(phrase) > len(tokens) {
		return 0, false
	}
	for k, want := range phrase {
		if tokens[i+k].folded != want {
			return 0, false
		}
	}

	return tokens[i+len(phrase)-1].end, true
}

func concatMatches(t nameToken, p namePhrase) []NameMatch {
	form := p.tokens[0]
	if t.folded == form {
		return []NameMatch{{Name: p.name, Start: t.start, End: t.end}}
	}
	if len(form) < MinConcatenatedSubstring {
		return nil
	}
	var out []NameMatch
	for at := 0; ; {
		k := strings.Index(t.folded[at:], form)
		if k < 0 {
			return out
		}
		at += k
		out = append(out, NameMatch{Name: p.name, Start: t.origin(at), End: t.originEnd(at + len(form))})
		at += len(form)
	}
}

func nonOverlapping(found []NameMatch) []NameMatch {
	sort.SliceStable(found, func(a, b int) bool {
		if found[a].Start != found[b].Start {
			return found[a].Start < found[b].Start
		}

		return found[a].End > found[b].End
	})
	var out []NameMatch
	for _, f := range found {
		if n := len(out); n > 0 && f.Start < out[n-1].End {
			continue
		}
		out = append(out, f)
	}

	return out
}

// nameToken is a run of letters and digits, folded. text[start:end] is the run; offsets[k] is the offset in text
// of the character folded[k] comes from.
type nameToken struct {
	folded  string
	start   int
	end     int
	offsets []int
	ends    []int
}

func (t nameToken) origin(k int) int {
	return t.offsets[k]
}

func (t nameToken) originEnd(k int) int {
	return t.ends[k-1]
}

func isApostrophe(r rune) bool {
	switch r {
	case '\'', '’', '‘', 'ʼ', '`':
		return true
	}

	return false
}

// tokenizeNames splits text into folded tokens of letters, digits and combining marks. With joinApostrophes, an
// apostrophe between two letters joins them into one token ("O'Brien" is "obrien"); otherwise it separates them.
func tokenizeNames(text string, joinApostrophes bool) []nameToken {
	var out []nameToken
	inToken := false
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) && inToken:
			if !inToken {
				out = append(out, nameToken{start: i})
				inToken = true
			}
			cur := &out[len(out)-1]
			folded := foldRune(r)
			for range len(folded) {
				cur.offsets = append(cur.offsets, i)
				cur.ends = append(cur.ends, i+size)
			}
			cur.folded += folded
			cur.end = i + size
		case joinApostrophes && isApostrophe(r) && inToken && startsWord(text[i+size:]):
		default:
			inToken = false
		}
		i += size
	}

	return out
}

func startsWord(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)

	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func foldRune(r rune) string {
	if r < utf8.RuneSelf {
		return string(unicode.ToLower(r))
	}

	return strings.ToLower(domain.FoldLetters(string(r)))
}

// isCapitalised reports whether word starts with an upper-case letter.
func isCapitalised(word string) bool {
	r, _ := utf8.DecodeRuneInString(word)

	return unicode.IsUpper(r)
}

// Word is a word of a text as NameMatcher reads it: a run of letters, digits and combining marks, folded to lower
// case without accents (Folded), at text[Start:End]. Apostrophes separate words.
type Word struct {
	Folded string
	Start  int
	End    int
}

// Words returns the words of text, in order, as NameMatcher tokenises it, for code that must agree with
// NameMatcher on what a word is (the scrubber's lyric n-grams).
func Words(text string) []Word {
	tokens := tokenizeNames(text, false)
	out := make([]Word, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, Word{Folded: t.folded, Start: t.start, End: t.end})
	}

	return out
}
