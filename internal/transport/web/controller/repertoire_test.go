package controller

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	repertoireEntryPath  = "/v1/me/repertoire/" + testPieceSlug
	repertoireNotes      = "Capo 2."
	publicRepertoirePath = "/v1/users/" + testPublicID + "/repertoire"
)

// repertoireEntryBody is the JSON of testRepertoireEntry.
var repertoireEntryBody = `{"piece": ` + pagedPieceJSON + `, "notes": "Capo 2.", "visibility": "public", ` +
	`"added_at": "2026-10-31T12:00:00Z", "updated_at": "2026-10-31T12:00:00Z"}`

var testRepertoireEntry = domain.RepertoireEntry{
	Piece: pagedPiece, Notes: new(repertoireNotes), Visibility: domain.RepertoireVisibilityPublic,
	AddedAt: testNow, UpdatedAt: testNow,
}

// repertoireEntryCases are the requests every operation on the caller's entry for a piece answers the same way:
// without credentials, with an invalid slug, and with the command failing.
func repertoireEntryCases(base revisionCase, notFound string, notFoundCases ...revisionCase) []revisionCase {
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	cases := []revisionCase{anonymous}
	for _, tc := range invalidSlugCases {
		invalid := base
		invalid.name, invalid.target = "slug_"+tc.name, "/v1/me/repertoire/"+url.PathEscape(tc.pathSlug)
		invalid.pathValues = map[string]string{slugVar: tc.pathSlug}
		invalid.invalidRequest, invalid.notCalled = true, true
		invalid.wantStatus, invalid.wantBody = http.StatusBadRequest, tc.wantBody
		cases = append(cases, invalid)
	}

	return append(cases, errorCases(base, append(notFoundCases,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
			wantBody: `{"code":"not_found","message":"` + notFound + `"}`},
		userBlockedCase, unavailableCase, internalCase)...)...)
}

func TestMyRepertoireList_ServeHTTP(t *testing.T) {
	t.Parallel()

	list := domain.RepertoireList{Entries: []domain.RepertoireEntry{testRepertoireEntry}, Total: 3}
	base := revisionCase{method: http.MethodGet, target: "/v1/me/repertoire"}
	ok := base
	ok.name, ok.target = "ok", "/v1/me/repertoire?visibility=public&limit=1&offset=2"
	ok.wantStatus, ok.wantBody = http.StatusOK, `{"data": [`+repertoireEntryBody+`], "total": 3}`
	empty := base
	empty.name, empty.wantStatus, empty.wantBody = emptyListsCase, http.StatusOK, `{"data": [], "total": 0}`
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badQuery := func(name, query, message string) revisionCase {
		tc := badBodyCase(base, name, "", message)
		tc.target = "/v1/me/repertoire?" + query
		return tc
	}
	cases := []revisionCase{ok, empty, anonymous,
		badQuery("unknown_visibility", "visibility=friends", "invalid visibility: not one of its allowed values"),
		badQuery("repeated_visibility", "visibility=public&visibility=private",
			"query parameter visibility must be given at most once"),
	}
	for _, page := range invalidPageCases {
		tc := badQuery(page.name, page.query, "")
		tc.wantBody = page.wantBody
		cases = append(cases, tc)
	}
	cases = append(cases, errorCases(base, userBlockedCase, unavailableCase, internalCase)...)

	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return MyRepertoireList{Command: commandFunc[command.RepertoireListRequest, domain.RepertoireList](
			func(_ context.Context, req command.RepertoireListRequest) (domain.RepertoireList, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				assert.Equal(t, principalUserID, req.Principal.User.ID)
				if tc.name != "ok" {
					assert.Nil(t, req.Visibility)
					assert.Equal(t, domain.Page{Limit: domain.DefaultPageLimit}, req.Page)
					return domain.RepertoireList{Entries: []domain.RepertoireEntry{}}, tc.err
				}
				assert.Equal(t, new(domain.RepertoireVisibilityPublic), req.Visibility)
				assert.Equal(t, domain.Page{Limit: 1, Offset: 2}, req.Page)
				return list, nil
			})}
	})
}

func TestMyRepertoireEntryGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodGet, target: repertoireEntryPath,
		pathValues: map[string]string{slugVar: testPieceSlug}}
	ok := base
	ok.name, ok.wantStatus, ok.wantBody = "ok", http.StatusOK, repertoireEntryBody
	cases := append([]revisionCase{ok}, repertoireEntryCases(base, "repertoire entry not found")...)

	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return MyRepertoireEntryGet{Command: commandFunc[command.RepertoireEntryRequest, domain.RepertoireEntry](
			func(_ context.Context, req command.RepertoireEntryRequest) (domain.RepertoireEntry, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				assert.Equal(t, command.RepertoireEntryRequest{Principal: req.Principal, PieceSlug: testPieceSlug}, req)
				assert.Equal(t, principalUserID, req.Principal.User.ID)
				return testRepertoireEntry, tc.err
			})}
	})
}

func TestMyRepertoireEntryPut_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodPut, target: repertoireEntryPath,
		pathValues: map[string]string{slugVar: testPieceSlug}, body: `{"notes":"Capo 2.","visibility":"public"}`}
	created := base
	created.name, created.wantStatus, created.wantBody = createdCase, http.StatusCreated, repertoireEntryBody
	updated := base
	updated.name, updated.wantStatus, updated.wantBody = "updated", http.StatusOK, repertoireEntryBody
	// Notes are moderation free text: CRLF and a lone CR are stored as LF.
	lineBreaks := updated
	lineBreaks.name, lineBreaks.body = "line_breaks_normalised", `{"notes":"Capo 2.\r\nSlow\rthird verse","visibility":"public"}`
	noNotes := updated
	noNotes.name, noNotes.body = "notes_absent", `{"visibility":"private"}`
	nullNotes := updated
	nullNotes.name, nullNotes.body = "notes_null", `{"notes":null,"visibility":"private"}`
	longest := updated
	longest.name = "notes_longest"
	longest.body = `{"notes":"` + strings.Repeat("a", domain.MaxRepertoireNotesLength) + `","visibility":"public"}`
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"notes":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `","visibility":"public"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	notJSON := base
	notJSON.name, notJSON.contentType, notJSON.invalidRequest, notJSON.notCalled = notJSONCase, plainText, true, true
	notJSON.wantStatus, notJSON.wantBody = http.StatusUnsupportedMediaType, unsupportedMediaTypeBody
	cases := append([]revisionCase{created, updated, lineBreaks, noNotes, nullNotes, longest, tooLarge, notJSON,
		badBodyCase(base, "no_visibility", `{"notes":"Capo 2."}`, "invalid visibility: must be private or public"),
		badBodyCase(base, "unknown_visibility", `{"visibility":"friends"}`,
			"invalid visibility: must be private or public"),
		badBodyCase(base, "unknown_field", `{"visibility":"public","owner":"x"}`, "request body has an unknown field"),
		badBodyCase(base, "notes_too_long",
			`{"notes":"`+strings.Repeat("a", domain.MaxRepertoireNotesLength+1)+`","visibility":"public"}`,
			"invalid notes: must be at most 2000 characters"),
		controlCharacterCase(base, "notes", `{"notes":"Capo\u202e2.","visibility":"public"}`),
	}, repertoireEntryCases(base, "piece not found",
		revisionCase{name: "repertoire_limit_reached", err: datasources.ErrRepertoireLimitReached,
			wantStatus: http.StatusConflict,
			wantBody:   `{"code":"repertoire_limit_reached","message":"a repertoire may hold at most 2000 entries"}`},
	)...)

	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return MyRepertoireEntryPut{Command: commandFunc[command.PutRepertoireEntryRequest, command.PutRepertoireEntryResult](
			func(_ context.Context, req command.PutRepertoireEntryRequest) (command.PutRepertoireEntryResult, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				assert.Equal(t, principalUserID, req.Principal.User.ID)
				assert.Equal(t, testPieceSlug, req.PieceSlug)
				switch tc.name {
				case lineBreaks.name:
					assert.Equal(t, new("Capo 2.\nSlow\nthird verse"), req.Notes)
				case noNotes.name, nullNotes.name:
					assert.Nil(t, req.Notes)
					assert.Equal(t, domain.RepertoireVisibilityPrivate, req.Visibility)
				case longest.name:
					assert.Len(t, *req.Notes, domain.MaxRepertoireNotesLength)
				default:
					assert.Equal(t, new(repertoireNotes), req.Notes)
					assert.Equal(t, domain.RepertoireVisibilityPublic, req.Visibility)
				}
				return command.PutRepertoireEntryResult{Entry: testRepertoireEntry, Created: tc.name == createdCase},
					tc.err
			})}
	})
}

func TestMyRepertoireEntryDelete_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodDelete, target: repertoireEntryPath,
		pathValues: map[string]string{slugVar: testPieceSlug}}
	removed := base
	removed.name, removed.wantStatus = "removed", http.StatusNoContent
	cases := append([]revisionCase{removed}, repertoireEntryCases(base, "repertoire entry not found")...)

	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return MyRepertoireEntryDelete{Command: commandFunc[command.RepertoireEntryRequest, command.Empty](
			func(_ context.Context, req command.RepertoireEntryRequest) (command.Empty, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				assert.Equal(t, principalUserID, req.Principal.User.ID)
				assert.Equal(t, testPieceSlug, req.PieceSlug)
				return command.Empty{}, tc.err
			})}
	})
}

func TestPublicRepertoireGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	vars := map[string]string{publicIDVar: testPublicID}
	repertoire := domain.PublicRepertoire{
		Owner:   domain.RepertoireOwner{DisplayName: testDisplayName, Role: domain.RoleContributor},
		Entries: []domain.PublicRepertoireEntry{{Piece: pagedPiece, AddedAt: testNow}},
		Total:   4,
	}
	listJSON := `{"owner": {"display_name": "Morwen", "role": "contributor"}, ` +
		`"data": [{"piece": ` + pagedPieceJSON + `, "added_at": "2026-10-31T12:00:00Z"}], "total": 4}`

	for _, tc := range pagedListCases(listJSON, repertoireNotFound) {
		if tc.name == emptyListsCase {
			// A repertoire with no public entry is not found, so an empty page has a total of at least one.
			tc.wantBody = `{"owner": {"display_name": "Morwen", "role": "contributor"}, "data": [], "total": 4}`
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var answer domain.PublicRepertoire
			switch tc.name {
			case listCase:
				answer = repertoire
			case emptyListsCase:
				answer = domain.PublicRepertoire{Owner: repertoire.Owner, Total: 4}
			}
			lister := mocks.NewPublicRepertoireLister(t)
			lister.EXPECT().ListPublicRepertoire(mock.Anything, testPublicID, tc.wantPage).Return(answer, tc.listErr)

			rec := serveGet(t, PublicRepertoireGet{Lister: lister}, publicRepertoirePath+"?"+tc.query, vars, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	const invalidPublicIDBody = `{"code":"invalid_request","message":"invalid public_id: must be 16 lowercase ` +
		`letters and digits from 2 to 7"}`
	for _, tc := range []struct{ name, publicID string }{
		{name: "user_id", publicID: "google-oauth2|123"},
		{name: "uppercase", publicID: "K3VQ7XW2MBNC4ZRA"},
		{name: "digit_1", publicID: "k3vq7xw2mbnc4zr1"},
		{name: "shorter", publicID: "k3vq7xw2mbnc4zr"},
		{name: "longer", publicID: "k3vq7xw2mbnc4zraa"},
	} {
		t.Run("public_id_"+tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveGet(t, PublicRepertoireGet{Lister: mocks.NewPublicRepertoireLister(t)},
				"/v1/users/"+url.PathEscape(tc.publicID)+"/repertoire", map[string]string{publicIDVar: tc.publicID}, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, invalidPublicIDBody, rec.Body.String())
		})
	}

	for _, tc := range invalidPageCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := serveGet(t, PublicRepertoireGet{Lister: mocks.NewPublicRepertoireLister(t)},
				publicRepertoirePath+"?"+tc.query, vars, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}

// publicIDVar is the path variable of a public repertoire's public ID.
const publicIDVar = "public_id"
