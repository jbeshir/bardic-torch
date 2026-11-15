package importer

import (
	"slices"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

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
		Title: title, Kind: p.Kind, ConservationStatus: p.Conservation(),
		NotesMD:          free(c.NotesMD),
		VersionNotesMD:   free(c.VersionNotesMD),
		LicensingStatus:  c.LicensingStatus,
		LicensingNotesMD: free(c.LicensingNotesMD),
		Aliases:          b.pieceAliases(p), TermIDs: b.pieceTerms(p),
		Credits: b.credits(b.c.People.Credits[p.PageID]), Links: b.pieceLinks(links)}
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

func (b *builder) pieceTerms(p mediawiki.PieceRecord) []int64 {
	out := []int64{}
	for _, category := range p.Terms {
		if id, ok := b.termIDs[category]; ok && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}

	return out
}

// credits are a piece's credits. A named credit whose person was not resolved, which the schema cannot hold, is
// imported as unknown. A person's lyrics and tune credits of one certainty become one lyrics and tune credit, in the
// place of the first (mergeLyricsAndTune).
func (b *builder) credits(credits []mediawiki.PieceCredit) []domain.DocCredit {
	out := []domain.DocCredit{}
	for _, c := range credits {
		d := domain.DocCredit{Role: c.Role, Attribution: c.Attribution, Certainty: c.Certainty}
		if c.Attribution == domain.AttributionNamed {
			if id, ok := b.personIDs[c.Person]; ok && c.Person != "" {
				d.PersonID = &id
			} else {
				d.Attribution = domain.AttributionUnknown
			}
		}
		out = append(out, d)
	}

	return b.mergeLyricsAndTune(out)
}

// mergeLyricsAndTune replaces each pair of a person's lyrics and tune credits with the same attribution and certainty
// by one lyrics and tune credit, where the first of the pair was, and counts the pairs in b.mergedCredits.
func (b *builder) mergeLyricsAndTune(credits []domain.DocCredit) []domain.DocCredit {
	out := make([]domain.DocCredit, 0, len(credits))
	merged := make([]bool, len(credits))
	for k, c := range credits {
		if merged[k] {
			continue
		}
		if j := lyricsAndTunePair(credits, merged, k); j >= 0 {
			merged[j] = true
			c.Role = domain.CreditRoleLyricsAndTune
			b.mergedCredits++
		}
		out = append(out, c)
	}

	return out
}

// lyricsAndTunePair is the index of the first credit after credits[k], not yet merged, that pairs with it: if
// credits[k] is a person's lyrics credit, their tune credit of the same attribution and certainty, and the other way
// round. It is -1 if there is none.
func lyricsAndTunePair(credits []domain.DocCredit, merged []bool, k int) int {
	c := credits[k]
	other := map[domain.CreditRole]domain.CreditRole{
		domain.CreditRoleLyrics: domain.CreditRoleTune, domain.CreditRoleTune: domain.CreditRoleLyrics}[c.Role]
	if c.PersonID == nil || other == "" {
		return -1
	}
	for j := k + 1; j < len(credits); j++ {
		o := credits[j]
		if !merged[j] && o.Role == other && o.PersonID != nil && *o.PersonID == *c.PersonID &&
			o.Attribution == c.Attribution && o.Certainty == c.Certainty {
			return j
		}
	}

	return -1
}

func (b *builder) stubPiece(sp mediawiki.StubPiece) domain.ImportedPiece {
	id := b.pieceIDs[sp.Title]
	title := plain(sp.Title)
	doc := domain.PieceDocument{SchemaVersion: domain.DocumentSchemaVersion, Title: title,
		Kind: domain.PieceKindSong, ConservationStatus: domain.ConservationStatusUnassessed, IsStub: true,
		LicensingStatus: domain.LicensingStatusUnknown, Aliases: []domain.DocPieceAlias{},
		TermIDs: []int64{}, Credits: b.credits(sp.Credits),
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
