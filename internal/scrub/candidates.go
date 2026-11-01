package scrub

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// PublicNames are names that docs/PLAN.md already publishes (a bard's name, and the wiki's own name) but that the
// classifier would hide. The scrubber neither replaces them nor lists them as deny phrases.
// TestPublicNamesArePublishedInPlan keeps the list to names the plan holds, so it cannot hold a new secret.
var PublicNames = []string{"Ambra Michelli", "Bardic Torch"}

// Reason is why a possible person name was or was not made a deny phrase.
type Reason string

// The reasons. A name is a deny phrase for the first four; the others keep it.
const (
	// ReasonHiddenAlias: a bard's alias, or a redirect to a bard, that the classifier does not make public.
	ReasonHiddenAlias Reason = "hidden_alias"
	// ReasonLegalMarker: the name follows "legal name", "mundane name", "aka" or "a.k.a.".
	ReasonLegalMarker Reason = "legal_marker"
	// ReasonUnsure: the classifier cannot say confidently that the name is not a legal name (fail closed).
	ReasonUnsure Reason = "unsure"
	// ReasonDenied: the name is already a deny phrase.
	ReasonDenied Reason = "denied"

	// ReasonPlaceholder: the name is a placeholder already.
	ReasonPlaceholder Reason = "placeholder"
	// ReasonPublishedName: the name is in PublicNames.
	ReasonPublishedName Reason = "published_name"
	// ReasonPublicAlias: the classifier makes the name public, or it is a bard's public alias.
	ReasonPublicAlias Reason = "public_alias"
	// ReasonPageTitle: the name's words are words of a page title or category name, which are public.
	ReasonPageTitle Reason = "page_title"
	// ReasonNoCapital: no word of the text is capitalised, so it is not a name ("the author").
	ReasonNoCapital Reason = "no_capital"
	// ReasonSingleWord: a single word that is not an uploader (an H1 or parenthesis "(Song)", "(Poem)").
	ReasonSingleWord Reason = "single_word"
)

// CandidateDecision is the decision on one possible person name of the crawl: found on the page PageID (0 for a
// redirect) by Rule (a mediawiki.AliasSource, a mediawiki.CandidateRule or "redirect"). A deny phrase is replaced
// with Placeholder everywhere. Text is a name of the crawl, so decisions are only written to the private
// candidates list, never to the repository or the output.
type CandidateDecision struct {
	PageID      int64
	Rule        string
	Text        string
	Deny        bool
	Reason      Reason
	Placeholder string
}

// ruleRedirect is the rule of a redirect title that resolves to a bard.
const ruleRedirect = "redirect"

// phraseCollector decides, in a fixed order, which possible names are deny phrases, and numbers those.
type phraseCollector struct {
	titles    [][]string
	public    map[string]bool
	published map[string]bool
	numbers   map[string]int
	phrases   []denyPhrase
	decisions []CandidateDecision
}

// collectPhrases decides on every possible person name of pages (which are sorted by page ID) and of redirects:
// first each bard's aliases (mediawiki.BardAliases) and each redirect to a bard, classified with
// mediawiki.ClassifyPersonAlias; then every mediawiki.PersonNameCandidates candidate, which is a deny phrase
// unless it is a placeholder, a published name, a public alias, words of a page title, has no capitalised word,
// is a single word not given as an uploader, or classifies confidently as public. A name after a marker is a deny
// phrase. Deny phrases are numbered in the order they are found.
func collectPhrases(pages []mediawiki.Page, redirects []mediawiki.Redirect) ([]denyPhrase, []CandidateDecision) {
	c := phraseCollector{public: map[string]bool{}, published: map[string]bool{}, numbers: map[string]int{}}
	for _, name := range PublicNames {
		c.published[mediawiki.FoldName(name)] = true
	}
	bards := map[string]bool{}
	maxYear := mediawiki.MinPerformanceYear
	for _, p := range pages {
		c.titles = append(c.titles, folded(strings.TrimPrefix(p.Title, "Category:")))
		maxYear = max(maxYear, p.LastRevTimestamp.Year())
		if mediawiki.TypePage(p) != mediawiki.PageTypeBard {
			continue
		}
		bards[mediawiki.NormTitle(p.Title)] = true
		for _, a := range mediawiki.BardAliases(p) {
			c.alias(p.PageID, string(a.Source), a.Name, mediawiki.ClassifyPersonAlias(a))
		}
	}
	for _, r := range redirects {
		if r.NS == mediawiki.NSMain && bards[mediawiki.NormTitle(r.To)] {
			a := mediawiki.AliasCandidate{Name: r.From, Source: mediawiki.AliasFromH1Parenthetical}
			c.alias(0, ruleRedirect, r.From, mediawiki.ClassifyPersonAlias(a))
		}
	}
	for _, p := range pages {
		for _, cand := range mediawiki.PersonNameCandidates(p, maxYear) {
			c.candidate(cand)
		}
	}

	return c.phrases, c.decisions
}

func (c *phraseCollector) alias(pageID int64, rule, name string, class mediawiki.AliasClassification) {
	key := mediawiki.FoldName(name)
	switch {
	case isPlaceholder(name):
		c.keep(pageID, rule, name, ReasonPlaceholder)
	case c.published[key]:
		c.keep(pageID, rule, name, ReasonPublishedName)
	case class.Public():
		c.public[key] = true
		c.keep(pageID, rule, name, ReasonPublicAlias)
	default:
		c.deny(pageID, rule, name, ReasonHiddenAlias)
	}
}

func (c *phraseCollector) candidate(cand mediawiki.NameCandidate) {
	reason, classified := c.reason(cand)
	if reason.denies() {
		c.deny(cand.PageID, string(cand.Rule), cand.Text, reason)

		return
	}
	c.keep(cand.PageID, string(cand.Rule), cand.Text, reason)
	if classified {
		c.lead(cand)
	}
}

// reason decides on a candidate; classified is whether the classifier made it public.
func (c *phraseCollector) reason(cand mediawiki.NameCandidate) (reason Reason, classified bool) {
	name := cand.Text
	if r, ok := c.known(cand); ok {
		return r, false
	}
	words := folded(name)
	switch {
	case !hasCapitalisedWord(name):
		return ReasonNoCapital, false
	case c.inTitle(withoutPossessive(name, words)):
		return ReasonPageTitle, false
	case len(words) == 1 && cand.Rule != mediawiki.CandidateUploader:
		return ReasonSingleWord, false
	case cand.Rule == mediawiki.CandidateMarker:
		return ReasonLegalMarker, false
	case mediawiki.ClassifyPersonAlias(mediawiki.AliasCandidate{
		Name: name, Source: mediawiki.AliasFromH1Parenthetical,
	}).Public():
		return ReasonPublicAlias, true
	}

	return ReasonUnsure, false
}

// known decides on a candidate that is a placeholder or a name already decided on.
func (c *phraseCollector) known(cand mediawiki.NameCandidate) (Reason, bool) {
	key := mediawiki.FoldName(cand.Text)
	switch {
	case isPlaceholder(cand.Text):
		return ReasonPlaceholder, true
	case c.numbers[key] > 0 && cand.Rule == mediawiki.CandidateMarker:
		return ReasonLegalMarker, true
	case c.numbers[key] > 0:
		return ReasonDenied, true
	case c.published[key]:
		return ReasonPublishedName, true
	case c.public[key]:
		return ReasonPublicAlias, true
	}

	return "", false
}

// denies reports whether a name so decided is a deny phrase.
func (r Reason) denies() bool {
	switch r {
	case ReasonHiddenAlias, ReasonLegalMarker, ReasonUnsure, ReasonDenied:
		return true
	case ReasonPlaceholder, ReasonPublishedName, ReasonPublicAlias, ReasonPageTitle, ReasonNoCapital,
		ReasonSingleWord:
		return false
	}

	return false
}

// lead decides on the words of a candidate before its first lower-case word, the byname or patronymic word that
// made it public ("First Last" of "First Last of Somewhere"): free text may give a legal name before an SCA
// byname, so a lead of two or more words is a deny phrase unless it is public.
func (c *phraseCollector) lead(cand mediawiki.NameCandidate) {
	fields := strings.Fields(cand.Text)
	for k, f := range fields {
		if !hasCapitalisedWord(f) {
			lead := strings.Join(fields[:k], " ")
			key := mediawiki.FoldName(lead)
			words := folded(lead)
			if len(words) < 2 || isPlaceholder(lead) || c.published[key] || c.public[key] || c.inTitle(words) {
				return
			}
			c.deny(cand.PageID, string(cand.Rule)+"_lead", lead, ReasonUnsure)

			return
		}
	}
}

func (c *phraseCollector) keep(pageID int64, rule, name string, reason Reason) {
	c.decisions = append(c.decisions, CandidateDecision{PageID: pageID, Rule: rule, Text: name, Reason: reason})
}

func (c *phraseCollector) deny(pageID int64, rule, name string, reason Reason) {
	key := mediawiki.FoldName(name)
	if c.numbers[key] == 0 {
		c.numbers[key] = len(c.phrases) + 1
		c.phrases = append(c.phrases, denyPhrase{text: name, number: c.numbers[key]})
	}
	if reason == ReasonHiddenAlias || reason == ReasonLegalMarker {
		c.phrases[c.numbers[key]-1].name = true
	}
	c.decisions = append(c.decisions, CandidateDecision{
		PageID: pageID, Rule: rule, Text: name, Deny: true, Reason: reason,
		Placeholder: placeholderFor(name, nameForm{number: c.numbers[key]}, false),
	})
}

// inTitle reports whether words are consecutive words of a page title.
func (c *phraseCollector) inTitle(words []string) bool {
	if len(words) == 0 {
		return false
	}
	for _, title := range c.titles {
		for start := 0; start+len(words) <= len(title); start++ {
			if equalWords(title[start:start+len(words)], words) {
				return true
			}
		}
	}

	return false
}

func equalWords(a, b []string) bool {
	for k := range a {
		if a[k] != b[k] {
			return false
		}
	}

	return true
}

func folded(text string) []string {
	var out []string
	for _, w := range mediawiki.Words(text) {
		out = append(out, w.Folded)
	}

	return out
}

// withoutPossessive drops the "s" of a final "'s" from words, the folded words of name.
func withoutPossessive(name string, words []string) []string {
	if strings.HasSuffix(name, "'s") || strings.HasSuffix(name, "’s") {
		return words[:len(words)-1]
	}

	return words
}

func hasCapitalisedWord(text string) bool {
	for _, w := range strings.Fields(text) {
		if r, _ := utf8.DecodeRuneInString(w); unicode.IsUpper(r) {
			return true
		}
	}

	return false
}
