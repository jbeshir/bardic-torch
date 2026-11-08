package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"time"
)

// Snapshot documents are the editable content of a catalogue entity, as stored in revisions.snapshot_json. A
// document holds only editor-owned fields:
//   - no IDs of its own, current_revision_id, legacy_* columns, claimed_by_user_id or slug, which the system
//     assigns, and no sort keys, which the writer derives (sort_title, sort_name, sort_letter);
//   - no conservation history, which is appended when a revision with a new conservation status is approved;
//   - no link system or moderator state (normalized_url, liveness, last_checked_at, archived_url, is_hidden), so
//     approving an old snapshot cannot un-hide a link or undo a liveness check.
//
// Other entities are referenced by internal ID. A document owns its outgoing relations only. Links are inline and
// identified by URL. The order of a list is the stored position of its elements.
// An editor tag names a reference field as the editor format names it, where the editor format references the
// entity by slug instead of ID ("person" for person_id); DocumentProblem paths use the editor names.
//
// TestDocumentCoverage (internal/datasources/mysql) fails when a column of a table a document owns is neither a
// document field nor classified as system-owned.
//
// A reader ignores fields it does not know, so a release may add a field to documents without a new schema
// version: a rolled-back release keeps serving the documents written in between, and their views show only the
// fields it knows. Any other change to a document's shape (removing, renaming or retyping a field, or changing
// what one means) needs a new schema version, which an older release refuses to read. A vocabulary value is
// added as for ENUM columns: the value ships one release before any document uses it.

// DocumentSchemaVersion is the schema version of every document this code writes and reads.
const DocumentSchemaVersion = 1

// ErrUnsupportedSchemaVersion is returned by DecodeDocument for a document whose schema_version is not
// DocumentSchemaVersion.
var ErrUnsupportedSchemaVersion = errors.New("unsupported document schema version")

// Document is the set of snapshot document types.
type Document interface {
	PieceDocument | PersonDocument | TermDocument | CollectionDocument | EventDocument
}

// DecodeDocument decodes a stored snapshot document. It fails with ErrUnsupportedSchemaVersion if the document's
// schema_version is not DocumentSchemaVersion, and with an error if the JSON is malformed or followed by more
// data, a value has the wrong type, a date is not a date, or a vocabulary field holds a value this release does
// not know. Fields the document type does not have are ignored. It does not validate the other field values.
func DecodeDocument[D Document](data []byte) (D, error) {
	var doc D

	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return doc, fmt.Errorf("decoding document schema version: %w", err)
	}
	if header.SchemaVersion != DocumentSchemaVersion {
		return doc, fmt.Errorf("%w [%d]", ErrUnsupportedSchemaVersion, header.SchemaVersion)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return doc, fmt.Errorf("decoding document: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return doc, errors.New("decoding document: unexpected data after the document")
	}
	if err := validateEnums(reflect.ValueOf(doc), ""); err != nil {
		return doc, fmt.Errorf("decoding document: %w", err)
	}

	return doc, nil
}

// ErrUnknownEnumValue is returned for a vocabulary value this release does not know.
var ErrUnknownEnumValue = errors.New("unknown enum value")

// validator is the method of Enum, which a type assertion can use.
type validator interface {
	Valid() bool
}

// validateEnums checks every Enum in v, descending into pointers, slices, arrays, map keys and values and exported
// struct fields, and returns ErrUnknownEnumValue, with the field's path, for the first that is not Valid.
func validateEnums(v reflect.Value, path string) error {
	switch kind := v.Kind(); {
	case kind == reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return validateEnums(v.Elem(), path)
	case isList(kind):
		return validateListEnums(v, path)
	case kind == reflect.Map:
		return validateMapEnums(v, path)
	case kind == reflect.Struct:
		return validateFieldEnums(v, path)
	case kind == reflect.String:
		if e, ok := v.Interface().(validator); ok && !e.Valid() {
			return fmt.Errorf("%w [%s] at %s", ErrUnknownEnumValue, v.String(), path)
		}
	}

	return nil
}

// validateListEnums is validateEnums for each element of the slice or array v.
func validateListEnums(v reflect.Value, path string) error {
	for i := range v.Len() {
		if err := validateEnums(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}

	return nil
}

// validateMapEnums is validateEnums for each key and value of the map v, in key order so that the error found
// first does not depend on Go's map iteration order.
func validateMapEnums(v reflect.Value, path string) error {
	keys := v.MapKeys()
	slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
	for _, key := range keys {
		keyPath := fmt.Sprintf("%s[%v]", path, key)
		if err := validateEnums(key, keyPath+" (key)"); err != nil {
			return err
		}
		if err := validateEnums(v.MapIndex(key), keyPath); err != nil {
			return err
		}
	}

	return nil
}

// validateFieldEnums is validateEnums for each exported field of the struct v.
func validateFieldEnums(v reflect.Value, path string) error {
	for i := range v.NumField() {
		if field := v.Type().Field(i); field.IsExported() {
			if err := validateEnums(v.Field(i), path+"."+field.Name); err != nil {
				return err
			}
		}
	}

	return nil
}

// Date is a calendar date, without a time of day, in UTC. In JSON it is a string, YYYY-MM-DD.
type Date struct {
	time.Time
}

// MarshalJSON encodes d as YYYY-MM-DD.
func (d Date) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Format(time.DateOnly))
}

// UnmarshalJSON decodes a YYYY-MM-DD string, failing for anything else.
func (d *Date) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("decoding date: %w", err)
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return fmt.Errorf("decoding date: %w", err)
	}
	d.Time = t

	return nil
}

// PieceDocument is the editable content of a piece (pieces and its child tables, plus the links attached to it).
type PieceDocument struct {
	SchemaVersion      int                `json:"schema_version"`
	Title              string             `json:"title"`
	Kind               PieceKind          `json:"kind"`
	SecondaryKinds     []PieceKind        `json:"secondary_kinds"`
	Conservation       DocConservation    `json:"conservation"`
	IsStub             bool               `json:"is_stub"`
	NotesMD            *string            `json:"notes_md"`
	VersionNotesMD     *string            `json:"version_notes_md"`
	LicensingStatus    LicensingStatus    `json:"licensing_status"`
	LicensingNotesMD   *string            `json:"licensing_notes_md"`
	Licence            *Licence           `json:"licence"`
	HistoricalSourceMD *string            `json:"historical_source_md"`
	PeriodStart        *int16             `json:"period_start"`
	PeriodEnd          *int16             `json:"period_end"`
	Aliases            []DocPieceAlias    `json:"aliases"`
	Terms              []DocPieceTerm     `json:"terms"`
	Relations          []DocPieceRelation `json:"relations"`
	Credits            []DocCredit        `json:"credits"`
	Links              []DocPieceLink     `json:"links"`
	Preferences        []DocPreference    `json:"preferences"`
}

// DocConservation is a piece's conservation status and the source supporting it. Approving a revision that
// changes the status appends a conservation_history row with this source.
type DocConservation struct {
	Status    ConservationStatus `json:"status"`
	SourceURL *string            `json:"source_url"`
}

// DocPieceAlias is another title of a piece.
type DocPieceAlias struct {
	Text string         `json:"text"`
	Kind PieceAliasKind `json:"kind"`
}

// DocPieceTerm tags a piece with a term. Part is the part of the piece the term applies to, if not all of it.
type DocPieceTerm struct {
	TermID    int64      `json:"term_id" editor:"term"`
	IsPrimary bool       `json:"is_primary"`
	Part      *PiecePart `json:"part"`
}

// DocPieceRelation relates the document's piece to another piece.
type DocPieceRelation struct {
	ToPieceID int64             `json:"to_piece_id" editor:"piece"`
	Type      PieceRelationType `json:"type"`
}

// DocCredit credits a piece, or part of it, to a person (attribution named) or to no one in particular.
type DocCredit struct {
	PersonID    *int64      `json:"person_id" editor:"person"`
	Role        CreditRole  `json:"role"`
	Attribution Attribution `json:"attribution"`
	Certainty   Certainty   `json:"certainty"`
	Part        *PiecePart  `json:"part"`
	Note        *string     `json:"note"`
}

// DocPreference is one dimension of a stated preference profile: what it states (Value, never nil in a valid
// document) and the source it is stated in.
type DocPreference struct {
	Dimension PreferenceDimension `json:"dimension"`
	Value     *PreferenceValue    `json:"value"`
	SourceURL string              `json:"source_url"`
}

// DocLink is the editor-owned part of a link attached to a piece or person, with the roles it has there.
type DocLink struct {
	URL          string         `json:"url"`
	Title        *string        `json:"title"`
	Description  *string        `json:"description"`
	Platform     *string        `json:"platform"`
	PlaceType    *LinkPlaceType `json:"place_type"`
	CollectionID *int64         `json:"collection_id"`
	Locator      *string        `json:"locator"`
	Roles        []LinkRole     `json:"roles"`
}

// DocPieceLink is a link attached to a piece, with the performances of the piece it records.
type DocPieceLink struct {
	DocLink
	Performances []DocPerformance `json:"performances"`
}

// DocPersonLink is a link attached to a person, with the performances it records.
type DocPersonLink struct {
	DocLink
	Performances []DocPersonPerformance `json:"performances"`
}

// DocPerformance is a performance recorded by a link. Key is its opaque identity for the editor (key_source.go),
// empty for a performance stored before keys were issued; it is never shown in a view.
type DocPerformance struct {
	Key                 string  `json:"key,omitempty"`
	EventID             *int64  `json:"event_id" editor:"event"`
	Year                *int16  `json:"year"`
	Uploader            *string `json:"uploader"`
	DescriptionVerbatim string  `json:"description_verbatim"`
	PerformerIDs        []int64 `json:"performer_ids" editor:"performers"`
}

// DocPersonPerformance is a performance recorded by a link attached to a person, of a piece if PieceID is set.
type DocPersonPerformance struct {
	PieceID *int64 `json:"piece_id" editor:"piece"`
	DocPerformance
}

// PersonDocument is the editable content of a person (persons and its child tables, plus the links attached to
// them).
type PersonDocument struct {
	SchemaVersion    int                 `json:"schema_version"`
	Name             string              `json:"name"`
	Kind             PersonKind          `json:"kind"`
	Status           PersonStatus        `json:"status"`
	StatusDate       *Date               `json:"status_date"`
	StatusSourceURL  *string             `json:"status_source_url"`
	Pronouns         *string             `json:"pronouns"`
	NotesMD          *string             `json:"notes_md"`
	LicensingStatus  LicensingStatus     `json:"licensing_status"`
	LicensingNotesMD *string             `json:"licensing_notes_md"`
	IsStub           bool                `json:"is_stub"`
	Aliases          []DocPersonAlias    `json:"aliases"`
	Roles            []PersonRole        `json:"roles"`
	AffiliationIDs   []int64             `json:"affiliation_ids" editor:"affiliations"`
	Relations        []DocPersonRelation `json:"relations"`
	Links            []DocPersonLink     `json:"links"`
	Preferences      []DocPreference     `json:"preferences"`
}

// DocPersonAlias is another name of a person. A name that is not public never appears in a public read.
type DocPersonAlias struct {
	Name     string          `json:"name"`
	Kind     PersonAliasKind `json:"kind"`
	IsPublic bool            `json:"is_public"`
}

// DocPersonRelation relates the document's person to another person.
type DocPersonRelation struct {
	ToPersonID int64              `json:"to_person_id" editor:"person"`
	Type       PersonRelationType `json:"type"`
}

// TermDocument is the editable content of a term (terms and term_aliases).
type TermDocument struct {
	SchemaVersion        int      `json:"schema_version"`
	Facet                Facet    `json:"facet"`
	Name                 string   `json:"name"`
	DisplayName          string   `json:"display_name"`
	ParentID             *int64   `json:"parent_id" editor:"parent"`
	RelatedAffiliationID *int64   `json:"related_affiliation_id" editor:"related_affiliation"`
	DescriptionMD        *string  `json:"description_md"`
	Position             int32    `json:"position"`
	Aliases              []string `json:"aliases"`
}

// CollectionDocument is the editable content of a collection.
type CollectionDocument struct {
	SchemaVersion    int                  `json:"schema_version"`
	Name             string               `json:"name"`
	URL              *string              `json:"url"`
	Kind             CollectionKind       `json:"kind"`
	DescriptionMD    *string              `json:"description_md"`
	RegionTermID     *int64               `json:"region_term_id" editor:"region"`
	ParentID         *int64               `json:"parent_id"`
	IsBardicResource bool                 `json:"is_bardic_resource"`
	Enumerable       CollectionEnumerable `json:"enumerable"`
}

// EventDocument is the editable content of an event or venue.
type EventDocument struct {
	SchemaVersion int       `json:"schema_version"`
	Name          string    `json:"name"`
	Kind          EventKind `json:"kind"`
	DescriptionMD *string   `json:"description_md"`
}
