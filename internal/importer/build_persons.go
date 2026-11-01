package importer

import (
	"slices"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// historicalBefore is the century origin a piece must start before for its stub credits to be guessed historical
// persons (N-2): a pre-1900 name with no SCA name form is a historical writer, not a community bard.
const historicalBefore = 1900

// traditionalCategory is the origin category of traditional and folk pieces, whose named writers are historical
// persons too.
const traditionalCategory = "Traditional & Folk"

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
	name, _ := b.name(bard.Title, entity, "name", bard.Title)
	doc := domain.PersonDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name,
		Kind: domain.PersonKindCommunityBard, Status: bard.Status, StatusDate: bard.StatusDate,
		NotesMD:          b.free(bard.Title, entity, "notes", bard.NotesMD),
		LicensingStatus:  bard.LicensingStatus,
		LicensingNotesMD: b.free(bard.Title, entity, "licensing notes", bard.LicensingNotesMD),
		Aliases:          b.personAliases(bard), Roles: uniqueOf(bard.Roles), AffiliationIDs: []int64{},
		Relations: []domain.DocPersonRelation{}, Links: b.personLinks(links), Preferences: []domain.DocPreference{}}
	if bard.Status != domain.PersonStatusActive {
		doc.StatusSourceURL = optional(bard.URL)
		b.review.Persons = append(b.review.Persons, item("status", bard.Title,
			"Status %s (date: %s) from the In Memoriam category, sourced to the wiki page.", bard.Status,
			dateText(bard.StatusDate)))
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

// personAliases are a bard's aliases, each once as the collation compares names: hidden ones as written, public
// ones hidden too if they hold a private name. Every alias is listed in the review, unsure ones first.
func (b *builder) personAliases(bard mediawiki.BardRecord) []domain.DocPersonAlias {
	out := []domain.DocPersonAlias{}
	index := map[string]int{domain.CollationKey(bard.Title): -1}
	var sure, unsure []Item
	for _, a := range bard.Aliases {
		name := clean(a.Name)
		public := a.IsPublic
		if public && b.w.holdsName(name) {
			public = false
			b.review.HiddenNames = append(b.review.HiddenNames, item("public_alias", bard.Title,
				"The alias %q holds a possible legal name: it is imported hidden.", name))
		}
		key := domain.CollationKey(name)
		if k, ok := index[key]; ok {
			if k >= 0 && !public {
				out[k].IsPublic = false
			}

			continue
		}
		index[key] = len(out)
		out = append(out, domain.DocPersonAlias{Name: name, Kind: a.Kind, IsPublic: public})
		visibility := "hidden"
		if public {
			visibility = "public"
		}
		it := item("alias", bard.Title, "%q: kind %s, %s (source %s, rule %s, %s).", name, a.Kind, visibility,
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
// century origin before historicalBefore.
func (b *builder) historicalCredits() map[mediawiki.PersonRef]bool {
	out := map[mediawiki.PersonRef]bool{}
	for _, p := range b.c.Pieces {
		period := p.Content.PeriodStart != nil && int(*p.Content.PeriodStart) < historicalBefore
		traditional := slices.ContainsFunc(p.Terms, func(t mediawiki.PieceTerm) bool {
			return t.Category == traditionalCategory
		})
		if !period && !traditional {
			continue
		}
		for _, c := range b.c.People.Credits[p.PageID] {
			if c.Person != "" {
				out[c.Person] = true
			}
		}
	}

	return out
}

func (b *builder) stubPerson(s mediawiki.StubPerson, links []mediawiki.LinkRecord,
	historical map[mediawiki.PersonRef]bool,
) domain.ImportedPerson {
	ref := mediawiki.PersonRef(s.Name)
	id := b.personIDs[ref]
	kind := s.Kind
	sca := mediawiki.ClassifyPersonAlias(mediawiki.AliasCandidate{Name: s.Name,
		Source: mediawiki.AliasFromH1Parenthetical}).Public()
	if kind == domain.PersonKindCommunityBard && historical[ref] && !sca {
		kind = domain.PersonKindHistorical
	}
	page := firstMention(s)
	name, _ := b.name(page, "stub person", "name", s.Name)
	doc := domain.PersonDocument{SchemaVersion: domain.DocumentSchemaVersion, Name: name, Kind: kind,
		Status: domain.PersonStatusActive, LicensingStatus: domain.LicensingStatusUnknown, IsStub: true,
		Aliases: []domain.DocPersonAlias{}, Roles: []domain.PersonRole{}, AffiliationIDs: []int64{},
		Relations: []domain.DocPersonRelation{}, Links: b.personLinks(links), Preferences: []domain.DocPreference{}}
	b.review.Persons = append(b.review.Persons, item("stub_kind", page,
		"Stub person %q (named on %d page(s)) is guessed to be %s.", name, len(s.Mentions), kind))
	person := domain.ImportedPerson{ID: id, Slug: b.slugs.Allocate(name, domain.SlugEntityTypePerson, id),
		SortName: name, SortLetter: domain.SortLetter(name), Doc: doc, Revision: b.revision(nil,
			domain.ImportSummaryStub)}
	if s.LegacyTitle != "" {
		person.Legacy = &domain.LegacyPage{Title: clean(s.LegacyTitle)}
		if b.w.holdsName(s.LegacyTitle) {
			b.review.HiddenNames = append(b.review.HiddenNames, item("legacy_title", page,
				"Stub person %q: its legacy title, which is never public, holds a possible legal name.", name))
		}
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
