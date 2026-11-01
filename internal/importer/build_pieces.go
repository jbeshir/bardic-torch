package importer

import (
	"regexp"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// contrafactaCategory is the origin category of pieces written to an existing tune.
const contrafactaCategory = "Contrafacta"

// formSuffix is a title that is another piece's title with a parenthetical naming its form or version.
var formSuffix = regexp.MustCompile(`^(.+) \((SCA Version|Poem|Song)\)$`)

func (b *builder) buildPieces() {
	links := map[int64][]mediawiki.LinkRecord{}
	for _, pl := range b.c.Links.Pieces {
		links[pl.PageID] = pl.Links
	}
	for _, p := range b.c.Pieces {
		b.out.Pieces = append(b.out.Pieces, b.piece(p, links[p.PageID]))
	}
	for _, sp := range b.stubPieces {
		b.out.Pieces = append(b.out.Pieces, b.stubPiece(sp))
	}
}

func (b *builder) piece(p mediawiki.PieceRecord, links []mediawiki.LinkRecord) domain.ImportedPiece {
	id := b.pieceIDs[p.Title]
	page, _ := b.c.Site.Page(p.Title)
	entity := "piece " + p.Title
	c := p.Content
	title, _ := b.name(p.Title, entity, "title", p.Title)
	doc := domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion,
		Title: title, Kind: p.Kind, SecondaryKinds: uniqueOf(p.SecondaryKinds),
		Conservation:       domain.DocConservation{Status: p.Conservation()},
		NotesMD:            b.free(p.Title, entity, "notes", c.NotesMD),
		VersionNotesMD:     b.free(p.Title, entity, "version notes", c.VersionNotesMD),
		LicensingStatus:    c.LicensingStatus,
		LicensingNotesMD:   b.free(p.Title, entity, "licensing notes", c.LicensingNotesMD),
		HistoricalSourceMD: b.free(p.Title, entity, "historical source", c.HistoricalSourceMD),
		PeriodStart:        c.PeriodStart, PeriodEnd: c.PeriodEnd,
		Aliases: b.pieceAliases(p), Terms: b.pieceTerms(p), Relations: b.pieceRelations(p),
		Credits: b.credits(p.Title, b.c.People.Credits[p.PageID]), Links: b.pieceLinks(links),
		Preferences: []domain.DocPreference{}}
	if doc.Conservation.Status != domain.ConservationStatusUnassessed {
		doc.Conservation.SourceURL = optional(p.URL)
	}
	sortTitle := domain.SortTitle(doc.Title)

	return domain.ImportedPiece{ID: id, Slug: b.slugs.Allocate(doc.Title, domain.SlugEntityTypePiece, id),
		SortTitle: sortTitle, SortLetter: domain.SortLetter(sortTitle), Doc: doc,
		LegacyHadInlineLyrics: p.Lyrics.HadInlineLyrics, Legacy: legacy(page), Revision: b.revision(&page, "")}
}

// pieceAliases are a piece's alternate titles, each once as the collation compares them and none equal to the
// title; one holding a legal name is dropped (and listed by name).
func (b *builder) pieceAliases(p mediawiki.PieceRecord) []domain.DocPieceAlias {
	out := []domain.DocPieceAlias{}
	seen := map[string]bool{domain.CollationKey(p.Title): true}
	for _, a := range p.Titles.Alternates {
		text, ok := b.name(p.Title, "piece "+p.Title, "alias", a.Text)
		if key := domain.CollationKey(text); ok && !seen[key] {
			seen[key] = true
			out = append(out, domain.DocPieceAlias{Text: text, Kind: a.Kind})
		}
	}

	return out
}

func (b *builder) pieceTerms(p mediawiki.PieceRecord) []domain.DocPieceTerm {
	out := []domain.DocPieceTerm{}
	for _, t := range p.Terms {
		id, ok := b.termIDs[t.Category]
		if !ok || slices.ContainsFunc(out, func(d domain.DocPieceTerm) bool { return d.TermID == id }) {
			continue
		}
		out = append(out, domain.DocPieceTerm{TermID: id, IsPrimary: t.IsPrimary, Part: t.Part})
	}

	return out
}

// pieceRelations relate a piece whose title is another piece's with a parenthetical form to that piece: an
// "(SCA Version)" is a contrafactum of it if the piece is a contrafactum, else a variant, and a "(Poem)" or
// "(Song)" is the same text in another form. Each is listed for review.
func (b *builder) pieceRelations(p mediawiki.PieceRecord) []domain.DocPieceRelation {
	out := []domain.DocPieceRelation{}
	m := formSuffix.FindStringSubmatch(p.Title)
	if m == nil {
		return out
	}
	to, ok := b.pieceIDs[m[1]]
	if !ok || to == b.pieceIDs[p.Title] {
		return out
	}
	t := domain.PieceRelationTypeSameTextOtherForm
	if m[2] == "SCA Version" {
		t = domain.PieceRelationTypeVariantOf
		if slices.ContainsFunc(p.Terms, func(pt mediawiki.PieceTerm) bool { return pt.Category == contrafactaCategory }) {
			t = domain.PieceRelationTypeContrafactumOf
		}
	}
	b.review.Titles = append(b.review.Titles, item("relation", p.Title, "Related to %q as %s, from the titles.",
		m[1], t))

	return append(out, domain.DocPieceRelation{ToPieceID: to, Type: t})
}

// credits are a piece's credits. A named credit whose person was not resolved, which the schema cannot hold, is
// imported as unknown and listed; one naming a person whose name is withheld (withheldStubs) keeps the fact that
// someone is named as the note WithheldName.
func (b *builder) credits(page string, credits []mediawiki.PieceCredit) []domain.DocCredit {
	out := []domain.DocCredit{}
	for _, c := range credits {
		d := domain.DocCredit{Role: c.Role, Attribution: c.Attribution, Certainty: c.Certainty, Part: c.Part,
			Note: b.free(page, "credit", "note", c.Note)}
		if c.Attribution == domain.AttributionNamed {
			if id, ok := b.personIDs[c.Person]; ok && c.Person != "" {
				d.PersonID = &id
			} else if b.withheldStubs[c.Person] {
				d.Attribution = domain.AttributionUnknown
				if d.Note == nil {
					note := domain.WithheldName
					d.Note = &note
				}
			} else {
				d.Attribution = domain.AttributionUnknown
				b.review.Credits = append(b.review.Credits, item("unresolved", page,
					"A %s credit names no person the importer could match; it is imported as unknown.", c.Role))
			}
		}
		out = append(out, d)
	}

	return out
}

func (b *builder) stubPiece(sp mediawiki.StubPiece) domain.ImportedPiece {
	id := b.pieceIDs[sp.Title]
	page := ""
	if len(sp.Credits) > 0 {
		page = string(sp.Credits[0].Person)
	}
	title, _ := b.name(page, "stub piece", "title", sp.Title)
	doc := domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: title,
		Kind: domain.PieceKindSong, SecondaryKinds: []domain.PieceKind{},
		Conservation: domain.DocConservation{Status: domain.ConservationStatusUnassessed}, IsStub: true,
		LicensingStatus: domain.LicensingStatusUnknown, Aliases: []domain.DocPieceAlias{},
		Terms: []domain.DocPieceTerm{}, Relations: []domain.DocPieceRelation{}, Credits: b.credits(page, sp.Credits),
		Links: []domain.DocPieceLink{}, Preferences: []domain.DocPreference{}}
	sortTitle := domain.SortTitle(title)

	return domain.ImportedPiece{ID: id, Slug: b.slugs.Allocate(title, domain.SlugEntityTypePiece, id),
		SortTitle: sortTitle, SortLetter: domain.SortLetter(sortTitle), Doc: doc,
		Legacy: &domain.LegacyPage{Title: clean(sp.Title)}, Revision: b.revision(nil, domain.ImportSummaryStub)}
}
