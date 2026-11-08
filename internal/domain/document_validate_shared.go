package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// performances checks the performances of the link numbered k.
func (c *docCheck) performances(k int, performances []DocPerformance) {
	c.cap(len(performances), maxPerformancesPerLink, fmt.Sprintf("/links/%d/performances", k))
	for j, p := range performances {
		c.performance(p, fmt.Sprintf("/links/%d/performances/%d", k, j))
	}
}

// Validate returns the problems of a term document.
func (d TermDocument) Validate(v ValidationContext[TermDocument]) []DocumentProblem {
	c := newDocCheck(d, v.Previous, v.Refs, v.ID)
	c.document(d, d.SchemaVersion, v.Summary)
	c.check(v.Previous == nil || v.Previous.Facet == d.Facet, "/facet", ProblemFacetImmutable)
	c.title(d.Name, "/name")
	c.title(d.DisplayName, "/display_name")
	for id, t := range v.Refs.Terms {
		c.check(id == v.ID || !t.Public || t.Facet != d.Facet || CollationKey(t.Name) != CollationKey(d.Name), "/name",
			ProblemDuplicate)
	}
	c.text(d.DescriptionMD, "/description_md")
	c.check(d.Position >= 0 && d.Position <= maxTermPosition, "/position", ProblemOutOfRange)
	c.cap(len(d.Aliases), maxAliases, "/aliases")
	aliases := make([]string, 0, len(d.Aliases))
	for k, a := range d.Aliases {
		path := fmt.Sprintf("/aliases/%d", k)
		c.title(a, path)
		c.check(CollationKey(a) != CollationKey(d.Name), path, ProblemDuplicate)
		aliases = append(aliases, CollationKey(a))
	}
	c.unique(aliases, "/aliases")
	c.references(documentReferences(d))
	if d.ParentID != nil && c.changed[fmt.Sprintf("parent:%d", *d.ParentID)] {
		c.termTree(d)
	}
	if d.RelatedAffiliationID != nil && c.changed[fmt.Sprintf("related_affiliation:%d", *d.RelatedAffiliationID)] {
		c.check(d.Facet == FacetTopic, "/related_affiliation", ProblemWrongFacet)
	}

	return c.problems
}

// Validate returns the problems of a collection document.
func (d CollectionDocument) Validate(v ValidationContext[CollectionDocument]) []DocumentProblem {
	c := newDocCheck(d, v.Previous, v.Refs, v.ID)
	c.document(d, d.SchemaVersion, v.Summary)
	c.title(d.Name, "/name")
	c.sourceURL(d.URL, "/url")
	c.text(d.DescriptionMD, "/description_md")
	c.references(documentReferences(d))
	if d.ParentID != nil && c.changed[fmt.Sprintf("parent:%d", *d.ParentID)] {
		c.collectionTree(d)
	}

	return c.problems
}

// Validate returns the problems of an event document.
func (d EventDocument) Validate(v ValidationContext[EventDocument]) []DocumentProblem {
	c := newDocCheck(d, v.Previous, v.Refs, v.ID)
	c.document(d, d.SchemaVersion, v.Summary)
	c.title(d.Name, "/name")
	c.text(d.DescriptionMD, "/description_md")

	return c.problems
}

// links checks a document's links: each normalizes, at most once (by normalized URL), with 1–12 roles, each once,
// and its fields fit.
func (c *docCheck) links(links []DocLink) {
	c.cap(len(links), maxLinks, "/links")
	urls := make([]string, 0, len(links))
	for k, l := range links {
		path := fmt.Sprintf("/links/%d", k)
		c.check(IsValidURL(l.URL), path+"/url", ProblemInvalidURL)
		urls = append(urls, linkKey(l.URL))
		c.optionalVarchar(l.Title, maxLinkTitleLength, path+"/title")
		c.text(l.Description, path+"/description")
		c.optionalVarchar(l.Locator, maxNameLength, path+"/locator")
		if l.Platform != nil {
			c.varchar(*l.Platform, maxPlatformLength, path+"/platform", true)
			c.check(c.previousStrings[*l.Platform] || CheckTextCharacters(*l.Platform) == nil, path+"/platform",
				ProblemInvalidCharacters)
		}
		c.check(len(l.Roles) > 0, path+"/roles", ProblemRequired)
		c.cap(len(l.Roles), maxLinkRoles, path+"/roles")
		c.unique(stringsOf(l.Roles), path+"/roles")
	}
	c.unique(urls, "/links")
}

// performance checks a performance at path.
func (c *docCheck) performance(p DocPerformance, path string) {
	c.check(p.Key == "" || validPerformanceKey(p.Key), path+"/key", ProblemOutOfRange)
	c.check(p.DescriptionVerbatim != "", path+"/description_verbatim", ProblemRequired)
	c.check(len(p.DescriptionVerbatim) <= maxTextBytes, path+"/description_verbatim", ProblemTooLong)
	c.check(p.Year == nil || *p.Year >= 0 && *p.Year <= maxYear, path+"/year", ProblemOutOfRange)
	if p.Uploader != nil {
		c.title(*p.Uploader, path+"/uploader")
	}
	c.cap(len(p.PerformerIDs), maxPerformers, path+"/performers")
	performers := make([]string, 0, len(p.PerformerIDs))
	for _, id := range p.PerformerIDs {
		performers = append(performers, fmt.Sprint(id))
	}
	c.unique(performers, path+"/performers")
}

// licensingSources checks the licensing rules pieces and persons share: changed licensing notes
// that are not empty, and a licensing status changed to known, need a licensing source.
func (c *docCheck) licensingSources(notes, prevNotes *string, status, prevStatus LicensingStatus, licensed bool) {
	if notes != nil && *notes != "" && !EqualPointers(notes, prevNotes) {
		c.check(licensed, "/licensing_notes_md", ProblemSourceRequired)
	}
	if status == LicensingStatusKnown && prevStatus != LicensingStatusKnown {
		c.check(licensed, "/licensing_status", ProblemSourceRequired)
	}
}

// hasLicensingSource reports whether links has a licensing source: a link with role licensing_source and a valid
// URL.
func hasLicensingSource(links []DocLink) bool {
	return slices.ContainsFunc(links, func(l DocLink) bool {
		return slices.Contains(l.Roles, LinkRoleLicensingSource) && IsValidURL(l.URL)
	})
}

// sourceURL checks a source URL field that is set: it must be a valid URL.
func (c *docCheck) sourceURL(s *string, path string) {
	c.check(s == nil || IsValidURL(*s), path, ProblemInvalidURL)
}

// requireSource checks that a source URL field a rule requires is set; sourceURL checks that it is valid.
func (c *docCheck) requireSource(s *string, path string) {
	c.check(s != nil && *s != "", path, ProblemSourceRequired)
}

// IsValidURL reports whether s is a valid source or link URL: at most MaxURLLength bytes, without control
// characters, and an absolute http(s) URL that NormalizeURL accepts.
func IsValidURL(s string) bool {
	if len(s) > MaxURLLength || strings.ContainsFunc(s, IsControlCharacter) {
		return false
	}
	_, err := NormalizeURL(s)

	return err == nil
}

// EqualPointers is whether two optional values are both nil or equal.
func EqualPointers[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// references checks the references of a document: each must exist and not be to the entity itself, and each the
// edit adds or changes must be to an entity its author may reference (public, or the author's own pending new term
// or person) and follow the rules of its field. A reference that is unchanged needs only to exist, so that a
// person's choosing not to have a page, for example, does not make every piece crediting them uneditable.
func (c *docCheck) references(refs []DocReference) {
	for _, r := range refs {
		if c.id != 0 && r.ID == c.id && selfReferenceUses[r.use] {
			c.report(r.Path, ProblemSelfReference)

			continue
		}
		changed := c.changed[r.Key]
		switch r.Type {
		case EntityTypePerson:
			c.personReference(r, changed)
		case EntityTypeTerm:
			c.termReference(r, changed)
		case EntityTypePiece, EntityTypeCollection, EntityTypeEvent:
			e, ok := c.entity(r)
			c.check(ok && (!changed || e.Public), r.Path, ProblemUnknownReference)
		}
	}
}

// entity is what the index knows of the referenced piece, collection or event.
func (c *docCheck) entity(r DocReference) (ReferencedEntity, bool) {
	if r.Type == EntityTypeCollection {
		e, ok := c.refs.Collections[r.ID]

		return e.ReferencedEntity, ok
	}
	e, ok := map[EntityType]map[int64]ReferencedEntity{
		EntityTypePiece: c.refs.Pieces, EntityTypeEvent: c.refs.Events,
	}[r.Type][r.ID]

	return e, ok
}

// selfReferenceUses are the fields that reference an entity of the document's own type.
var selfReferenceUses = map[referenceUse]bool{
	usePieceRelation: true, usePersonRelation: true, useTermParent: true, useRelatedAffiliation: true,
	useCollectionParent: true,
}

// noPageProblems are the problems of an added or changed reference to a person who prefers no page, by field.
var noPageProblems = map[referenceUse]ProblemCode{
	useCredit: ProblemCreditToNoPagePerson, usePersonRelation: ProblemRelationToNoPagePerson,
}

// affiliationUses are the fields that reference an affiliation term.
var affiliationUses = map[referenceUse]bool{
	useAffiliation: true, useRelatedAffiliation: true, useCollectionRegion: true,
}

func (c *docCheck) personReference(r DocReference, changed bool) {
	p, ok := c.refs.Persons[r.ID]
	if !ok || changed && !p.Public && !p.OwnPending {
		c.report(r.Path, ProblemUnknownReference)

		return
	}
	if code, ok := noPageProblems[r.use]; ok && changed && p.PrefersNoPage {
		c.report(r.Path, code)
	}
}

func (c *docCheck) termReference(r DocReference, changed bool) {
	t, ok := c.refs.Terms[r.ID]
	if !ok || changed && !t.Public && !t.OwnPending {
		c.report(r.Path, ProblemUnknownReference)

		return
	}
	if changed && r.use == usePieceTerm {
		c.check(t.Facet != FacetAffiliation, r.Path, ProblemWrongFacet)
	}
	if changed && affiliationUses[r.use] {
		c.check(t.Facet == FacetAffiliation, r.Path, ProblemWrongFacet)
	}
}

// termTree checks a term's new parent: of the term's facet, and leaving the facet's tree without a cycle and at
// most MaxTermDepth levels deep (TermClosure over the facet with the new parent).
func (c *docCheck) termTree(d TermDocument) {
	parent, ok := c.refs.Terms[*d.ParentID]
	if !ok {
		return
	}
	if parent.Facet != d.Facet {
		c.report("/parent", ProblemWrongFacet)

		return
	}
	parents := map[int64]*int64{c.id: d.ParentID}
	for id, t := range c.refs.Terms {
		if id != c.id && t.Facet == d.Facet {
			parents[id] = t.ParentID
		}
	}
	_, err := TermClosure(parents)
	switch {
	case errors.Is(err, ErrTermCycle):
		c.report("/parent", ProblemCycle)
	case errors.Is(err, ErrTermTooDeep):
		c.report("/parent", ProblemTooDeep)
	}
}

// collectionTree checks a collection's new parent: walking up from it reaches neither the collection itself nor
// more than maxCollectionParentDepth ancestors.
func (c *docCheck) collectionTree(d CollectionDocument) {
	depth := 0
	for parent := d.ParentID; parent != nil; depth++ {
		if c.id != 0 && *parent == c.id {
			c.report("/parent_id", ProblemCycle)

			return
		}
		if depth >= maxCollectionParentDepth {
			c.report("/parent_id", ProblemTooDeep)

			return
		}
		parent = c.refs.Collections[*parent].ParentID
	}
}
