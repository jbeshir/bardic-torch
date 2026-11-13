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

// StubPiece is a work a bard page lists that has no page: imported as a stub song credited to each bard listing it
// (role lyrics, certainty possible). A work whose title differs from a page only by letter case is a different page
// for MediaWiki, so a separate stub.
type StubPiece struct {
	Title   string
	Credits []PieceCredit
}

// People are the persons of the wiki and the credits of its pieces: the bard pages, the stub persons for the
// names that match none, every piece's credits after the union of the credit signals, the stub pieces for the
// works without pages.
type People struct {
	Bards      []BardRecord
	Persons    Persons
	Credits    map[int64][]PieceCredit
	StubPieces []StubPiece
}

// ParsePeople parses the bard pages and matches every person the pieces and bard pages name: infobox credits,
// bard-name categories, the persons bards are heir of, the red links naming persons in prose and the performers of
// performance items.
// Each piece's credits are then the union of three signals: its infobox's credits, its bard-name categories and the
// bard pages listing it. A category or a works list naming a person the infobox does not credit adds a credit
// (role lyrics, certainty possible), unless the person is related to a credited person.
func ParsePeople(s Site, pieces []PieceRecord) People {
	bards := s.Bards()
	redLinks := ClassifyRedLinks(s, pieces, bards)
	persons := ResolvePersons(NewPersonIndex(bards), personMentions(s, pieces, bards, redLinks), s)
	u := creditUnion{bards: bards, persons: persons, related: relatedPersons(bards, persons)}
	p := People{Bards: bards, Persons: persons, Credits: map[int64][]PieceCredit{}}
	listers := u.listers()
	for _, piece := range pieces {
		p.Credits[piece.PageID] = u.pieceCredits(piece, listers[piece.Title])
	}
	p.StubPieces = u.stubPieces()

	return p
}

// personMentions are the persons the pages name, in a fixed order: each piece's infobox credits and bard-name
// categories, the persons the bards are heir of, the red links naming persons in prose and the performers of the
// performance items.
func personMentions(s Site, pieces []PieceRecord, bards []BardRecord, redLinks []RedLink) []PersonMention {
	var out []PersonMention
	for _, p := range pieces {
		for _, c := range p.Credits {
			if c.Attribution == domain.AttributionNamed {
				out = append(out, PersonMention{Name: c.Name})
			}
		}
		for _, c := range p.BardNameCategories {
			out = append(out, PersonMention{Name: PersonName{Name: c, Form: NameCategory}})
		}
	}
	for _, b := range bards {
		for _, heir := range b.HeirOf {
			out = append(out, PersonMention{Name: heirName(heir)})
		}
	}

	out = append(out, personRedLinkMentions(redLinks)...)

	return append(out, performerMentions(s, pieces, bards, newWorkTitles(s, pieces, bards))...)
}

// performerMentions are the performers the performance items of the pieces and bards link to: bard pages and
// pages that do not exist and are no work (a link "of [[X]]" to a piece names no performer).
func performerMentions(s Site, pieces []PieceRecord, bards []BardRecord, works workTitles) []PersonMention {
	isBard := map[string]bool{}
	for _, b := range bards {
		isBard[b.Title] = true
	}
	var out []PersonMention
	add := func(parsed ParsedPage) {
		for _, it := range performanceItems(parsed) {
			for _, target := range ParsePerformance(it.ListItem, s.Year, works.has).Performers {
				if name := performerName(s, target); isBard[name.Name] || !s.HasPage(name.Name) {
					out = append(out, PersonMention{Name: name})
				}
			}
		}
	}
	for _, p := range pieces {
		add(p.Parsed)
	}
	for _, b := range bards {
		add(b.Parsed)
	}

	return out
}

// performerName is a performer link's target as the person it names, read through redirects.
func performerName(s Site, target string) PersonName {
	return PersonName{Name: s.ResolveRedirect(target), Form: NamePageLink}
}

func heirName(title string) PersonName {
	return PersonName{Name: title, Form: NamePageLink}
}

// relatedPersons maps each person to those a bard page makes them heir of or heir to.
func relatedPersons(bards []BardRecord, persons Persons) map[PersonRef][]PersonRef {
	out := map[PersonRef][]PersonRef{}
	for _, b := range bards {
		for _, heir := range b.HeirOf {
			if to, ok := persons.Resolve(heirName(heir)); ok {
				out[PersonRef(b.Title)] = append(out[PersonRef(b.Title)], to)
				out[to] = append(out[to], PersonRef(b.Title))
			}
		}
	}

	return out
}

type creditUnion struct {
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

// stubPieces are the works the bard pages list that have no page, by title, each credited to the bards listing
// it. A bard's own works list is the bard's claim, so the credit is probable (on a piece with a page, where the
// infobox credits others, a works list only makes it possible).
func (u creditUnion) stubPieces() []StubPiece {
	var out []StubPiece
	for _, b := range u.bards {
		for _, w := range b.Works {
			if w.Exists {
				continue
			}
			k := slices.IndexFunc(out, func(sp StubPiece) bool { return sp.Title == w.Title })
			if k < 0 {
				out = append(out, StubPiece{Title: w.Title})
				k = len(out) - 1
			}
			name := PersonName{Name: b.Title, Form: NamePageLink}
			if !slices.ContainsFunc(out[k].Credits, func(c PieceCredit) bool { return c.Person == PersonRef(b.Title) }) {
				c := signalCredit(PersonRef(b.Title), name, SignalWorksList)
				c.Certainty = domain.CertaintyProbable
				out[k].Credits = append(out[k].Credits, c)
			}
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Title < out[b].Title })

	return out
}
