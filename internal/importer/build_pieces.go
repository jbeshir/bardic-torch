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
	for _, sp := range b.c.People.StubPieces {
		b.out.Pieces = append(b.out.Pieces, b.stubPiece(sp))
	}
}

func (b *builder) piece(p mediawiki.PieceRecord, links []mediawiki.LinkRecord) domain.ImportedPiece {
	id := b.pieceIDs[p.Title]
	page, _ := b.c.Site.Page(p.Title)
	c := p.Content
	title := plain(p.Title)
	doc := domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion,
		Title: title, Kind: p.Kind, SecondaryKinds: uniqueOf(p.SecondaryKinds),
		Conservation:       domain.DocConservation{Status: p.Conservation()},
		NotesMD:            free(c.NotesMD),
		VersionNotesMD:     free(c.VersionNotesMD),
		LicensingStatus:    c.LicensingStatus,
		LicensingNotesMD:   free(c.LicensingNotesMD),
		HistoricalSourceMD: free(c.HistoricalSourceMD),
		PeriodStart:        c.PeriodStart, PeriodEnd: c.PeriodEnd,
		Aliases: b.pieceAliases(p), Terms: b.pieceTerms(p), Relations: b.pieceRelations(p),
		Credits: b.credits(b.c.People.Credits[p.PageID]), Links: b.pieceLinks(links)}
	if doc.Conservation.Status != domain.ConservationStatusUnassessed {
		doc.Conservation.SourceURL = optionalURL(p.URL)
	}
	sortTitle := domain.SortTitle(doc.Title)

	return domain.ImportedPiece{ID: id, Slug: b.slugs.Allocate(doc.Title, domain.SlugEntityTypePiece, id),
		SortTitle: sortTitle, SortLetter: domain.SortLetter(sortTitle), Doc: doc,
		LegacyHadInlineLyrics: p.HadInlineLyrics, Legacy: legacy(page), Revision: b.revision(&page, "")}
}

// pieceAliases are a piece's alternate titles, each once as the collation compares them and none equal to the
// title.
func (b *builder) pieceAliases(p mediawiki.PieceRecord) []domain.DocPieceAlias {
	out := []domain.DocPieceAlias{}
	seen := map[string]bool{domain.CollationKey(p.Title): true}
	for _, a := range p.Titles.Alternates {
		text := plain(a.Text)
		if key := domain.CollationKey(text); !seen[key] {
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

// pieceRelations relate a piece whose title is another piece's with a parenthetical form to that piece, the two
// titles compared as the collation does ("When A Knight …" is "When a Knight …"): an "(SCA Version)" is a
// contrafactum of it if the piece is a contrafactum, else a variant, and a "(Poem)" or "(Song)" is the same text in
// another form. With no piece of the bare title, a "(Poem)" and a "(Song)" of the same title are the same text in
// another form, related once, from the later page.
func (b *builder) pieceRelations(p mediawiki.PieceRecord) []domain.DocPieceRelation {
	out := []domain.DocPieceRelation{}
	m := formSuffix.FindStringSubmatch(p.Title)
	if m == nil {
		return out
	}
	id := b.pieceIDs[p.Title]
	base, ok := b.pieceByKey(domain.CollationKey(m[1]))
	if !ok && m[2] != "SCA Version" {
		base, ok = b.otherForm(m[1], m[2])
		if ok && b.pieceIDs[base.Title] > id {
			return out
		}
	}
	to := b.pieceIDs[base.Title]
	if !ok || to == id {
		return out
	}
	t := domain.PieceRelationTypeSameTextOtherForm
	if m[2] == "SCA Version" {
		t = domain.PieceRelationTypeVariantOf
		if slices.ContainsFunc(p.Terms, func(pt mediawiki.PieceTerm) bool { return pt.Category == contrafactaCategory }) {
			t = domain.PieceRelationTypeContrafactumOf
		}
	}

	return append(out, domain.DocPieceRelation{ToPieceID: to, Type: t})
}

// pieceByKey is the first piece page, in page order, whose title has a collation key.
func (b *builder) pieceByKey(key string) (mediawiki.PieceRecord, bool) {
	for _, p := range b.c.Pieces {
		if domain.CollationKey(p.Title) == key {
			return p, true
		}
	}

	return mediawiki.PieceRecord{}, false
}

// otherForm is the piece page of a "(Poem)" or "(Song)" title's other form: "X (Song)" for "X (Poem)".
func (b *builder) otherForm(base, form string) (mediawiki.PieceRecord, bool) {
	other := "Song"
	if form == other {
		other = "Poem"
	}

	return b.pieceByKey(domain.CollationKey(base + " (" + other + ")"))
}

// credits are a piece's credits. A named credit whose person was not resolved, which the schema cannot hold, is
// imported as unknown.
func (b *builder) credits(credits []mediawiki.PieceCredit) []domain.DocCredit {
	out := []domain.DocCredit{}
	for _, c := range credits {
		d := domain.DocCredit{Role: c.Role, Attribution: c.Attribution, Certainty: c.Certainty, Part: c.Part,
			Note: free(c.Note)}
		if c.Attribution == domain.AttributionNamed {
			if id, ok := b.personIDs[c.Person]; ok && c.Person != "" {
				d.PersonID = &id
			} else {
				d.Attribution = domain.AttributionUnknown
			}
		}
		out = append(out, d)
	}

	return out
}

func (b *builder) stubPiece(sp mediawiki.StubPiece) domain.ImportedPiece {
	id := b.pieceIDs[sp.Title]
	title := plain(sp.Title)
	doc := domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: title,
		Kind: domain.PieceKindSong, SecondaryKinds: []domain.PieceKind{},
		Conservation: domain.DocConservation{Status: domain.ConservationStatusUnassessed}, IsStub: true,
		LicensingStatus: domain.LicensingStatusUnknown, Aliases: []domain.DocPieceAlias{},
		Terms: []domain.DocPieceTerm{}, Relations: []domain.DocPieceRelation{}, Credits: b.credits(sp.Credits),
		Links: []domain.DocPieceLink{}}
	sortTitle := domain.SortTitle(title)

	return domain.ImportedPiece{ID: id, Slug: b.slugs.Allocate(title, domain.SlugEntityTypePiece, id),
		SortTitle: sortTitle, SortLetter: domain.SortLetter(sortTitle), Doc: doc,
		Legacy: &domain.LegacyPage{Title: clean(sp.Title)}, Revision: b.stubPieceRevision(sp)}
}

// stubPieceRevision is a stub piece's revision, crediting the contributors of the bard pages that list it, from
// which its title and credits come.
func (b *builder) stubPieceRevision(sp mediawiki.StubPiece) domain.ImportedRevision {
	r := b.revision(nil, domain.ImportSummaryStub)
	for _, c := range sp.Credits {
		for _, u := range b.activity[pageKey{mediawiki.NSMain, string(c.Person)}] {
			if u = clean(u); !slices.Contains(r.Contributors, u) {
				r.Contributors = append(r.Contributors, u)
			}
		}
	}

	return r
}
