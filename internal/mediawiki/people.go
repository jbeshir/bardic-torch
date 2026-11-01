package mediawiki

import (
	"slices"
	"sort"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// CreditSignal is a way the wiki says who wrote a piece.
type CreditSignal string

// The credit signals, strongest first.
const (
	// SignalInfobox is a credit of the piece's infobox.
	SignalInfobox CreditSignal = "infobox"
	// SignalCategory is the piece's category named after the person.
	SignalCategory CreditSignal = "category"
	// SignalWorksList is the person's bard page listing the piece among their works.
	SignalWorksList CreditSignal = "works_list"
)

// PieceCredit is a credit of an imported piece: the credit, the person it names (empty unless it is named) and the
// signals behind it.
type PieceCredit struct {
	CreditRecord
	Person  PersonRef
	Signals []CreditSignal
}

// CreditDisagreementKind is a way the credit signals of a piece disagree.
type CreditDisagreementKind string

// The credit disagreement kinds.
const (
	// DisagreementMissingBardCategory: the infobox links a name (Name) that is not one of the piece's categories
	// (the reference's missing_bard_category).
	DisagreementMissingBardCategory CreditDisagreementKind = "missing_bard_category"
	// DisagreementCategoryWithoutLinkedCredit: a bard-name category of the piece (Name) is not the target of any
	// infobox link (the reference's bard_category_without_infobox_link).
	DisagreementCategoryWithoutLinkedCredit CreditDisagreementKind = "category_without_linked_credit"
	// DisagreementCategoryOnly: the category's person has no infobox credit, so a credit is added (role lyrics,
	// certainty possible).
	DisagreementCategoryOnly CreditDisagreementKind = "category_only"
	// DisagreementWorksListOnly: the bard lists the piece but has no infobox credit, so a credit is added (role
	// lyrics, certainty possible).
	DisagreementWorksListOnly CreditDisagreementKind = "works_list_only"
	// DisagreementRelatedPerson: the category or works list is the person's, who is related (heir_of, same_as) to
	// a credited person, so no credit is added (March of Cambreadth's category for the heir).
	DisagreementRelatedPerson CreditDisagreementKind = "related_person"
	// DisagreementNotListed: the piece credits a bard whose works list does not list it.
	DisagreementNotListed CreditDisagreementKind = "credited_not_listed"
)

// CreditDisagreement is one disagreement between the credit signals of a piece, for review: the piece's title,
// the person concerned (if any) and the name as the signal writes it.
type CreditDisagreement struct {
	Kind   CreditDisagreementKind
	Piece  string
	Person PersonRef
	Name   string
}

// StubPiece is a work a bard page lists that has no page: imported as a stub song credited to each bard listing it
// (role lyrics, certainty possible). CaseVariantOf is a page whose title differs only by letter case: a different
// page for MediaWiki, so a separate stub, listed as ambiguous.
type StubPiece struct {
	Title         string
	Credits       []PieceCredit
	CaseVariantOf string
}

// People are the persons of the wiki and the credits of its pieces: the bard pages, the stub persons for the
// names that match none, every piece's credits after the union of the credit signals, the stub pieces for the
// works without pages, the disagreements between the signals and the red links classified.
type People struct {
	Bards         []BardRecord
	Persons       Persons
	Credits       map[int64][]PieceCredit
	StubPieces    []StubPiece
	Disagreements []CreditDisagreement
	RedLinks      []RedLink
}

// ParsePeople parses the bard pages and matches every person the pieces and bard pages name: infobox credits,
// bard-name categories, relations and the red links naming persons in prose. Each piece's credits are then the
// union of three signals: its infobox's credits, its bard-name categories and the bard pages listing it. A
// category or a works list naming a person the infobox does not credit adds a credit (role lyrics, certainty
// possible), unless the person is related to a credited person.
func ParsePeople(s Site, pieces []PieceRecord) People {
	bards := s.Bards()
	redLinks := ClassifyRedLinks(s, pieces, bards)
	persons := ResolvePersons(NewPersonIndex(bards), personMentions(pieces, bards, redLinks), s)
	u := creditUnion{site: s, bards: bards, persons: persons, related: relatedPersons(bards, persons)}
	p := People{Bards: bards, Persons: persons, Credits: map[int64][]PieceCredit{}, RedLinks: redLinks}
	listers := u.listers()
	for _, piece := range pieces {
		p.Credits[piece.PageID] = u.pieceCredits(piece, listers[piece.Title])
		p.Disagreements = append(p.Disagreements, u.disagreements(piece, p.Credits[piece.PageID], listers[piece.Title])...)
	}
	p.StubPieces = u.stubPieces()

	return p
}

// personMentions are the persons the pages name, in a fixed order: each piece's infobox credits and bard-name
// categories, the bards' relations, and the red links naming persons in prose.
func personMentions(pieces []PieceRecord, bards []BardRecord, redLinks []RedLink) []PersonMention {
	var out []PersonMention
	for _, p := range pieces {
		for _, c := range p.Credits {
			if c.Attribution == domain.AttributionNamed {
				out = append(out, PersonMention{Name: c.Name, Page: p.Title})
			}
		}
		for _, c := range p.BardNameCategories {
			out = append(out, PersonMention{Name: PersonName{Name: c, Form: NameCategory}, Page: p.Title})
		}
	}
	for _, b := range bards {
		for _, r := range b.Relations {
			out = append(out, PersonMention{Name: relationName(r), Page: b.Title})
		}
	}

	return append(out, personRedLinkMentions(redLinks)...)
}

func relationName(r PersonRelation) PersonName {
	return PersonName{Name: r.To, Form: NamePageLink}
}

// relatedPersons maps each person to those a bard page relates them to, both ways.
func relatedPersons(bards []BardRecord, persons Persons) map[PersonRef][]PersonRef {
	out := map[PersonRef][]PersonRef{}
	for _, b := range bards {
		for _, r := range b.Relations {
			if to, ok := persons.Resolve(relationName(r)); ok {
				out[PersonRef(b.Title)] = append(out[PersonRef(b.Title)], to)
				out[to] = append(out[to], PersonRef(b.Title))
			}
		}
	}

	return out
}

type creditUnion struct {
	site    Site
	bards   []BardRecord
	persons Persons
	related map[PersonRef][]PersonRef
}

// listers maps each work title a bard page lists, read through redirects, to the bards listing it, in page order.
func (u creditUnion) listers() map[string][]PersonRef {
	out := map[string][]PersonRef{}
	for _, b := range u.bards {
		for _, w := range b.Works {
			if w.Exists && !slices.Contains(out[w.Target], PersonRef(b.Title)) {
				out[w.Target] = append(out[w.Target], PersonRef(b.Title))
			}
		}
	}

	return out
}

// signalCredit is the credit a category or works list adds for a person the infobox does not credit.
func signalCredit(person PersonRef, name PersonName, signal CreditSignal) PieceCredit {
	return PieceCredit{
		CreditRecord: CreditRecord{Role: domain.CreditRoleLyrics, Attribution: domain.AttributionNamed,
			Certainty: domain.CertaintyPossible, Name: name},
		Person: person, Signals: []CreditSignal{signal},
	}
}

// pieceCredits are a piece's infobox credits with their persons, then the credits its categories and the bards
// listing it add.
func (u creditUnion) pieceCredits(piece PieceRecord, listers []PersonRef) []PieceCredit {
	var out []PieceCredit
	for _, c := range piece.Credits {
		pc := PieceCredit{CreditRecord: c, Signals: []CreditSignal{SignalInfobox}}
		if c.Attribution == domain.AttributionNamed {
			pc.Person, _ = u.persons.Resolve(c.Name)
		}
		out = append(out, pc)
	}
	for _, c := range piece.BardNameCategories {
		name := PersonName{Name: c, Form: NameCategory}
		if person, ok := u.persons.Resolve(name); ok {
			out = u.addSignal(out, person, name, SignalCategory)
		}
	}
	for _, b := range listers {
		out = u.addSignal(out, b, PersonName{Name: string(b), Form: NamePageLink}, SignalWorksList)
	}

	return out
}

// addSignal returns the credits with a signal for person: added to the person's credits, or as a credit of its
// own unless the person is related to a credited one.
func (u creditUnion) addSignal(
	credits []PieceCredit, person PersonRef, name PersonName, signal CreditSignal,
) []PieceCredit {
	out := slices.Clone(credits)
	credited := false
	for k, c := range out {
		if c.Person == person {
			credited = true
			if !slices.Contains(c.Signals, signal) {
				out[k].Signals = append(slices.Clone(c.Signals), signal)
			}
		}
	}
	if credited || u.relatedToCredited(out, person) {
		return out
	}

	return append(out, signalCredit(person, name, signal))
}

func (u creditUnion) relatedToCredited(credits []PieceCredit, person PersonRef) bool {
	return slices.ContainsFunc(credits, func(c PieceCredit) bool {
		return c.Person != "" && slices.Contains(u.related[person], c.Person)
	})
}

// disagreements lists how a piece's signals disagree: the reference's missing and unlinked bard categories, the
// credits a category or works list added or would have added for a related person, and the credited bards not
// listing the piece.
func (u creditUnion) disagreements(piece PieceRecord, credits []PieceCredit, listers []PersonRef) []CreditDisagreement {
	d := disagreementList{piece: piece.Title}
	u.referenceDisagreements(&d, piece)
	for _, c := range credits {
		switch {
		case slices.Contains(c.Signals, SignalInfobox):
			if u.isBard(c.Person) && !u.lists(c.Person, piece.Title) {
				d.add(DisagreementNotListed, c.Person, c.Name.Name)
			}
		case c.Signals[0] == SignalCategory:
			d.add(DisagreementCategoryOnly, c.Person, c.Name.Name)
		default:
			d.add(DisagreementWorksListOnly, c.Person, c.Name.Name)
		}
	}
	credited := func(person PersonRef) bool {
		return slices.ContainsFunc(credits, func(pc PieceCredit) bool { return pc.Person == person })
	}
	for _, c := range piece.BardNameCategories {
		if person, ok := u.persons.Resolve(PersonName{Name: c, Form: NameCategory}); ok && !credited(person) {
			d.add(DisagreementRelatedPerson, person, c)
		}
	}
	for _, b := range listers {
		if !credited(b) {
			d.add(DisagreementRelatedPerson, b, string(b))
		}
	}

	return d.list
}

// referenceDisagreements adds the reference's missing_bard_category and bard_category_without_infobox_link.
func (u creditUnion) referenceDisagreements(d *disagreementList, piece PieceRecord) {
	targets := u.linkedTargets(piece)
	for _, t := range targets.pages {
		if !slices.Contains(piece.Categories, t) {
			person, _ := u.persons.Resolve(PersonName{Name: t, Form: NamePageLink})
			d.add(DisagreementMissingBardCategory, person, t)
		}
	}
	for _, c := range piece.BardNameCategories {
		if !slices.Contains(targets.all, c) {
			person, _ := u.persons.Resolve(PersonName{Name: c, Form: NameCategory})
			d.add(DisagreementCategoryWithoutLinkedCredit, person, c)
		}
	}
}

// disagreementList collects one piece's disagreements, each once.
type disagreementList struct {
	piece string
	list  []CreditDisagreement
}

func (d *disagreementList) add(kind CreditDisagreementKind, person PersonRef, name string) {
	item := CreditDisagreement{Kind: kind, Piece: d.piece, Person: person, Name: name}
	if !slices.Contains(d.list, item) {
		d.list = append(d.list, item)
	}
}

// linkedTargets are the targets of a piece's infobox person parameters' links, read through redirects: page
// links (pages) and page and category links (all), as the reference compares them with the categories.
type linkedTargets struct {
	pages []string
	all   []string
}

func (u creditUnion) linkedTargets(piece PieceRecord) linkedTargets {
	var t linkedTargets
	for _, b := range piece.Parsed.Infoboxes {
		for _, param := range personParams {
			p, ok := b.Param(param.name)
			if !ok {
				continue
			}
			for _, it := range p.Value.Items {
				switch it.Type {
				case ValuePageLink:
					t.pages = appendUnique(t.pages, u.site.ResolveRedirect(it.Target))
					t.all = appendUnique(t.all, u.site.ResolveRedirect(it.Target))
				case ValueCategoryLink, ValueCategoryTag:
					t.all = appendUnique(t.all, it.Target)
				case ValueExternalLink, ValueInterwiki, ValueText:
				}
			}
		}
	}

	return t
}

func (u creditUnion) isBard(person PersonRef) bool {
	return slices.ContainsFunc(u.bards, func(b BardRecord) bool { return PersonRef(b.Title) == person })
}

// lists reports whether the bard's works list lists the piece.
func (u creditUnion) lists(bard PersonRef, piece string) bool {
	for _, b := range u.bards {
		if PersonRef(b.Title) == bard {
			return slices.ContainsFunc(b.Works, func(w WorkEntry) bool { return w.Target == piece })
		}
	}

	return false
}

// stubPieces are the works the bard pages list that have no page, by title, each credited to the bards listing
// it.
func (u creditUnion) stubPieces() []StubPiece {
	var out []StubPiece
	for _, b := range u.bards {
		for _, w := range b.Works {
			if w.Exists {
				continue
			}
			k := slices.IndexFunc(out, func(sp StubPiece) bool { return sp.Title == w.Title })
			if k < 0 {
				sp := StubPiece{Title: w.Title}
				sp.CaseVariantOf, _ = u.site.caseVariantOf(w.Title)
				out = append(out, sp)
				k = len(out) - 1
			}
			name := PersonName{Name: b.Title, Form: NamePageLink}
			if !slices.ContainsFunc(out[k].Credits, func(c PieceCredit) bool { return c.Person == PersonRef(b.Title) }) {
				out[k].Credits = append(out[k].Credits, signalCredit(PersonRef(b.Title), name, SignalWorksList))
			}
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Title < out[b].Title })

	return out
}

// AmbiguityKind is a kind of record the importer cannot decide (PLAN §2.9): it imports its best guess and lists it.
type AmbiguityKind string

// The ambiguity kinds.
const (
	// AmbiguityCanonicalName: a bard page's H1 lead names the bard otherwise than the page title ("Lead (Title)").
	// The guess: the title is the name, the lead an alias.
	AmbiguityCanonicalName AmbiguityKind = "canonical_name"
	// AmbiguityFuzzyPerson: a name matched a person fuzzily. The guess: that person; the alternative: a person of
	// its own.
	AmbiguityFuzzyPerson AmbiguityKind = "fuzzy_person"
	// AmbiguityManyPersons: a name matched more than one person. The guess: a stub person of its own; the
	// alternatives: the candidates.
	AmbiguityManyPersons AmbiguityKind = "many_persons"
	// AmbiguityCaseVariantWork: a listed work differs from an existing page only by letter case. The guess: a
	// separate stub piece; the alternative: the existing piece.
	AmbiguityCaseVariantWork AmbiguityKind = "case_variant_work"
)

// Ambiguity is one ambiguous record: the page it is on, the name or title as written, the importer's guess and
// the alternatives.
type Ambiguity struct {
	Kind         AmbiguityKind
	Page         string
	Name         string
	Guess        string
	Alternatives []string
}

// Ambiguities are the ambiguous records among the people: bards' canonical names, fuzzy and many-person matches
// and works that are case variants of existing pages.
func (p People) Ambiguities() []Ambiguity {
	var out []Ambiguity
	for _, b := range p.Bards {
		if a, ok := b.H1LeadAlias(); ok {
			out = append(out, Ambiguity{Kind: AmbiguityCanonicalName, Page: b.Title, Name: a.Name, Guess: b.Title,
				Alternatives: []string{a.Name}})
		}
	}
	for _, m := range p.Persons.Matches {
		switch m.Match.Kind {
		case MatchFuzzy:
			out = append(out, Ambiguity{Kind: AmbiguityFuzzyPerson, Page: m.Mention.Page, Name: m.Mention.Name.Name,
				Guess: string(m.Match.Person), Alternatives: []string{m.Mention.Name.Name}})
		case MatchNone:
			ref, _ := p.Persons.Resolve(m.Mention.Name)
			alternatives := make([]string, 0, len(m.Match.Candidates))
			for _, c := range m.Match.Candidates {
				alternatives = append(alternatives, string(c))
			}
			out = append(out, Ambiguity{Kind: AmbiguityManyPersons, Page: m.Mention.Page, Name: m.Mention.Name.Name,
				Guess: string(ref), Alternatives: alternatives})
		case MatchExact:
		}
	}
	for _, sp := range p.StubPieces {
		if sp.CaseVariantOf != "" {
			out = append(out, Ambiguity{Kind: AmbiguityCaseVariantWork, Page: string(sp.Credits[0].Person), Name: sp.Title,
				Guess:        sp.Title,
				Alternatives: []string{sp.CaseVariantOf}})
		}
	}

	return out
}
