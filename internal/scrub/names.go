package scrub

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// nameReplacer replaces the private names (mediawiki.PrivateNames) and their forms with numbered placeholders: the
// name numbered k+1 is the matcher's names[k]. last is the highest number given, so that honorific names found
// later get fresh ones.
type nameReplacer struct {
	matcher mediawiki.PrivateNameMatcher
	last    int
}

func newNameReplacer(names []mediawiki.PrivateName) nameReplacer {
	return nameReplacer{matcher: mediawiki.NewPrivateNameMatcher(names), last: len(names)}
}

// fresh returns the placeholder of a new number.
func (r *nameReplacer) fresh() string {
	r.last++

	return fmt.Sprintf(mediawiki.LegalNameFormat, r.last)
}

// replace returns text with every form of every private name replaced, and how many were. A match starting inside
// one of urls gets the URL form of its placeholder.
func (r *nameReplacer) replace(text string, urls [][2]int) (string, int) {
	matches := r.matcher.Find(text)
	if len(matches) == 0 {
		return text, 0
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(text[last:m.Start])
		b.WriteString(placeholderFor(text[m.Start:m.End], m, inside(urls, m.Start)))
		last = m.End
	}
	b.WriteString(text[last:])

	return b.String(), len(matches)
}

// placeholderFor is the placeholder that replaces span, the occurrence m: in a URL "Legal_Name_001" if span has
// underscores, "legalname001" if it is one word and "legal-name-001" otherwise; elsewhere "LegalName001" for a
// name of several words written as one, the honorific as written followed by "Legal Name 001" for an honorific
// form, and "Legal Name 001" otherwise.
func placeholderFor(span string, m mediawiki.PrivateNameMatch, inURL bool) string {
	number := m.Name + 1
	words := mediawiki.Words(span)
	oneWord := len(words) == 1
	switch {
	case inURL && strings.Contains(span, "_"):
		return fmt.Sprintf(mediawiki.LegalNameURLUnderscoresFormat, number)
	case inURL && oneWord:
		return fmt.Sprintf(mediawiki.LegalNameURLWordFormat, number)
	case inURL:
		return fmt.Sprintf(mediawiki.LegalNameURLHyphensFormat, number)
	case oneWord && m.Multiword:
		return fmt.Sprintf(mediawiki.LegalNameJoinedFormat, number)
	case m.Honorific:
		prefix := span[:words[0].End]
		if strings.HasPrefix(span[words[0].End:], ".") {
			prefix += "."
		}

		return prefix + " " + fmt.Sprintf(mediawiki.LegalNameFormat, number)
	}

	return fmt.Sprintf(mediawiki.LegalNameFormat, number)
}

func inside(spans [][2]int, offset int) bool {
	for _, s := range spans {
		if s[0] <= offset && offset < s[1] {
			return true
		}
	}

	return false
}

// startsWithPlaceholder reports whether text starts with a prose placeholder.
func startsWithPlaceholder(text string) bool {
	return slices.ContainsFunc(mediawiki.PlaceholderPrefixes, func(p string) bool { return strings.HasPrefix(text, p) })
}
