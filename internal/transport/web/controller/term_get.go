package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// Term is the JSON representation of a public term's page.
type Term struct {
	TermSummary

	UpdatedAt          time.Time       `json:"updated_at"`
	Name               string          `json:"name"`
	DescriptionMD      *string         `json:"description_md"`
	Aliases            []string        `json:"aliases"`
	Ancestors          []TermSummary   `json:"ancestors"`
	Children           []TermChild     `json:"children"`
	RelatedAffiliation *TermSummary    `json:"related_affiliation"`
	PieceCount         int64           `json:"piece_count"`
	PersonCount        int64           `json:"person_count"`
	Pieces             []PieceSummary  `json:"pieces"`
	Persons            []PersonSummary `json:"persons"`
}

// TermChild is the JSON representation of a child of a term, with its counts.
type TermChild struct {
	Term        TermSummary `json:"term"`
	PieceCount  int64       `json:"piece_count"`
	PersonCount int64       `json:"person_count"`
}

// TermGet handles GET /v1/terms/{slug}, a public term's page with one page of its pieces or persons. A term
// that is not public, being unapproved or under an unapproved ancestor, is not found, exactly like an unknown
// one.
type TermGet struct {
	Catalogue datasources.Catalogue
}

func (c TermGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	page, ok := pageOrBadRequest(w, r, query)
	if !ok {
		return
	}

	term, err := c.Catalogue.GetPublicTerm(ctx, slug, page)
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, "term not found")
	case err != nil:
		writeInternalError(w, r, "unable to get term", err)
	default:
		httpjson.WriteJSON(ctx, w, http.StatusOK, termFromDomain(term))
	}
}

func termFromDomain(t domain.Term) Term {
	return Term{
		TermSummary:   termSummaryFromDomain(t.TermSummary),
		UpdatedAt:     t.UpdatedAt,
		Name:          t.Name,
		DescriptionMD: t.DescriptionMD,
		Aliases:       nonNil(t.Aliases),
		Ancestors:     mapSlice(t.Ancestors, termSummaryFromDomain),
		Children: mapSlice(t.Children, func(child domain.TermChild) TermChild {
			return TermChild{
				Term:        termSummaryFromDomain(child.Term),
				PieceCount:  child.PieceCount,
				PersonCount: child.PersonCount,
			}
		}),
		RelatedAffiliation: optional(t.RelatedAffiliation, termSummaryFromDomain),
		PieceCount:         t.PieceCount,
		PersonCount:        t.PersonCount,
		Pieces:             mapSlice(t.Pieces, pieceSummaryFromDomain),
		Persons:            mapSlice(t.Persons, personSummaryFromDomain),
	}
}
