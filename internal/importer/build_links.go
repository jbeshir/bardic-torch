package importer

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// buildLinkRows makes one links row per normalised URL, with the canonical fields (mediawiki.Links.Canonical)
// every document attaching it carries.
func (b *builder) buildLinkRows() {
	owners := b.canonicalOwners()
	for k, f := range b.c.Links.Canonical {
		page := owners[f.NormalizedURL]
		l := b.docLink(page, f, nil)
		b.links[f.NormalizedURL] = l
		row := domain.ImportedLink{ID: int64(k + 1), Link: l}
		b.out.Links = append(b.out.Links, row)
	}
}

// canonicalOwners maps each normalised URL to the page whose link gave its canonical fields.
func (b *builder) canonicalOwners() map[string]string {
	out := map[string]string{}
	for _, pl := range b.c.Links.Pieces {
		for _, l := range pl.Links {
			if _, ok := out[l.NormalizedURL]; !ok {
				out[l.NormalizedURL] = pl.Title
			}
		}
	}
	for _, pl := range b.c.Links.Persons {
		for _, l := range pl.Links {
			if _, ok := out[l.NormalizedURL]; !ok {
				out[l.NormalizedURL] = string(pl.Person)
			}
		}
	}

	return out
}

// docLink is the document form of link fields, with roles: its URL without tracking parameters, which can tie a
// shared link to the sharer, and its title and description as free text.
func (b *builder) docLink(page string, f mediawiki.LinkFields, roles []domain.LinkRole) domain.DocLink {
	entity := "link " + f.URL
	l := domain.DocLink{URL: cleanURL(domain.WithoutTrackingParameters(f.URL)),
		Title:       b.free(page, entity, "title", f.Title),
		Description: b.free(page, entity, "description", f.Description), Platform: optional(f.Platform),
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

// attachment is a document's link: the canonical fields of its normalised URL (already withheld once, so that
// every document carries the same) and its roles here.
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

// performance is a performance's document form. Its description is kept as written, but for the addresses
// withheld from it and its URLs' tracking parameters.
func (b *builder) performance(p mediawiki.Performance) domain.DocPerformance {
	entity := "performance item"
	out := domain.DocPerformance{
		Uploader:            b.free(p.Page, entity, "uploader", p.Uploader),
		DescriptionVerbatim: b.freeText(p.Page, entity, "description", withoutTracking(p.DescriptionVerbatim)),
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
	if missing := missingFields(out); len(missing) > 0 {
		b.review.Performances = append(b.review.Performances, item("incomplete", p.Page,
			"A performance item has no %s the importer could read: %s.", joinWords(missing),
			fmt.Sprintf("%q", truncate(p.DescriptionVerbatim, 2*contextRunes))))
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

// missingFields names what a performance lacks of its performers, event and year.
func missingFields(p domain.DocPerformance) []string {
	var missing []string
	if len(p.PerformerIDs) == 0 {
		missing = append(missing, "performer")
	}
	if p.EventID == nil {
		missing = append(missing, "event")
	}
	if p.Year == nil {
		missing = append(missing, "year")
	}

	return missing
}

// truncate cuts s to at most n runes.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}

	return string(r[:n]) + "…"
}

// joinWords joins words as prose: "a", "a or b", "a, b or c".
func joinWords(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	out := ""
	for k, w := range words[:len(words)-1] {
		if k > 0 {
			out += ", "
		}
		out += w
	}

	return out + " or " + words[len(words)-1]
}
