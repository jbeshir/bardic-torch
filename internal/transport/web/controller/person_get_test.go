package controller

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const testPersonSlug = "wren-of-the-vale"

// testPerson has every field and list of a person set, including each nullable reference both set and null.
func testPerson() domain.Person {
	checkedAt := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	kettle := domain.PieceSummary{
		Slug: "the-copper-kettle", Title: testPieceTitle, Kind: domain.PieceKindSong,
		SecondaryKinds: []domain.PieceKind{domain.PieceKindComedy}, ConservationStatus: domain.ConservationStatusLowRisk,
	}

	return domain.Person{
		PersonSummary: domain.PersonSummary{
			Slug: testPersonSlug, Name: testPersonName, Kind: domain.PersonKindCommunityBard,
		},
		Status:          domain.PersonStatusDeceased,
		StatusDate:      new(time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)),
		StatusSourceURL: new("https://example.org/notice"),
		Profile: &domain.PersonProfile{
			Pronouns:         new("she/her"),
			NotesMD:          new("A bard."),
			LicensingStatus:  domain.LicensingStatusKnown,
			LicensingNotesMD: new("Sing freely."),
			Aliases:          []domain.PersonAlias{{Name: "Wren", Kind: domain.PersonAliasKindNickname}},
			Roles:            []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRolePoet},
			Affiliations: []domain.TermSummary{
				{Slug: "northmarch", Facet: domain.FacetAffiliation, DisplayName: "Northmarch"},
			},
			Relations: []domain.PersonRelation{{
				Type: domain.PersonRelationTypeMemberOf, Direction: domain.RelationDirectionOutgoing,
				Person: domain.PersonSummary{
					Slug: "northmarch-bards", Name: "Northmarch Bards", Kind: domain.PersonKindGroup, IsStub: true,
				},
			}},
			Links: []domain.Link{{
				URL:           "https://example.org/wren",
				Title:         new("Wren's songs"),
				Description:   new("Songs."),
				Platform:      new("example.org"),
				PlaceType:     new(domain.LinkPlaceTypeAuthorSite),
				Collection:    &domain.CollectionRef{ID: 3, Name: "Bards of the North"},
				Locator:       new("entry 4"),
				Liveness:      domain.LivenessDead,
				LastCheckedAt: &checkedAt,
				ArchivedURL:   new("https://archive.example.org/wren"),
				Roles:         []domain.LinkRole{domain.LinkRoleWebsite},
			}},
			Works: []domain.PersonWork{
				{Piece: kettle, Role: domain.CreditRoleLyrics, Certainty: domain.CertaintyProbable},
				{Piece: kettle, Role: domain.CreditRoleTune, Certainty: domain.CertaintyCertain, Part: new(domain.PiecePartChorus)},
			},
			WorksTotal: 250,
			Performances: []domain.PersonPerformance{
				{
					Piece: &kettle,
					Link: domain.PerformanceLink{
						URL: "https://video.example.org/watch/kettle", Title: new("At the revel"),
						Platform: new("video.example.org"),
					},
					Year:                new(int16(2025)),
					Event:               &domain.EventSummary{Slug: "the-green-hall", Name: "The Green Hall", Kind: domain.EventKindVenue},
					DescriptionVerbatim: "Wren sings the Kettle",
				},
				{Link: domain.PerformanceLink{URL: "https://example.org/opening"}, DescriptionVerbatim: "Opening"},
			},
			PerformancesTotal: 2,
		},
	}
}

const testPersonBody = `{
	"slug": "wren-of-the-vale", "name": "Wren of the Vale", "kind": "community_bard", "is_stub": false,
	"status": "deceased", "status_date": "2025-03-01", "status_source_url": "https://example.org/notice",
	"profile": {
		"pronouns": "she/her", "notes_md": "A bard.", "licensing_status": "known", "licensing_notes_md": "Sing freely.",
		"aliases": [{"name": "Wren", "kind": "nickname"}],
		"roles": ["singer", "poet"],
		"affiliations": [{"slug": "northmarch", "facet": "affiliation", "display_name": "Northmarch"}],
		"relations": [{"type": "member_of", "direction": "outgoing", "person": {"slug": "northmarch-bards",
			"name": "Northmarch Bards", "kind": "group", "is_stub": true}}],
		"links": [{"url": "https://example.org/wren", "title": "Wren's songs", "description": "Songs.",
			"platform": "example.org", "place_type": "author_site",
			"collection": {"id": 3, "name": "Bards of the North"}, "locator": "entry 4", "liveness": "dead",
			"last_checked_at": "2026-10-01T09:00:00Z", "archived_url": "https://archive.example.org/wren",
			"roles": ["website"]}],
		"works": [
			{"piece": {"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "song",
				"secondary_kinds": ["comedy"], "conservation_status": "low_risk", "is_stub": false},
				"role": "lyrics", "certainty": "probable", "part": null},
			{"piece": {"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "song",
				"secondary_kinds": ["comedy"], "conservation_status": "low_risk", "is_stub": false},
				"role": "tune", "certainty": "certain", "part": "chorus"}
		],
		"works_total": 250,
		"performances": [
			{"piece": {"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "song",
				"secondary_kinds": ["comedy"], "conservation_status": "low_risk", "is_stub": false},
				"link": {"url": "https://video.example.org/watch/kettle", "title": "At the revel",
					"platform": "video.example.org"},
				"year": 2025, "event": {"slug": "the-green-hall", "name": "The Green Hall", "kind": "venue"},
				"description_verbatim": "Wren sings the Kettle"},
			{"piece": null, "link": {"url": "https://example.org/opening", "title": null, "platform": null},
				"year": null, "event": null, "description_verbatim": "Opening"}
		],
		"performances_total": 2
	}
}`

func TestPersonGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	noPage := domain.Person{
		PersonSummary: domain.PersonSummary{
			Slug: testPersonSlug, Name: testPersonName, Kind: domain.PersonKindHistorical, IsStub: true,
		},
		Status: domain.PersonStatusPrefersNoPage,
	}
	emptyProfile := domain.Person{
		PersonSummary: domain.PersonSummary{Slug: testPersonSlug, Name: testPersonName, Kind: domain.PersonKindGroup},
		Status:        domain.PersonStatusActive,
		Profile:       &domain.PersonProfile{LicensingStatus: domain.LicensingStatusUnknown},
	}

	cases := []struct {
		name       string
		person     domain.Person
		getErr     error
		wantStatus int
		wantBody   string
	}{
		{name: "person", person: testPerson(), wantStatus: http.StatusOK, wantBody: testPersonBody},
		{
			name:       "no_page",
			person:     noPage,
			wantStatus: http.StatusOK,
			wantBody: `{"slug": "wren-of-the-vale", "name": "Wren of the Vale", "kind": "historical",
				"is_stub": true, "status": "prefers_no_page", "status_date": null, "status_source_url": null,
				"profile": null}`,
		},
		{
			name:       "empty_lists",
			person:     emptyProfile,
			wantStatus: http.StatusOK,
			wantBody: `{"slug": "wren-of-the-vale", "name": "Wren of the Vale", "kind": "group", "is_stub": false,
				"status": "active", "status_date": null, "status_source_url": null,
				"profile": {"pronouns": null, "notes_md": null, "licensing_status": "unknown",
					"licensing_notes_md": null, "aliases": [], "roles": [], "affiliations": [], "relations": [],
					"links": [], "works": [], "works_total": 0, "performances": [], "performances_total": 0}}`,
		},
		{
			name:       notFoundCase,
			getErr:     datasources.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"code":"not_found","message":"person not found"}`,
		},
		{
			name:       "get_failure",
			getErr:     errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			getter := mocks.NewPublicPersonGetter(t)
			getter.EXPECT().GetPublicPerson(mock.Anything, testPersonSlug).Return(tc.person, tc.getErr)

			rec := serveSlug(t, PersonGet{Getter: getter}, "persons", testPersonSlug, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidSlugCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveSlug(t, PersonGet{Getter: mocks.NewPublicPersonGetter(t)}, "persons", tc.pathSlug, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
