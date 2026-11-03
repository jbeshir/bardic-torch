package importer

import (
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// contextRunes is how much text on each side of a withheld span the review shows.
const contextRunes = 40

// withholder withholds private names and email addresses from public fields. It knows two kinds of private name:
//   - legal names: every alias the import hides, the corpus's legal names (mediawiki.PrivateName.Legal) and the
//     placeholders of the scrubbed copy, which may stand for any private name. They are withheld from every public
//     field, names and titles included (name), and a URL holding one is hidden (hidesURL);
//   - unsure names: the corpus's other private names, which the classifier could not show are not legal names.
//     They are withheld from free text only (text): a name the wiki itself gives an entity (a credited person, a
//     piece title or alias, a collection label, an event) is imported as written, and listed in the review. An
//     unsure name the import publishes as a person's name (publishing) is not withheld from free text either, as
//     withholding it there would protect nothing; each occurrence is listed (published).
//
// Honorific names ("Mr. Surname") and email addresses are withheld wherever legal names are.
type withholder struct {
	legal  []mediawiki.PrivateName
	unsure []mediawiki.PrivateName
	legalM mediawiki.PrivateNameMatcher
	// privateM finds every private name, legal or unsure.
	privateM mediawiki.PrivateNameMatcher
	// textM finds the private names withheld from free text: privateM's but the published unsure names, which
	// publishedM finds.
	textM      mediawiki.PrivateNameMatcher
	publishedM mediawiki.PrivateNameMatcher
	// unsureURL finds the unsure names in URLs, concatenated forms included.
	unsureURL domain.NameMatcher
}

// withheld is one span withheld from a field: the text as written (Text, a name or an email address), whether it
// is an email, and the field's text around it after withholding.
type withheld struct {
	Text    string
	Email   bool
	Context string
}

func newWithholder(c mediawiki.Corpus, hiddenAliases []string) withholder {
	var legal, unsure []mediawiki.PrivateName
	seen := map[string]bool{}
	add := func(text string, isLegal bool) {
		key := domain.FoldName(text)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		if isLegal {
			legal = append(legal, mediawiki.PrivateName{Text: text, Legal: true})
		} else {
			unsure = append(unsure, mediawiki.PrivateName{Text: text})
		}
	}
	for _, a := range hiddenAliases {
		add(a, true)
	}
	for _, d := range c.Privacy {
		if d.Reason == mediawiki.PrivacyPlaceholder {
			add(d.Text, true)
		}
	}
	for _, n := range c.PrivateNames {
		add(n.Text, n.Legal)
	}

	unsureTexts := make([]string, 0, len(unsure))
	for _, n := range unsure {
		unsureTexts = append(unsureTexts, n.Text)
	}

	privateM := mediawiki.NewPrivateNameMatcher(slices.Concat(legal, unsure))

	return withholder{legal: legal, unsure: unsure, legalM: mediawiki.NewPrivateNameMatcher(legal),
		privateM: privateM, textM: privateM, publishedM: mediawiki.NewPrivateNameMatcher(nil),
		unsureURL: domain.NewNameMatcher(unsureTexts, nil)}
}

// publishing returns the withholder for an import that publishes names as persons' names (display names): the
// unsure names found in one of them are not withheld from free text, but found by published. Legal names always
// are.
func (w withholder) publishing(names []string) withholder {
	unsureM := mediawiki.NewPrivateNameMatcher(w.unsure)
	published := map[int]bool{}
	for _, n := range names {
		for _, f := range unsureM.Find(norm.NFC.String(n)) {
			published[f.Name] = true
		}
	}
	var withheldNames, publishedNames []mediawiki.PrivateName
	for k, n := range w.unsure {
		if published[k] {
			publishedNames = append(publishedNames, n)
		} else {
			withheldNames = append(withheldNames, n)
		}
	}
	w.textM = mediawiki.NewPrivateNameMatcher(slices.Concat(w.legal, withheldNames))
	w.publishedM = mediawiki.NewPrivateNameMatcher(publishedNames)

	return w
}

// text returns free text s in NFC with its email addresses, private names (legal and unsure, but the published
// ones) and honorific names withheld, and what was withheld.
func (w withholder) text(s string) (string, []withheld) {
	return withhold(norm.NFC.String(s), w.textM)
}

// published returns the occurrences in free text s (as text returned it) of the unsure names the import publishes
// as persons' names, which text keeps, each with its context.
func (w withholder) published(s string) []withheld {
	var out []withheld
	for _, f := range w.publishedM.Find(s) {
		out = append(out, withheld{Text: s[f.Start:f.End], Context: around(s, f.Start, f.End)})
	}

	return out
}

// name returns a name or title s in NFC with its email addresses, legal names and honorific names withheld, and
// what was withheld: a name with anything withheld must not be imported.
func (w withholder) name(s string) (string, []withheld) {
	return withhold(norm.NFC.String(s), w.legalM)
}

// hidesURL reports whether a URL holds a legal name, in any of the forms the matcher finds (words joined by "_",
// "-" or ".", or concatenated).
func (w withholder) hidesURL(u string) bool {
	return len(w.legalM.Find(u)) > 0
}

// urlNote says why a URL that is not hidden may still name a person, or is "": it has a home directory ("~user")
// in its path, or holds an unsure private name (a personal-name domain or handle). URLs are never rewritten, so
// such a link is listed for the moderators.
func (w withholder) urlNote(u string) string {
	if parsed, err := url.Parse(u); err == nil && strings.Contains(parsed.Path, "/~") {
		return "has a home directory (~user) in its path"
	}
	if len(w.unsureURL.Find(u)) > 0 {
		return "holds a possible person name"
	}

	return ""
}

// holdsName reports whether s holds a private name, legal or unsure, or an honorific name.
func (w withholder) holdsName(s string) bool {
	return len(w.privateM.Find(s)) > 0 || mediawiki.HonorificName.MatchString(s)
}

// span is a span of text to withhold, and whether it is an email address.
type span struct {
	start, end int
	email      bool
}

// withhold replaces in s every email address, every name m finds (with a scrubbed placeholder's number, and the
// whole wikilink it is in, so no "[[[name withheld]]]" is left) and the surname of every honorific name. Spans
// that overlap are withheld as one, an email if any of them is, so that each occurrence is withheld and listed
// once. Each is recorded with its context in the result.
func withhold(s string, m mediawiki.PrivateNameMatcher) (string, []withheld) {
	merged := mergeSpans(withheldSpans(s, m))
	if len(merged) == 0 {
		return s, nil
	}
	var b strings.Builder
	at := 0
	starts := make([]int, 0, len(merged))
	for _, sp := range merged {
		b.WriteString(s[at:sp.start])
		starts = append(starts, b.Len())
		b.WriteString(placeholderFor(sp.email))
		at = sp.end
	}
	b.WriteString(s[at:])
	out := b.String()
	found := make([]withheld, 0, len(merged))
	for k, sp := range merged {
		start := starts[k]
		found = append(found, withheld{Text: s[sp.start:sp.end], Email: sp.email,
			Context: around(out, start, start+len(placeholderFor(sp.email)))})
	}

	return out, found
}

// withheldSpans are the spans of s to withhold: every email address, every name m finds (with a scrubbed
// placeholder's number, and the whole wikilink it is in) and the surname of every honorific name.
func withheldSpans(s string, m mediawiki.PrivateNameMatcher) []span {
	var spans []span
	for _, e := range mediawiki.EmailAddress.FindAllStringIndex(s, -1) {
		spans = append(spans, span{start: e[0], end: e[1], email: true})
	}
	links := mediawiki.FindWikilinks(s)
	for _, f := range m.Find(s) {
		sp := span{start: f.Start, end: f.End}
		// The number ending a placeholder is withheld with it, so public text never shows a withheld name's number.
		if mediawiki.IsPlaceholderName(s[sp.start:sp.end]) {
			sp.end += len(mediawiki.PlaceholderNumber.FindString(s[sp.end:]))
		}
		for _, l := range links {
			if l.Start <= sp.start && sp.end <= l.End {
				sp.start, sp.end = l.Start, l.End
			}
		}
		spans = append(spans, sp)
	}
	for _, h := range mediawiki.HonorificName.FindAllStringSubmatchIndex(s, -1) {
		spans = append(spans, span{start: h[2], end: h[3]})
	}

	return spans
}

// mergeSpans sorts spans and merges those that overlap, into an email if any of them is one.
func mergeSpans(spans []span) []span {
	if len(spans) == 0 {
		return nil
	}
	slices.SortFunc(spans, func(a, b span) int { return a.start - b.start })
	merged := []span{spans[0]}
	for _, sp := range spans[1:] {
		last := &merged[len(merged)-1]
		if sp.start < last.end {
			last.end = max(last.end, sp.end)
			last.email = last.email || sp.email

			continue
		}
		merged = append(merged, sp)
	}

	return merged
}

func placeholderFor(email bool) string {
	if email {
		return domain.WithheldEmail
	}

	return domain.WithheldName
}

// around is s[start:end] with up to contextRunes runes on each side, on one line.
func around(s string, start, end int) string {
	from := start
	for n := 0; n < contextRunes && from > 0; n++ {
		_, size := utf8.DecodeLastRuneInString(s[:from])
		from -= size
	}
	to := end
	for n := 0; n < contextRunes && to < len(s); n++ {
		_, size := utf8.DecodeRuneInString(s[to:])
		to += size
	}

	return oneLine(s[from:to])
}
