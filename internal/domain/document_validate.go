package domain

import (
	"reflect"
	"slices"
	"strings"
)

// The element caps of a document, and the bounds of its numbers.
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

// ValidationContext is what validating a document of type D needs besides the document.
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
	// withheldSources are the paths of the scalar sources the contributor's projection withheld, which the first
	// phase does not require: restore puts them back.
	withheldSources map[string]bool
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

// ValidateSubmission validates a contributor's submission in two phases, so that validation is never an
// oracle for what the contributor could not see:
//  1. editor, the document as submitted (before restore), compared with editorPrevious, the projection of the
//     base the contributor edited. Its problems, with paths into the document the contributor sees, are the
//     only problems returned. A source the projection withheld (a hidden link) and that the contributor left
//     null is not required there: phase 2 checks the source restore puts back.
//  2. restored, the document with the withheld elements put back, compared with v.Previous, the full base. Only
//     when phase 1 passes; each problem it finds exists because of data the contributor could not see, and is
//     returned as a hidden_data_conflict flag with its path into the full document.
func ValidateSubmission[D Document](editor, restored D, editorPrevious *D, v ValidationContext[D]) (
	[]DocumentProblem, []Flag,
) {
	phase1 := v
	phase1.Previous, phase1.editor = editorPrevious, true
	phase1.withheldSources = withheldSourcePaths(v.Previous, editorPrevious)
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
	// withheldSources are the paths of the sources a contributor was not given, which requireSource does not
	// require (ValidationContext.withheldSources).
	withheldSources map[string]bool
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
	c.varchar(summary, MaxSummaryLength, "/summary", false)
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
