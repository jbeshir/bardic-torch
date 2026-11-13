package domain

import (
	"errors"
	"fmt"
)

// Validate returns the problems of a piece document: the column widths and CHECKs of its tables, its terms of
// piece facets, its credits naming a person exactly when the attribution is named, and its children each once.
func (d PieceDocument) Validate(ctx ValidationContext) []Problem {
	var p problems
	p.document(d, d.SchemaVersion)
	p.name(d.Title, "title")
	p.text(d.NotesMD, "notes_md")
	p.text(d.VersionNotesMD, "version_notes_md")
	p.text(d.LicensingNotesMD, "licensing_notes_md")
	aliases := make([]string, 0, len(d.Aliases))
	for _, a := range d.Aliases {
		aliases = append(aliases, a.Text)
	}
	p.aliases(aliases, "aliases", ".text")
	p.pieceTerms(ctx.Terms, d.TermIDs)
	p.credits(d.Credits)
	links := make([]DocLink, 0, len(d.Links))
	for k, l := range d.Links {
		links = append(links, l.DocLink)
		for j, perf := range l.Performances {
			p.performance(perf, fmt.Sprintf("links[%d].performances[%d]", k, j))
		}
	}
	p.links(links)
	p.collections(ctx, links)

	return p
}

// pieceTerms checks a piece's terms: each a known term of a piece facet (not affiliation), once.
func (p *problems) pieceTerms(terms map[int64]TermInfo, termIDs []int64) {
	ids := make([]string, 0, len(termIDs))
	for k, id := range termIDs {
		path := fmt.Sprintf("term_slugs[%d]", k)
		term, ok := p.term(terms, id, "", path)
		p.check(!ok || term.Facet != FacetAffiliation, path, ProblemWrongFacet)
		ids = append(ids, fmt.Sprint(id))
	}
	p.unique(ids, "term_slugs")
}

// credits checks a piece's credits: a person exactly when the attribution is named
// (chk_piece_credits_named_person).
func (p *problems) credits(credits []DocCredit) {
	for k, c := range credits {
		path := fmt.Sprintf("credits[%d]", k)
		named := c.Attribution == AttributionNamed
		p.check(!named || c.PersonID != nil, path+".person_slug", ProblemRequired)
		p.check(named || c.PersonID == nil, path+".person_slug", ProblemInvalid)
	}
}

// collections checks that the collections of links are known.
func (p *problems) collections(ctx ValidationContext, links []DocLink) {
	for k, l := range links {
		if l.CollectionID != nil {
			p.check(ctx.Collections[*l.CollectionID], fmt.Sprintf("links[%d].collection_id", k),
				ProblemUnknownReference)
		}
	}
}

// Validate returns the problems of a person document: the column widths of its tables, affiliations that are
// affiliation terms, and its children each once.
func (d PersonDocument) Validate(ctx ValidationContext) []Problem {
	var p problems
	p.document(d, d.SchemaVersion)
	p.name(d.Name, "name")
	p.text(d.NotesMD, "notes_md")
	p.text(d.LicensingNotesMD, "licensing_notes_md")
	aliases := make([]string, 0, len(d.Aliases))
	for _, a := range d.Aliases {
		aliases = append(aliases, a.Name)
	}
	p.aliases(aliases, "aliases", ".name")
	p.unique(stringsOf(d.Roles), "roles")
	affiliations := make([]string, 0, len(d.AffiliationIDs))
	for k, id := range d.AffiliationIDs {
		p.term(ctx.Terms, id, FacetAffiliation, fmt.Sprintf("affiliation_slugs[%d]", k))
		affiliations = append(affiliations, fmt.Sprint(id))
	}
	p.unique(affiliations, "affiliation_slugs")
	links := make([]DocLink, 0, len(d.Links))
	for k, l := range d.Links {
		links = append(links, l.DocLink)
		for j, perf := range l.Performances {
			p.performance(perf.DocPerformance, fmt.Sprintf("links[%d].performances[%d]", k, j))
		}
	}
	p.links(links)
	p.collections(ctx, links)

	return p
}

// Validate returns the problems of a term document: its widths, a name unique in its facet (ctx.Terms holds
// the facet's terms), a parent of its facet leaving the tree without a cycle and at most MaxTermDepth deep, and a
// related affiliation that is an affiliation term.
func (d TermDocument) Validate(ctx ValidationContext) []Problem {
	var p problems
	p.document(d, d.SchemaVersion)
	p.name(d.Name, "name")
	p.name(d.DisplayName, "display_name")
	p.text(d.DescriptionMD, "description_md")
	for id, t := range ctx.Terms {
		p.check(id == ctx.ID || t.Facet != d.Facet || CollationKey(t.Name) != CollationKey(d.Name), "name",
			ProblemDuplicate)
	}
	if d.ParentID != nil {
		if _, ok := p.term(ctx.Terms, *d.ParentID, d.Facet, "parent_slug"); ok {
			p.termTree(ctx, d)
		}
	}
	if d.RelatedAffiliationID != nil {
		p.term(ctx.Terms, *d.RelatedAffiliationID, FacetAffiliation, "related_affiliation_slug")
	}

	return p
}

// termTree checks that the term's facet, with the term under its parent, has no cycle and no term deeper than
// MaxTermDepth (TermClosure).
func (p *problems) termTree(ctx ValidationContext, d TermDocument) {
	parents := map[int64]*int64{ctx.ID: d.ParentID}
	for id, t := range ctx.Terms {
		if id != ctx.ID && t.Facet == d.Facet {
			parents[id] = t.ParentID
		}
	}
	_, err := TermClosure(parents)
	p.check(!errors.Is(err, ErrTermCycle), "parent_slug", ProblemCycle)
	p.check(!errors.Is(err, ErrTermTooDeep), "parent_slug", ProblemTooDeep)
}

// Validate returns the problems of a collection document: its widths and a known region term.
func (d CollectionDocument) Validate(ctx ValidationContext) []Problem {
	var p problems
	p.document(d, d.SchemaVersion)
	p.name(d.Name, "name")
	p.url(d.URL, "url")
	p.text(d.DescriptionMD, "description_md")
	if d.RegionTermID != nil {
		p.term(ctx.Terms, *d.RegionTermID, "", "region_term_slug")
	}

	return p
}

// Validate returns the problems of an event document: its widths.
func (d EventDocument) Validate(ValidationContext) []Problem {
	var p problems
	p.document(d, d.SchemaVersion)
	p.name(d.Name, "name")

	return p
}
