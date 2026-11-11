package domain_test

import (
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
)

// validationContext describes the entities the full documents reference.
func validationContext(id int64) domain.ValidationContext {
	return domain.ValidationContext{ID: id, Terms: map[int64]domain.TermInfo{
		1:  {Facet: domain.FacetTopic, Name: "Weapons"},
		2:  {Facet: domain.FacetAffiliation, Name: "Calontir"},
		3:  {Facet: domain.FacetTopic, Name: "Drinking"},
		4:  {Facet: domain.FacetMood, Name: "Merry"},
		11: {Facet: domain.FacetAffiliation, Name: "SCA"},
	}, CollectionParents: map[int64]*int64{1: nil, 2: ptr[int64](1)}}
}

func TestValidate_FullDocumentsAreValid(t *testing.T) {
	ctx := validationContext(100)
	assert.Empty(t, fullPieceDocument().Validate(ctx))
	assert.Empty(t, fullPersonDocument().Validate(ctx))
	assert.Empty(t, fullTermDocument().Validate(ctx))
	assert.Empty(t, fullCollectionDocument().Validate(ctx))
	assert.Empty(t, fullEventDocument().Validate(ctx))
}

func TestPieceDocument_Validate(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*domain.PieceDocument)
		want   domain.Problem
	}{
		"schema version": {func(d *domain.PieceDocument) { d.SchemaVersion = 2 }, problem("schema_version", "invalid")},
		"blank title":    {func(d *domain.PieceDocument) { d.Title = " " }, problem("title", "required")},
		"width":          {func(d *domain.PieceDocument) { d.Title = strings.Repeat("x", 256) }, problem("title", "too_long")},
		"NFC":            {func(d *domain.PieceDocument) { d.Title = "Cafe\u0301" }, problem("title", "not_nfc")},
		"control": {func(d *domain.PieceDocument) { d.Links[0].Title = ptr("A\x1b[31m") },
			problem("links[0].title", "control_character")},
		"enum": {func(d *domain.PieceDocument) { d.Credits[0].Role = "singer" }, problem("credits[0].role", "invalid")},
		"alias collation": {func(d *domain.PieceDocument) {
			d.Aliases = append(d.Aliases, domain.DocPieceAlias{Text: "KETTLE SÖNG", Kind: domain.PieceAliasKindNickname})
		}, problem("aliases[1]", "duplicate")},
		"period": {func(d *domain.PieceDocument) { d.PeriodEnd = ptr[int16](1400) }, problem("period_end", "out_of_range")},
		"licence needs known": {func(d *domain.PieceDocument) { d.LicensingStatus = domain.LicensingStatusUnknown },
			problem("licence", "invalid")},
		"named credit needs person": {func(d *domain.PieceDocument) { d.Credits[0].PersonID = nil },
			problem("credits[0].person_slug", "required")},
		"person needs named": {func(d *domain.PieceDocument) { d.Credits[1].PersonID = ptr[int64](5) },
			problem("credits[1].person_slug", "invalid")},
		"unknown term": {func(d *domain.PieceDocument) { d.Terms[0].TermID = 99 },
			problem("terms[0].term_slug", "unknown_reference")},
		"affiliation term": {func(d *domain.PieceDocument) { d.Terms[1].TermID = 11 },
			problem("terms[1].term_slug", "wrong_facet")},
		"second primary": {func(d *domain.PieceDocument) {
			d.Terms = append(d.Terms, domain.DocPieceTerm{TermID: 1, IsPrimary: true})
		}, problem("terms[2].is_primary", "duplicate")},
		"self relation": {func(d *domain.PieceDocument) { d.Relations[0].ToPieceID = 100 },
			problem("relations[0].to_piece_slug", "invalid")},
		"link roles": {func(d *domain.PieceDocument) { d.Links[0].Roles = nil }, problem("links[0].roles", "required")},
		"link URL": {func(d *domain.PieceDocument) { d.Links[0].URL = "mailto:wren@example.org" },
			problem("links[0].url", "invalid")},
		"same link twice": {func(d *domain.PieceDocument) {
			l := d.Links[0]
			l.URL = "https://EXAMPLE.org/kettle"
			d.Links = append(d.Links, l)
		}, problem("links[1]", "duplicate")},
		"unknown collection": {func(d *domain.PieceDocument) { d.Links[0].CollectionID = ptr[int64](99) },
			problem("links[0].collection_id", "unknown_reference")},
		"performer twice": {func(d *domain.PieceDocument) { d.Links[0].Performances[0].PerformerIDs = []int64{5, 5} },
			problem("links[0].performances[0].performer_slugs[1]", "duplicate")},
	} {
		t.Run(name, func(t *testing.T) {
			doc := fullPieceDocument()
			tc.mutate(&doc)
			assert.Contains(t, doc.Validate(validationContext(100)), tc.want)
		})
	}
}

func TestPersonDocument_Validate(t *testing.T) {
	doc := fullPersonDocument()
	doc.AffiliationIDs = []int64{1, 11, 11}
	doc.Relations[0].ToPersonID = 100

	assert.ElementsMatch(t, []domain.Problem{problem("affiliation_slugs[0]", "wrong_facet"),
		problem("affiliation_slugs[2]", "duplicate"), problem("relations[0].to_person_slug", "invalid")},
		doc.Validate(validationContext(100)))
}

func TestTermDocument_Validate(t *testing.T) {
	// Term 100 is under 1; 5 is under 100, 6 under 5.
	for name, tc := range map[string]struct {
		mutate func(*domain.TermDocument, domain.ValidationContext)
		want   domain.Problem
	}{
		"name in facet": {func(d *domain.TermDocument, _ domain.ValidationContext) { d.Name = "DRINKING" },
			problem("name", "duplicate")},
		"cycle": {func(d *domain.TermDocument, _ domain.ValidationContext) { d.ParentID = ptr[int64](6) },
			problem("parent_slug", "cycle")},
		"too deep": {func(_ *domain.TermDocument, ctx domain.ValidationContext) {
			ctx.Terms[7] = domain.TermInfo{Facet: domain.FacetTopic, ParentID: ptr[int64](6), Name: "Sabres"}
		}, problem("parent_slug", "too_deep")},
		"parent of another facet": {func(d *domain.TermDocument, _ domain.ValidationContext) {
			d.ParentID = ptr[int64](4)
		}, problem("parent_slug", "wrong_facet")},
		"related affiliation": {func(d *domain.TermDocument, _ domain.ValidationContext) {
			d.RelatedAffiliationID = ptr[int64](3)
		}, problem("related_affiliation_slug", "wrong_facet")},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := validationContext(100)
			ctx.Terms[5] = domain.TermInfo{Facet: domain.FacetTopic, ParentID: ptr[int64](100), Name: "Swords"}
			ctx.Terms[6] = domain.TermInfo{Facet: domain.FacetTopic, ParentID: ptr[int64](5), Name: "Rapiers"}
			doc := fullTermDocument()
			tc.mutate(&doc, ctx)
			assert.Contains(t, doc.Validate(ctx), tc.want)
		})
	}
}

func TestCollectionDocument_Validate(t *testing.T) {
	doc := fullCollectionDocument()
	doc.ParentID = ptr[int64](2)
	assert.Equal(t, []domain.Problem{problem("parent_id", "cycle")}, doc.Validate(validationContext(1)))
	doc.ParentID = ptr[int64](99)
	assert.Equal(t, []domain.Problem{problem("parent_id", "unknown_reference")}, doc.Validate(validationContext(1)))
}

func TestValidatePieceSources(t *testing.T) {
	unsourced := fullPieceDocument()
	unsourced.Links[0].Roles = []domain.LinkRole{domain.LinkRoleLyrics}
	sourced := fullPieceDocument()
	sourced.Links[0].Roles = []domain.LinkRole{domain.LinkRoleLicensingSource}

	assert.ElementsMatch(t, []domain.Problem{problem("licence", "source_required"),
		problem("licence_version", "source_required"), problem("licensing_notes_md", "source_required"),
		problem("licensing_status", "source_required"), problem("summary", "summary_required")},
		domain.ValidatePieceSources(nil, unsourced, ""))
	assert.Empty(t, domain.ValidatePieceSources(nil, sourced, "Surveyed."))
	assert.Empty(t, domain.ValidatePieceSources(&unsourced, unsourced, ""), "unchanged fields need nothing")

	changed := unsourced
	changed.Licence = ptr(domain.LicencePD)
	changed.Conservation = domain.DocConservation{Status: domain.ConservationStatusLowRisk}
	assert.ElementsMatch(t, []domain.Problem{problem("licence", "source_required"),
		problem("conservation.source_url", "source_required"), problem("summary", "summary_required")},
		domain.ValidatePieceSources(&unsourced, changed, " "))
}

func TestValidatePersonSources(t *testing.T) {
	doc := fullPersonDocument()
	assert.Empty(t, domain.ValidatePersonSources(doc))
	doc.StatusSourceURL = nil
	assert.Equal(t, []domain.Problem{problem("status_source_url", "source_required")},
		domain.ValidatePersonSources(doc))
	doc.Status = domain.PersonStatusActive
	assert.Empty(t, domain.ValidatePersonSources(doc))
}

func problem(path string, code domain.ProblemCode) domain.Problem {
	return domain.Problem{Path: path, Code: code}
}
