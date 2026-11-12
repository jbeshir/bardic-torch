package importer

import (
	"slices"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// buildLinkRows makes one links row per normalised URL, with the canonical fields (mediawiki.Links.Canonical)
// every document attaching it carries.
func (b *builder) buildLinkRows() {
	for k, f := range b.c.Links.Canonical {
		l := b.docLink(f, nil)
		b.links[f.NormalizedURL] = l
		row := domain.ImportedLink{ID: int64(k + 1), Link: l}
		b.out.Links = append(b.out.Links, row)
	}
}

// docLink is the document form of link fields, with roles: its URL without tracking parameters, which can tie a
// shared link to the sharer, and its title and description as free text.
func (b *builder) docLink(f mediawiki.LinkFields, roles []domain.LinkRole) domain.DocLink {
	l := domain.DocLink{URL: cleanURL(domain.WithoutTrackingParameters(f.URL)),
		Title:       free(f.Title),
		Description: free(f.Description), Platform: optional(f.Platform),
		Locator: optional(f.Locator), Roles: roles}
	if f.PlaceType != "" {
		pt := f.PlaceType
		l.PlaceType = &pt
	}
	if id, ok := b.collectionIDs[f.CollectionURL]; ok && f.CollectionURL != "" {
		l.CollectionID = &id
	}

	return l
}

// attachment is a document's link: the canonical fields of its normalised URL (so that every document
// carries the same) and its roles here.
func (b *builder) attachment(l mediawiki.LinkRecord) domain.DocLink {
	out := b.links[l.NormalizedURL]
	out.Roles = slices.Clone(l.Roles)
	if out.Roles == nil {
		out.Roles = []domain.LinkRole{}
	}

	return out
}

func (b *builder) pieceLinks(links []mediawiki.LinkRecord) []domain.DocPieceLink {
	out := []domain.DocPieceLink{}
	for _, l := range links {
		dl := domain.DocPieceLink{DocLink: b.attachment(l), Performances: []domain.DocPerformance{}}
		for _, p := range l.Performances {
			dl.Performances = append(dl.Performances, b.performance(p))
		}
		out = append(out, dl)
	}

	return out
}

func (b *builder) personLinks(links []mediawiki.LinkRecord) []domain.DocPersonLink {
	out := []domain.DocPersonLink{}
	for _, l := range links {
		dl := domain.DocPersonLink{DocLink: b.attachment(l), Performances: []domain.DocPersonPerformance{}}
		for _, p := range l.Performances {
			dp := domain.DocPersonPerformance{DocPerformance: b.performance(p)}
			if id, ok := b.pieceIDs[b.c.Site.ResolveRedirect(p.Piece)]; ok && p.Piece != "" {
				dp.PieceID = &id
			}
			dl.Performances = append(dl.Performances, dp)
		}
		out = append(out, dl)
	}

	return out
}

// performance is a performance's document form. Its description is kept as written, but for its URLs' tracking
// parameters.
func (b *builder) performance(p mediawiki.Performance) domain.DocPerformance {
	out := domain.DocPerformance{
		Uploader:            free(p.Uploader),
		DescriptionVerbatim: plainName(withoutTracking(p.DescriptionVerbatim)),
		PerformerIDs:        []int64{},
	}
	if id, ok := b.eventIDs[p.Event]; ok && p.Event != "" {
		out.EventID = &id
	}
	if p.Year > 0 && p.Year <= 9999 {
		y := int16(p.Year)
		out.Year = &y
	}
	for _, ref := range p.PerformerRefs {
		if id, ok := b.personIDs[ref]; ok && !slices.Contains(out.PerformerIDs, id) {
			out.PerformerIDs = append(out.PerformerIDs, id)
		}
	}

	return out
}

// withoutTracking is a wikitext with its external links' URLs without tracking parameters.
func withoutTracking(text string) string {
	var out strings.Builder
	last := 0
	for _, e := range mediawiki.FindExternalLinks(text) {
		at := e.Start + strings.Index(text[e.Start:e.End], e.URL)
		out.WriteString(text[last:at])
		out.WriteString(domain.WithoutTrackingParameters(e.URL))
		last = at + len(e.URL)
	}
	out.WriteString(text[last:])

	return out.String()
}
