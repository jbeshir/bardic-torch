package importer

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// What withheld text is replaced with in public fields.
const (
	NameWithheld  = "[name withheld]"
	EmailWithheld = "[email withheld]"
)

// contextRunes is how much text on each side of a withheld span the review shows.
const contextRunes = 40

// placeholderNumber is the number ending a scrubbed copy's placeholder ("Legal Name 001", "Wiki User 01"), which
// the matcher does not take as part of the name. It is withheld with the placeholder, so that public text never
// shows a withheld name's number.
var placeholderNumber = regexp.MustCompile(`^[ _-]?[0-9]{2,3}\b`)

// withholder replaces private names and email addresses in the text of public fields. Its names are the corpus's
// private names (mediawiki.PrivateNames), the placeholders of the scrubbed copy and every alias the import hides,
// so that no name the scrubber would replace and no hidden alias reaches public text.
type withholder struct {
	names   []mediawiki.PrivateName
	matcher mediawiki.PrivateNameMatcher
}

// withheld is one span withheld from a field: the text as written (Text, a name or an email address), whether it
// is an email, and the field's text around it after withholding.
type withheld struct {
	Text    string
	Email   bool
	Context string
}

func newWithholder(c mediawiki.Corpus, hiddenAliases []string) withholder {
	names := slices.Clone(c.PrivateNames)
	seen := map[string]bool{}
	for _, n := range names {
		seen[mediawiki.FoldName(n.Text)] = true
	}
	add := func(text string) {
		if key := mediawiki.FoldName(text); key != "" && !seen[key] {
			seen[key] = true
			names = append(names, mediawiki.PrivateName{Text: text, Legal: true})
		}
	}
	for _, d := range c.Privacy {
		if d.Reason == mediawiki.PrivacyPlaceholder {
			add(d.Text)
		}
	}
	for _, a := range hiddenAliases {
		add(a)
	}

	return withholder{names: names, matcher: mediawiki.NewPrivateNameMatcher(names)}
}

// text returns s in NFC with its email addresses, private names and honorific names ("Mr. Surname") withheld, and
// what was withheld, each with its context after every replacement.
func (w withholder) text(s string) (string, []withheld) {
	s = norm.NFC.String(s)
	var found []withheld
	s = replaceAll(s, mediawiki.EmailAddress.FindAllStringIndex(s, -1), EmailWithheld, true, &found)
	s = w.withholdNames(s, &found)
	for k := range found {
		var again []withheld
		found[k].Context = w.withholdNames(found[k].Context, &again)
	}

	return s, found
}

// withholdNames returns s with its private names and then its honorific names replaced, recording each in found.
func (w withholder) withholdNames(s string, found *[]withheld) string {
	var spans [][]int
	for _, m := range w.matcher.Find(s) {
		end := m.End
		if mediawiki.IsPlaceholderName(s[m.Start:end]) {
			end += len(placeholderNumber.FindString(s[end:]))
		}
		spans = append(spans, []int{m.Start, end})
	}
	s = replaceAll(s, spans, NameWithheld, false, found)
	var honorifics [][]int
	for _, m := range mediawiki.HonorificName.FindAllStringSubmatchIndex(s, -1) {
		honorifics = append(honorifics, m[2:4])
	}

	return replaceAll(s, honorifics, NameWithheld, false, found)
}

// hidesURL reports whether a URL holds a private name, in any of the forms the matcher finds (words joined by
// "_", "-" or ".", or concatenated).
func (w withholder) hidesURL(u string) bool {
	return len(w.matcher.Find(u)) > 0
}

// holdsName reports whether s holds a private name.
func (w withholder) holdsName(s string) bool {
	return len(w.matcher.Find(s)) > 0 || mediawiki.HonorificName.MatchString(s)
}

// replaceAll replaces the spans of s, sorted and not overlapping, with replacement, recording each in found with
// its context in the result.
func replaceAll(s string, spans [][]int, replacement string, email bool, found *[]withheld) string {
	if len(spans) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	var at []int
	var texts []string
	for _, sp := range spans {
		b.WriteString(s[last:sp[0]])
		at = append(at, b.Len())
		texts = append(texts, s[sp[0]:sp[1]])
		b.WriteString(replacement)
		last = sp[1]
	}
	b.WriteString(s[last:])
	out := b.String()
	for k, start := range at {
		*found = append(*found, withheld{Text: texts[k], Email: email,
			Context: around(out, start, start+len(replacement))})
	}

	return out
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
