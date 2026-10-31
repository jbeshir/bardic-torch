package controller

import (
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// dateLayout is how dates without a time are written, as OpenAPI's date format.
const dateLayout = "2006-01-02"

// Person is the JSON representation of a public person's page.
type Person struct {
	PersonSummary

	Status          domain.PersonStatus `json:"status"`
	StatusDate      *string             `json:"status_date"`
	StatusSourceURL *string             `json:"status_source_url"`
	Profile         *PersonProfile      `json:"profile"`
}

// PersonProfile is the JSON representation of everything a person's page shows beyond their identity.
type PersonProfile struct {
	Pronouns          *string                `json:"pronouns"`
	NotesMD           *string                `json:"notes_md"`
	LicensingStatus   domain.LicensingStatus `json:"licensing_status"`
	LicensingNotesMD  *string                `json:"licensing_notes_md"`
	Aliases           []PersonAlias          `json:"aliases"`
	Roles             []domain.PersonRole    `json:"roles"`
	Affiliations      []TermSummary          `json:"affiliations"`
	Relations         []PersonRelation       `json:"relations"`
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

// PersonRelation is the JSON representation of a relation between two persons.
type PersonRelation struct {
	Type      domain.PersonRelationType `json:"type"`
	Direction domain.RelationDirection  `json:"direction"`
	Person    PersonSummary             `json:"person"`
}

// PersonWork is the JSON representation of a person's credit on a piece.
type PersonWork struct {
	Piece     PieceSummary      `json:"piece"`
	Role      domain.CreditRole `json:"role"`
	Certainty domain.Certainty  `json:"certainty"`
	Part      *domain.PiecePart `json:"part"`
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
	Getter datasources.PublicPersonGetter
}

func (c PersonGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}

	person, err := c.Getter.GetPublicPerson(ctx, slug)
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
	person := Person{
		PersonSummary:   personSummaryFromDomain(p.PersonSummary),
		Status:          p.Status,
		StatusSourceURL: p.StatusSourceURL,
		Profile:         optional(p.Profile, personProfileFromDomain),
	}
	if p.StatusDate != nil {
		date := p.StatusDate.Format(dateLayout)
		person.StatusDate = &date
	}

	return person
}

func personProfileFromDomain(p domain.PersonProfile) PersonProfile {
	return PersonProfile{
		Pronouns:         p.Pronouns,
		NotesMD:          p.NotesMD,
		LicensingStatus:  p.LicensingStatus,
		LicensingNotesMD: p.LicensingNotesMD,
		Aliases: mapSlice(p.Aliases, func(a domain.PersonAlias) PersonAlias {
			return PersonAlias{Name: a.Name, Kind: a.Kind}
		}),
		Roles:        nonNil(p.Roles),
		Affiliations: mapSlice(p.Affiliations, termSummaryFromDomain),
		Relations: mapSlice(p.Relations, func(r domain.PersonRelation) PersonRelation {
			return PersonRelation{Type: r.Type, Direction: r.Direction, Person: personSummaryFromDomain(r.Person)}
		}),
		Links: mapSlice(p.Links, linkFromDomain),
		Works: mapSlice(p.Works, func(w domain.PersonWork) PersonWork {
			return PersonWork{Piece: pieceSummaryFromDomain(w.Piece), Role: w.Role, Certainty: w.Certainty, Part: w.Part}
		}),
		WorksTotal: p.WorksTotal,
		Performances: mapSlice(p.Performances, func(pp domain.PersonPerformance) PersonPerformance {
			return PersonPerformance{
				Piece:               optional(pp.Piece, pieceSummaryFromDomain),
				Link:                PerformanceLink{URL: pp.Link.URL, Title: pp.Link.Title, Platform: pp.Link.Platform},
				Year:                pp.Year,
				Event:               optional(pp.Event, eventSummaryFromDomain),
				DescriptionVerbatim: pp.DescriptionVerbatim,
			}
		}),
		PerformancesTotal: p.PerformancesTotal,
	}
}
