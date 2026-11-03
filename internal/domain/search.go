package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The rules of catalogue search. A search query is matched against InnoDB FULLTEXT indexes built with the
// server's default stopwords and token sizes, so the tokeniser drops exactly the tokens those indexes never hold:
// a required token the index cannot contain would make every search containing it find nothing.
const (
	// MaxSearchLength is the longest search query, in code points.
	MaxSearchLength = 200
	// MaxSearchTokens is the most tokens of a search query that are matched; later ones are ignored.
	MaxSearchTokens = 8
	// MinSearchTokenLength is the shortest token, in code points, that FULLTEXT indexes hold
	// (innodb_ft_min_token_size).
	MinSearchTokenLength = 3
	// MaxSearchTokenLength is the longest token, in code points, that FULLTEXT indexes hold
	// (innodb_ft_max_token_size).
	MaxSearchTokenLength = 84
)

// innoDBDefaultStopwords is InnoDB's default FULLTEXT stopword list
// (INFORMATION_SCHEMA.INNODB_FT_DEFAULT_STOPWORD), which an integration test keeps in step with the server.
var innoDBDefaultStopwords = []string{
	"a", "about", "an", "are", "as", "at", "be", "by", "com", "de", "en", "for", "from", "how", "i", "in", "is",
	"it", "la", "of", "on", "or", "that", "the", "this", "to", "und", "was", "what", "when", "where", "who",
	"will", "with", "www",
}

var (
	errSearchInvalidUTF8 = errors.New("must be valid UTF-8")
	errSearchTooLong     = fmt.Errorf("must be at most %d characters", MaxSearchLength)
	errSearchBlank       = errors.New("must contain a character other than spaces")
)

// SearchQuery is a parsed search query. Tokens are what a FULLTEXT search matches as word prefixes, all of which
// a match must contain. If there are none, because every word is too short, too long or a stopword, Text is
// matched as a prefix of titles and names instead.
type SearchQuery struct {
	// Text is the query with leading and trailing spaces removed and each run of spaces made one space.
	Text string
	// Tokens are the query's distinct words, lower-cased and without combining marks, at most MaxSearchTokens of
	// them, in query order. A word is a run of letters, digits, underscores and combining marks, as InnoDB's
	// parser splits text, so other punctuation, including apostrophes and every FULLTEXT operator, separates
	// words and never reaches the database: "O'Neill" is "o" and "neill", and "foo_bar" one word. Words without
	// a letter or digit, words shorter than MinSearchTokenLength or longer than MaxSearchTokenLength, and
	// stopwords are dropped.
	Tokens []string
}

// ParseSearchQuery parses a search query of at most MaxSearchLength code points of valid UTF-8 containing a
// character other than spaces. Spaces are the ASCII space, tab, line feed, form feed and carriage return: the
// characters the spec's [^ \t\n\f\r] pattern excludes.
func ParseSearchQuery(q string) (SearchQuery, error) {
	switch {
	case !utf8.ValidString(q):
		return SearchQuery{}, errSearchInvalidUTF8
	case utf8.RuneCountInString(q) > MaxSearchLength:
		return SearchQuery{}, errSearchTooLong
	}

	words := strings.FieldsFunc(q, isSearchSpace)
	if len(words) == 0 {
		return SearchQuery{}, errSearchBlank
	}

	return SearchQuery{Text: strings.Join(words, " "), Tokens: searchTokens(q)}, nil
}

func isSearchSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r'
}

// isWordRune reports whether r is part of a word: a letter, a digit, an underscore, or a combining mark (as in a
// decomposed accented letter).
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || unicode.Is(unicode.M, r)
}

// isMark reports whether r is a combining mark. InnoDB's parser does not hold marks in tokens, so a token of
// marks alone would match everything.
func isMark(r rune) bool {
	return unicode.Is(unicode.M, r)
}

func searchTokens(q string) []string {
	var tokens []string
	for _, word := range strings.FieldsFunc(strings.ToLower(q), func(r rune) bool { return !isWordRune(r) }) {
		word = strings.Map(func(r rune) rune {
			if isMark(r) {
				return -1
			}
			return r
		}, word)
		length := utf8.RuneCountInString(word)
		if length < MinSearchTokenLength || length > MaxSearchTokenLength ||
			!strings.ContainsFunc(word, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) ||
			slices.Contains(innoDBDefaultStopwords, word) || slices.Contains(tokens, word) {
			continue
		}
		tokens = append(tokens, word)
		if len(tokens) == MaxSearchTokens {
			break
		}
	}

	return tokens
}

// Letter is a letter of the A–Z index: an upper-case ASCII letter, or "0" for everything that does not sort
// under one (digits, punctuation and other scripts). Entities store theirs (sort_letter), computed by the writer.
type Letter string

// LetterOther is the A–Z index's bucket for sort keys that do not start with a letter from A to Z.
const LetterOther Letter = "0"

var errInvalidLetter = errors.New("must be an upper-case letter from A to Z, or 0")

// ParseLetter returns the Letter s names: "A" to "Z", or "0".
func ParseLetter(s string) (Letter, error) {
	if s == string(LetterOther) || (len(s) == 1 && s[0] >= 'A' && s[0] <= 'Z') {
		return Letter(s), nil
	}

	return "", errInvalidLetter
}
