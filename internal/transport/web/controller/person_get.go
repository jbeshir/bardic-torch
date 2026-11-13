package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// Person is the JSON representation of a public person's page.
type Person struct {
	PersonSummary

	UpdatedAt time.Time           `json:"updated_at"`
	Status    domain.PersonStatus `json:"status"`
	Profile   *PersonProfile      `json:"profile"`
}

// PersonProfile is the JSON representation of everything a person's page shows beyond their identity.
type PersonProfile struct {
	NotesMD           *string                `json:"notes_md"`
	LicensingStatus   domain.LicensingStatus `json:"licensing_status"`
	LicensingNotesMD  *string                `json:"licensing_notes_md"`
	Aliases           []PersonAlias          `json:"aliases"`
	Roles             []domain.PersonRole    `json:"roles"`
	Affiliations      []TermSummary          `json:"affiliations"`
	Links             []Link                 `json:"links"`
	Works             []PersonWork           `json:"works"`
	WorksTotal        int64                  `json:"works_total"`
	Performances      []PersonPerformance    `json:"performances"`
	PerformancesTotal int64                  `json:"performances_total"`
}

// PersonAlias is the JSON representation of another public name of a person.
type PersonAlias struct {
	Name string                 `json:"name"`
	Kind domain.PersonAliasKind `json:"kind"`
}

// PersonWork is the JSON representation of a person's credit on a piece.
type PersonWork struct {
	Piece     PieceSummary      `json:"piece"`
	Role      domain.CreditRole `json:"role"`
	Certainty domain.Certainty  `json:"certainty"`
}

// PersonPerformance is the JSON representation of a performance on a person's page.
type PersonPerformance struct {
	Piece               *PieceSummary   `json:"piece"`
	Link                PerformanceLink `json:"link"`
	Year                *int16          `json:"year"`
	Event               *EventSummary   `json:"event"`
	DescriptionVerbatim string          `json:"description_verbatim"`
}

// PerformanceLink is the JSON representation of the link recording a performance.
type PerformanceLink struct {
	URL      string  `json:"url"`
	Title    *string `json:"title"`
	Platform *string `json:"platform"`
}

// PersonGet handles GET /v1/persons/{slug}, a public person's page. An unapproved person is not found,
// exactly like an unknown one; a person who prefers not to have a page has a null profile.
type PersonGet struct {
	Catalogue datasources.Catalogue
}

func (c PersonGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}

	person, err := c.Catalogue.GetPublicPerson(ctx, slug)
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, "person not found")
	case err != nil:
		writeInternalError(w, r, "unable to get person", err)
	default:
		httpjson.WriteJSON(ctx, w, http.StatusOK, personFromDomain(person))
	}
}

func personFromDomain(p domain.Person) Person {
	return Person{
		PersonSummary: personSummaryFromDomain(p.PersonSummary),
		UpdatedAt:     p.UpdatedAt,
		Status:        p.Status,
		Profile:       optional(p.Profile, personProfileFromDomain),
	}
}

func personProfileFromDomain(p domain.PersonProfile) PersonProfile {
	return PersonProfile{
		NotesMD:          p.NotesMD,
		LicensingStatus:  p.LicensingStatus,
		LicensingNotesMD: p.LicensingNotesMD,
		Aliases:          mapSlice(p.Aliases, personAliasFromDomain),
		Roles:            nonNil(p.Roles),
		Affiliations:     mapSlice(p.Affiliations, termSummaryFromDomain),
		Links:            mapSlice(p.Links, linkFromDomain),
		Works: mapSlice(p.Works, func(w domain.PersonWork) PersonWork {
			return PersonWork{Piece: pieceSummaryFromDomain(w.Piece), Role: w.Role, Certainty: w.Certainty}
		}),
		WorksTotal: p.WorksTotal,
		Performances: mapSlice(p.Performances, func(pp domain.PersonPerformance) PersonPerformance {
			return PersonPerformance{
				Piece:               optional(pp.Piece, pieceSummaryFromDomain),
				Link:                performanceLinkFromDomain(pp.Link),
				Year:                pp.Year,
				Event:               optional(pp.Event, eventSummaryFromDomain),
				DescriptionVerbatim: pp.DescriptionVerbatim,
			}
		}),
		PerformancesTotal: p.PerformancesTotal,
	}
}
func personAliasFromDomain(a domain.PersonAlias) PersonAlias {
	return PersonAlias{Name: a.Name, Kind: a.Kind}
}
