package mediawiki

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// PublicNames are names that docs/PLAN.md already publishes (a bard's name, and the wiki's own name) but that the
// classifier would hide. They are never private names. TestPublicNamesArePublishedInPlan (internal/scrub) keeps the
// list to names the plan holds, so it cannot hold a new secret.
var PublicNames = []string{"Ambra Michelli", "Bardic Torch"}

// PrivacyReason is why a possible person name of the wiki is or is not a private name.
type PrivacyReason string

// The reasons. A name is private for the first five; the others make it public.
const (
	// PrivacyHiddenAlias: a bard's alias, or a redirect to a bard, that the classifier does not make public.
	PrivacyHiddenAlias PrivacyReason = "hidden_alias"
	// PrivacyLegalMarker: the name follows "legal name", "mundane name", "aka" or "a.k.a.".
	PrivacyLegalMarker PrivacyReason = "legal_marker"
	// PrivacyUnsure: the classifier cannot say confidently that the name is not a legal name (fail closed).
	PrivacyUnsure PrivacyReason = "unsure"
	// PrivacyRepeated: the name is already a private name.
	PrivacyRepeated PrivacyReason = "denied"
	// PrivacyPairedName: the name is paired by "/" with a public name (CandidateSlashPair), so it is another name
	// of a person who has a public one: a legal name.
	PrivacyPairedName PrivacyReason = "paired_name"

	// PrivacyPlaceholder: the name is a placeholder of the scrubbed copy already (IsPlaceholderName).
	PrivacyPlaceholder PrivacyReason = "placeholder"
	// PrivacyPublishedName: the name is in PublicNames.
	PrivacyPublishedName PrivacyReason = "published_name"
	// PrivacyPublicAlias: the classifier makes the name public, or it is a bard's public alias.
	PrivacyPublicAlias PrivacyReason = "public_alias"
	// PrivacyPageTitle: the name's words are words of a page title, category name or SCA kingdom, which are public.
	PrivacyPageTitle PrivacyReason = "page_title"
	// PrivacyNoCapital: no word of the text is capitalised, so it is not a name ("the author").
	PrivacyNoCapital PrivacyReason = "no_capital"
	// PrivacySingleWord: a single word that is not an uploader (an H1 or parenthesis "(Song)", "(Poem)").
	PrivacySingleWord PrivacyReason = "single_word"
	// PrivacyTitlePair: the name is paired by "/" with the title of a page that is not a bard's, so it is an
	// alternate title ("Song Title / Other Spelling").
	PrivacyTitlePair PrivacyReason = "title_pair"
)

// Private reports whether a name so decided is a private name.
func (r PrivacyReason) Private() bool {
	switch r {
	case PrivacyHiddenAlias, PrivacyLegalMarker, PrivacyUnsure, PrivacyRepeated, PrivacyPairedName:
		return true
	case PrivacyPlaceholder, PrivacyPublishedName, PrivacyPublicAlias, PrivacyPageTitle, PrivacyNoCapital,
		PrivacySingleWord, PrivacyTitlePair:
		return false
	}

	return false
}

// PrivacyDecision is the decision on one possible person name of the wiki: found on the page PageID (0 for a
// redirect) by Rule (an AliasSource, a CandidateRule, or RuleRedirect). Text is a name of the wiki, which may be a
// legal name, so decisions are only written to private lists, never to the repository or to logs.
type PrivacyDecision struct {
	PageID  int64
	Rule    string
	Text    string
	Private bool
	Reason  PrivacyReason
}

// RuleRedirect is the rule of a redirect title that resolves to a bard.
const RuleRedirect = "redirect"

// PrivateName is a name no public text may hold. A legal name (a hidden alias or a name after a marker) is found
// in every form PrivateNameMatcher knows; any other private name, which may be words that are not a name at all,
// only as its words in order.
type PrivateName struct {
	Text  string
	Legal bool
}

// PrivateNames decides on every possible person name of pages (sorted by page ID) and of redirects: first each
// bard's aliases (BardAliases) and each redirect to a bard, classified with ClassifyPersonAlias; then every
// PersonNameCandidates candidate, which is private unless it is a placeholder, a published name, a public alias,
// words of a page title, has no capitalised word, is a single word not given as an uploader, classifies
// confidently as public, or is paired by "/" with the title of a page that is not a bard's (an alternate title). A
// name after a marker is a legal name, and so is a name paired by "/" with a person's public name: the other name
// of someone who has a public one. The private names are returned in the order they are
// found, each once (by FoldName), with every decision taken. The scrubber replaces them in the committed copy and
// the importer withholds them from public text, so both agree on what is private.
func PrivateNames(pages []Page, redirects []Redirect) ([]PrivateName, []PrivacyDecision) {
	c := privacyCollector{public: map[string]bool{}, published: map[string]bool{}, numbers: map[string]int{},
		bards: map[string]bool{}, otherTitles: map[string]bool{}}
	for _, name := range PublicNames {
		c.published[domain.FoldName(name)] = true
	}
	maxYear := LatestYear(pages)
	for kingdom := range scaKingdoms {
		c.titles = append(c.titles, foldedWords(kingdom))
	}
	for _, p := range pages {
		c.titles = append(c.titles, foldedWords(strings.TrimPrefix(p.Title, categoryPrefix)))
		if TypePage(p) != PageTypeBard {
			c.otherTitles[NormTitle(p.Title)] = true

			continue
		}
		c.bards[NormTitle(p.Title)] = true
		for _, a := range BardAliases(p) {
			c.alias(p.PageID, string(a.Source), a.Name, ClassifyPersonAlias(a))
		}
	}
	for _, r := range redirects {
		if r.NS == NSMain && c.bards[NormTitle(r.To)] {
			a := AliasCandidate{Name: r.From, Source: AliasFromH1Parenthetical}
			c.alias(0, RuleRedirect, r.From, ClassifyPersonAlias(a))
		}
	}
	for _, p := range pages {
		for _, cand := range PersonNameCandidates(p, maxYear) {
			c.candidate(cand)
		}
	}

	return c.names, c.decisions
}

// privacyCollector decides, in a fixed order, which possible names are private, and numbers those.
type privacyCollector struct {
	titles [][]string
	bards  map[string]bool
	// otherTitles are the titles of the pages that are not bards', by NormTitle.
	otherTitles map[string]bool
	public      map[string]bool
	published   map[string]bool
	numbers     map[string]int
	names       []PrivateName
	decisions   []PrivacyDecision
}

func (c *privacyCollector) alias(pageID int64, rule, name string, class AliasClassification) {
	key := domain.FoldName(name)
	switch {
	case IsPlaceholderName(name):
		c.keep(pageID, rule, name, PrivacyPlaceholder)
	case c.published[key]:
		c.keep(pageID, rule, name, PrivacyPublishedName)
	case class.Public():
		c.public[key] = true
		c.keep(pageID, rule, name, PrivacyPublicAlias)
	default:
		c.private(pageID, rule, name, PrivacyHiddenAlias)
	}
}

func (c *privacyCollector) candidate(cand NameCandidate) {
	reason, classified := c.reason(cand)
	if reason.Private() {
		c.private(cand.PageID, string(cand.Rule), cand.Text, reason)

		return
	}
	c.keep(cand.PageID, string(cand.Rule), cand.Text, reason)
	if classified {
		c.lead(cand)
	}
}

// reason decides on a candidate; classified is whether the classifier made it public.
func (c *privacyCollector) reason(cand NameCandidate) (reason PrivacyReason, classified bool) {
	name := cand.Text
	if r, ok := c.known(cand); ok {
		return r, false
	}
	words := foldedWords(name)
	switch {
	case !hasCapitalisedWord(name):
		return PrivacyNoCapital, false
	case c.inTitle(withoutPossessive(name, words)):
		return PrivacyPageTitle, false
	case len(words) == 1 && cand.Rule != CandidateUploader:
		return PrivacySingleWord, false
	case cand.Rule == CandidateMarker:
		return PrivacyLegalMarker, false
	case ClassifyPersonAlias(AliasCandidate{Name: name, Source: AliasFromH1Parenthetical}).Public():
		return PrivacyPublicAlias, true
	case cand.Partner != "" && c.isPublicPerson(cand.Partner):
		return PrivacyPairedName, false
	case cand.Partner != "" && c.otherTitles[NormTitle(cand.Partner)]:
		return PrivacyTitlePair, false
	}

	return PrivacyUnsure, false
}

// isPublicPerson reports whether name is a person's public name: a published name, a bard's page title or public
// alias, or a name the classifier makes public. A piece title is not ("Song Title / Other Title").
func (c *privacyCollector) isPublicPerson(name string) bool {
	key := domain.FoldName(name)

	return c.published[key] || c.public[key] || c.bards[NormTitle(name)] ||
		ClassifyPersonAlias(AliasCandidate{Name: name, Source: AliasFromH1Parenthetical}).Public()
}

// known decides on a candidate that is a placeholder or a name already decided on.
func (c *privacyCollector) known(cand NameCandidate) (PrivacyReason, bool) {
	key := domain.FoldName(cand.Text)
	switch {
	case IsPlaceholderName(cand.Text):
		return PrivacyPlaceholder, true
	case c.numbers[key] > 0 && cand.Rule == CandidateMarker:
		return PrivacyLegalMarker, true
	case c.numbers[key] > 0 && cand.Partner != "" && c.isPublicPerson(cand.Partner):
		return PrivacyPairedName, true
	case c.numbers[key] > 0:
		return PrivacyRepeated, true
	case c.published[key]:
		return PrivacyPublishedName, true
	case c.public[key]:
		return PrivacyPublicAlias, true
	}

	return "", false
}

// lead decides on the words of a candidate before its first lower-case word, the byname or patronymic word that
// made it public ("First Last" of "First Last of Somewhere"): free text may give a legal name before an SCA
// byname, so a lead of two or more words is private unless it is public.
func (c *privacyCollector) lead(cand NameCandidate) {
	fields := strings.Fields(cand.Text)
	for k, f := range fields {
		if !hasCapitalisedWord(f) {
			lead := strings.Join(fields[:k], " ")
			key := domain.FoldName(lead)
			words := foldedWords(lead)
			if len(words) < 2 || IsPlaceholderName(lead) || c.published[key] || c.public[key] || c.inTitle(words) {
				return
			}
			c.private(cand.PageID, string(cand.Rule)+"_lead", lead, PrivacyUnsure)

			return
		}
	}
}

func (c *privacyCollector) keep(pageID int64, rule, name string, reason PrivacyReason) {
	c.decisions = append(c.decisions, PrivacyDecision{PageID: pageID, Rule: rule, Text: name, Reason: reason})
}

func (c *privacyCollector) private(pageID int64, rule, name string, reason PrivacyReason) {
	key := domain.FoldName(name)
	if c.numbers[key] == 0 {
		c.numbers[key] = len(c.names) + 1
		c.names = append(c.names, PrivateName{Text: name})
	}
	if reason == PrivacyHiddenAlias || reason == PrivacyLegalMarker || reason == PrivacyPairedName {
		c.names[c.numbers[key]-1].Legal = true
	}
	c.decisions = append(c.decisions, PrivacyDecision{
		PageID: pageID, Rule: rule, Text: name, Private: true, Reason: reason,
	})
}

// inTitle reports whether words are consecutive words of a page title.
func (c *privacyCollector) inTitle(words []string) bool {
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

// foldedWords are the folded words of text, as NameMatcher reads them.
func foldedWords(text string) []string {
	var out []string
	for _, w := range domain.Words(text) {
		out = append(out, w.Folded)
	}

	return out
}

// withoutPossessive drops the "s" of a final "'s" from words, the folded words of name.
func withoutPossessive(name string, words []string) []string {
	stem, possessive := strings.CutSuffix(name, "s")
	if r, _ := utf8.DecodeLastRuneInString(stem); possessive && domain.IsApostrophe(r) {
		return words[:len(words)-1]
	}

	return words
}

func hasCapitalisedWord(text string) bool {
	return slices.ContainsFunc(strings.Fields(text), isCapitalised)
}

// EmailAddress matches an email address.
var EmailAddress = regexp.MustCompile(`[\p{L}\p{N}._%+-]+@[\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)+`)

// PrivateNameMatcher finds private names in text: a legal name in every form NameMatcher finds names, with its
// last word before its first ("Last First") and with an honorific before its surname or a part of a hyphenated
// surname ("Mr. Last"); any other private name only as its words in order.
type PrivateNameMatcher struct {
	matcher domain.NameMatcher
	forms   []privateNameForm
}

// privateNameForm is one form a PrivateNameMatcher looks for: of the private name numbered name, whether it has
// several words and whether it is an honorific form.
type privateNameForm struct {
	name      int
	multiword bool
	honorific bool
}

// PrivateNameMatch is one occurrence of a private name: text[Start:End] is a form of names[Name]. Multiword is
// whether the form has several words, and Honorific whether it is an honorific with the surname.
type PrivateNameMatch struct {
	Name      int
	Start     int
	End       int
	Multiword bool
	Honorific bool
}

// NewPrivateNameMatcher returns a matcher for names.
func NewPrivateNameMatcher(names []PrivateName) PrivateNameMatcher {
	var legal, others []string
	var legalForms, otherForms []privateNameForm
	for k, p := range names {
		multiword := len(domain.Words(p.Text)) > 1
		if !p.Legal {
			others = append(others, p.Text)
			otherForms = append(otherForms, privateNameForm{name: k, multiword: multiword})

			continue
		}
		add := func(text string, form privateNameForm) {
			legal = append(legal, text)
			legalForms = append(legalForms, form)
		}
		add(p.Text, privateNameForm{name: k, multiword: multiword})
		words := strings.Fields(p.Text)
		if len(words) >= 2 {
			add(words[len(words)-1]+" "+words[0], privateNameForm{name: k, multiword: true})
		}
		if len(words) == 0 {
			continue
		}
		surnames := []string{words[len(words)-1]}
		if parts := strings.Split(surnames[0], "-"); len(parts) > 1 {
			surnames = append(surnames, parts...)
		}
		for _, h := range honorifics {
			for _, surname := range surnames {
				if len(domain.Words(surname)) > 0 {
					add(h+" "+surname, privateNameForm{name: k, multiword: true, honorific: true})
				}
			}
		}
	}

	return PrivateNameMatcher{matcher: domain.NewNameMatcher(legal, others), forms: append(legalForms, otherForms...)}
}

// Find returns the occurrences of the private names in text, sorted by position and not overlapping, as
// NameMatcher.Find.
func (m PrivateNameMatcher) Find(text string) []PrivateNameMatch {
	found := m.matcher.Find(text)
	out := make([]PrivateNameMatch, 0, len(found))
	for _, f := range found {
		form := m.forms[f.Name]
		out = append(out, PrivateNameMatch{Name: form.name, Start: f.Start, End: f.End, Multiword: form.multiword,
			Honorific: form.honorific})
	}

	return out
}
