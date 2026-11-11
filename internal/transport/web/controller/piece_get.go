package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// Piece is the JSON representation of a public piece's page.
type Piece struct {
	PieceSummary

	NotesMD             *string                `json:"notes_md"`
	VersionNotesMD      *string                `json:"version_notes_md"`
	LicensingStatus     domain.LicensingStatus `json:"licensing_status"`
	LicensingNotesMD    *string                `json:"licensing_notes_md"`
	Licence             *domain.Licence        `json:"licence"`
	HistoricalSourceMD  *string                `json:"historical_source_md"`
	PeriodStart         *int16                 `json:"period_start"`
	PeriodEnd           *int16                 `json:"period_end"`
	Aliases             []PieceAlias           `json:"aliases"`
	Terms               []PieceTerm            `json:"terms"`
	Credits             []PieceCredit          `json:"credits"`
	Relations           []PieceRelation        `json:"relations"`
	Links               []PieceLink            `json:"links"`
	ConservationHistory []ConservationChange   `json:"conservation_history"`
}

// PieceAlias is the JSON representation of another title of a piece.
type PieceAlias struct {
	Text string                `json:"text"`
	Kind domain.PieceAliasKind `json:"kind"`
}

// PieceTerm is the JSON representation of a term a piece is classified with.
type PieceTerm struct {
	Term      TermSummary       `json:"term"`
	IsPrimary bool              `json:"is_primary"`
	Part      *domain.PiecePart `json:"part"`
}

// PieceCredit is the JSON representation of a credit of a piece.
type PieceCredit struct {
	Role        domain.CreditRole  `json:"role"`
	Attribution domain.Attribution `json:"attribution"`
	Certainty   domain.Certainty   `json:"certainty"`
	Part        *domain.PiecePart  `json:"part"`
	Note        *string            `json:"note"`
	Person      *PersonSummary     `json:"person"`
}

// PieceRelation is the JSON representation of a relation between two pieces.
type PieceRelation struct {
	Type      domain.PieceRelationType `json:"type"`
	Direction domain.RelationDirection `json:"direction"`
	Piece     PieceSummary             `json:"piece"`
}

// PieceLink is the JSON representation of a link attached to a piece.
type PieceLink struct {
	Link

	Performances []PiecePerformance `json:"performances"`
}

// PiecePerformance is the JSON representation of a recorded performance of a piece.
type PiecePerformance struct {
	Year                *int16          `json:"year"`
	Uploader            *string         `json:"uploader"`
	DescriptionVerbatim string          `json:"description_verbatim"`
	Event               *EventSummary   `json:"event"`
	Performers          []PersonSummary `json:"performers"`
}

// ConservationChange is the JSON representation of an entry in a piece's conservation history.
type ConservationChange struct {
	ConservationStatus domain.ConservationStatus `json:"conservation_status"`
	Reason             string                    `json:"reason"`
	SourceURL          *string                   `json:"source_url"`
	At                 time.Time                 `json:"at"`
}

// PieceGet handles GET /v1/pieces/{slug}, a public piece's page. An unapproved piece is not found, exactly
// like an unknown one.
type PieceGet struct {
	Catalogue datasources.Catalogue
}

func (c PieceGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}

	piece, err := c.Catalogue.GetPublicPiece(ctx, slug)
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, "piece not found")
	case err != nil:
		writeInternalError(w, r, "unable to get piece", err)
	default:
		httpjson.WriteJSON(ctx, w, http.StatusOK, pieceFromDomain(piece))
	}
}

func pieceFromDomain(p domain.Piece) Piece {
	return Piece{
		PieceSummary:       pieceSummaryFromDomain(p.PieceSummary),
		NotesMD:            p.NotesMD,
		VersionNotesMD:     p.VersionNotesMD,
		LicensingStatus:    p.LicensingStatus,
		LicensingNotesMD:   p.LicensingNotesMD,
		Licence:            p.Licence,
		HistoricalSourceMD: p.HistoricalSourceMD,
		PeriodStart:        p.PeriodStart,
		PeriodEnd:          p.PeriodEnd,
		Aliases:            mapSlice(p.Aliases, pieceAliasFromDomain),
		Terms:              mapSlice(p.Terms, pieceTermFromDomain),
		Credits:            mapSlice(p.Credits, pieceCreditFromDomain),
		Relations: mapSlice(p.Relations, func(r domain.PieceRelation) PieceRelation {
			return PieceRelation{Type: r.Type, Direction: r.Direction, Piece: pieceSummaryFromDomain(r.Piece)}
		}),
		Links: mapSlice(p.Links, func(l domain.PieceLink) PieceLink {
			return PieceLink{Link: linkFromDomain(l.Link), Performances: mapSlice(l.Performances, piecePerformanceFromDomain)}
		}),
		ConservationHistory: mapSlice(p.ConservationHistory, func(c domain.ConservationChange) ConservationChange {
			return ConservationChange{ConservationStatus: c.Status, Reason: c.Reason, SourceURL: c.SourceURL, At: c.At}
		}),
	}
}

func pieceAliasFromDomain(a domain.PieceAlias) PieceAlias {
	return PieceAlias{Text: a.Text, Kind: a.Kind}
}

func pieceTermFromDomain(t domain.PieceTerm) PieceTerm {
	return PieceTerm{Term: termSummaryFromDomain(t.Term), IsPrimary: t.IsPrimary, Part: t.Part}
}

func pieceCreditFromDomain(c domain.PieceCredit) PieceCredit {
	return PieceCredit{
		Role:        c.Role,
		Attribution: c.Attribution,
		Certainty:   c.Certainty,
		Part:        c.Part,
		Note:        c.Note,
		Person:      optional(c.Person, personSummaryFromDomain),
	}
}

func piecePerformanceFromDomain(p domain.PiecePerformance) PiecePerformance {
	return PiecePerformance{
		Year:                p.Year,
		Uploader:            p.Uploader,
		DescriptionVerbatim: p.DescriptionVerbatim,
		Event:               optional(p.Event, eventSummaryFromDomain),
		Performers:          mapSlice(p.Performers, personSummaryFromDomain),
	}
}
