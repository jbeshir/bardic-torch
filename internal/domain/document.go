package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
//
// TestDocumentCoverage (internal/datasources/mysql) fails when a column of a table a document owns is neither a
// document field nor classified as system-owned. Any change to a document's shape needs a new schema version.

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
// schema_version is not DocumentSchemaVersion, and with an error if the JSON is malformed, has a field the
// document type does not have, or is followed by more data. It does not validate the field values.
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
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return doc, fmt.Errorf("decoding document: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return doc, errors.New("decoding document: unexpected data after the document")
	}

	return doc, nil
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
	TermID    int64      `json:"term_id"`
	IsPrimary bool       `json:"is_primary"`
	Part      *PiecePart `json:"part"`
}

// DocPieceRelation relates the document's piece to another piece.
type DocPieceRelation struct {
	ToPieceID int64             `json:"to_piece_id"`
	Type      PieceRelationType `json:"type"`
}

// DocCredit credits a piece, or part of it, to a person (attribution named) or to no one in particular.
type DocCredit struct {
	PersonID    *int64      `json:"person_id"`
	Role        CreditRole  `json:"role"`
	Attribution Attribution `json:"attribution"`
	Certainty   Certainty   `json:"certainty"`
	Part        *PiecePart  `json:"part"`
	Note        *string     `json:"note"`
}

// DocPreference is one dimension of a stated preference profile. Value's shape per dimension is defined, and
// validated, in BE5; until then it is carried as raw JSON.
type DocPreference struct {
	Dimension PreferenceDimension `json:"dimension"`
	Value     json.RawMessage     `json:"value"`
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

// DocPerformance is a performance recorded by a link.
type DocPerformance struct {
	EventID             *int64  `json:"event_id"`
	Year                *int16  `json:"year"`
	Uploader            *string `json:"uploader"`
	DescriptionVerbatim string  `json:"description_verbatim"`
	PerformerIDs        []int64 `json:"performer_ids"`
}

// DocPersonPerformance is a performance recorded by a link attached to a person, of a piece if PieceID is set.
type DocPersonPerformance struct {
	PieceID *int64 `json:"piece_id"`
	DocPerformance
}

// PersonDocument is the editable content of a person (persons and its child tables, plus the links attached to
// them).
type PersonDocument struct {
	SchemaVersion    int                 `json:"schema_version"`
	Name             string              `json:"name"`
	Kind             PersonKind          `json:"kind"`
	Status           PersonStatus        `json:"status"`
	StatusDate       *string             `json:"status_date"` // YYYY-MM-DD
	StatusSourceURL  *string             `json:"status_source_url"`
	Pronouns         *string             `json:"pronouns"`
	NotesMD          *string             `json:"notes_md"`
	LicensingStatus  LicensingStatus     `json:"licensing_status"`
	LicensingNotesMD *string             `json:"licensing_notes_md"`
	IsStub           bool                `json:"is_stub"`
	Aliases          []DocPersonAlias    `json:"aliases"`
	Roles            []PersonRole        `json:"roles"`
	AffiliationIDs   []int64             `json:"affiliation_ids"`
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
	ToPersonID int64              `json:"to_person_id"`
	Type       PersonRelationType `json:"type"`
}

// TermDocument is the editable content of a term (terms and term_aliases).
type TermDocument struct {
	SchemaVersion        int      `json:"schema_version"`
	Facet                Facet    `json:"facet"`
	Name                 string   `json:"name"`
	DisplayName          string   `json:"display_name"`
	ParentID             *int64   `json:"parent_id"`
	RelatedAffiliationID *int64   `json:"related_affiliation_id"`
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
	RegionTermID     *int64               `json:"region_term_id"`
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
