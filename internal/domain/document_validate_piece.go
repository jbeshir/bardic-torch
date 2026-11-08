package domain

import (
	"fmt"
	"slices"
	"strings"
)

// Validate returns the problems of a piece document.
func (d PieceDocument) Validate(v ValidationContext[PieceDocument]) []DocumentProblem {
	prev := DefaultPieceDocument()
	if v.Previous != nil {
		prev = *v.Previous
	}
	c := newDocCheck(d, v.Previous, v.Refs, v.ID)
	c.withheldSources = v.withheldSources
	c.document(d, d.SchemaVersion, v.Summary)
	c.pieceFields(d)
	c.pieceTerms(d.Terms)
	relations := make([]string, 0, len(d.Relations))
	for _, r := range d.Relations {
		relations = append(relations, fmt.Sprint(r.ToPieceID, r.Type))
	}
	c.relations(relations)
	c.credits(d.Credits)
	c.links(c.piecePerformances(d.Links))
	c.pieceSources(d, prev, v.Summary)
	c.preferences(d.Preferences, EntityTypePiece)
	c.references(documentReferences(d))

	return c.problems
}

// pieceFields checks a piece's own fields: its title, Markdown, kinds, period and aliases.
func (c *docCheck) pieceFields(d PieceDocument) {
	c.title(d.Title, "/title")
	for _, f := range []struct {
		s    *string
		path string
	}{
		{d.NotesMD, "/notes_md"}, {d.VersionNotesMD, "/version_notes_md"},
		{d.LicensingNotesMD, "/licensing_notes_md"}, {d.HistoricalSourceMD, "/historical_source_md"},
	} {
		c.text(f.s, f.path)
	}
	kinds := append([]string{string(d.Kind)}, stringsOf(d.SecondaryKinds)...)
	for k := 1; k < len(kinds); k++ {
		c.check(!slices.Contains(kinds[:k], kinds[k]), fmt.Sprintf("/secondary_kinds/%d", k-1), ProblemDuplicate)
	}
	for _, p := range []struct {
		year *int16
		path string
	}{{d.PeriodStart, "/period_start"}, {d.PeriodEnd, "/period_end"}} {
		c.check(p.year == nil || *p.year >= minPeriodYear && *p.year <= maxYear, p.path, ProblemOutOfRange)
	}
	c.check(d.PeriodStart == nil || d.PeriodEnd == nil || *d.PeriodStart <= *d.PeriodEnd, "/period_end",
		ProblemOutOfRange)
	c.check(d.Licence == nil || d.LicensingStatus == LicensingStatusKnown, "/licence", ProblemLicenceNeedsKnown)
	c.cap(len(d.Aliases), maxAliases, "/aliases")
	aliases := make([]string, 0, len(d.Aliases))
	for k, a := range d.Aliases {
		c.title(a.Text, fmt.Sprintf("/aliases/%d/text", k))
		aliases = append(aliases, CollationKey(a.Text))
	}
	c.unique(aliases, "/aliases")
}

// relations checks a document's relations, by their keys: at most maxRelations, each once.
func (c *docCheck) relations(keys []string) {
	c.cap(len(keys), maxRelations, "/relations")
	c.unique(keys, "/relations")
}

// piecePerformances checks the performances of a piece's links, and returns the links.
func (c *docCheck) piecePerformances(links []DocPieceLink) []DocLink {
	out := make([]DocLink, 0, len(links))
	for k, l := range links {
		out = append(out, l.DocLink)
		c.performances(k, l.Performances)
	}

	return out
}

// pieceTerms checks a piece's terms: each once, at most one primary term per facet, and a part only on an origin
// term.
func (c *docCheck) pieceTerms(terms []DocPieceTerm) {
	c.cap(len(terms), maxPieceTerms, "/terms")
	ids := make([]string, 0, len(terms))
	primaries := map[Facet]bool{}
	for k, t := range terms {
		ids = append(ids, fmt.Sprint(t.TermID))
		term, ok := c.refs.Terms[t.TermID]
		if !ok {
			continue
		}
		if t.IsPrimary {
			c.check(!primaries[term.Facet], fmt.Sprintf("/terms/%d/is_primary", k), ProblemSecondPrimaryTerm)
			primaries[term.Facet] = true
		}
		c.check(t.Part == nil || term.Facet == FacetOrigin, fmt.Sprintf("/terms/%d/part", k), ProblemPartNotAllowed)
	}
	c.unique(ids, "/terms")
}

// credits checks a piece's credits: a person iff named (chk_piece_credits_named_person), and the note.
func (c *docCheck) credits(credits []DocCredit) {
	c.cap(len(credits), maxCredits, "/credits")
	for k, cr := range credits {
		path := fmt.Sprintf("/credits/%d", k)
		c.check(cr.Attribution != AttributionNamed || cr.PersonID != nil, path+"/person", ProblemNamedNeedsPerson)
		c.check(cr.Attribution == AttributionNamed || cr.PersonID == nil, path+"/attribution",
			ProblemPersonNeedsNamed)
		c.optionalVarchar(cr.Note, maxCreditNoteLength, path+"/note")
	}
}

// pieceSources checks a piece's sourced fields (licensing, conservation and source URLs).
func (c *docCheck) pieceSources(d, prev PieceDocument, summary string) {
	links := make([]DocLink, 0, len(d.Links))
	for _, l := range d.Links {
		links = append(links, l.DocLink)
	}
	licensed := hasLicensingSource(links)
	c.licensingSources(d.LicensingNotesMD, prev.LicensingNotesMD, d.LicensingStatus, prev.LicensingStatus, licensed)
	if d.Licence != nil && !EqualPointers(d.Licence, prev.Licence) {
		c.check(licensed, "/licence", ProblemSourceRequired)
	}
	c.sourceURL(d.Conservation.SourceURL, "/conservation/source_url")
	changed := d.Conservation.Status != prev.Conservation.Status
	if d.Conservation.Status != ConservationStatusUnassessed || changed {
		c.requireSource(d.Conservation.SourceURL, "/conservation/source_url")
	}
	if changed {
		c.check(strings.TrimSpace(summary) != "", "/summary", ProblemReasonRequired)
	}
}
