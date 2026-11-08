package controller

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// The JSON representations of the catalogue's public read models, shared by the catalogue controllers. Every
// field is always present; lists are never null.

// PieceSummary is the JSON representation of a piece another entity refers to.
type PieceSummary struct {
	Slug               string                    `json:"slug"`
	Title              string                    `json:"title"`
	Kind               domain.PieceKind          `json:"kind"`
	SecondaryKinds     []domain.PieceKind        `json:"secondary_kinds"`
	ConservationStatus domain.ConservationStatus `json:"conservation_status"`
	IsStub             bool                      `json:"is_stub"`
	CreditNames        []string                  `json:"credit_names"`
}

// PersonSummary is the JSON representation of a person another entity refers to.
type PersonSummary struct {
	Slug   string            `json:"slug"`
	Name   string            `json:"name"`
	Kind   domain.PersonKind `json:"kind"`
	IsStub bool              `json:"is_stub"`
}

// TermSummary is the JSON representation of a term another entity refers to.
type TermSummary struct {
	Slug        string       `json:"slug"`
	Facet       domain.Facet `json:"facet"`
	DisplayName string       `json:"display_name"`
}

// CollectionRef is the JSON representation of a collection another entity refers to.
type CollectionRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// EventSummary is the JSON representation of an event or venue another entity refers to.
type EventSummary struct {
	Slug string           `json:"slug"`
	Name string           `json:"name"`
	Kind domain.EventKind `json:"kind"`
}

// Link is the JSON representation of a link attached to a person (the spec's PersonLink), and the common part
// of a piece's links.
type Link struct {
	URL           string                `json:"url"`
	Title         *string               `json:"title"`
	Description   *string               `json:"description"`
	Platform      *string               `json:"platform"`
	PlaceType     *domain.LinkPlaceType `json:"place_type"`
	Collection    *CollectionRef        `json:"collection"`
	Locator       *string               `json:"locator"`
	Liveness      domain.Liveness       `json:"liveness"`
	LastCheckedAt *time.Time            `json:"last_checked_at"`
	ArchivedURL   *string               `json:"archived_url"`
	Roles         []domain.LinkRole     `json:"roles"`
}

// mapSlice returns f of each item, as a non-nil slice, so that empty lists encode as [] rather than null.
func mapSlice[S, T any](items []S, f func(S) T) []T {
	mapped := make([]T, 0, len(items))
	for _, item := range items {
		mapped = append(mapped, f(item))
	}

	return mapped
}

// nonNil returns items, or an empty slice if it is nil.
func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}

	return items
}

// optional returns f of v, or nil if v is nil.
func optional[S, T any](v *S, f func(S) T) *T {
	if v == nil {
		return nil
	}
	mapped := f(*v)

	return &mapped
}

func pieceSummaryFromDomain(s domain.PieceSummary) PieceSummary {
	return PieceSummary{
		Slug:               s.Slug,
		Title:              s.Title,
		Kind:               s.Kind,
		SecondaryKinds:     nonNil(s.SecondaryKinds),
		ConservationStatus: s.ConservationStatus,
		IsStub:             s.IsStub,
		CreditNames:        nonNil(s.CreditNames),
	}
}

func personSummaryFromDomain(s domain.PersonSummary) PersonSummary {
	return PersonSummary{Slug: s.Slug, Name: s.Name, Kind: s.Kind, IsStub: s.IsStub}
}

func termSummaryFromDomain(s domain.TermSummary) TermSummary {
	return TermSummary{Slug: s.Slug, Facet: s.Facet, DisplayName: s.DisplayName}
}

func collectionRefFromDomain(c domain.CollectionRef) CollectionRef {
	return CollectionRef{ID: c.ID, Name: c.Name}
}

func eventSummaryFromDomain(e domain.EventSummary) EventSummary {
	return EventSummary{Slug: e.Slug, Name: e.Name, Kind: e.Kind}
}

func linkFromDomain(l domain.Link) Link {
	return Link{
		URL:           l.URL,
		Title:         l.Title,
		Description:   l.Description,
		Platform:      l.Platform,
		PlaceType:     l.PlaceType,
		Collection:    optional(l.Collection, collectionRefFromDomain),
		Locator:       l.Locator,
		Liveness:      l.Liveness,
		LastCheckedAt: l.LastCheckedAt,
		ArchivedURL:   l.ArchivedURL,
		Roles:         nonNil(l.Roles),
	}
}

// slugOrBadRequest returns the request's slug path value, which the router has decoded. If it is not a valid slug
// it writes 400 invalid_request and returns false; the caller should then return without writing.
func slugOrBadRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	slug := r.PathValue("slug")
	if !domain.IsSlug(slug) {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest, "invalid slug: "+slugRule)
		return "", false
	}

	return slug, true
}

func performanceLinkFromDomain(l domain.PerformanceLink) PerformanceLink {
	return PerformanceLink{
		URL: l.URL, Title: l.Title, Platform: l.Platform, Liveness: l.Liveness, LastCheckedAt: l.LastCheckedAt,
		ArchivedURL: l.ArchivedURL,
	}
}

// slugRule is what an invalid slug parameter's 400 says a slug must be.
var slugRule = fmt.Sprintf("must be lowercase ASCII letters and digits, in runs joined by single hyphens, "+
	"at most %d characters", domain.MaxSlugLength)

var errNotSlug = errors.New("not a slug")

// slugValue returns s if it is a slug, for listOrBadRequest.
func slugValue(s string) (string, error) {
	if !domain.IsSlug(s) {
		return "", errNotSlug
	}

	return s, nil
}
