package mediawiki

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// PersonRef is an imported person, by name: a bard's page title or a stub person's name. No two persons' names
// fold (FoldName) alike.
type PersonRef string

// IndexKeyKind is what a name matched in a PersonIndex: a bard's page title, an alias of a bard
// (redirects included), or a stub person's name.
type IndexKeyKind string

// The index key kinds, strongest first.
const (
	KeyTitle IndexKeyKind = "title"
	KeyStub  IndexKeyKind = "stub"
	KeyAlias IndexKeyKind = "alias"
)

var keyStrength = []IndexKeyKind{KeyTitle, KeyStub, KeyAlias}

// MatchKind is how a name matched a person.
type MatchKind string

// The match kinds.
const (
	// MatchExact: the name folds to exactly one person's key.
	MatchExact MatchKind = "exact"
	// MatchFuzzy: the name is near exactly one person's key (MatchRule), listed for review.
	MatchFuzzy MatchKind = "fuzzy"
	// MatchNone: no person, or more than one (Candidates), matches; the name becomes a stub person.
	MatchNone MatchKind = "none"
)

// MatchRule is the rule a fuzzy match, or an ambiguous name's candidates, came from.
type MatchRule string

// The fuzzy match rules.
const (
	// RuleEditDistance: the folded name is at least MinFuzzyNameLength runes long and one edit (an insertion, a
	// deletion, a substitution or a swap of neighbours) from a key ("Marian of Heatherdale", "Marion …"). Names
	// with digits ("Legal Name 001") are never matched so: their digits tell them apart.
	RuleEditDistance MatchRule = "edit_distance"
	// RuleFirstWord: the name is one word, the first word of a person's name ("Dunstan").
	RuleFirstWord MatchRule = "first_word"
)

// MinFuzzyNameLength is the shortest folded name, in runes, matched by edit distance.
const MinFuzzyNameLength = 8

// PersonMatch is the result of matching a name: the person and the key kind it matched (Via), or, for a name
// matching more than one person, their Candidates and no Person.
type PersonMatch struct {
	Kind       MatchKind
	Rule       MatchRule
	Person     PersonRef
	Via        IndexKeyKind
	Candidates []PersonRef
}

type indexKey struct {
	fold   string
	person PersonRef
	kind   IndexKeyKind
}

// PersonIndex finds persons by name: bards by their page titles and their aliases, and stub persons by their
// names. It is immutable: adding stubs returns a new index.
type PersonIndex struct {
	keys []indexKey
}

// NewPersonIndex indexes bards by their titles and aliases.
func NewPersonIndex(bards []BardRecord) PersonIndex {
	var x PersonIndex
	for _, b := range bards {
		ref := PersonRef(b.Title)
		x.keys = append(x.keys, indexKey{fold: FoldName(b.Title), person: ref, kind: KeyTitle})
		for _, a := range b.Aliases {
			x.keys = append(x.keys, indexKey{fold: FoldName(a.Name), person: ref, kind: KeyAlias})
		}
	}

	return x
}

// withStubs returns a new index that also finds stubs by their names.
func (x PersonIndex) withStubs(stubs []StubPerson) PersonIndex {
	out := PersonIndex{keys: slices.Clone(x.keys)}
	for _, s := range stubs {
		out.keys = append(out.keys, indexKey{fold: FoldName(s.Name), person: PersonRef(s.Name), kind: KeyStub})
	}

	return out
}

// Match matches a name to a person: exactly, by FoldName, to a key; otherwise fuzzily, by one edit to a key (for
// a folded name of at least MinFuzzyNameLength runes) or, for a one-word name, as the first word of a person's
// name. A name that exactly or fuzzily matches more than one person matches none, and lists them as candidates.
func (x PersonIndex) Match(name string) PersonMatch {
	fold := FoldName(name)
	if fold == "" {
		return PersonMatch{Kind: MatchNone}
	}
	exact := x.matching(func(k indexKey) bool { return k.fold == fold })
	if len(exact) > 0 {
		return x.result(MatchExact, "", exact)
	}
	if utf8.RuneCountInString(fold) >= MinFuzzyNameLength && !strings.ContainsFunc(fold, unicode.IsDigit) {
		near := x.matching(func(k indexKey) bool {
			return !strings.ContainsFunc(k.fold, unicode.IsDigit) && withinOneEdit(k.fold, fold)
		})
		if len(near) > 0 {
			return x.result(MatchFuzzy, RuleEditDistance, near)
		}
	}
	if !strings.Contains(fold, " ") {
		first := x.matching(func(k indexKey) bool {
			return (k.kind == KeyTitle || k.kind == KeyStub) && strings.HasPrefix(k.fold, fold+" ")
		})
		if len(first) > 0 {
			return x.result(MatchFuzzy, RuleFirstWord, first)
		}
	}

	return PersonMatch{Kind: MatchNone}
}

// matching returns the keys that match, the strongest key of each person first.
func (x PersonIndex) matching(match func(indexKey) bool) []indexKey {
	var out []indexKey
	for _, k := range x.keys {
		if match(k) {
			out = append(out, k)
		}
	}
	slices.SortStableFunc(out, func(a, b indexKey) int {
		return slices.Index(keyStrength, a.kind) - slices.Index(keyStrength, b.kind)
	})

	return out
}

func (x PersonIndex) result(kind MatchKind, rule MatchRule, keys []indexKey) PersonMatch {
	var persons []PersonRef
	for _, k := range keys {
		if !slices.Contains(persons, k.person) {
			persons = append(persons, k.person)
		}
	}
	if len(persons) > 1 {
		slices.Sort(persons)

		return PersonMatch{Kind: MatchNone, Rule: rule, Candidates: persons}
	}
	m := PersonMatch{Kind: kind, Rule: rule, Person: keys[0].person, Via: keys[0].kind}
	if kind == MatchFuzzy {
		m.Candidates = persons
	}

	return m
}

// withinOneEdit reports whether a and b are at most one insertion, deletion, substitution or swap of neighbouring
// runes apart.
func withinOneEdit(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	if len(ra) < len(rb) {
		ra, rb = rb, ra
	}
	if len(ra)-len(rb) > 1 {
		return false
	}
	k := 0
	for k < len(rb) && ra[k] == rb[k] {
		k++
	}
	if k == len(rb) {
		return true
	}
	if len(ra) > len(rb) {
		return slices.Equal(ra[k+1:], rb[k:])
	}
	if slices.Equal(ra[k+1:], rb[k+1:]) {
		return true
	}

	return k+1 < len(ra) && ra[k] == rb[k+1] && ra[k+1] == rb[k] && slices.Equal(ra[k+2:], rb[k+2:])
}

// PersonMention is a person named on a page: in a credit, by a bard-name category or by a red link.
type PersonMention struct {
	Name PersonName
	Page string
}

// StubPerson is a person the wiki names without a page, imported as a public stub. Kind is a guess, listed for
// review: group for a name like a band's, historical for a person named by an external link, otherwise a
// community bard. ProfileURLs are the external links naming the person (imported with role external_profile),
// LegacyTitle the page title the wiki links to for them, if it does, and Mentions where the wiki names them.
type StubPerson struct {
	Name        string
	Kind        domain.PersonKind
	ProfileURLs []string
	LegacyTitle string
	Mentions    []PersonMention
}

// MentionMatch is a mention matched otherwise than exactly to a bard's title or public alias or to a stub, for
// review: fuzzily, to a hidden alias, or to more than one person (its stub is then the mention's own).
type MentionMatch struct {
	Mention PersonMention
	Match   PersonMatch
}

// Persons are the persons the mentions name: bards and the stub persons created for the names that match no
// bard, with the mentions matched to them.
type Persons struct {
	Index    PersonIndex
	Stubs    []StubPerson
	Matches  []MentionMatch
	resolved map[PersonName]PersonRef
}

// Resolve returns the person a name, as mentioned, was matched to or created as.
func (p Persons) Resolve(n PersonName) (PersonRef, bool) {
	ref, ok := p.resolved[n]

	return ref, ok
}

var groupName = regexp.MustCompile(`(?i)\b(?:band|ensemble|consort|choir|chorus|quartet|trio|duo|players|waits|` +
	`company|brothers|sisters)\b|&|\band\b`)

// ResolvePersons matches every mention to a person (PersonIndex.Match), creating a stub person for each name that
// matches none, one per folded name. Linked names (links and categories) are matched first, to the bards, and
// text names then, to the bards and the stubs the linked names created, so that "Dunstan" finds the stub of a
// linked "Dunstan le Placeholder".
func ResolvePersons(index PersonIndex, mentions []PersonMention, s Site) Persons {
	p := Persons{Index: index, resolved: map[PersonName]PersonRef{}}
	var names []string
	for _, linked := range []bool{true, false} {
		var created []StubPerson
		for _, m := range mentions {
			if m.Name.linked() == linked && FoldName(m.Name.Name) != "" {
				created = resolveMention(&p, m, created)
			}
		}
		p.Index = p.Index.withStubs(created)
		for _, st := range created {
			names = append(names, st.Name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		p.Stubs = append(p.Stubs, stubPerson(name, mentions, p.resolved, s))
	}

	return p
}

// resolveMention matches one mention, returning the stubs created so far with one for it if it names no person
// of the index or of them.
func resolveMention(p *Persons, m PersonMention, created []StubPerson) []StubPerson {
	if _, ok := p.resolved[m.Name]; ok {
		return created
	}
	match := p.Index.Match(m.Name.Name)
	if match.Kind == MatchFuzzy || len(match.Candidates) > 1 {
		p.Matches = append(p.Matches, MentionMatch{Mention: m, Match: match})
	}
	if match.Kind != MatchNone {
		p.resolved[m.Name] = match.Person

		return created
	}
	fold := FoldName(m.Name.Name)
	k := slices.IndexFunc(created, func(st StubPerson) bool { return FoldName(st.Name) == fold })
	if k < 0 {
		created = append(created, StubPerson{Name: m.Name.Name})
		k = len(created) - 1
	}
	p.resolved[m.Name] = PersonRef(created[k].Name)

	return created
}

// stubPerson is the stub person name with the mentions resolved to it: its kind guess, profile links and legacy
// title come from them.
func stubPerson(name string, mentions []PersonMention, resolved map[PersonName]PersonRef, s Site) StubPerson {
	st := StubPerson{Name: name, Kind: domain.PersonKindCommunityBard}
	for _, m := range mentions {
		if resolved[m.Name] != PersonRef(name) {
			continue
		}
		st.Mentions = append(st.Mentions, m)
		switch {
		case groupName.MatchString(m.Name.Name):
			st.Kind = domain.PersonKindGroup
		case m.Name.Form == NameExternalLink && st.Kind == domain.PersonKindCommunityBard:
			st.Kind = domain.PersonKindHistorical
		}
		if m.Name.URL != "" && !slices.Contains(st.ProfileURLs, m.Name.URL) {
			st.ProfileURLs = append(st.ProfileURLs, m.Name.URL)
		}
		redLink := m.Name.Form == NamePageLink || m.Name.Form == NameRedLink
		if st.LegacyTitle == "" && redLink && !s.HasPage(m.Name.Name) {
			st.LegacyTitle = m.Name.Name
		}
	}

	return st
}
