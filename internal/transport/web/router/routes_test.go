package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The route table is the one place that checks every operation: that the routes, the OpenAPI spec and this table
// agree, who may call each operation, and that its happy path answers as the spec says.

// Users the validator authenticates as, by the ID after "Bearer ".
const (
	healthzPath = "/healthz"
	mePath      = "/v1/me"

	badBearerName = "bad_bearer"

	contributorID  = "google-oauth2|contributor"
	moderatorID    = "google-oauth2|moderator"
	blockedID      = "google-oauth2|blocked"
	unresolvableID = "google-oauth2|unresolvable"
	invalidSecret  = "invalid"
	blockTargetID  = "google-oauth2|123"
	routeSlug      = "the-copper-kettle"
)

type access int

const (
	public access = iota
	signedIn
	moderator
)

// routeRow is an operation's access, and a request to it that succeeds when the caller may make it.
type routeRow struct {
	access  access
	path    string
	body    string
	success int
	// seed gives the fake what the happy path reads.
	seed func(*fakerepo.Repo)
}

func getting(path string, seed func(*fakerepo.Repo)) routeRow {
	return routeRow{access: public, path: path, success: http.StatusOK, seed: seed}
}

var (
	piece = domain.Piece{
		PieceSummary: domain.PieceSummary{Slug: routeSlug, Title: "The Copper Kettle", Kind: domain.PieceKindSong,
			ConservationStatus: domain.ConservationStatusUnassessed},
		LicensingStatus: domain.LicensingStatusUnknown,
	}
	person = domain.Person{
		PersonSummary: domain.PersonSummary{Slug: routeSlug, Name: "Wren of the Vale", Kind: domain.PersonKindCommunityBard,
			IsStub: true},
		Status: domain.PersonStatusPrefersNoPage,
	}
	term = domain.Term{
		TermSummary: domain.TermSummary{Slug: routeSlug, Facet: domain.FacetTopic, DisplayName: "Kettles"},
		Name:        "Kettles",
	}
	collection = domain.Collection{
		CollectionSummary: domain.CollectionSummary{ID: 12, Name: "The Northmarch Songbook",
			Kind: domain.CollectionKindSongbookPDF},
		Enumerable: domain.CollectionEnumerableUnknown,
	}
	event = domain.Event{
		EventSummary: domain.EventSummary{Slug: routeSlug, Name: "Midwinter Revel", Kind: domain.EventKindEvent},
	}
	revision = domain.EventRevision{
		RevisionSummary: domain.RevisionSummary{ID: 42, Source: domain.RevisionSourceImport},
		Entity:          event.EventSummary,
		Snapshot:        domain.EventSnapshot{Name: event.Name, Kind: event.Kind},
	}
)

var (
	linkReports = domain.LinkReportList{Total: 1, Reports: []domain.LinkReport{{ID: 7,
		Link:     domain.ReportedLink{ID: 9, URL: "https://example.org/kettle"},
		Reporter: domain.ModerationUser{UserID: contributorID, DisplayName: "Rook"},
		Reason:   domain.LinkReportReasonBroken, Status: domain.ReportStatusOpen, CreatedAt: testNow,
	}}}
	moderationLink = domain.ModerationLink{ID: 9, URL: "https://example.org/kettle", Liveness: domain.LivenessOK,
		Attachments: []domain.LinkAttachment{{EntityType: domain.EntityTypePiece, Slug: routeSlug,
			Title: "The Copper Kettle", Roles: []domain.LinkRole{domain.LinkRoleLyrics}}}}
	contactRequests = domain.ContactRequestList{Total: 1, Requests: []domain.ContactRequest{{ID: 3,
		Kind: domain.ContactKindGeneral, Status: domain.ReportStatusOpen, CreatedAt: testNow, Purged: true,
	}}}
)

// turnstilePasses is a Turnstile verifier that passes every token.
type turnstilePasses struct{}

func (turnstilePasses) VerifyTurnstile(context.Context, string) (bool, error) { return true, nil }

var (
	eventDocument = domain.EditorEventDocument{SchemaVersion: 1, Name: event.Name, Kind: event.Kind}
	revisionList  = domain.RevisionList{Total: 1, Revisions: []domain.RevisionListItem{{ID: 51,
		EntityType: domain.EntityTypeEvent, Entity: &domain.RevisionEntity{Slug: new(routeSlug), Name: event.Name},
		Status: domain.RevisionStatusPending, Summary: "Describe the revel.", CreatedAt: testNow,
	}}}
)

// seedRevisionView gives the fake a pending event revision to view.
func seedRevisionView(r *fakerepo.Repo) {
	r.RevisionView = domain.EventRevisionView{
		RevisionSummary: domain.RevisionSummary{ID: 51, Source: domain.RevisionSourceUser, CreatedAt: testNow,
			Author: &domain.RevisionAuthor{DisplayName: "Rook"}},
		Status:   domain.RevisionStatusPending,
		Entity:   &event.EventSummary,
		Snapshot: domain.EventSnapshot{Name: event.Name, Kind: event.Kind},
		Base:     &domain.RevisionBase[domain.EventSnapshot]{ID: 42, Snapshot: revision.Snapshot},
	}
}

// editableRow is a signed-in row reading the editable document doc.
func editableRow(path string, doc any) routeRow {
	return routeRow{access: signedIn, path: path, success: http.StatusOK, seed: func(r *fakerepo.Repo) {
		r.Editable = &datasources.Editable{BaseRevisionID: 42, Document: doc}
	}}
}

func nothingSeeded(*fakerepo.Repo) {}

var routeTable = map[openapitest.Operation]routeRow{
	{Method: http.MethodGet, Path: healthzPath}: getting(healthzPath, nothingSeeded),
	{Method: http.MethodGet, Path: "/readyz"}:   getting("/readyz", nothingSeeded),
	{Method: http.MethodGet, Path: mePath}: {access: signedIn, path: mePath, success: http.StatusOK,
		seed: nothingSeeded},
	{Method: http.MethodPatch, Path: mePath}: {access: signedIn, path: mePath, body: `{"display_name":"  Morwen "}`,
		success: http.StatusOK, seed: nothingSeeded},
	{Method: http.MethodPut, Path: "/v1/users/{user_id}/blocked"}: {access: moderator,
		path: "/v1/users/google-oauth2%7C123/blocked", body: `{"blocked":true}`, success: http.StatusOK,
		seed: nothingSeeded},
	{Method: http.MethodPost, Path: "/v1/link-reports"}: {access: signedIn, path: "/v1/link-reports",
		body: `{"url":"https://example.org/kettle","reason":"broken","message":"Gone."}`, success: http.StatusCreated,
		seed: nothingSeeded},
	{Method: http.MethodGet, Path: "/v1/moderation/link-reports"}: {access: moderator,
		path: "/v1/moderation/link-reports", success: http.StatusOK, seed: func(r *fakerepo.Repo) { r.LinkReports = linkReports }},
	{Method: http.MethodPost, Path: "/v1/moderation/link-reports/{report_id}"}: {access: moderator,
		path: "/v1/moderation/link-reports/7", body: `{"status":"resolved","note":"Hidden.","hide_link":true}`,
		success: http.StatusNoContent, seed: nothingSeeded},
	{Method: http.MethodGet, Path: "/v1/moderation/links"}: {access: moderator,
		path: "/v1/moderation/links?url=https%3A%2F%2Fexample.org%2Fkettle", success: http.StatusOK,
		seed: func(r *fakerepo.Repo) { r.ModerationLink = &moderationLink }},
	{Method: http.MethodPut, Path: "/v1/links/{link_id}/hidden"}: {access: moderator, path: "/v1/links/9/hidden",
		body: `{"hidden":true}`, success: http.StatusNoContent, seed: nothingSeeded},
	{Method: http.MethodPost, Path: "/v1/contact"}: {access: public, path: "/v1/contact",
		body: `{"kind":"takedown","name":"Wren","email":"wren@example.org","message":"Please remove it.",` +
			`"url":"https://example.org/kettle","turnstile_token":"tok"}`,
		success: http.StatusAccepted, seed: nothingSeeded},
	{Method: http.MethodGet, Path: "/v1/moderation/contact-requests"}: {access: moderator,
		path: "/v1/moderation/contact-requests", success: http.StatusOK,
		seed: func(r *fakerepo.Repo) { r.ContactRequests = contactRequests }},
	{Method: http.MethodPost, Path: "/v1/moderation/contact-requests/{request_id}"}: {access: moderator,
		path: "/v1/moderation/contact-requests/3", body: `{"status":"dismissed","note":null}`,
		success: http.StatusNoContent, seed: nothingSeeded},
	{Method: http.MethodGet, Path: "/v1/pieces"}: getting("/v1/pieces", nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/pieces/{slug}"}: getting("/v1/pieces/"+routeSlug,
		func(r *fakerepo.Repo) { r.Piece = &piece }),
	{Method: http.MethodGet, Path: "/v1/pieces/{slug}/history"}: getting("/v1/pieces/"+routeSlug+"/history",
		nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/persons"}: getting("/v1/persons", nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/persons/{slug}"}: getting("/v1/persons/"+routeSlug,
		func(r *fakerepo.Repo) { r.Person = &person }),
	{Method: http.MethodGet, Path: "/v1/persons/{slug}/history"}: getting("/v1/persons/"+routeSlug+"/history",
		nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/terms"}: getting("/v1/terms", nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/terms/{slug}"}: getting("/v1/terms/"+routeSlug,
		func(r *fakerepo.Repo) { r.Term = &term }),
	{Method: http.MethodGet, Path: "/v1/terms/{slug}/history"}: getting("/v1/terms/"+routeSlug+"/history",
		nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/collections"}: getting("/v1/collections", nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/collections/{collection_id}"}: getting("/v1/collections/12",
		func(r *fakerepo.Repo) { r.Collection = &collection }),
	{Method: http.MethodGet, Path: "/v1/collections/{collection_id}/history"}: getting("/v1/collections/12/history",
		nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/events"}: getting("/v1/events", nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/events/{slug}"}: getting("/v1/events/"+routeSlug,
		func(r *fakerepo.Repo) { r.Event = &event }),
	{Method: http.MethodGet, Path: "/v1/events/{slug}/history"}: getting("/v1/events/"+routeSlug+"/history",
		nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/revisions/{revision_id}"}: getting("/v1/revisions/42",
		func(r *fakerepo.Repo) { r.Revision = revision }),
	{Method: http.MethodGet, Path: "/v1/needs-work"}: getting("/v1/needs-work", nothingSeeded),
	{Method: http.MethodGet, Path: "/v1/me/revisions"}: {access: signedIn, path: "/v1/me/revisions?status=pending",
		success: http.StatusOK, seed: func(r *fakerepo.Repo) { r.RevisionList = revisionList }},
	{Method: http.MethodGet, Path: "/v1/me/revisions/{revision_id}"}: {access: signedIn,
		path: "/v1/me/revisions/51", success: http.StatusOK, seed: seedRevisionView},
	{Method: http.MethodGet, Path: "/v1/pieces/{slug}/editable"}: editableRow("/v1/pieces/"+routeSlug+"/editable",
		domain.PieceDocument{SchemaVersion: 1, Title: piece.Title, Kind: piece.Kind,
			Conservation:    domain.DocConservation{Status: piece.ConservationStatus},
			LicensingStatus: piece.LicensingStatus}.ToEditor(domain.Slugs{})),
	{Method: http.MethodGet, Path: "/v1/persons/{slug}/editable"}: editableRow("/v1/persons/"+routeSlug+"/editable",
		domain.PersonDocument{SchemaVersion: 1, Name: person.Name, Kind: person.Kind, Status: person.Status,
			LicensingStatus: domain.LicensingStatusUnknown}.ToEditor(domain.Slugs{})),
	{Method: http.MethodGet, Path: "/v1/terms/{slug}/editable"}: editableRow("/v1/terms/"+routeSlug+"/editable",
		domain.TermDocument{SchemaVersion: 1, Facet: term.Facet, Name: term.Name,
			DisplayName: term.DisplayName}.ToEditor(domain.Slugs{})),
	{Method: http.MethodGet, Path: "/v1/events/{slug}/editable"}: editableRow("/v1/events/"+routeSlug+"/editable",
		eventDocument),
	{Method: http.MethodGet, Path: "/v1/collections/{collection_id}/editable"}: editableRow(
		"/v1/collections/12/editable", domain.EditorCollectionDocument{SchemaVersion: 1, Name: collection.Name,
			Kind: collection.Kind, Enumerable: collection.Enumerable}),
	{Method: http.MethodPost, Path: "/v1/revisions"}: {access: signedIn, path: "/v1/revisions",
		body: `{"entity_type":"event","base_revision_id":42,"summary":"Describe the revel.","document":` +
			`{"schema_version":1,"name":"Midwinter Revel","kind":"event","description_md":"Held at midwinter."},` +
			`"acknowledge_duplicates":false,"acknowledge_stale":false}`,
		success: http.StatusCreated, seed: func(r *fakerepo.Repo) {
			seedRevisionView(r)
			r.RevisionBase = &datasources.RevisionBase{EntityID: 1, Document: domain.EventDocument(eventDocument)}
		}},
	{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/withdraw"}: {access: signedIn,
		path: "/v1/revisions/51/withdraw", success: http.StatusNoContent, seed: nothingSeeded},
	{Method: http.MethodGet, Path: "/v1/moderation/queue"}: {access: moderator, path: "/v1/moderation/queue",
		success: http.StatusOK, seed: func(r *fakerepo.Repo) { r.RevisionList = revisionList }},
	{Method: http.MethodGet, Path: "/v1/moderation/revisions/{revision_id}"}: {access: moderator,
		path: "/v1/moderation/revisions/51", success: http.StatusOK, seed: seedRevisionView},
	{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/approve"}: {access: moderator,
		path: "/v1/revisions/51/approve", body: `{"acknowledge_stale":false}`, success: http.StatusNoContent,
		seed: nothingSeeded},
	{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/reject"}: {access: moderator,
		path: "/v1/revisions/51/reject", body: `{"note":"Please cite a source."}`, success: http.StatusNoContent,
		seed: nothingSeeded},
}

// caller is who a request is from, and what the middleware does with them.
type caller struct {
	name          string
	authorization string
}

var callers = []caller{
	{name: "anonymous"},
	{name: "contributor", authorization: "Bearer " + contributorID},
	{name: "moderator", authorization: "Bearer " + moderatorID},
	{name: "blocked", authorization: "Bearer " + blockedID},
	{name: badBearerName, authorization: "Bearer " + invalidSecret},
	{name: "unresolvable", authorization: "Bearer " + unresolvableID},
}

// outcome is an error status and code, or the zero outcome for a request that succeeds.
type outcome struct {
	status int
	code   httpjson.ErrorCode
}

// want is what a caller gets from an operation of the given access. Public operations are not authenticated, so
// they answer every caller.
func want(a access, c caller) outcome {
	unauthenticated := outcome{http.StatusUnauthorized, httpjson.CodeUnauthenticated}
	switch {
	case a == public:
		return outcome{}
	case c.name == "anonymous" || c.name == badBearerName:
		return unauthenticated
	case c.name == "unresolvable":
		return outcome{http.StatusInternalServerError, httpjson.CodeInternal}
	case c.name == "blocked":
		return outcome{http.StatusForbidden, httpjson.CodeUserBlocked}
	case a == moderator && c.name != "moderator":
		return outcome{http.StatusForbidden, httpjson.CodeModeratorRequired}
	default:
		return outcome{}
	}
}

// lookupFails is a repository that fails to look up the unresolvable user.
type lookupFails struct{ *fakerepo.Repo }

func (r lookupFails) GetUser(ctx context.Context, userID string) (domain.User, error) {
	if userID == unresolvableID {
		return domain.User{}, errors.New("connection refused")
	}
	return r.Repo.GetUser(ctx, userID)
}

// newHandler is the handler over a fake repository holding every user the callers and the happy paths name,
// which seed prepares. Its validator fails the test if a preflight request reaches it.
func newHandler(t *testing.T, seed func(*fakerepo.Repo)) http.Handler {
	t.Helper()

	user := func(id string, role domain.Role) domain.User {
		return domain.User{ID: id, DisplayName: "Morwen", Role: role, CreatedAt: testNow}
	}
	blocked := user(blockedID, domain.RoleContributor)
	blocked.BlockedAt = new(testNow.Add(-time.Hour))
	repo := &fakerepo.Repo{Users: map[string]domain.User{
		contributorID: user(contributorID, domain.RoleContributor),
		moderatorID:   user(moderatorID, domain.RoleModerator),
		blockedID:     blocked,
		blockTargetID: user(blockTargetID, domain.RoleContributor),
		"auth0|user":  user("auth0|user", domain.RoleContributor),
	}}
	seed(repo)

	validate := func(r *http.Request) (*Identity, error) {
		assert.NotEqual(t, http.MethodOptions, r.Method, "a preflight request was authenticated")
		userID, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch {
		case !ok:
			return nil, nil
		case userID == invalidSecret:
			return nil, fmt.Errorf("%w: test credential", ErrInvalidCredentials)
		default:
			return &Identity{UserID: userID}, nil
		}
	}
	users := lookupFails{repo}
	now := func() time.Time { return testNow }
	return New(Dependencies{
		Repository:        users,
		AuthValidators:    []AuthValidator{validate},
		ResolveUser:       &command.ResolveUser{Users: users, Now: now},
		UpdateDisplayName: &command.UpdateDisplayName{Users: users},
		SetUserBlocked:    &command.SetUserBlocked{Users: users, Now: now},
		Now:               now,
		CreateLinkReport:  &command.CreateLinkReport{Reports: users, Now: now},
		SubmitRevision:    &command.SubmitRevision{Revisions: users, Now: now},
		CreateContactRequest: &command.CreateContactRequest{
			Verifier: turnstilePasses{}, Contacts: users, Now: now,
		},
	})
}

// wildcard matches a path pattern's wildcard segments.
var wildcard = regexp.MustCompile(`\{[^}]*\}`)

func TestRoutes(t *testing.T) {
	t.Parallel()

	contract := openapitest.New(t)

	t.Run("parity", func(t *testing.T) {
		t.Parallel()

		table := routes(Dependencies{})
		lookup := newRouter(table, authMiddleware(nil, nil))

		var routed []openapitest.Operation
		for _, rt := range table {
			op := openapitest.Operation{Method: rt.method, Path: rt.path}
			assert.False(t, slices.Contains(routed, op), "%s %s is routed twice", op.Method, op.Path)
			routed = append(routed, op)

			// A path with every wildcard filled in must reach this route, not another one that also matches it.
			req := httptest.NewRequestWithContext(t.Context(), rt.method, wildcard.ReplaceAllString(rt.path, "x"), nil)
			template, ok := lookup.lookup(req)
			assert.True(t, ok, "%s %s is not routed", op.Method, op.Path)
			assert.Equal(t, rt.path, template, "%s %s is routed to another route", op.Method, op.Path)
		}

		documented := contract.Operations()
		var tabled []openapitest.Operation
		for op, row := range routeTable {
			tabled = append(tabled, op)
			assert.Equal(t, contract.Public(op), row.access == public,
				"%s %s: the row is public exactly when the spec declares no security", op.Method, op.Path)
		}
		assert.ElementsMatch(t, documented, routed, "the routes and the operations in openapi/api.yaml differ")
		assert.ElementsMatch(t, documented, tabled, "the route table and the operations in openapi/api.yaml differ")
	})

	for op, row := range routeTable {
		t.Run(op.Method+" "+op.Path, func(t *testing.T) {
			t.Parallel()

			for _, c := range callers {
				t.Run(c.name, func(t *testing.T) {
					t.Parallel()

					wantOutcome := want(row.access, c)
					wantStatus := wantOutcome.status
					if wantOutcome == (outcome{}) {
						wantStatus = row.success
					}

					req := httptest.NewRequestWithContext(t.Context(), op.Method, row.path, strings.NewReader(row.body))
					if row.body != "" {
						req.Header.Set("Content-Type", "application/json")
					}
					if c.authorization != "" {
						req.Header.Set("Authorization", c.authorization)
					}
					rec := contract.Serve(t, newHandler(t, row.seed), req)

					assert.Equal(t, wantStatus, rec.Code)
					assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
					var body struct {
						Code httpjson.ErrorCode `json:"code"`
					}
					if wantOutcome != (outcome{}) {
						require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
					}
					assert.Equal(t, wantOutcome.code, body.Code)
				})
			}

			t.Run("preflight", func(t *testing.T) {
				t.Parallel()

				req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, row.path, nil)
				req.Header.Set("Access-Control-Request-Method", op.Method)
				req.Header.Set("Authorization", "Bearer "+invalidSecret)
				rec := httptest.NewRecorder()
				newHandler(t, row.seed).ServeHTTP(rec, req)

				assert.Equal(t, http.StatusNoContent, rec.Code)
				assert.Empty(t, rec.Body.String())
			})
		})
	}
}

// unrouted are requests no route answers as a route does: none of them authenticates, so invalid credentials do not
// turn them into 401s.
var unrouted = []struct {
	name          string
	method        string
	target        string
	authorization string
	body          string
	status        int
	allow         string
	location      string
	// bodyContains is a part of the response body.
	bodyContains string
}{
	{name: "unknown_path", method: http.MethodGet, target: "/v1/missing", authorization: "Bearer " + invalidSecret,
		status: http.StatusNotFound, bodyContains: `"code":"not_found"`},
	{name: "wrong_method", method: http.MethodDelete, target: mePath, authorization: "Bearer " + invalidSecret,
		status: http.StatusMethodNotAllowed, allow: "GET, PATCH", bodyContains: `"code":"method_not_allowed"`},
	{name: "wrong_method_on_public_path", method: http.MethodPost, target: "/v1/pieces/" + routeSlug,
		authorization: "Bearer " + invalidSecret, status: http.StatusMethodNotAllowed, allow: http.MethodGet,
		bodyContains: `"code":"method_not_allowed"`},
	{name: "path_cleaning_redirect", method: http.MethodGet, target: "/v1//me", status: http.StatusMovedPermanently,
		location: mePath},
	{name: "encoded_slash_slug", method: http.MethodGet, target: "/v1/pieces/%2F", status: http.StatusNotFound,
		bodyContains: `"code":"not_found"`},
	{name: "encoded_bar_user_id", method: http.MethodPut, target: "/v1/users/auth0%7Cuser/blocked",
		authorization: "Bearer " + moderatorID, body: `{"blocked":true}`, status: http.StatusOK,
		bodyContains: `"id":"auth0|user"`},
	{name: "public_read_ignores_invalid_credentials", method: http.MethodGet, target: healthzPath,
		authorization: "Bearer " + invalidSecret, status: http.StatusOK},
	{name: "preflight_to_unknown_path", method: http.MethodOptions, target: "/v1/missing",
		status: http.StatusNoContent},
}

func TestRoutes_Unrouted(t *testing.T) {
	t.Parallel()

	for _, tc := range unrouted {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.target, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			if tc.method == http.MethodOptions {
				req.Header.Set("Access-Control-Request-Method", http.MethodGet)
			}
			rec := httptest.NewRecorder()
			newHandler(t, nothingSeeded).ServeHTTP(rec, req)

			assert.Equal(t, tc.status, rec.Code)
			assert.Equal(t, tc.allow, rec.Header().Get("Allow"))
			assert.Equal(t, tc.location, rec.Header().Get("Location"))
			assert.Contains(t, rec.Body.String(), tc.bodyContains)
		})
	}
}
