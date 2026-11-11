package domain

import (
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

// Validate checks that the import can be written as it is, so that a dry run that validates is an import that
// writes: every document passes its Validate, and the import's own fields hold:
//   - IDs are positive and unique per table (revisions across all entities), slugs are slugs and unique per table,
//     legacy page IDs are unique per table, sort keys are set and each letter is its key's (SortLetter);
//   - every string is valid UTF-8 in NFC, holds no control character (IsControlCharacter) and fits its column;
//   - every reference resolves, and the parents (and related affiliations) of terms and the parents of collections
//     come before them, so the writer can insert in list order;
//   - each document link's URL normalises to a Links row whose fields it carries, roles aside;
//   - every summary is an import summary;
//   - no name, title, alias, slug, sort key or uploader holds a withheld-text placeholder (HoldsWithheldText):
//     only free text may.
//
// It fails with ErrInvalidImport, naming the entity type, ID and field of the first problem, never its value.
func (imp CatalogueImport) Validate() error {
	v := importValidator{imp: imp, revisions: map[int64]bool{}}
	v.index()
	steps := []func() error{v.checkTerms, v.checkCollections, v.checkEvents, v.checkLinks, v.checkPersons, v.checkPieces}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}

	return nil
}

// importValidator holds the IDs of each table, the context documents are validated in, and the links by normalised
// URL.
type importValidator struct {
	imp       CatalogueImport
	ctx       ValidationContext
	events    map[int64]bool
	persons   map[int64]bool
	pieces    map[int64]bool
	links     map[string]DocLink
	revisions map[int64]bool
}

func (v *importValidator) index() {
	v.ctx = ValidationContext{Terms: map[int64]TermInfo{}, CollectionParents: map[int64]*int64{}}
	v.events, v.persons, v.pieces, v.links = map[int64]bool{}, map[int64]bool{}, map[int64]bool{}, map[string]DocLink{}
	for _, t := range v.imp.Terms {
		v.ctx.Terms[t.ID] = TermInfo{Facet: t.Doc.Facet, ParentID: t.Doc.ParentID, Name: t.Doc.Name}
	}
	for _, c := range v.imp.Collections {
		v.ctx.CollectionParents[c.ID] = c.Doc.ParentID
	}
	for _, e := range v.imp.Events {
		v.events[e.ID] = true
	}
	for _, p := range v.imp.Persons {
		v.persons[p.ID] = true
	}
	for _, p := range v.imp.Pieces {
		v.pieces[p.ID] = true
	}
	for _, l := range v.imp.Links {
		if u, err := NormalizeURL(l.Link.URL); err == nil {
			v.links[u] = l.Link
		}
	}
}

// context is the validation context of the entity id.
func (v *importValidator) context(id int64) ValidationContext {
	ctx := v.ctx
	ctx.ID = id

	return ctx
}

// invalid is the error for a problem with a field of the entity of type entity and ID id.
func invalid(entity EntityType, id int64, field, problem string) error {
	return fmt.Errorf("%w: %s %d: %s: %s", ErrInvalidImport, entity, id, field, problem)
}

// entityCheck checks the fields of one entity, keeping the first problem.
type entityCheck struct {
	entity EntityType
	id     int64
	err    error
}

func (c *entityCheck) fail(field, problem string) {
	if c.err == nil {
		c.err = invalid(c.entity, c.id, field, problem)
	}
}

func (c *entityCheck) require(ok bool, field, problem string) {
	if !ok {
		c.fail(field, problem)
	}
}

// problems fails with the first of ps, its path below prefix.
func (c *entityCheck) problems(prefix string, ps []Problem) {
	if len(ps) > 0 {
		c.fail(prefix+ps[0].Path, string(ps[0].Code))
	}
}

// system checks the system fields every entity has: its ID, slug (if it has one), legacy page and revision, and
// the strings of the whole entity (contents).
func (v *importValidator) system(c *entityCheck, ids map[int64]bool, slugs map[string]bool, slug *string,
	legacy *LegacyPage, legacyIDs map[uint32]bool, rev ImportedRevision, entity any,
) {
	c.require(c.id > 0, "id", "is not positive")
	c.require(!ids[c.id], "id", "is not unique")
	ids[c.id] = true
	if slug != nil {
		c.require(IsSlug(*slug), "slug", "is not a slug")
		c.require(!HoldsWithheldText(*slug), "slug", "holds a withheld-text placeholder")
		c.require(!slugs[*slug], "slug", "is not unique")
		slugs[*slug] = true
	}
	var p problems
	if legacy != nil {
		p.varchar(&legacy.Title, maxNameLength, "legacy_title")
		if legacy.PageID != nil {
			c.require(!legacyIDs[*legacy.PageID], "legacy_wiki_pageid", "is not unique")
			legacyIDs[*legacy.PageID] = true
		}
	}
	c.require(rev.ID > 0, "revision.id", "is not positive")
	c.require(!v.revisions[rev.ID], "revision.id", "is not unique")
	v.revisions[rev.ID] = true
	c.require(IsImportSummary(rev.Summary), "revision.summary", "is not an import summary")
	p.check(utf8.RuneCountInString(rev.Summary) <= maxSummaryLength, "revision.summary", ProblemTooLong)
	c.require(!rev.CreatedAt.IsZero(), "revision.created_at", "is not set")
	c.require(len(rev.Contributors) <= maxContributorsCount, "revision.contributors", "has too many entries")
	for k, name := range rev.Contributors {
		p.name(name, fmt.Sprintf("revision.contributors[%d]", k))
	}
	p.contents(reflect.ValueOf(entity), "")
	c.problems("", p)
}

// sortKey checks a sort key and its letter, which must be the key's (SortLetter).
func (c *entityCheck) sortKey(key string, letter Letter, field string) {
	var p problems
	p.name(key, field)
	c.problems("", p)
	c.require(!HoldsWithheldText(key), field, "holds a withheld-text placeholder")
	c.require(letter == SortLetter(key), "sort_letter", "is not the letter of the sort key")
}

// withheldKey is the DuplicateKey of the withheld-text placeholder, which a name's key holds if the name holds it.
var withheldKey = DuplicateKey(WithheldEmail)

// names checks that no name (its DuplicateKey) holds a withheld-text placeholder.
func (c *entityCheck) names(keys []string) {
	for _, key := range keys {
		c.require(!strings.Contains(key, withheldKey), "doc", "a name holds a withheld-text placeholder")
	}
}

func (v *importValidator) checkTerms() error {
	ids, slugs, legacy := map[int64]bool{}, map[string]bool{}, map[uint32]bool{}
	for _, t := range v.imp.Terms {
		c := &entityCheck{entity: EntityTypeTerm, id: t.ID}
		v.system(c, ids, slugs, &t.Slug, t.Legacy, legacy, t.Revision, t)
		c.problems("doc.", t.Doc.Validate(v.context(t.ID)))
		c.names(t.Doc.DuplicateKeys())
		d := t.Doc
		if d.ParentID != nil {
			c.require(ids[*d.ParentID] && *d.ParentID != t.ID, "doc.parent_id", "is not an earlier term")
		}
		if d.RelatedAffiliationID != nil {
			c.require(ids[*d.RelatedAffiliationID] && *d.RelatedAffiliationID != t.ID, "doc.related_affiliation_id",
				"is not an earlier term")
		}
		if c.err != nil {
			return c.err
		}
	}

	return nil
}

func (v *importValidator) checkCollections() error {
	ids := map[int64]bool{}
	for _, col := range v.imp.Collections {
		c := &entityCheck{entity: EntityTypeCollection, id: col.ID}
		v.system(c, ids, nil, nil, nil, nil, col.Revision, col)
		c.sortKey(col.SortName, col.SortLetter, "sort_name")
		c.problems("doc.", col.Doc.Validate(v.context(col.ID)))
		c.names(col.Doc.DuplicateKeys())
		if col.Doc.ParentID != nil {
			c.require(ids[*col.Doc.ParentID] && *col.Doc.ParentID != col.ID, "doc.parent_id",
				"is not an earlier collection")
		}
		if c.err != nil {
			return c.err
		}
	}

	return nil
}

func (v *importValidator) checkEvents() error {
	ids, slugs, legacy := map[int64]bool{}, map[string]bool{}, map[uint32]bool{}
	for _, e := range v.imp.Events {
		c := &entityCheck{entity: EntityTypeEvent, id: e.ID}
		v.system(c, ids, slugs, &e.Slug, e.Legacy, legacy, e.Revision, e)
		c.problems("doc.", e.Doc.Validate(v.context(e.ID)))
		c.names(e.Doc.DuplicateKeys())
		if c.err != nil {
			return c.err
		}
	}

	return nil
}

func (v *importValidator) checkPersons() error {
	ids, slugs, legacy := map[int64]bool{}, map[string]bool{}, map[uint32]bool{}
	for _, p := range v.imp.Persons {
		c := &entityCheck{entity: EntityTypePerson, id: p.ID}
		v.system(c, ids, slugs, &p.Slug, p.Legacy, legacy, p.Revision, p)
		c.sortKey(p.SortName, p.SortLetter, "sort_name")
		c.problems("doc.", p.Doc.Validate(v.context(p.ID)))
		c.names(p.Doc.DuplicateKeys())
		var refs SnapshotReferences
		p.Doc.AddReferences(p.ID, &refs)
		v.references(c, refs)
		links := make([]DocLink, 0, len(p.Doc.Links))
		for _, l := range p.Doc.Links {
			links = append(links, l.DocLink)
			for _, perf := range l.Performances {
				c.names(duplicateKeys(deref(perf.Uploader)...))
			}
		}
		v.docLinks(c, links)
		if c.err != nil {
			return c.err
		}
	}

	return nil
}

func (v *importValidator) checkPieces() error {
	ids, slugs, legacy := map[int64]bool{}, map[string]bool{}, map[uint32]bool{}
	for _, p := range v.imp.Pieces {
		c := &entityCheck{entity: EntityTypePiece, id: p.ID}
		v.system(c, ids, slugs, &p.Slug, p.Legacy, legacy, p.Revision, p)
		c.sortKey(p.SortTitle, p.SortLetter, "sort_title")
		c.problems("doc.", p.Doc.Validate(v.context(p.ID)))
		c.names(p.Doc.DuplicateKeys())
		var refs SnapshotReferences
		p.Doc.AddReferences(&refs)
		v.references(c, refs)
		links := make([]DocLink, 0, len(p.Doc.Links))
		for _, l := range p.Doc.Links {
			links = append(links, l.DocLink)
			for _, perf := range l.Performances {
				c.names(duplicateKeys(deref(perf.Uploader)...))
			}
		}
		v.docLinks(c, links)
		if c.err != nil {
			return c.err
		}
	}

	return nil
}

// references checks that the pieces, persons and events of refs are in the import (Validate checks the terms and
// collections).
func (v *importValidator) references(c *entityCheck, refs SnapshotReferences) {
	for _, r := range []struct {
		ids   []int64
		known map[int64]bool
		what  EntityType
	}{{refs.PieceIDs, v.pieces, EntityTypePiece}, {refs.PersonIDs, v.persons, EntityTypePerson},
		{refs.EventIDs, v.events, EntityTypeEvent}} {
		for _, id := range r.ids {
			c.require(r.known[id], "doc", fmt.Sprintf("references %s %d, which is not in the import", r.what, id))
		}
	}
}

// docLinks checks that each of a document's links carries the fields of the Links row of its normalised URL.
func (v *importValidator) docLinks(c *entityCheck, links []DocLink) {
	for k, l := range links {
		field := fmt.Sprintf("doc.links[%d]", k)
		u, _ := NormalizeURL(l.URL)
		row, ok := v.links[u]
		c.require(ok, field+".url", "has no links row")
		c.require(!ok || equalLinkFields(row, l), field, "does not carry its links row's fields")
	}
}

// equalLinkFields reports whether a and b have the same fields, roles aside.
func equalLinkFields(a, b DocLink) bool {
	a.Roles, b.Roles = nil, nil

	return reflect.DeepEqual(a, b)
}

// checkLinks checks the Links rows: unique IDs and normalised URLs, the link's fields, no roles and known
// collections.
func (v *importValidator) checkLinks() error {
	ids, urls := map[int64]bool{}, map[string]bool{}
	for _, row := range v.imp.Links {
		c := &entityCheck{entity: "link", id: row.ID}
		l := row.Link
		c.require(row.ID > 0, "id", "is not positive")
		c.require(!ids[row.ID], "id", "is not unique")
		ids[row.ID] = true
		var p problems
		p.link(l, "")
		p.contents(reflect.ValueOf(row), "")
		c.problems("", p)
		u, err := NormalizeURL(l.URL)
		c.require(err != nil || !urls[u], "url", "has the normalised URL of another link")
		urls[u] = true
		c.require(len(l.Roles) == 0, "roles", "is set")
		if l.CollectionID != nil {
			_, ok := v.ctx.CollectionParents[*l.CollectionID]
			c.require(ok, "collection_id", "is not a collection")
		}
		if c.err != nil {
			return c.err
		}
	}

	return nil
}

// deref returns the strings of the optional s: none or one.
func deref(s *string) []string {
	if s == nil {
		return nil
	}

	return []string{*s}
}
