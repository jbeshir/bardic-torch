package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
)

// commandFunc adapts a function to a command.
type commandFunc[Req, Res any] func(ctx context.Context, req Req) (Res, error)

func (f commandFunc[Req, Res]) Execute(ctx context.Context, req Req) (Res, error) { return f(ctx, req) }

// returning is a command that returns res and err. It fails the test if the command is called when it must not be
// (wantCalled is false), and, when the test ends, if it was never called when it must be.
func returning[Req, Res any](t *testing.T, wantCalled bool, res Res, err error) commandFunc[Req, Res] {
	var calls atomic.Int64
	t.Cleanup(func() {
		assert.Equal(t, wantCalled, calls.Load() > 0, "command called: want %t", wantCalled)
	})

	return func(context.Context, Req) (Res, error) {
		calls.Add(1)
		if !wantCalled {
			t.Error("command called unexpectedly")
		}

		return res, err
	}
}

// revisionCase is one request to a revision controller and its answer.
type revisionCase struct {
	name       string
	method     string
	target     string
	pathValues map[string]string
	body       string
	// contentType replaces application/json for a body.
	contentType string
	anonymous   bool
	// invalidRequest marks requests that break the request schema, so only the response is validated.
	invalidRequest bool
	wantStatus     int
	// wantBody is compared as JSON if set; wantContains are substrings of the body otherwise.
	wantBody     string
	wantContains []string
	// err is what the command fails with; notCalled marks requests refused before the command runs.
	err       error
	notCalled bool
}

func serveRevisionCase(t *testing.T, handler http.Handler, tc revisionCase) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.target, strings.NewReader(tc.body))
	if tc.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if tc.contentType != "" {
		req.Header.Set("Content-Type", tc.contentType)
	}
	for name, value := range tc.pathValues {
		req.SetPathValue(name, value)
	}
	if !tc.anonymous {
		req = asPrincipal(req)
	}

	validator := openapitest.New(t)
	var rec *httptest.ResponseRecorder
	if tc.invalidRequest {
		rec = validator.ServeResponseOnly(t, handler, req)
	} else {
		rec = validator.Serve(t, handler, req)
	}

	assert.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
	if tc.wantBody != "" {
		assert.JSONEq(t, tc.wantBody, rec.Body.String())
	}
	for _, s := range tc.wantContains {
		assert.Contains(t, rec.Body.String(), s)
	}
}

var (
	testRevisionEntity = domain.RevisionEntity{Type: domain.EntityTypeEvent, ID: 7, Slug: testEventSlug}
	testEventDocument  = domain.EventDocument{
		SchemaVersion: domain.DocumentSchemaVersion, Name: testEventName, Kind: domain.EventKindEvent,
	}
	testEventSnapshot = domain.EventSnapshot{Name: testEventName, Kind: domain.EventKindEvent}
	testDescribed     = domain.EventSnapshot{Name: testEventName, Kind: domain.EventKindEvent,
		DescriptionMD: new("Held every year at the turning of the year.")}
	testMyRevision = domain.MyRevision{
		MyRevisionSummary: domain.MyRevisionSummary{
			ID: 51, Status: domain.RevisionStatusPending, EntityType: domain.EntityTypeEvent,
			Entity: &testRevisionEntity, Title: testEventName, Summary: "Describe the revel.",
			Source: domain.RevisionSourceUser, Author: domain.AuthorRef{DisplayName: testDisplayName,
				Role: domain.RoleContributor},
			CreatedAt: testNow, SubmittedAt: &testNow, BaseRevisionID: new(int64(46)),
			SubmittedBaseRevisionID: new(int64(46)),
		},
		Snapshot: testDescribed, BaseSnapshot: testEventSnapshot,
		PendingReferences: []domain.EditorReference{
			{EntityType: domain.EntityTypeTerm, ID: 3, Slug: "pending-term-3", Label: "Revels", IsPending: true},
		},
	}
	testDraft = domain.Draft{
		ID: 51, EntityType: domain.EntityTypeEvent, Entity: &testRevisionEntity, BaseRevisionID: new(int64(46)),
		Document: testEventDocument, Summary: "Describe the revel.", CreatedAt: testNow, UpdatedAt: testNow,
		Preview: testEventSnapshot,
	}
	errCommand = errors.New("connection refused")
)

const (
	testMyRevisionBody = `{"id":51,"status":"pending","entity_type":"event",` +
		`"entity":{"slug":"midwinter-revel","collection_id":null,"is_new":false},"title":"Midwinter Revel",` +
		`"summary":"Describe the revel.","source":"user","author":{"display_name":"Morwen","role":"contributor"},` +
		`"created_at":"2026-10-31T12:00:00Z","submitted_at":"2026-10-31T12:00:00Z","reviewed_at":null,` +
		`"review_note":null,"base_revision_id":46,"submitted_base_revision_id":46,` +
		`"snapshot":{"name":"Midwinter Revel","kind":"event",` +
		`"description_md":"Held every year at the turning of the year."},` +
		`"base_snapshot":{"name":"Midwinter Revel","kind":"event","description_md":null},` +
		`"changed_fields":["description_md"],"pending_references":[{"entity_type":"term",` +
		`"slug":"pending-term-3","collection_id":null,"label":"Revels","is_pending":true}]}`
	testDraftBody = `{"id":51,"entity_type":"event",` +
		`"entity":{"slug":"midwinter-revel","collection_id":null,"is_new":false},"base_revision_id":46,` +
		`"document":{"name":"Midwinter Revel","kind":"event","description_md":null},` +
		`"summary":"Describe the revel.","created_at":"2026-10-31T12:00:00Z","updated_at":"2026-10-31T12:00:00Z",` +
		`"duplicates":[],"references":[],` +
		`"preview":{"name":"Midwinter Revel","kind":"event","description_md":null}}`
	testEditorEventJSON = `{"name":"Midwinter Revel","kind":"event","description_md":null}`
	createDraftBody     = `{"entity_type":"event","entity":{"slug":"midwinter-revel","collection_id":null},` +
		`"base_revision_id":46,"document":` + testEditorEventJSON + `,"summary":"Describe the revel."}`
	updateDraftBody = `{"entity_type":"event","document":` + testEditorEventJSON + `,"summary":"Describe the revel."}`
	submitBody      = `{"draft_id":51,"acknowledge_duplicates":false,"acknowledged_flags":[]}`
	revertBody      = `{"summary":null,"acknowledged_flags":[]}`

	draftIDVar       = "draft_id"
	draftNotFound    = `{"code":"not_found","message":"draft not found"}`
	revisionNotFound = `{"code":"not_found","message":"revision not found"}`
	notDraftBody     = `{"code":"not_draft","message":"the revision is not a draft"}`
	unavailableBody  = `{"code":"unavailable","message":"the database is busy or cannot be reached; try again"}`
	invalidIDBody    = `{"code":"invalid_request","message":"invalid %s: must be an integer from 1 to 9007199254740991"}`
)

// invalidID is the 400 body of an invalid path ID name.
func invalidID(name string) string { return strings.Replace(invalidIDBody, "%s", name, 1) }

// runRevisionCases serves every case with the handler that handler builds for it.
func runRevisionCases(t *testing.T, cases []revisionCase, handler func(t *testing.T, tc revisionCase) http.Handler) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveRevisionCase(t, handler(t, tc), tc)
		})
	}
}

// errorCases are base with the command failing with each error, answered with status and body.
func errorCases(base revisionCase, errs ...revisionCase) []revisionCase {
	cases := make([]revisionCase, 0, len(errs))
	for _, e := range errs {
		tc := base
		tc.name, tc.err, tc.wantStatus, tc.wantBody, tc.wantContains = e.name, e.err, e.wantStatus, e.wantBody,
			e.wantContains
		cases = append(cases, tc)
	}
	return cases
}

// The answers to the revision errors every write may give.
var (
	internalCase        = revisionCase{name: "internal", err: errCommand, wantStatus: http.StatusInternalServerError, wantBody: internalErrorBody}
	unavailableCase     = revisionCase{name: "unavailable", err: datasources.ErrUnavailable, wantStatus: http.StatusServiceUnavailable, wantBody: unavailableBody}
	notDraftCase        = revisionCase{name: "not_draft", err: domain.ErrNotDraft, wantStatus: http.StatusConflict, wantBody: notDraftBody}
	repeatedFlagCase    = "repeated_flag"
	repeatedFlagMessage = "invalid acknowledged_flags: must not repeat a flag"
	duplicateNameCase   = revisionCase{name: "duplicate_name", err: command.ErrDuplicateName, wantStatus: http.StatusConflict,
		wantBody: `{"code":"duplicate_name","message":"another term of the facet has this name"}`}
	pendingLimit = revisionCase{name: "pending_limit_reached", err: datasources.ErrPendingLimitReached, wantStatus: http.StatusConflict,
		wantBody: `{"code":"pending_limit_reached","message":"a user may have at most 30 pending revisions"}`}
	entityGone = revisionCase{name: "entity_gone", err: datasources.ErrEntityGone, wantStatus: http.StatusConflict,
		wantBody: `{"code":"entity_gone","message":"the entity, or an entity it references, no longer exists"}`}
	hiddenDataConflict = revisionCase{name: "hidden_data_conflict", err: domain.ErrHiddenDataConflict,
		wantStatus: http.StatusConflict, wantBody: `{"code":"hidden_data_conflict","message":"the revision conflicts with hidden data"}`}
	unacknowledgedFlags = revisionCase{name: "unacknowledged_flags",
		err:        &domain.UnacknowledgedFlagsError{Flags: []domain.Flag{{Code: domain.FlagHiddenAliasInText, Path: "/notes_md"}, {Code: domain.FlagPossibleDuplicate}}},
		wantStatus: http.StatusConflict,
		wantBody: `{"code":"unacknowledged_flags","message":"the revision has flags that are not acknowledged",` +
			`"flags":[{"code":"hidden_alias_in_text","path":"/notes_md"},{"code":"possible_duplicate","path":null}]}`}
	pendingReferenceFlagged = revisionCase{name: "pending_reference_flagged",
		err:        &domain.PendingReferenceFlaggedError{RevisionID: 9, Flags: []domain.Flag{{Code: domain.FlagPossibleDuplicate}}},
		wantStatus: http.StatusConflict,
		wantBody: `{"code":"pending_reference_flagged","message":"the referenced pending revision 9 has flags: ` +
			`approve it on its own first","flags":[{"code":"possible_duplicate","path":null}]}`}
	invalidDocument = revisionCase{name: "invalid_document",
		err:        &domain.DocumentError{Problems: []domain.DocumentProblem{{Path: "/name", Code: domain.ProblemBlank}}},
		wantStatus: http.StatusUnprocessableEntity,
		wantBody: `{"code":"invalid_document","message":"the document is invalid",` +
			`"problems":[{"path":"/name","problem":"blank"}]}`}
)

func TestEditableGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	editable := domain.Editable{
		Entity: testRevisionEntity, BaseRevisionID: new(int64(46)), StartingPoint: domain.StartingPointPending,
		Document:   domain.EventDocument{SchemaVersion: 1, Name: testEventName, Kind: domain.EventKindEvent},
		References: []domain.EditorReference{},
		LinkStates: []domain.LinkState{{URL: "https://example.org/revel", LinkID: new(int64(7)),
			IsHidden: true, Liveness: new(domain.LivenessOK)}},
		Draft:   &domain.DraftRef{ID: 52, UpdatedAt: testNow},
		Pending: &domain.PendingRef{ID: 51, SubmittedAt: testNow},
	}
	const editableBody = `{"entity_type":"event",` +
		`"entity":{"slug":"midwinter-revel","collection_id":null,"is_new":false},"base_revision_id":46,` +
		`"starting_point":"pending","document":` + testEditorEventJSON + `,"references":[],` +
		`"link_states":[{"url":"https://example.org/revel","link_id":7,"is_hidden":true,"liveness":"ok"}],` +
		`"draft":{"id":52,"updated_at":"2026-10-31T12:00:00Z"},"pending":{"id":51,"submitted_at":"2026-10-31T12:00:00Z"}}`
	base := revisionCase{method: http.MethodGet, target: "/v1/events/midwinter-revel/editable",
		pathValues: map[string]string{slugVar: testEventSlug}}
	ok := base
	ok.name, ok.wantStatus, ok.wantBody = "ok", http.StatusOK, editableBody
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badSlug := base
	badSlug.name, badSlug.pathValues, badSlug.target, badSlug.invalidRequest = "invalid_slug",
		map[string]string{slugVar: "Bad Slug"}, "/v1/events/Bad%20Slug/editable", true
	badSlug.notCalled, badSlug.wantStatus, badSlug.wantBody = true, http.StatusBadRequest, invalidSlugBody
	cases := append([]revisionCase{ok, anonymous, badSlug}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
			wantBody: `{"code":"not_found","message":"event not found"}`},
		internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return EditableGet{EntityType: domain.EntityTypeEvent,
			Command: returning[command.EntityRequest](t, !tc.notCalled, editable, tc.err)}
	})

	// Every type: a collection by its ID, the others by slug.
	collection := domain.Editable{Entity: domain.RevisionEntity{Type: domain.EntityTypeCollection, ID: 12},
		StartingPoint: domain.StartingPointCurrent, BaseRevisionID: new(int64(3)),
		Document: domain.CollectionDocument{SchemaVersion: 1, Name: "The Northmarch Songbook",
			Kind: domain.CollectionKindSongbookPDF, Enumerable: domain.CollectionEnumerableUnknown}}
	runRevisionCases(t, []revisionCase{
		{name: "collection_by_id", method: http.MethodGet, target: "/v1/collections/12/editable",
			pathValues: map[string]string{collectionIDVar: "12"}, wantStatus: http.StatusOK,
			wantContains: []string{`"entity":{"slug":null,"collection_id":12,"is_new":false}`}},
		{name: "collection_invalid_id", method: http.MethodGet, target: "/v1/collections/0/editable",
			pathValues: map[string]string{collectionIDVar: "0"}, invalidRequest: true, notCalled: true,
			wantStatus: http.StatusBadRequest, wantBody: invalidID("collection_id")},
	}, func(t *testing.T, tc revisionCase) http.Handler {
		return EditableGet{EntityType: domain.EntityTypeCollection, Command: commandFunc[command.EntityRequest, domain.Editable](
			func(_ context.Context, req command.EntityRequest) (domain.Editable, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				assert.Equal(t, domain.EntityKey{Type: domain.EntityTypeCollection, CollectionID: 12}, req.Key)
				return collection, nil
			})}
	})
}

func TestPendingGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodGet, target: "/v1/events/pending-event-7/pending",
		pathValues: map[string]string{slugVar: "pending-event-7"}}
	ok := base
	ok.name, ok.wantStatus, ok.wantBody = "ok", http.StatusOK, testMyRevisionBody
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badSlug := base
	badSlug.name, badSlug.pathValues, badSlug.target, badSlug.invalidRequest = "invalid_slug",
		map[string]string{slugVar: "-"}, "/v1/events/-/pending", true
	badSlug.notCalled, badSlug.wantStatus, badSlug.wantBody = true, http.StatusBadRequest, invalidSlugBody
	cases := append([]revisionCase{ok, anonymous, badSlug}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
			wantBody: `{"code":"not_found","message":"pending revision not found"}`},
		internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return PendingGet{EntityType: domain.EntityTypeEvent,
			Command: returning[command.EntityRequest](t, !tc.notCalled, testMyRevision, tc.err)}
	})
}

func TestDraftCreate_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodPost, target: draftsTarget, body: createDraftBody}
	created := base
	created.name, created.wantStatus, created.wantBody = createdCase, http.StatusCreated, testDraftBody
	cases := []revisionCase{created,
		{name: anonymousCase, method: http.MethodPost, target: draftsTarget, body: createDraftBody, anonymous: true,
			notCalled: true, wantStatus: http.StatusUnauthorized, wantBody: unauthenticatedBody},
		{name: "unknown_document_field", method: http.MethodPost, target: draftsTarget, invalidRequest: true,
			body: `{"entity_type":"event","entity":null,"base_revision_id":null,` +
				`"document":{"name":"Midwinter Revel","kind":"event","description_md":null,"title":"x"},"summary":""}`,
			notCalled: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"field document has an unknown field"}`},
		{name: "document_of_another_type", method: http.MethodPost, target: draftsTarget, invalidRequest: true,
			body: `{"entity_type":"term","entity":null,"base_revision_id":null,"document":` + testEditorEventJSON +
				`,"summary":""}`,
			notCalled: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"field document has an unknown field"}`},
		{name: "unknown_entity_type", method: http.MethodPost, target: draftsTarget, invalidRequest: true,
			body:      `{"entity_type":"song","entity":null,"base_revision_id":null,"document":{},"summary":""}`,
			notCalled: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid entity_type: not one of its allowed values"}`},
		{name: "collection_by_slug", method: http.MethodPost, target: draftsTarget, invalidRequest: true,
			body: `{"entity_type":"collection","entity":{"slug":"songbook","collection_id":null},` +
				`"base_revision_id":null,"document":{"name":"Songbook","url":null,"kind":"other",` +
				`"description_md":null,"region":null,"parent_id":null,"is_bardic_resource":false,` +
				`"enumerable":"unknown"},"summary":""}`,
			notCalled: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid entity: a collection is addressed by a ` +
				`positive collection_id, any other entity by a slug"}`},
		{name: "missing_document", method: http.MethodPost, target: draftsTarget, invalidRequest: true,
			body: `{"entity_type":"event","entity":null,"base_revision_id":null,"summary":""}`, notCalled: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"invalid_request","message":"field document must be a JSON object"}`},
		{name: tooLargeCase, method: http.MethodPost, target: draftsTarget, invalidRequest: true, notCalled: true,
			body:       `{"summary":"` + strings.Repeat("a", httpjson.MaxDocumentBodyBytes) + `"}`,
			wantStatus: http.StatusRequestEntityTooLarge, wantBody: tooLargeBody},
		{name: notJSONCase, method: http.MethodPost, target: draftsTarget, body: "x", contentType: plainText,
			invalidRequest: true, notCalled: true, wantStatus: http.StatusUnsupportedMediaType,
			wantBody: unsupportedMediaTypeBody},
	}
	cases = append(cases, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
			wantBody: `{"code":"not_found","message":"entity not found"}`},
		revisionCase{name: "draft_limit_reached", err: datasources.ErrDraftLimitReached,
			wantStatus: http.StatusConflict,
			wantBody:   `{"code":"draft_limit_reached","message":"a user may hold at most 50 drafts"}`},
		invalidDocument, unavailableCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return DraftCreate{Command: returning[command.SaveDraftRequest](t, !tc.notCalled,
			command.SavedDraft{DraftResult: command.DraftResult{Draft: testDraft}, Created: true}, tc.err)}
	})

	// A request replacing the caller's draft of the entity is 200, and the request reaches the command converted.
	runRevisionCases(t, []revisionCase{{name: "replaced", method: http.MethodPost, target: draftsTarget,
		body: createDraftBody, wantStatus: http.StatusOK, wantBody: testDraftBody}},
		func(t *testing.T, _ revisionCase) http.Handler {
			return DraftCreate{Command: commandFunc[command.SaveDraftRequest, command.SavedDraft](
				func(_ context.Context, req command.SaveDraftRequest) (command.SavedDraft, error) {
					assert.Equal(t, principalUserID, req.Principal.User.ID)
					assert.Equal(t, &domain.EntityKey{Type: domain.EntityTypeEvent, Slug: testEventSlug}, req.Entity)
					assert.Equal(t, new(int64(46)), req.BaseRevisionID)
					assert.Equal(t, domain.EditorEventDocument{Name: testEventName, Kind: domain.EventKindEvent},
						req.Document)
					assert.Equal(t, "Describe the revel.", req.Summary)
					return command.SavedDraft{DraftResult: command.DraftResult{Draft: testDraft}}, nil
				})}
		})
}

func TestDraftGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodGet, target: draftTarget, pathValues: map[string]string{draftIDVar: "51"}}
	ok := base
	ok.name, ok.wantStatus, ok.wantBody = "ok", http.StatusOK, testDraftBody
	cases := append([]revisionCase{ok,
		{name: anonymousCase, method: http.MethodGet, target: draftTarget,
			pathValues: map[string]string{draftIDVar: "51"}, anonymous: true, notCalled: true,
			wantStatus: http.StatusUnauthorized, wantBody: unauthenticatedBody},
		{name: badIDCase, method: http.MethodGet, target: "/v1/drafts/x", pathValues: map[string]string{draftIDVar: "x"},
			invalidRequest: true, notCalled: true, wantStatus: http.StatusBadRequest, wantBody: invalidID(draftIDVar)},
	}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound, wantBody: draftNotFound},
		notDraftCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return DraftGet{Command: returning[command.RevisionRequest](t, !tc.notCalled,
			command.DraftResult{Draft: testDraft}, tc.err)}
	})

	// A draft of a new entity lists the entities it may duplicate.
	newDraft := testDraft
	newDraft.Entity, newDraft.BaseRevisionID, newDraft.Preview = nil, nil, nil
	runRevisionCases(t, []revisionCase{{name: "new_entity", method: http.MethodGet, target: draftTarget,
		pathValues: map[string]string{draftIDVar: "51"}, wantStatus: http.StatusOK, wantContains: []string{
			`"entity":null`, `"preview":null`,
			`"duplicates":[{"entity_type":"event","slug":"midwinter-revel","collection_id":null,` +
				`"name":"Midwinter Revel","matched":"name"}]`,
		}}}, func(t *testing.T, _ revisionCase) http.Handler {
		return DraftGet{Command: returning[command.RevisionRequest](t, true, command.DraftResult{Draft: newDraft,
			Duplicates: []domain.DuplicateMatch{{EntityType: domain.EntityTypeEvent, Slug: testEventSlug,
				Name: testEventName, Matched: domain.DuplicateFieldName}}}, nil)}
	})
}

func TestDraftUpdate_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodPut, target: draftTarget, pathValues: map[string]string{draftIDVar: "51"},
		body: updateDraftBody}
	ok := base
	ok.name, ok.wantStatus, ok.wantBody = "ok", http.StatusOK, testDraftBody
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badID := base
	badID.name, badID.pathValues, badID.target, badID.invalidRequest, badID.notCalled = badIDCase,
		map[string]string{draftIDVar: "0"}, "/v1/drafts/0", true, true
	badID.wantStatus, badID.wantBody = http.StatusBadRequest, invalidID(draftIDVar)
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"summary":"` + strings.Repeat("a", httpjson.MaxDocumentBodyBytes) + `"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	notJSON := base
	notJSON.name, notJSON.contentType, notJSON.invalidRequest, notJSON.notCalled = notJSONCase, plainText, true, true
	notJSON.wantStatus, notJSON.wantBody = http.StatusUnsupportedMediaType, unsupportedMediaTypeBody
	cases := append([]revisionCase{ok, anonymous, badID, tooLarge, notJSON}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound, wantBody: draftNotFound},
		revisionCase{name: "another_entity_type", err: datasources.ErrDocumentType, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid entity_type: not the entity type of the draft or revision"}`},
		notDraftCase, invalidDocument, unavailableCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return DraftUpdate{Command: returning[command.UpdateDraftRequest](t, !tc.notCalled,
			command.DraftResult{Draft: testDraft}, tc.err)}
	})
}

func TestDraftDelete_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodDelete, target: draftTarget, pathValues: map[string]string{draftIDVar: "51"}}
	ok := base
	ok.name, ok.wantStatus = "ok", http.StatusNoContent
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badID := base
	badID.name, badID.pathValues, badID.target, badID.invalidRequest, badID.notCalled = badIDCase,
		map[string]string{draftIDVar: "-1"}, "/v1/drafts/-1", true, true
	badID.wantStatus, badID.wantBody = http.StatusBadRequest, invalidID(draftIDVar)
	cases := append([]revisionCase{ok, anonymous, badID}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound, wantBody: draftNotFound},
		notDraftCase, unavailableCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return DraftDelete{Command: returning[command.RevisionRequest](t, !tc.notCalled, command.Empty{}, tc.err)}
	})
}

func TestRevisionSubmit_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodPost, target: revisionsTarget, body: submitBody}
	created := base
	created.name, created.wantStatus, created.wantBody = createdCase, http.StatusCreated, testMyRevisionBody
	cases := []revisionCase{created,
		{name: anonymousCase, method: http.MethodPost, target: revisionsTarget, body: submitBody, anonymous: true,
			notCalled: true, wantStatus: http.StatusUnauthorized, wantBody: unauthenticatedBody},
		{name: "invalid_draft_id", method: http.MethodPost, target: revisionsTarget, invalidRequest: true,
			body: `{"draft_id":0,"acknowledge_duplicates":false,"acknowledged_flags":[]}`, notCalled: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"invalid_request","message":"invalid draft_id: must be a positive integer"}`},
		{name: "unknown_flag", method: http.MethodPost, target: revisionsTarget, invalidRequest: true,
			body: `{"draft_id":51,"acknowledge_duplicates":false,"acknowledged_flags":["suspicious"]}`, notCalled: true,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"invalid_request","message":"invalid acknowledged_flags: not one of its allowed values"}`},
		{name: repeatedFlagCase, method: http.MethodPost, target: revisionsTarget, notCalled: true,
			body:           `{"draft_id":51,"acknowledge_duplicates":true,"acknowledged_flags":["possible_duplicate","possible_duplicate"]}`,
			invalidRequest: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"` + repeatedFlagMessage + `"}`},
		{name: tooLargeCase, method: http.MethodPost, target: revisionsTarget, invalidRequest: true, notCalled: true,
			body:       `{"draft_id":51,"x":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `"}`,
			wantStatus: http.StatusRequestEntityTooLarge, wantBody: tooLargeBody},
		{name: notJSONCase, method: http.MethodPost, target: revisionsTarget, body: "x", contentType: plainText,
			invalidRequest: true, notCalled: true, wantStatus: http.StatusUnsupportedMediaType,
			wantBody: unsupportedMediaTypeBody},
	}
	cases = append(cases, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound, wantBody: draftNotFound},
		notDraftCase, pendingLimit, entityGone, hiddenDataConflict, unacknowledgedFlags, pendingReferenceFlagged,
		revisionCase{name: "possible_duplicate", err: &command.PossibleDuplicateError{Duplicates: []domain.DuplicateMatch{
			{EntityType: domain.EntityTypeCollection, CollectionID: 12, Name: "The Northmarch Songbook",
				Matched: domain.DuplicateFieldURL}}}, wantStatus: http.StatusConflict,
			wantBody: `{"code":"possible_duplicate","message":"the new entity may duplicate an existing one: ` +
				`acknowledge_duplicates confirms it","duplicates":[{"entity_type":"collection","slug":null,` +
				`"collection_id":12,"name":"The Northmarch Songbook","matched":"url"}]}`},
		revisionCase{name: "rebase_required", err: domain.ErrRebaseRequired, wantStatus: http.StatusConflict,
			wantBody: `{"code":"rebase_required","message":"the entity changed since the base, and the changes ` +
				`conflict: edit the current version"}`},
		revisionCase{name: "duplicate_blocked", err: domain.ErrDuplicateBlocked, wantStatus: http.StatusUnprocessableEntity,
			wantBody: `{"code":"duplicate_blocked","message":"a person with this name has asked not to have a page"}`},
		duplicateNameCase, invalidDocument, unavailableCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return RevisionSubmit{Command: returning[command.SubmitRevisionRequest](t, !tc.notCalled,
			command.SubmittedRevision{Revision: testMyRevision}, tc.err)}
	})

	// A replay is 200, and the request reaches the command as sent.
	runRevisionCases(t, []revisionCase{{name: "replayed", method: http.MethodPost, target: revisionsTarget,
		body:       `{"draft_id":51,"acknowledge_duplicates":true,"acknowledged_flags":["possible_duplicate"]}`,
		wantStatus: http.StatusOK, wantBody: testMyRevisionBody}}, func(t *testing.T, _ revisionCase) http.Handler {
		return RevisionSubmit{Command: commandFunc[command.SubmitRevisionRequest, command.SubmittedRevision](
			func(_ context.Context, req command.SubmitRevisionRequest) (command.SubmittedRevision, error) {
				assert.Equal(t, int64(51), req.DraftID)
				assert.True(t, req.AcknowledgeDuplicates)
				assert.Equal(t, []domain.FlagCode{domain.FlagPossibleDuplicate}, req.AcknowledgedFlags)
				return command.SubmittedRevision{Revision: testMyRevision, Replayed: true}, nil
			})}
	})
}

func TestRevisionWithdraw_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodPost, target: "/v1/revisions/51/withdraw",
		pathValues: map[string]string{revisionIDVar: "51"}}
	ok := base
	ok.name, ok.wantStatus, ok.wantBody = "ok", http.StatusOK, testMyRevisionBody
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badID := base
	badID.name, badID.pathValues, badID.target, badID.invalidRequest, badID.notCalled = badIDCase,
		map[string]string{revisionIDVar: "x"}, "/v1/revisions/x/withdraw", true, true
	badID.wantStatus, badID.wantBody = http.StatusBadRequest, invalidID(revisionIDVar)
	cases := append([]revisionCase{ok, anonymous, badID}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound, wantBody: revisionNotFound},
		revisionCase{name: "not_pending", err: domain.ErrNotPending, wantStatus: http.StatusConflict,
			wantBody: `{"code":"not_pending","message":"the revision is not pending"}`},
		unavailableCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return RevisionWithdraw{Command: returning[command.RevisionRequest](t, !tc.notCalled, testMyRevision, tc.err)}
	})
}

func TestRevisionRevert_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodPost, target: "/v1/revisions/46/revert",
		pathValues: map[string]string{revisionIDVar: "46"}, body: revertBody}
	created := base
	created.name, created.wantStatus, created.wantBody = createdCase, http.StatusCreated, testMyRevisionBody
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badID := base
	badID.name, badID.pathValues, badID.target, badID.invalidRequest, badID.notCalled = badIDCase,
		map[string]string{revisionIDVar: "0"}, "/v1/revisions/0/revert", true, true
	badID.wantStatus, badID.wantBody = http.StatusBadRequest, invalidID(revisionIDVar)
	badFlag := base
	badFlag.name, badFlag.body, badFlag.invalidRequest, badFlag.notCalled = "unknown_flag",
		`{"summary":null,"acknowledged_flags":["x"]}`, true, true
	badFlag.wantStatus = http.StatusBadRequest
	badFlag.wantBody = `{"code":"invalid_request","message":"invalid acknowledged_flags: not one of its allowed values"}`
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"summary":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	notJSON := base
	notJSON.name, notJSON.contentType, notJSON.invalidRequest, notJSON.notCalled = notJSONCase, plainText, true, true
	notJSON.wantStatus, notJSON.wantBody = http.StatusUnsupportedMediaType, unsupportedMediaTypeBody
	repeatedFlag := badFlag
	repeatedFlag.name, repeatedFlag.body = repeatedFlagCase, `{"summary":null,"acknowledged_flags":["possible_duplicate","possible_duplicate"]}`
	repeatedFlag.wantBody = `{"code":"invalid_request","message":"` + repeatedFlagMessage + `"}`
	cases := append([]revisionCase{created, anonymous, badID, badFlag, repeatedFlag, tooLarge, notJSON}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound, wantBody: revisionNotFound},
		revisionCase{name: "already_current", err: datasources.ErrAlreadyCurrent, wantStatus: http.StatusConflict,
			wantBody: `{"code":"already_current","message":"the revision is already the entity's current revision"}`},
		pendingLimit, entityGone, hiddenDataConflict, unacknowledgedFlags, pendingReferenceFlagged,
		duplicateNameCase, invalidDocument, unavailableCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return RevisionRevert{Command: returning[command.RevertRequest](t, !tc.notCalled,
			command.SubmittedRevision{Revision: testMyRevision}, tc.err)}
	})

	runRevisionCases(t, []revisionCase{{name: "replayed", method: http.MethodPost, target: "/v1/revisions/46/revert",
		pathValues: map[string]string{revisionIDVar: "46"}, body: `{"summary":"Restore it.","acknowledged_flags":[]}`,
		wantStatus: http.StatusOK, wantBody: testMyRevisionBody}}, func(t *testing.T, _ revisionCase) http.Handler {
		return RevisionRevert{Command: commandFunc[command.RevertRequest, command.SubmittedRevision](
			func(_ context.Context, req command.RevertRequest) (command.SubmittedRevision, error) {
				assert.Equal(t, int64(46), req.RevisionID)
				assert.Equal(t, "Restore it.", req.Summary)
				return command.SubmittedRevision{Revision: testMyRevision, Replayed: true}, nil
			})}
	})
}

func TestMyRevisionsList_ServeHTTP(t *testing.T) {
	t.Parallel()

	list := domain.MyRevisionList{Revisions: []domain.MyRevisionSummary{testMyRevision.MyRevisionSummary}, Total: 1}
	cases := []revisionCase{
		{name: "ok", method: http.MethodGet, target: "/v1/me/revisions?status=pending&entity_type=event&limit=5",
			wantStatus:   http.StatusOK,
			wantContains: []string{`"total":1`, `"data":[{"id":51,"status":"pending"`}},
		{name: anonymousCase, method: http.MethodGet, target: "/v1/me/revisions", anonymous: true, notCalled: true,
			wantStatus: http.StatusUnauthorized, wantBody: unauthenticatedBody},
		{name: "unknown_status", method: http.MethodGet, target: "/v1/me/revisions?status=deleted", invalidRequest: true,
			notCalled: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid status: not one of its allowed values"}`},
		{name: "unknown_entity_type", method: http.MethodGet, target: "/v1/me/revisions?entity_type=song",
			invalidRequest: true, notCalled: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid entity_type: not one of its allowed values"}`},
		{name: "repeated_status", method: http.MethodGet, target: "/v1/me/revisions?status=draft&status=pending",
			invalidRequest: true, notCalled: true, wantStatus: http.StatusBadRequest,
			wantContains: []string{`"code":"invalid_request"`}},
		{name: "limit_out_of_range", method: http.MethodGet, target: "/v1/me/revisions?limit=101", invalidRequest: true,
			notCalled: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid limit: must be an integer from 1 to 100"}`},
		{name: "internal", method: http.MethodGet, target: "/v1/me/revisions", err: errCommand,
			wantStatus: http.StatusInternalServerError, wantBody: internalErrorBody},
	}
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return MyRevisionsList{Command: commandFunc[command.ListMyRevisionsRequest, domain.MyRevisionList](
			func(_ context.Context, req command.ListMyRevisionsRequest) (domain.MyRevisionList, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				if tc.name == "ok" {
					assert.Equal(t, datasources.MyRevisionQuery{Status: new(domain.RevisionStatusPending),
						EntityType: new(domain.EntityTypeEvent), Page: domain.Page{Limit: 5}}, req.Query)
				}
				return list, tc.err
			})}
	})
}

func TestMyRevisionGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodGet, target: "/v1/me/revisions/51", pathValues: map[string]string{revisionIDVar: "51"}}
	ok := base
	ok.name, ok.wantStatus, ok.wantBody = "ok", http.StatusOK, testMyRevisionBody
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badID := base
	badID.name, badID.pathValues, badID.target, badID.invalidRequest, badID.notCalled = badIDCase,
		map[string]string{revisionIDVar: "1x"}, "/v1/me/revisions/1x", true, true
	badID.wantStatus, badID.wantBody = http.StatusBadRequest, invalidID(revisionIDVar)
	cases := append([]revisionCase{ok, anonymous, badID}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound, wantBody: revisionNotFound},
		internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return MyRevisionGet{Command: returning[command.RevisionRequest](t, !tc.notCalled, testMyRevision, tc.err)}
	})

	// A new entity's revision has no base snapshot; every field it has that is not empty changed.
	newRevision := testMyRevision
	newRevision.BaseSnapshot, newRevision.BaseRevisionID, newRevision.SubmittedBaseRevisionID = nil, nil, nil
	runRevisionCases(t, []revisionCase{{name: "new_entity", method: http.MethodGet, target: "/v1/me/revisions/51",
		pathValues: map[string]string{revisionIDVar: "51"}, wantStatus: http.StatusOK,
		wantContains: []string{`"base_snapshot":null`, `"changed_fields":["description_md","kind","name"]`}}},
		func(t *testing.T, _ revisionCase) http.Handler {
			return MyRevisionGet{Command: returning[command.RevisionRequest](t, true, newRevision, nil)}
		})
}

const (
	draftsTarget    = "/v1/drafts"
	draftTarget     = "/v1/drafts/51"
	revisionsTarget = "/v1/revisions"
	plainText       = "text/plain"
	badIDCase       = "invalid_id"
	createdCase     = "created"
)
