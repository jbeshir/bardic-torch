package scrub

import (
	"fmt"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// honorifics are the honorifics whose forms with a deny phrase's surname ("Mr. Surname") are deny phrases too, and
// which, before any other capitalised word in prose, make that word a placeholder.
var honorifics = []string{"Mr", "Mrs", "Ms", "Mx", "Dr", "Miss"}

// denyPhrase is a name the scrubbed copy must not hold, and the number of its placeholder. A legal name (name) is
// found in every form mediawiki.NameMatcher knows and also with an honorific before its surname and with its last
// word before its first; any other deny phrase, which may be words that are not a name at all, is found only as
// its words in order.
type denyPhrase struct {
	text   string
	number int
	name   bool
}

// nameForm is one form NameMatcher looks for: a deny phrase, its last word before its first, or an honorific
// with its surname.
type nameForm struct {
	number    int
	multiword bool
	honorific bool
}

// nameReplacer replaces the deny phrases and their forms with numbered placeholders. last is the highest number
// given, so that honorific names found later get fresh ones.
type nameReplacer struct {
	matcher mediawiki.NameMatcher
	forms   []nameForm
	last    int
}

func newNameReplacer(phrases []denyPhrase) nameReplacer {
	var names, others []string
	var nameForms, otherForms []nameForm
	for _, p := range phrases {
		multiword := len(mediawiki.Words(p.text)) > 1
		if !p.name {
			others = append(others, p.text)
			otherForms = append(otherForms, nameForm{number: p.number, multiword: multiword})

			continue
		}
		add := func(name string, form nameForm) {
			names = append(names, name)
			nameForms = append(nameForms, form)
		}
		add(p.text, nameForm{number: p.number, multiword: multiword})
		words := strings.Fields(p.text)
		if len(words) >= 2 {
			add(words[len(words)-1]+" "+words[0], nameForm{number: p.number, multiword: true})
		}
		surnames := []string{words[len(words)-1]}
		if parts := strings.Split(surnames[0], "-"); len(parts) > 1 {
			surnames = append(surnames, parts...)
		}
		for _, h := range honorifics {
			for _, surname := range surnames {
				if len(mediawiki.Words(surname)) > 0 {
					add(h+" "+surname, nameForm{number: p.number, multiword: true, honorific: true})
				}
			}
		}
	}
	r := nameReplacer{matcher: mediawiki.NewNameMatcher(names, others), forms: append(nameForms, otherForms...)}
	for _, p := range phrases {
		r.last = max(r.last, p.number)
	}

	return r
}

// fresh returns the placeholder of a new number.
func (r *nameReplacer) fresh() string {
	r.last++

	return fmt.Sprintf(legalNameFormat, r.last)
}

// replace returns text with every form of every deny phrase replaced, and how many were. A match starting inside
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
		b.WriteString(placeholderFor(text[m.Start:m.End], r.forms[m.Name], inside(urls, m.Start)))
		last = m.End
	}
	b.WriteString(text[last:])

	return b.String(), len(matches)
}

// placeholderFor is the placeholder that replaces span, an occurrence of form: in a URL "Legal_Name_001" if span
// has underscores, "legalname001" if it is one word and "legal-name-001" otherwise; elsewhere "LegalName001" for
// a name of several words written as one, the honorific as written followed by "Legal Name 001" for an honorific
// form, and "Legal Name 001" otherwise.
func placeholderFor(span string, form nameForm, inURL bool) string {
	words := mediawiki.Words(span)
	oneWord := len(words) == 1
	switch {
	case inURL && strings.Contains(span, "_"):
		return fmt.Sprintf("Legal_Name_%03d", form.number)
	case inURL && oneWord:
		return fmt.Sprintf("legalname%03d", form.number)
	case inURL:
		return fmt.Sprintf("legal-name-%03d", form.number)
	case oneWord && form.multiword:
		return fmt.Sprintf("LegalName%03d", form.number)
	case form.honorific:
		prefix := span[:words[0].End]
		if strings.HasPrefix(span[words[0].End:], ".") {
			prefix += "."
		}

		return prefix + " " + fmt.Sprintf(legalNameFormat, form.number)
	}

	return fmt.Sprintf(legalNameFormat, form.number)
}

func inside(spans [][2]int, offset int) bool {
	for _, s := range spans {
		if s[0] <= offset && offset < s[1] {
			return true
		}
	}

	return false
}

// isPlaceholder reports whether text holds a placeholder, in any of its forms: "Legal Name 001",
// "legal-name-001", "LegalName001" or "Wiki User 01".
func isPlaceholder(text string) bool {
	words := mediawiki.Words(text)
	for k, w := range words {
		if strings.HasPrefix(w.Folded, "legalname") {
			return true
		}
		if k+1 < len(words) && (w.Folded == "legal" && words[k+1].Folded == "name" ||
			w.Folded == "wiki" && words[k+1].Folded == "user") {
			return true
		}
	}

	return false
}

// startsWithPlaceholder reports whether text starts with a prose placeholder.
func startsWithPlaceholder(text string) bool {
	return strings.HasPrefix(text, "Legal Name ") || strings.HasPrefix(text, "Wiki User ")
}
