package domain

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// The column widths Validate checks: VARCHAR widths in characters, TEXT in bytes.
const (
	maxNameLength        = 255
	maxLinkTitleLength   = 500
	maxCreditNoteLength  = 500
	maxSummaryLength     = 500
	maxPlatformLength    = 64
	maxPronounsLength    = 64
	maxTextBytes         = 65535
	maxContributorsCount = 65535
)

// Validate checks that the import can be written as it is: it mirrors every constraint of the catalogue schema
// (migrations 003–009) and the document rules, so that a dry run that validates is an import that writes:
//   - IDs are positive and unique per table (revisions across all entities), slugs are slugs and unique per table,
//     legacy page IDs are unique per table, sort keys are set and each letter is its key's (SortLetter);
//   - every string is valid UTF-8 in NFC, holds no control character (IsControlCharacter) and fits its column;
//     every document has the current schema version and encodes and decodes (DecodeDocument, so every vocabulary
//     value is known);
//   - every reference resolves, and the parents (and related affiliations) of terms and the parents of collections
//     come before them, so the writer can insert in list order;
//   - the UNIQUE indexes and primary keys of the child tables hold, names compared as the collation compares them
//     (CollationKey), and the CHECKs hold: a credit names a person iff its attribution is named, a period starts
//     before it ends, a licence needs a known licensing status, nothing relates to itself;
//   - each document link's URL normalises to a Links row whose fields it carries, roles aside;
//   - the terms' parents have a closure (TermAncestors), and every summary is an import summary;
//   - no name, title, alias, slug, sort key or uploader holds a withheld-text placeholder (HoldsWithheldText): a
//     name the import withholds is dropped with its entity, never published as a placeholder.
//
// It fails with ErrInvalidImport, naming the entity type, ID and field of the first problem, never its value.
func (imp CatalogueImport) Validate() error {
	v := importValidator{imp: imp, revisions: map[int64]bool{}}
	v.index()
	steps := []func() error{
		v.checkTerms, v.checkCollections, v.checkEvents, v.checkLinks, v.checkPersons, v.checkPieces, v.checkStrings,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}

	return nil
}

// importValidator holds the IDs of each table and the links by normalised URL.
type importValidator struct {
	imp         CatalogueImport
	terms       map[int64]TermDocument
	collections map[int64]bool
	events      map[int64]bool
	persons     map[int64]bool
	pieces      map[int64]bool
	links       map[string]DocLink
	revisions   map[int64]bool
}

func (v *importValidator) index() {
	v.terms, v.collections, v.events = map[int64]TermDocument{}, map[int64]bool{}, map[int64]bool{}
	v.persons, v.pieces, v.links = map[int64]bool{}, map[int64]bool{}, map[string]DocLink{}
	for _, t := range v.imp.Terms {
		v.terms[t.ID] = t.Doc
	}
	for _, c := range v.imp.Collections {
		v.collections[c.ID] = true
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

// name checks a name or title of the catalogue (a required VARCHAR(255)): it fits, and holds no withheld-text
// placeholder (WithheldName, WithheldEmail), which only free text may hold.
func (c *entityCheck) name(s, field string) {
	c.varchar(s, maxNameLength, field, true)
	c.require(!HoldsWithheldText(s), field, "holds a withheld-text placeholder")
}

// varchar checks a VARCHAR(n) value: at most n characters, and not empty if required.
func (c *entityCheck) varchar(s string, n int, field string, required bool) {
	c.require(!required || s != "", field, "is empty")
	c.require(utf8.RuneCountInString(s) <= n, field, fmt.Sprintf("is longer than %d characters", n))
}

// optionalVarchar checks a nullable VARCHAR(n) value.
func (c *entityCheck) optionalVarchar(s *string, n int, field string) {
	if s != nil {
		c.varchar(*s, n, field, false)
	}
}

// text checks a nullable TEXT value.
func (c *entityCheck) text(s *string, field string) {
	if s != nil {
		c.require(len(*s) <= maxTextBytes, field, fmt.Sprintf("is longer than %d bytes", maxTextBytes))
	}
}

// url checks a nullable VARCHAR(2048) URL.
func (c *entityCheck) url(s *string, field string) {
	if s != nil {
		c.require(len(*s) <= MaxURLLength, field, fmt.Sprintf("is longer than %d bytes", MaxURLLength))
	}
}

// unique checks that no two of keys are equal.
func (c *entityCheck) unique(keys []string, field string) {
	seen := map[string]bool{}
	for k, key := range keys {
		c.require(!seen[key], fmt.Sprintf("%s[%d]", field, k), "duplicates an earlier entry")
		seen[key] = true
	}
}

// system checks the system fields every entity has: its ID, slug (if it has one), legacy page and revision.
func (v *importValidator) system(c *entityCheck, ids map[int64]bool, slugs map[string]bool, slug *string,
	legacy *LegacyPage, legacyIDs map[uint32]bool, rev ImportedRevision,
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
	if legacy != nil {
		c.varchar(legacy.Title, maxNameLength, "legacy_title", false)
		if legacy.PageID != nil {
			c.require(!legacyIDs[*legacy.PageID], "legacy_wiki_pageid", "is not unique")
			legacyIDs[*legacy.PageID] = true
		}
	}
	c.require(rev.ID > 0, "revision.id", "is not positive")
	c.require(!v.revisions[rev.ID], "revision.id", "is not unique")
	v.revisions[rev.ID] = true
	c.require(IsImportSummary(rev.Summary), "revision.summary", "is not an import summary")
	c.varchar(rev.Summary, maxSummaryLength, "revision.summary", true)
	c.require(!rev.CreatedAt.IsZero(), "revision.created_at", "is not set")
	c.require(len(rev.Contributors) <= maxContributorsCount, "revision.contributors", "has too many entries")
	for k, name := range rev.Contributors {
		c.varchar(name, maxNameLength, fmt.Sprintf("revision.contributors[%d]", k), true)
	}
}

// sortKey checks a sort key and its letter, which must be the key's (SortLetter).
func (c *entityCheck) sortKey(key string, letter Letter, field string) {
	c.name(key, field)
	c.require(letter == SortLetter(key), "sort_letter", "is not the letter of the sort key")
}

// document checks that doc has the current schema version and decodes as a document of its type.
func document[D Document](c *entityCheck, doc D, version int) {
	c.require(version == DocumentSchemaVersion, "doc.schema_version", "is not the current schema version")
	data, err := json.Marshal(doc)
	if err != nil {
		c.fail("doc", "does not encode")

		return
	}
	if _, err := DecodeDocument[D](data); err != nil {
		c.fail("doc", "does not decode")
	}
}

func (v *importValidator) checkTerms() error {
	ids, slugs, legacy := map[int64]bool{}, map[string]bool{}, map[uint32]bool{}
	names := map[string]bool{}
	for _, t := range v.imp.Terms {
		c := &entityCheck{entity: EntityTypeTerm, id: t.ID}
		v.system(c, ids, slugs, &t.Slug, t.Legacy, legacy, t.Revision)
		document(c, t.Doc, t.Doc.SchemaVersion)
		d := t.Doc
		c.name(d.Name, "doc.name")
		c.name(d.DisplayName, "doc.display_name")
		c.text(d.DescriptionMD, "doc.description_md")
		key := string(d.Facet) + "\x00" + CollationKey(d.Name)
		c.require(!names[key], "doc.name", "is not unique in its facet")
		names[key] = true
		if d.ParentID != nil {
			c.require(ids[*d.ParentID] && *d.ParentID != t.ID, "doc.parent_id", "is not an earlier term")
		}
		if d.RelatedAffiliationID != nil {
			c.require(ids[*d.RelatedAffiliationID] && *d.RelatedAffiliationID != t.ID, "doc.related_affiliation_id",
				"is not an earlier term")
			c.require(v.terms[*d.RelatedAffiliationID].Facet == FacetAffiliation, "doc.related_affiliation_id",
				"is not an affiliation term")
		}
		aliases := make([]string, 0, len(d.Aliases))
		for k, a := range d.Aliases {
			c.name(a, fmt.Sprintf("doc.aliases[%d]", k))
			aliases = append(aliases, CollationKey(a))
		}
		c.unique(aliases, "doc.aliases")
		if c.err != nil {
			return c.err
		}
	}
	if _, err := v.imp.TermAncestors(); err != nil {
		return fmt.Errorf("%w: terms: %w", ErrInvalidImport, err)
	}

	return nil
}

func (v *importValidator) checkCollections() error {
	ids := map[int64]bool{}
	for _, col := range v.imp.Collections {
		c := &entityCheck{entity: EntityTypeCollection, id: col.ID}
		v.system(c, ids, nil, nil, nil, nil, col.Revision)
		c.sortKey(col.SortName, col.SortLetter, "sort_name")
		document(c, col.Doc, col.Doc.SchemaVersion)
		d := col.Doc
		c.name(d.Name, "doc.name")
		c.url(d.URL, "doc.url")
		c.text(d.DescriptionMD, "doc.description_md")
		if d.RegionTermID != nil {
			_, ok := v.terms[*d.RegionTermID]
			c.require(ok, "doc.region_term_id", "is not a term")
		}
		if d.ParentID != nil {
			c.require(ids[*d.ParentID] && *d.ParentID != col.ID, "doc.parent_id", "is not an earlier collection")
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
		v.system(c, ids, slugs, &e.Slug, e.Legacy, legacy, e.Revision)
		document(c, e.Doc, e.Doc.SchemaVersion)
		c.name(e.Doc.Name, "doc.name")
		c.text(e.Doc.DescriptionMD, "doc.description_md")
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
		v.system(c, ids, slugs, &p.Slug, p.Legacy, legacy, p.Revision)
		c.sortKey(p.SortName, p.SortLetter, "sort_name")
		document(c, p.Doc, p.Doc.SchemaVersion)
		d := p.Doc
		c.name(d.Name, "doc.name")
		c.url(d.StatusSourceURL, "doc.status_source_url")
		c.optionalVarchar(d.Pronouns, maxPronounsLength, "doc.pronouns")
		c.text(d.NotesMD, "doc.notes_md")
		c.text(d.LicensingNotesMD, "doc.licensing_notes_md")
		aliases := make([]string, 0, len(d.Aliases))
		for k, a := range d.Aliases {
			c.name(a.Name, fmt.Sprintf("doc.aliases[%d].name", k))
			aliases = append(aliases, CollationKey(a.Name))
		}
		c.unique(aliases, "doc.aliases")
		c.unique(stringsOf(d.Roles), "doc.roles")
		affiliations := make([]string, 0, len(d.AffiliationIDs))
		for k, id := range d.AffiliationIDs {
			t, ok := v.terms[id]
			c.require(ok && t.Facet == FacetAffiliation, fmt.Sprintf("doc.affiliation_ids[%d]", k),
				"is not an affiliation term")
			affiliations = append(affiliations, fmt.Sprint(id))
		}
		c.unique(affiliations, "doc.affiliation_ids")
		relations := make([]string, 0, len(d.Relations))
		for k, r := range d.Relations {
			field := fmt.Sprintf("doc.relations[%d].to_person_id", k)
			c.require(v.persons[r.ToPersonID], field, "is not a person")
			c.require(r.ToPersonID != p.ID, field, "is the person itself")
			relations = append(relations, fmt.Sprint(r.ToPersonID, r.Type))
		}
		c.unique(relations, "doc.relations")
		links := make([]DocLink, 0, len(d.Links))
		for k, l := range d.Links {
			links = append(links, l.DocLink)
			for j, perf := range l.Performances {
				field := fmt.Sprintf("doc.links[%d].performances[%d]", k, j)
				if perf.PieceID != nil {
					c.require(v.pieces[*perf.PieceID], field+".piece_id", "is not a piece")
				}
				v.performance(c, perf.DocPerformance, field)
			}
		}
		v.docLinks(c, links)
		v.preferences(c, d.Preferences)
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
		v.system(c, ids, slugs, &p.Slug, p.Legacy, legacy, p.Revision)
		c.sortKey(p.SortTitle, p.SortLetter, "sort_title")
		document(c, p.Doc, p.Doc.SchemaVersion)
		v.pieceDocument(c, p)
		if c.err != nil {
			return c.err
		}
	}

	return nil
}

func (v *importValidator) pieceDocument(c *entityCheck, p ImportedPiece) {
	d := p.Doc
	c.name(d.Title, "doc.title")
	for _, f := range []struct {
		s     *string
		field string
	}{
		{d.NotesMD, "doc.notes_md"}, {d.VersionNotesMD, "doc.version_notes_md"},
		{d.LicensingNotesMD, "doc.licensing_notes_md"}, {d.HistoricalSourceMD, "doc.historical_source_md"},
	} {
		c.text(f.s, f.field)
	}
	c.url(d.Conservation.SourceURL, "doc.conservation.source_url")
	c.require(d.PeriodStart == nil || d.PeriodEnd == nil || *d.PeriodStart <= *d.PeriodEnd, "doc.period_end",
		"is before period_start")
	c.require(d.Licence == nil || d.LicensingStatus == LicensingStatusKnown, "doc.licence",
		"is set while the licensing status is not known")
	c.unique(stringsOf(d.SecondaryKinds), "doc.secondary_kinds")
	aliases := make([]string, 0, len(d.Aliases))
	for k, a := range d.Aliases {
		c.name(a.Text, fmt.Sprintf("doc.aliases[%d].text", k))
		aliases = append(aliases, CollationKey(a.Text))
	}
	c.unique(aliases, "doc.aliases")
	v.pieceTerms(c, d.Terms)
	relations := make([]string, 0, len(d.Relations))
	for k, r := range d.Relations {
		field := fmt.Sprintf("doc.relations[%d].to_piece_id", k)
		c.require(v.pieces[r.ToPieceID], field, "is not a piece")
		c.require(r.ToPieceID != p.ID, field, "is the piece itself")
		relations = append(relations, fmt.Sprint(r.ToPieceID, r.Type))
	}
	c.unique(relations, "doc.relations")
	v.credits(c, d.Credits)
	links := make([]DocLink, 0, len(d.Links))
	for k, l := range d.Links {
		links = append(links, l.DocLink)
		for j, perf := range l.Performances {
			v.performance(c, perf, fmt.Sprintf("doc.links[%d].performances[%d]", k, j))
		}
	}
	v.docLinks(c, links)
	v.preferences(c, d.Preferences)
}

// pieceTerms checks a piece's terms: each a term, once (piece_terms' primary key), with at most one primary term
// per facet.
func (v *importValidator) pieceTerms(c *entityCheck, terms []DocPieceTerm) {
	ids := make([]string, 0, len(terms))
	primaries := map[Facet]bool{}
	for k, t := range terms {
		term, ok := v.terms[t.TermID]
		c.require(ok, fmt.Sprintf("doc.terms[%d].term_id", k), "is not a term")
		if t.IsPrimary {
			c.require(!primaries[term.Facet], fmt.Sprintf("doc.terms[%d].is_primary", k),
				"is a second primary term of its facet")
			primaries[term.Facet] = true
		}
		ids = append(ids, fmt.Sprint(t.TermID))
	}
	c.unique(ids, "doc.terms")
}

// credits checks a piece's credits: a person iff named (chk_piece_credits_named_person), a known person, and
// the note's width.
func (v *importValidator) credits(c *entityCheck, credits []DocCredit) {
	for k, cr := range credits {
		field := fmt.Sprintf("doc.credits[%d]", k)
		c.require((cr.Attribution == AttributionNamed) == (cr.PersonID != nil), field+".person_id",
			"is set iff the attribution is named, but is not")
		if cr.PersonID != nil {
			c.require(v.persons[*cr.PersonID], field+".person_id", "is not a person")
		}
		c.optionalVarchar(cr.Note, maxCreditNoteLength, field+".note")
	}
}

// docLinks checks a document's links: one per normalised URL (link_attachments' UNIQUE indexes), each with roles,
// and each carrying the fields of its Links row.
func (v *importValidator) docLinks(c *entityCheck, links []DocLink) {
	urls := make([]string, 0, len(links))
	for k, l := range links {
		field := fmt.Sprintf("doc.links[%d]", k)
		u, err := NormalizeURL(l.URL)
		if err != nil {
			c.fail(field+".url", "does not normalise")

			continue
		}
		urls = append(urls, u)
		row, ok := v.links[u]
		c.require(ok, field+".url", "has no links row")
		c.require(!ok || equalLinkFields(row, l), field, "does not carry its links row's fields")
		c.require(len(l.Roles) > 0, field+".roles", "is empty")
		c.unique(stringsOf(l.Roles), field+".roles")
	}
	c.unique(urls, "doc.links")
}

// equalLinkFields reports whether a and b have the same fields, roles aside.
func equalLinkFields(a, b DocLink) bool {
	a.Roles, b.Roles = nil, nil

	return reflect.DeepEqual(a, b)
}

func (v *importValidator) performance(c *entityCheck, p DocPerformance, field string) {
	if p.EventID != nil {
		c.require(v.events[*p.EventID], field+".event_id", "is not an event")
	}
	if p.Uploader != nil {
		c.name(*p.Uploader, field+".uploader")
	}
	c.require(len(p.DescriptionVerbatim) <= maxTextBytes, field+".description_verbatim",
		fmt.Sprintf("is longer than %d bytes", maxTextBytes))
	performers := make([]string, 0, len(p.PerformerIDs))
	for k, id := range p.PerformerIDs {
		c.require(v.persons[id], fmt.Sprintf("%s.performer_ids[%d]", field, k), "is not a person")
		performers = append(performers, fmt.Sprint(id))
	}
	c.unique(performers, field+".performer_ids")
}

func (v *importValidator) preferences(c *entityCheck, prefs []DocPreference) {
	dims := make([]string, 0, len(prefs))
	for k, p := range prefs {
		c.require(len(p.SourceURL) <= MaxURLLength && p.SourceURL != "",
			fmt.Sprintf("doc.preferences[%d].source_url", k), "is empty or too long")
		dims = append(dims, string(p.Dimension))
	}
	c.unique(dims, "doc.preferences")
}

// links checks the Links rows: unique IDs and normalised URLs, column widths, no roles and known collections.
func (v *importValidator) checkLinks() error {
	ids, urls := map[int64]bool{}, map[string]bool{}
	for _, row := range v.imp.Links {
		c := &entityCheck{entity: "link", id: row.ID}
		l := row.Link
		c.require(row.ID > 0, "id", "is not positive")
		c.require(!ids[row.ID], "id", "is not unique")
		ids[row.ID] = true
		u, err := NormalizeURL(l.URL)
		c.require(err == nil, "url", "does not normalise")
		c.require(err != nil || !urls[u], "url", "has the normalised URL of another link")
		urls[u] = true
		c.require(len(l.Roles) == 0, "roles", "is set")
		c.optionalVarchar(l.Title, maxLinkTitleLength, "title")
		c.text(l.Description, "description")
		c.optionalVarchar(l.Locator, maxNameLength, "locator")
		if l.Platform != nil {
			c.varchar(*l.Platform, maxPlatformLength, "platform", true)
			c.require(isASCII(*l.Platform), "platform", "is not ASCII")
		}
		if l.CollectionID != nil {
			c.require(v.collections[*l.CollectionID], "collection_id", "is not a collection")
		}
		if c.err != nil {
			return c.err
		}
	}

	return nil
}

// checkStrings checks that every string of the import is valid UTF-8 in NFC and holds no control character
// (IsControlCharacter).
func (v *importValidator) checkStrings() error {
	type entity struct {
		entity EntityType
		id     int64
		value  any
	}
	var all []entity
	for _, t := range v.imp.Terms {
		all = append(all, entity{EntityTypeTerm, t.ID, t})
	}
	for _, c := range v.imp.Collections {
		all = append(all, entity{EntityTypeCollection, c.ID, c})
	}
	for _, e := range v.imp.Events {
		all = append(all, entity{EntityTypeEvent, e.ID, e})
	}
	for _, p := range v.imp.Persons {
		all = append(all, entity{EntityTypePerson, p.ID, p})
	}
	for _, p := range v.imp.Pieces {
		all = append(all, entity{EntityTypePiece, p.ID, p})
	}
	for _, l := range v.imp.Links {
		all = append(all, entity{"link", l.ID, l})
	}
	for _, e := range all {
		if err := normalizedStrings(e.entity, e.id, reflect.ValueOf(e.value), ""); err != nil {
			return err
		}
	}

	return nil
}

// normalizedStrings checks every string in v, descending as validateEnums does.
func normalizedStrings(entity EntityType, id int64, v reflect.Value, path string) error {
	kind := v.Kind()
	if kind == reflect.Pointer && !v.IsNil() {
		return normalizedStrings(entity, id, v.Elem(), path)
	}
	if (kind == reflect.Slice && v.Type() != reflect.TypeFor[json.RawMessage]()) || kind == reflect.Array {
		return normalizedListStrings(entity, id, v, path)
	}
	if kind == reflect.Struct {
		return normalizedFieldStrings(entity, id, v, path)
	}
	if kind == reflect.String {
		s := v.String()
		if !utf8.ValidString(s) || !norm.NFC.IsNormalString(s) {
			return invalid(entity, id, path, "is not valid UTF-8 in NFC")
		}
		if strings.ContainsFunc(s, IsControlCharacter) {
			return invalid(entity, id, path, "holds a control character")
		}
	}

	return nil
}

// normalizedFieldStrings is normalizedStrings for each exported field of the struct v.
func normalizedFieldStrings(entity EntityType, id int64, v reflect.Value, path string) error {
	for i := range v.NumField() {
		if f := v.Type().Field(i); f.IsExported() {
			if err := normalizedStrings(entity, id, v.Field(i), path+"."+f.Name); err != nil {
				return err
			}
		}
	}

	return nil
}

// normalizedListStrings is normalizedStrings for each element of the slice or array v.
func normalizedListStrings(entity EntityType, id int64, v reflect.Value, path string) error {
	for i := range v.Len() {
		if err := normalizedStrings(entity, id, v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}

	return nil
}

func stringsOf[S ~string](values []S) []string {
	out := make([]string, 0, len(values))
	for _, s := range values {
		out = append(out, string(s))
	}

	return out
}

func isASCII(s string) bool {
	for k := range len(s) {
		if s[k] >= utf8.RuneSelf {
			return false
		}
	}

	return true
}
