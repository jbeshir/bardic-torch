package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The JSON representations of the snapshot views a revision shows: each document's editor-owned fields, with
// references resolved to summaries, links by URL, and no system fields.

// PieceSnapshot is the JSON representation of a piece snapshot view.
type PieceSnapshot struct {
	Title                 string                    `json:"title"`
	Kind                  domain.PieceKind          `json:"kind"`
	SecondaryKinds        []domain.PieceKind        `json:"secondary_kinds"`
	ConservationStatus    domain.ConservationStatus `json:"conservation_status"`
	ConservationSourceURL *string                   `json:"conservation_source_url"`
	IsStub                bool                      `json:"is_stub"`
	NotesMD               *string                   `json:"notes_md"`
	VersionNotesMD        *string                   `json:"version_notes_md"`
	LicensingStatus       domain.LicensingStatus    `json:"licensing_status"`
	LicensingNotesMD      *string                   `json:"licensing_notes_md"`
	Licence               *domain.Licence           `json:"licence"`
	LicenceVersion        *domain.LicenceVersion    `json:"licence_version"`
	HistoricalSourceMD    *string                   `json:"historical_source_md"`
	PeriodStart           *int16                    `json:"period_start"`
	PeriodEnd             *int16                    `json:"period_end"`
	Aliases               []PieceAlias              `json:"aliases"`
	Terms                 []PieceTerm               `json:"terms"`
	Credits               []PieceCredit             `json:"credits"`
	Relations             []SnapshotPieceRelation   `json:"relations"`
	Links                 []SnapshotPieceLink       `json:"links"`
	Preferences           []PreferenceEntry         `json:"preferences"`
}

// SnapshotPieceRelation is the JSON representation of a piece snapshot's relation to another piece.
type SnapshotPieceRelation struct {
	Type  domain.PieceRelationType `json:"type"`
	Piece PieceSummary             `json:"piece"`
}

// SnapshotLink is the JSON representation of the editor-owned part of a link in a snapshot.
type SnapshotLink struct {
	URL         string                `json:"url"`
	Title       *string               `json:"title"`
	Description *string               `json:"description"`
	Platform    *string               `json:"platform"`
	PlaceType   *domain.LinkPlaceType `json:"place_type"`
	Collection  *CollectionRef        `json:"collection"`
	Locator     *string               `json:"locator"`
	Roles       []domain.LinkRole     `json:"roles"`
}

// SnapshotPieceLink is the JSON representation of a link of a piece snapshot.
type SnapshotPieceLink struct {
	SnapshotLink

	Performances []PiecePerformance `json:"performances"`
}

// SnapshotPersonLink is the JSON representation of a link of a person snapshot.
type SnapshotPersonLink struct {
	SnapshotLink

	Performances []SnapshotPersonPerformance `json:"performances"`
}

// SnapshotPersonPerformance is the JSON representation of a performance a person snapshot's link records.
type SnapshotPersonPerformance struct {
	Piece *PieceSummary `json:"piece"`
	PiecePerformance
}

// PersonSnapshot is the JSON representation of a person snapshot view.
type PersonSnapshot struct {
	Name            string                 `json:"name"`
	Kind            domain.PersonKind      `json:"kind"`
	Status          domain.PersonStatus    `json:"status"`
	StatusDate      *string                `json:"status_date"`
	StatusSourceURL *string                `json:"status_source_url"`
	IsStub          bool                   `json:"is_stub"`
	Profile         *PersonSnapshotProfile `json:"profile"`
}

// PersonSnapshotProfile is the JSON representation of everything a person snapshot shows beyond the identity.
type PersonSnapshotProfile struct {
	Pronouns         *string                  `json:"pronouns"`
	NotesMD          *string                  `json:"notes_md"`
	LicensingStatus  domain.LicensingStatus   `json:"licensing_status"`
	LicensingNotesMD *string                  `json:"licensing_notes_md"`
	Aliases          []PersonAlias            `json:"aliases"`
	Roles            []domain.PersonRole      `json:"roles"`
	Affiliations     []TermSummary            `json:"affiliations"`
	Relations        []SnapshotPersonRelation `json:"relations"`
	Links            []SnapshotPersonLink     `json:"links"`
	Preferences      []PreferenceEntry        `json:"preferences"`
}

// SnapshotPersonRelation is the JSON representation of a person snapshot's relation to another person.
type SnapshotPersonRelation struct {
	Type   domain.PersonRelationType `json:"type"`
	Person PersonSummary             `json:"person"`
}

// TermSnapshot is the JSON representation of a term snapshot view.
type TermSnapshot struct {
	Facet              domain.Facet `json:"facet"`
	Name               string       `json:"name"`
	DisplayName        string       `json:"display_name"`
	Parent             *TermSummary `json:"parent"`
	RelatedAffiliation *TermSummary `json:"related_affiliation"`
	DescriptionMD      *string      `json:"description_md"`
	Position           int32        `json:"position"`
	Aliases            []string     `json:"aliases"`
}

// CollectionSnapshot is the JSON representation of a collection snapshot view.
type CollectionSnapshot struct {
	Name             string                      `json:"name"`
	URL              *string                     `json:"url"`
	Kind             domain.CollectionKind       `json:"kind"`
	DescriptionMD    *string                     `json:"description_md"`
	Region           *TermSummary                `json:"region"`
	Parent           *CollectionSummary          `json:"parent"`
	IsBardicResource bool                        `json:"is_bardic_resource"`
	Enumerable       domain.CollectionEnumerable `json:"enumerable"`
}

// EventSnapshot is the JSON representation of an event snapshot view.
type EventSnapshot struct {
	Name          string           `json:"name"`
	Kind          domain.EventKind `json:"kind"`
	DescriptionMD *string          `json:"description_md"`
}

func pieceSnapshotFromDomain(s domain.PieceSnapshot) PieceSnapshot {
	return PieceSnapshot{
		Title:                 s.Title,
		Kind:                  s.Kind,
		SecondaryKinds:        nonNil(s.SecondaryKinds),
		ConservationStatus:    s.ConservationStatus,
		ConservationSourceURL: s.ConservationSourceURL,
		IsStub:                s.IsStub,
		NotesMD:               s.NotesMD,
		VersionNotesMD:        s.VersionNotesMD,
		LicensingStatus:       s.LicensingStatus,
		LicensingNotesMD:      s.LicensingNotesMD,
		Licence:               s.Licence,
		LicenceVersion:        s.LicenceVersion,
		HistoricalSourceMD:    s.HistoricalSourceMD,
		PeriodStart:           s.PeriodStart,
		PeriodEnd:             s.PeriodEnd,
		Aliases:               mapSlice(s.Aliases, pieceAliasFromDomain),
		Terms:                 mapSlice(s.Terms, pieceTermFromDomain),
		Credits:               mapSlice(s.Credits, pieceCreditFromDomain),
		Relations: mapSlice(s.Relations, func(r domain.SnapshotPieceRelation) SnapshotPieceRelation {
			return SnapshotPieceRelation{Type: r.Type, Piece: pieceSummaryFromDomain(r.Piece)}
		}),
		Links: mapSlice(s.Links, func(l domain.SnapshotPieceLink) SnapshotPieceLink {
			return SnapshotPieceLink{
				SnapshotLink: snapshotLinkFromDomain(l.SnapshotLink),
				Performances: mapSlice(l.Performances, piecePerformanceFromDomain),
			}
		}),
		Preferences: mapSlice(s.Preferences, preferenceEntryFromDomain),
	}
}

func snapshotLinkFromDomain(l domain.SnapshotLink) SnapshotLink {
	return SnapshotLink{
		URL:         l.URL,
		Title:       l.Title,
		Description: l.Description,
		Platform:    l.Platform,
		PlaceType:   l.PlaceType,
		Collection:  optional(l.Collection, collectionRefFromDomain),
		Locator:     l.Locator,
		Roles:       nonNil(l.Roles),
	}
}

func personSnapshotFromDomain(s domain.PersonSnapshot) PersonSnapshot {
	snapshot := PersonSnapshot{
		Name:            s.Name,
		Kind:            s.Kind,
		Status:          s.Status,
		StatusSourceURL: s.StatusSourceURL,
		IsStub:          s.IsStub,
		Profile:         optional(s.Profile, personSnapshotProfileFromDomain),
	}
	if s.StatusDate != nil {
		date := s.StatusDate.Format(dateLayout)
		snapshot.StatusDate = &date
	}

	return snapshot
}

func personSnapshotProfileFromDomain(p domain.PersonSnapshotProfile) PersonSnapshotProfile {
	return PersonSnapshotProfile{
		Pronouns:         p.Pronouns,
		NotesMD:          p.NotesMD,
		LicensingStatus:  p.LicensingStatus,
		LicensingNotesMD: p.LicensingNotesMD,
		Aliases:          mapSlice(p.Aliases, personAliasFromDomain),
		Roles:            nonNil(p.Roles),
		Affiliations:     mapSlice(p.Affiliations, termSummaryFromDomain),
		Relations: mapSlice(p.Relations, func(r domain.SnapshotPersonRelation) SnapshotPersonRelation {
			return SnapshotPersonRelation{Type: r.Type, Person: personSummaryFromDomain(r.Person)}
		}),
		Links: mapSlice(p.Links, func(l domain.SnapshotPersonLink) SnapshotPersonLink {
			return SnapshotPersonLink{
				SnapshotLink: snapshotLinkFromDomain(l.SnapshotLink),
				Performances: mapSlice(l.Performances, func(p domain.SnapshotPersonPerformance) SnapshotPersonPerformance {
					return SnapshotPersonPerformance{
						Piece:            optional(p.Piece, pieceSummaryFromDomain),
						PiecePerformance: piecePerformanceFromDomain(p.PiecePerformance),
					}
				}),
			}
		}),
		Preferences: mapSlice(p.Preferences, preferenceEntryFromDomain),
	}
}

func termSnapshotFromDomain(s domain.TermSnapshot) TermSnapshot {
	return TermSnapshot{
		Facet:              s.Facet,
		Name:               s.Name,
		DisplayName:        s.DisplayName,
		Parent:             optional(s.Parent, termSummaryFromDomain),
		RelatedAffiliation: optional(s.RelatedAffiliation, termSummaryFromDomain),
		DescriptionMD:      s.DescriptionMD,
		Position:           s.Position,
		Aliases:            nonNil(s.Aliases),
	}
}

func collectionSnapshotFromDomain(s domain.CollectionSnapshot) CollectionSnapshot {
	return CollectionSnapshot{
		Name:             s.Name,
		URL:              s.URL,
		Kind:             s.Kind,
		DescriptionMD:    s.DescriptionMD,
		Region:           optional(s.Region, termSummaryFromDomain),
		Parent:           optional(s.Parent, collectionSummaryFromDomain),
		IsBardicResource: s.IsBardicResource,
		Enumerable:       s.Enumerable,
	}
}

func eventSnapshotFromDomain(s domain.EventSnapshot) EventSnapshot {
	return EventSnapshot{Name: s.Name, Kind: s.Kind, DescriptionMD: s.DescriptionMD}
}

// emptyJSONValues are the encodings of the values a field of a snapshot with no base counts as unchanged from.
var emptyJSONValues = [][]byte{[]byte("null"), []byte("[]"), []byte(`""`), []byte("false"), []byte("0")}

// changedFields returns the sorted names of the top-level fields of the snapshot whose JSON encodings differ from
// the base's or, with no base, are not empty: null, [], "", false or 0. It compares what the response shows, so
// nothing a view leaves out can show as a change.
func changedFields[S any](snapshot S, base *S) ([]string, error) {
	fields, err := jsonFields(snapshot)
	if err != nil {
		return nil, err
	}
	var baseFields map[string]json.RawMessage
	if base != nil {
		if baseFields, err = jsonFields(*base); err != nil {
			return nil, err
		}
	}

	changed := []string{}
	for name, value := range fields {
		var unchanged bool
		if base != nil {
			unchanged = bytes.Equal(value, baseFields[name])
		} else {
			unchanged = slices.ContainsFunc(emptyJSONValues, func(empty []byte) bool { return bytes.Equal(value, empty) })
		}
		if !unchanged {
			changed = append(changed, name)
		}
	}
	slices.Sort(changed)

	return changed, nil
}

// jsonFields returns the JSON encoding of each top-level field of v, an object.
func jsonFields(v any) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding snapshot: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, fmt.Errorf("decoding snapshot fields: %w", err)
	}

	return fields, nil
}
