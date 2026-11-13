package importer

import (
	"slices"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// historicalBefore is the century origin a piece must start before for its stub credits to be guessed historical
// persons: a pre-1900 name with no SCA name form is a historical writer, not a community bard.
const historicalBefore = 1900

// traditionalCategory is the origin category of traditional and folk pieces, whose named writers are historical
// persons too.
const traditionalCategory = "Traditional & Folk"

// scaPiecesCategory is the origin category of pieces written in the SCA.
const scaPiecesCategory = "SCA Pieces"

func (b *builder) buildPersons() {
	links := map[mediawiki.PersonRef][]mediawiki.LinkRecord{}
	for _, pl := range b.c.Links.Persons {
		links[pl.Person] = append(links[pl.Person], pl.Links...)
	}
	for _, bard := range b.c.People.Bards {
		b.out.Persons = append(b.out.Persons, b.bard(bard, links[mediawiki.PersonRef(bard.Title)]))
	}
	historical := b.historicalCredits()
	for _, s := range b.stubs {
		b.out.Persons = append(b.out.Persons, b.stubPerson(s, links[mediawiki.PersonRef(s.Name)], historical))
	}
}

func (b *builder) bard(bard mediawiki.BardRecord, links []mediawiki.LinkRecord) domain.ImportedPerson {
	ref := mediawiki.PersonRef(bard.Title)
	id := b.personIDs[ref]
	page, _ := b.c.Site.Page(bard.Title)
	name := plain(bard.Title)
	doc := domain.PersonDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name,
		Kind: domain.PersonKindCommunityBard, Status: bard.Status,
		NotesMD:          free(bard.NotesMD),
		LicensingStatus:  bard.LicensingStatus,
		LicensingNotesMD: free(bard.LicensingNotesMD),
		Aliases:          b.personAliases(bard), Roles: uniqueOf(bard.Roles), AffiliationIDs: []int64{},
		Links: b.personLinks(links)}
	for _, cat := range bard.Affiliations {
		if tid, ok := b.termIDs[cat]; ok && !slices.Contains(doc.AffiliationIDs, tid) {
			doc.AffiliationIDs = append(doc.AffiliationIDs, tid)
		}
	}
	sortName := doc.Name

	return domain.ImportedPerson{ID: id, Slug: b.slugs.Allocate(doc.Name, domain.SlugEntityTypePerson, id),
		SortName: sortName, SortLetter: domain.SortLetter(sortName), Doc: doc, Legacy: legacy(page),
		Revision: b.revision(&page, "")}
}

// personAliases are a bard's aliases, each once as the collation compares names.
func (b *builder) personAliases(bard mediawiki.BardRecord) []domain.DocPersonAlias {
	out := []domain.DocPersonAlias{}
	seen := map[string]bool{domain.CollationKey(bard.Title): true}
	for _, a := range bard.Aliases {
		name := clean(a.Name)
		key := domain.CollationKey(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, domain.DocPersonAlias{Name: name, Kind: a.Kind})
	}

	return out
}

// historicalCredits are the persons credited on a piece of a traditional origin (traditionalCategory) or of a
// century origin before historicalBefore. On a piece that is also of the SCA (scaPiecesCategory or
// contrafactaCategory), such as new words to a traditional tune, only the tune's credits are: its words are modern.
func (b *builder) historicalCredits() map[mediawiki.PersonRef]bool {
	out := map[mediawiki.PersonRef]bool{}
	for _, p := range b.c.Pieces {
		early := p.Content.EarliestYear != nil && int(*p.Content.EarliestYear) < historicalBefore
		if !early && !hasTerm(p, traditionalCategory) {
			continue
		}
		modern := hasTerm(p, scaPiecesCategory) || hasTerm(p, contrafactaCategory)
		for _, c := range b.c.People.Credits[p.PageID] {
			if c.Person != "" && (!modern || c.Role == domain.CreditRoleTune) {
				out[c.Person] = true
			}
		}
	}

	return out
}

func hasTerm(p mediawiki.PieceRecord, category string) bool {
	return slices.Contains(p.Terms, category)
}

func (b *builder) stubPerson(s mediawiki.StubPerson, links []mediawiki.LinkRecord,
	historical map[mediawiki.PersonRef]bool,
) domain.ImportedPerson {
	ref := mediawiki.PersonRef(s.Name)
	id := b.personIDs[ref]
	kind := s.Kind
	class := mediawiki.ClassifyPersonAlias(mediawiki.AliasCandidate{Name: s.Name,
		Source: mediawiki.AliasFromH1Parenthetical})
	sca := class.Confident && class.Kind == domain.PersonAliasKindSCA
	if kind == domain.PersonKindCommunityBard && historical[ref] && !sca {
		kind = domain.PersonKindHistorical
	}
	name := plain(s.Name)
	doc := domain.PersonDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name, Kind: kind,
		Status: domain.PersonStatusActive, LicensingStatus: domain.LicensingStatusUnknown, IsStub: true,
		Aliases: []domain.DocPersonAlias{}, Roles: []domain.PersonRole{}, AffiliationIDs: []int64{},
		Links: b.personLinks(links)}
	person := domain.ImportedPerson{ID: id, Slug: b.slugs.Allocate(name, domain.SlugEntityTypePerson, id),
		SortName: name, SortLetter: domain.SortLetter(name), Doc: doc, Revision: b.revision(nil,
			domain.ImportSummaryStub)}
	if s.LegacyTitle != "" {
		person.Legacy = &domain.LegacyPage{Title: clean(s.LegacyTitle)}
	}

	return person
}

func uniqueOf[T comparable](values []T) []T {
	out := []T{}
	for _, v := range values {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}

	return out
}
