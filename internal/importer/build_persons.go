package importer

import (
	"slices"
	"strings"

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
	entity := "person " + bard.Title
	name := b.name(bard.Title, entity, "name", bard.Title)
	doc := domain.PersonDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name,
		Kind: domain.PersonKindCommunityBard, Status: bard.Status, StatusDate: bard.StatusDate,
		NotesMD:          b.free(bard.Title, entity, "notes", bard.NotesMD),
		LicensingStatus:  bard.LicensingStatus,
		LicensingNotesMD: b.free(bard.Title, entity, "licensing notes", bard.LicensingNotesMD),
		Aliases:          b.personAliases(bard), Roles: uniqueOf(bard.Roles), AffiliationIDs: []int64{},
		Relations: []domain.DocPersonRelation{}, Links: b.personLinks(links)}
	if bard.Status != domain.PersonStatusActive {
		b.status(&doc, bard)
	}
	for _, cat := range bard.Affiliations {
		if tid, ok := b.termIDs[cat]; ok && !slices.Contains(doc.AffiliationIDs, tid) {
			doc.AffiliationIDs = append(doc.AffiliationIDs, tid)
		}
	}
	if len(bard.Affiliations) > 0 {
		b.review.Persons = append(b.review.Persons, item("affiliations", bard.Title,
			"Affiliations from categories: %s.", strings.Join(bard.Affiliations, ", ")))
	}
	if len(bard.Roles) > 0 {
		b.review.Persons = append(b.review.Persons, item("roles", bard.Title,
			"Roles from performer-type categories: %s.", strings.Join(stringsOf(bard.Roles), ", ")))
	}
	for _, r := range bard.Relations {
		to, ok := b.c.People.Persons.Resolve(mediawiki.PersonName{Name: r.To, Form: mediawiki.NamePageLink})
		toID := b.personIDs[to]
		if !ok || toID == 0 || toID == id {
			continue
		}
		rel := domain.DocPersonRelation{ToPersonID: toID, Type: r.Type}
		if !slices.Contains(doc.Relations, rel) {
			doc.Relations = append(doc.Relations, rel)
			b.review.Persons = append(b.review.Persons, item("relation", bard.Title, "Relation %s %s, from the "+
				"page's prose.", r.Type, to))
		}
	}
	sortName := doc.Name

	return domain.ImportedPerson{ID: id, Slug: b.slugs.Allocate(doc.Name, domain.SlugEntityTypePerson, id),
		SortName: sortName, SortLetter: domain.SortLetter(sortName), Doc: doc, Legacy: legacy(page),
		Revision: b.revision(&page, "")}
}

func dateText(d *domain.Date) string {
	if d == nil {
		return "none"
	}

	return d.Format("2006-01-02")
}

// personAliases are a bard's aliases, each once as the collation compares names. Every alias is listed in the
// review, unsure ones first.
func (b *builder) personAliases(bard mediawiki.BardRecord) []domain.DocPersonAlias {
	out := []domain.DocPersonAlias{}
	seen := map[string]bool{domain.CollationKey(bard.Title): true}
	var sure, unsure []Item
	for _, a := range bard.Aliases {
		name := clean(a.Name)
		key := domain.CollationKey(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, domain.DocPersonAlias{Name: name, Kind: a.Kind, IsPublic: true})
		it := item("alias", bard.Title, "%q: kind %s (source %s, rule %s, %s).", name, a.Kind,
			a.Source, a.Classification.Rule, confidence(a.Classification.Confident))
		if a.Classification.Confident {
			sure = append(sure, it)
		} else {
			unsure = append(unsure, it)
		}
	}
	b.aliasItems = append(b.aliasItems, aliasItems{unsure: unsure, sure: sure})

	return out
}

// status sources a bard's status to the link its date was read from (StatusSourceURL), or else to the wiki page.
func (b *builder) status(doc *domain.PersonDocument, bard mediawiki.BardRecord) {
	source, from := bard.URL, "the wiki page"
	if bard.StatusSourceURL != "" {
		source, from = bard.StatusSourceURL, "the link its date is read from"
	}
	doc.StatusSourceURL = optionalURL(source)
	b.review.Persons = append(b.review.Persons, item("status", bard.Title,
		"Status %s (date: %s) from the In Memoriam category, sourced to %s: %s.", bard.Status,
		dateText(bard.StatusDate), from, source))
}

// aliasItems are the alias review items of one person.
type aliasItems struct {
	unsure []Item
	sure   []Item
}

func confidence(confident bool) string {
	if confident {
		return "confident"
	}

	return "unsure"
}

// historicalCredits are the persons credited on a piece of a traditional origin (traditionalCategory) or of a
// century origin before historicalBefore. On a piece that is also of the SCA (scaPiecesCategory or
// contrafactaCategory), such as new words to a traditional tune, only the tune's credits are: its words are modern.
func (b *builder) historicalCredits() map[mediawiki.PersonRef]bool {
	out := map[mediawiki.PersonRef]bool{}
	for _, p := range b.c.Pieces {
		period := p.Content.PeriodStart != nil && int(*p.Content.PeriodStart) < historicalBefore
		if !period && !hasTerm(p, traditionalCategory) {
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
	return slices.ContainsFunc(p.Terms, func(t mediawiki.PieceTerm) bool { return t.Category == category })
}

// firstMention is the first page naming a stub person, or "".
func firstMention(s mediawiki.StubPerson) string {
	if len(s.Mentions) > 0 {
		return s.Mentions[0].Page
	}

	return ""
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
	page := firstMention(s)
	name := b.name(page, "stub person", "name", s.Name)
	doc := domain.PersonDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name, Kind: kind,
		Status: domain.PersonStatusActive, LicensingStatus: domain.LicensingStatusUnknown, IsStub: true,
		Aliases: []domain.DocPersonAlias{}, Roles: []domain.PersonRole{}, AffiliationIDs: []int64{},
		Relations: []domain.DocPersonRelation{}, Links: b.personLinks(links)}
	check := ""
	if slices.ContainsFunc(s.Mentions, func(m mediawiki.PersonMention) bool {
		return m.Name.Form == mediawiki.NameExternalLink
	}) {
		check = " Credited by an external link: check whether historical or a group."
	}
	b.review.Persons = append(b.review.Persons, item("stub_kind", page,
		"Stub person %q (named on %d page(s)) is guessed to be %s.%s", s.Name, len(s.Mentions), kind, check))
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

func stringsOf[S ~string](values []S) []string {
	out := make([]string, 0, len(values))
	for _, s := range values {
		out = append(out, string(s))
	}

	return out
}
