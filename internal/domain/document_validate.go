package domain

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// The element caps of a document (D12), and the bounds of its numbers.
const (
	maxAliases               = 100
	maxPieceTerms            = 100
	maxCredits               = 100
	maxRelations             = 100
	maxAffiliations          = 100
	maxLinks                 = 200
	maxLinkRoles             = 12
	maxPerformancesPerLink   = 200
	maxPerformers            = 50
	minPeriodYear            = -3000
	maxYear                  = 2100
	maxTermPosition          = 32767
	maxCollectionParentDepth = 10
)

// ValidationContext is what validating a document of type D needs besides the document (D12).
type ValidationContext[D Document] struct {
	// Author is the role of the revision's author: the contributor restrictions apply to a contributor's
	// documents, whoever validates them.
	Author Role
	// ID is the entity's ID, or 0 for a new entity, for the rules against referencing oneself.
	ID int64
	// Previous is the document the sourced-field and reference rules compare with: the base at submit, the
	// current document at approval, rebase and revert. It is nil for a new entity, whose fields are then changed
	// when they are not the defaults (DefaultPieceDocument, DefaultPersonDocument).
	Previous *D
	// Refs describes the entities the document and Previous reference.
	Refs ReferenceIndex
	// Summary is the revision's summary, which a conservation status change needs as its reason.
	Summary string
	// Today is the date status dates may not be after.
	Today Date
	// editor is set for the first phase of ValidateSubmission, which validates the document as the contributor
	// submitted it, before restore.
	editor bool
}

// ReferenceIndex is what validation knows of the entities a document references, by ID. An ID in no map does not
// exist (or has been deleted). The maps may describe more entities than a document references.
type ReferenceIndex struct {
	Pieces  map[int64]ReferencedEntity
	Persons map[int64]ReferencedPerson
	// Terms are the referenced terms, and for a term document every term of its facet, which the depth and
	// cycle rules need.
	Terms map[int64]ReferencedTerm
	// Collections are the referenced collections and their ancestors.
	Collections map[int64]ReferencedCollection
	Events      map[int64]ReferencedEntity
}

// ReferencedEntity is whether a referenced entity is public, and whether it is a pending new entity of the
// revision's author, which only a term or a person may be.
type ReferencedEntity struct {
	Public     bool
	OwnPending bool
}

// ReferencedPerson is a referenced person.
type ReferencedPerson struct {
	ReferencedEntity
	PrefersNoPage bool
}

// ReferencedTerm is a referenced term. Name is its name if it is public, for the duplicate-name rule.
type ReferencedTerm struct {
	ReferencedEntity
	Facet    Facet
	ParentID *int64
	Name     string
}

// ReferencedCollection is a referenced collection.
type ReferencedCollection struct {
	ReferencedEntity
	ParentID *int64
}

// DefaultPieceDocument is what a new piece is compared with: no licensing information, not assessed.
func DefaultPieceDocument() PieceDocument {
	return PieceDocument{
		SchemaVersion: DocumentSchemaVersion, LicensingStatus: LicensingStatusUnknown,
		Conservation: DocConservation{Status: ConservationStatusUnassessed},
	}
}

// DefaultPersonDocument is what a new person is compared with: active, with no licensing information.
func DefaultPersonDocument() PersonDocument {
	return PersonDocument{
		SchemaVersion: DocumentSchemaVersion, Status: PersonStatusActive, LicensingStatus: LicensingStatusUnknown,
	}
}

// DefaultDocument is what a new entity's document of type D is compared with: DefaultPieceDocument,
// DefaultPersonDocument, or the empty document of the other types.
func DefaultDocument[D Document]() D {
	var doc D
	switch any(doc).(type) {
	case PieceDocument:
		return any(DefaultPieceDocument()).(D)
	case PersonDocument:
		return any(DefaultPersonDocument()).(D)
	}

	return doc
}

// ValidateSubmission validates a contributor's submission in two phases (D12), so that validation is never an
// oracle for what the contributor could not see:
//  1. editor, the document as submitted (before restore), compared with editorPrevious, the projection of the
//     base the contributor edited. Its problems, with paths into the document the contributor sees, are the
//     only problems returned.
//  2. restored, the document with the withheld elements put back, compared with v.Previous, the full base. Only
//     when phase 1 passes; each problem it finds exists because of data the contributor could not see, and is
//     returned as a hidden_data_conflict flag with its path into the full document.
func ValidateSubmission[D Document](editor, restored D, editorPrevious *D, v ValidationContext[D]) (
	[]DocumentProblem, []Flag,
) {
	phase1 := v
	phase1.Previous, phase1.editor = editorPrevious, true
	if problems := validateDocument(editor, phase1); len(problems) > 0 {
		return problems, nil
	}
	var flags []Flag
	for _, p := range validateDocument(restored, v) {
		flags = append(flags, Flag{Code: FlagHiddenDataConflict, Path: p.Path})
	}

	return nil, flags
}

// validateDocument is the Validate method of D.
func validateDocument[D Document](doc D, v ValidationContext[D]) []DocumentProblem {
	switch d := any(doc).(type) {
	case PieceDocument:
		return d.Validate(any(v).(ValidationContext[PieceDocument]))
	case PersonDocument:
		return d.Validate(any(v).(ValidationContext[PersonDocument]))
	case TermDocument:
		return d.Validate(any(v).(ValidationContext[TermDocument]))
	case CollectionDocument:
		return d.Validate(any(v).(ValidationContext[CollectionDocument]))
	case EventDocument:
		return d.Validate(any(v).(ValidationContext[EventDocument]))
	}

	return nil
}

// Validate returns the problems of a piece document (D11, D12).
func (d PieceDocument) Validate(v ValidationContext[PieceDocument]) []DocumentProblem {
	prev := DefaultPieceDocument()
	if v.Previous != nil {
		prev = *v.Previous
	}
	c := newDocCheck(d, v.Previous, v.Refs, v.ID)
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
	c.preferences(d.Preferences, prev.Preferences)
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

// personPerformances checks the performances of a person's links, and returns the links.
func (c *docCheck) personPerformances(links []DocPersonLink) []DocLink {
	out := make([]DocLink, 0, len(links))
	for k, l := range links {
		out = append(out, l.DocLink)
		performances := make([]DocPerformance, 0, len(l.Performances))
		for _, p := range l.Performances {
			performances = append(performances, p.DocPerformance)
		}
		c.performances(k, performances)
	}

	return out
}

// performances checks the performances of the link numbered k.
func (c *docCheck) performances(k int, performances []DocPerformance) {
	c.cap(len(performances), maxPerformancesPerLink, fmt.Sprintf("/links/%d/performances", k))
	for j, p := range performances {
		c.performance(p, fmt.Sprintf("/links/%d/performances/%d", k, j))
	}
}

// Validate returns the problems of a person document (D5, D11, D12).
func (d PersonDocument) Validate(v ValidationContext[PersonDocument]) []DocumentProblem {
	prev := DefaultPersonDocument()
	if v.Previous != nil {
		prev = *v.Previous
	}
	c := newDocCheck(d, v.Previous, v.Refs, v.ID)
	c.document(d, d.SchemaVersion, v.Summary)
	c.title(d.Name, "/name")
	c.optionalVarchar(d.Pronouns, maxPronounsLength, "/pronouns")
	c.text(d.NotesMD, "/notes_md")
	c.text(d.LicensingNotesMD, "/licensing_notes_md")
	c.personAliases(d, prev, v.Author)
	c.unique(stringsOf(d.Roles), "/roles")
	c.cap(len(d.AffiliationIDs), maxAffiliations, "/affiliations")
	affiliations := make([]string, 0, len(d.AffiliationIDs))
	for _, id := range d.AffiliationIDs {
		affiliations = append(affiliations, fmt.Sprint(id))
	}
	c.unique(affiliations, "/affiliations")
	relations := make([]string, 0, len(d.Relations))
	for _, r := range d.Relations {
		relations = append(relations, fmt.Sprint(r.ToPersonID, r.Type))
	}
	c.relations(relations)
	links := c.personPerformances(d.Links)
	c.links(links)
	c.licensingSources(d.LicensingNotesMD, prev.LicensingNotesMD, d.LicensingStatus, prev.LicensingStatus,
		hasLicensingSource(links))
	c.personStatus(d, prev, v.Today)
	c.preferences(d.Preferences, prev.Preferences)
	c.references(documentReferences(d))
	if v.Author == RoleContributor && !v.editor && v.Previous != nil &&
		(prev.Status == PersonStatusPrefersNoPage || v.Refs.Persons[v.ID].PrefersNoPage) {
		c.identityOnly(d, prev)
	}

	return c.problems
}

// personStatus checks a person's status (D11 S5–S7): any status but active, and any change of status, needs a
// valid source; only a status other than active has a date, which is not after today.
func (c *docCheck) personStatus(d, prev PersonDocument, today Date) {
	c.sourceURL(d.StatusSourceURL, "/status_source_url")
	if d.Status != PersonStatusActive || d.Status != prev.Status {
		c.requireSource(d.StatusSourceURL, "/status_source_url")
	}
	c.check(d.Status != PersonStatusActive || d.StatusDate == nil, "/status_date", ProblemDateNeedsStatus)
	c.check(d.StatusDate == nil || !d.StatusDate.After(today.Time), "/status_date", ProblemOutOfRange)
}

// Validate returns the problems of a term document (D12).
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

// Validate returns the problems of a collection document (D12).
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

// Validate returns the problems of an event document (D12).
func (d EventDocument) Validate(v ValidationContext[EventDocument]) []DocumentProblem {
	c := newDocCheck(d, v.Previous, v.Refs, v.ID)
	c.document(d, d.SchemaVersion, v.Summary)
	c.title(d.Name, "/name")
	c.text(d.DescriptionMD, "/description_md")

	return c.problems
}

// docCheck collects the problems of one document.
type docCheck struct {
	fieldCheck
	problems []DocumentProblem
	refs     ReferenceIndex
	id       int64
	// previousStrings are the strings of the comparison document: a name among them is not new, so the stricter
	// character rule for names (CheckTextCharacters) does not make imported names uneditable.
	previousStrings map[string]bool
	// changed are the keys of the references the edit adds or changes (ChangedReferences).
	changed map[string]bool
}

func newDocCheck[D Document](doc D, prev *D, refs ReferenceIndex, id int64) *docCheck {
	c := newProblemCheck(refs, id)
	if prev != nil {
		walkStrings(reflect.ValueOf(*prev), "", documentPaths, func(_ string, v reflect.Value) {
			c.previousStrings[v.String()] = true
		})
	}
	for _, r := range ChangedReferences(prev, doc) {
		c.changed[r.Key] = true
	}

	return c
}

// newProblemCheck returns a docCheck with no comparison document and no changed references, recording each
// problem once.
func newProblemCheck(refs ReferenceIndex, id int64) *docCheck {
	c := &docCheck{refs: refs, id: id, previousStrings: map[string]bool{}, changed: map[string]bool{}}
	c.fieldCheck = fieldCheck{report: func(path string, code ProblemCode) {
		if p := (DocumentProblem{Path: path, Code: code}); !slices.Contains(c.problems, p) {
			c.problems = append(c.problems, p)
		}
	}, paths: documentPaths}

	return c
}

// DraftProblems returns the problems even a draft may not have, since drafts may be incomplete (§3.3): vocabulary
// values that are given but unknown, strings that are not valid UTF-8 in NFC or that hold control characters, and a
// summary that does not fit. Every other rule is checked when the draft is submitted.
func DraftProblems(doc AnyDocument, summary string) []DocumentProblem {
	c := newProblemCheck(ReferenceIndex{}, 0)
	c.contents(doc, summary, true)

	return c.problems
}

// document checks what every document must be: of the current schema version, with its contents (contents).
func (c *docCheck) document(doc any, version int, summary string) {
	c.check(version == DocumentSchemaVersion, "/schema_version", ProblemOutOfRange)
	c.contents(doc, summary, false)
}

// contents checks that every vocabulary value of doc is known (or, if incomplete, empty), that every string is
// valid UTF-8 in NFC without control characters, and the summary of its revision.
func (c *docCheck) contents(doc any, summary string, incomplete bool) {
	walkStrings(reflect.ValueOf(doc), "", documentPaths, func(path string, v reflect.Value) {
		if e, ok := v.Interface().(validator); ok {
			c.check(e.Valid() || incomplete && v.String() == "", path, ProblemOutOfRange)
		}
	})
	c.strings(reflect.ValueOf(doc), "")
	c.varchar(summary, maxSummaryLength, "/summary", false)
	c.strings(reflect.ValueOf(summary), "/summary")
}

// title checks a name, title or alias: not blank, it fits, it holds no withheld-text placeholder and, unless the
// comparison document has it already, it has only the characters names may have (CheckTextCharacters).
func (c *docCheck) title(s, path string) {
	if strings.TrimSpace(s) == "" {
		c.report(path, ProblemBlank)

		return
	}
	c.name(s, path)
	c.check(c.previousStrings[s] || CheckTextCharacters(s) == nil, path, ProblemInvalidCharacters)
}

// cap checks that a list of n elements at path has at most limit.
func (c *docCheck) cap(n, limit int, path string) {
	c.check(n <= limit, path, ProblemTooMany)
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

// links checks a document's links: each normalizes, at most once (by normalized URL), with 1–12 roles, each once,
// and its fields fit.
func (c *docCheck) links(links []DocLink) {
	c.cap(len(links), maxLinks, "/links")
	urls := make([]string, 0, len(links))
	for k, l := range links {
		path := fmt.Sprintf("/links/%d", k)
		c.check(isValidURL(l.URL), path+"/url", ProblemInvalidURL)
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

// personAliases checks a person's aliases: each a name, unique and not the person's name (as the collation
// compares them), and, of a contributor's document, each alias the edit adds or changes is hidden exactly when it
// is a legal name (D5): a contributor cannot publish a legal name or hide another.
func (c *docCheck) personAliases(d, prev PersonDocument, author Role) {
	c.cap(len(d.Aliases), maxAliases, "/aliases")
	before := map[string]DocPersonAlias{}
	for _, a := range prev.Aliases {
		before[CollationKey(a.Name)] = a
	}
	keys := make([]string, 0, len(d.Aliases))
	for k, a := range d.Aliases {
		path := fmt.Sprintf("/aliases/%d", k)
		c.title(a.Name, path+"/name")
		key := CollationKey(a.Name)
		c.check(key != CollationKey(d.Name), path+"/name", ProblemDuplicate)
		keys = append(keys, key)
		if old, ok := before[key]; author == RoleContributor && (!ok || old != a) {
			c.check(a.IsPublic == (a.Kind != PersonAliasKindLegal), path+"/is_public", ProblemAliasVisibility)
		}
	}
	c.unique(keys, "/aliases")
}

// identityOnly checks that a contributor's document of a person who prefers no page differs from the comparison
// document in the identity fields only. Restore makes any other difference impossible; this is defence in depth.
func (c *docCheck) identityOnly(d, prev PersonDocument) {
	dv, pv := reflect.ValueOf(d), reflect.ValueOf(prev)
	for i := range dv.NumField() {
		f := dv.Type().Field(i)
		if !slices.Contains(identityFields, jsonFieldName(f)) && !sameValue(dv.Field(i), pv.Field(i)) {
			c.report(documentPaths.field("", f), ProblemWithheldFieldChanged)
		}
	}
}

// sameValue reports whether a and b are deeply equal, an empty list being equal to none.
func sameValue(a, b reflect.Value) bool {
	if a.Kind() == reflect.Slice && a.Len() == 0 && b.Len() == 0 {
		return true
	}

	return reflect.DeepEqual(a.Interface(), b.Interface())
}

// pieceSources checks a piece's sourced fields (D11 S1–S3, S8, S9, S11).
func (c *docCheck) pieceSources(d, prev PieceDocument, summary string) {
	links := make([]DocLink, 0, len(d.Links))
	for _, l := range d.Links {
		links = append(links, l.DocLink)
	}
	licensed := hasLicensingSource(links)
	c.licensingSources(d.LicensingNotesMD, prev.LicensingNotesMD, d.LicensingStatus, prev.LicensingStatus, licensed)
	if d.Licence != nil && !equalPointers(d.Licence, prev.Licence) {
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

// licensingSources checks the licensing rules pieces and persons share (D11 S1, S3, S4): changed licensing notes
// that are not empty, and a licensing status changed to known, need a licensing source.
func (c *docCheck) licensingSources(notes, prevNotes *string, status, prevStatus LicensingStatus, licensed bool) {
	if notes != nil && *notes != "" && !equalPointers(notes, prevNotes) {
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
		return slices.Contains(l.Roles, LinkRoleLicensingSource) && isValidURL(l.URL)
	})
}

// preferences checks preference profiles: each dimension once, and each added or changed one sourced (D11 S10).
func (c *docCheck) preferences(prefs, prev []DocPreference) {
	dims := make([]string, 0, len(prefs))
	for k, p := range prefs {
		path := fmt.Sprintf("/preferences/%d/source_url", k)
		dims = append(dims, string(p.Dimension))
		if p.SourceURL != "" {
			c.sourceURL(&p.SourceURL, path)
		}
		unchanged := slices.ContainsFunc(prev, func(q DocPreference) bool { return reflect.DeepEqual(p, q) })
		c.check(unchanged || p.SourceURL != "", path, ProblemSourceRequired)
	}
	c.unique(dims, "/preferences")
}

// sourceURL checks a source URL field that is set: it must be a valid URL (D11 S11).
func (c *docCheck) sourceURL(s *string, path string) {
	c.check(s == nil || isValidURL(*s), path, ProblemInvalidURL)
}

// requireSource checks that a source URL field a rule requires is set; sourceURL checks that it is valid.
func (c *docCheck) requireSource(s *string, path string) {
	c.check(s != nil && *s != "", path, ProblemSourceRequired)
}

// isValidURL reports whether s is a valid source or link URL: at most MaxURLLength bytes, without control
// characters, and an absolute http(s) URL that NormalizeURL accepts.
func isValidURL(s string) bool {
	if len(s) > MaxURLLength || strings.ContainsFunc(s, IsControlCharacter) {
		return false
	}
	_, err := NormalizeURL(s)

	return err == nil
}

func equalPointers[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// references checks the references of a document: each must exist and not be to the entity itself, and each the
// edit adds or changes must be to an entity its author may reference (public, or the author's own pending new term
// or person) and follow the rules of its field (D12). A reference that is unchanged needs only to exist, so that a
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
