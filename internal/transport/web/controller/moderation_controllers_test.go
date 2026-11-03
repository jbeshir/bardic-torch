package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
)

var (
	testModerationAuthor   = domain.ModerationAuthor{ID: principalUserID, DisplayName: testDisplayName, Role: domain.RoleContributor}
	testModerationRevision = domain.ModerationRevision{
		ID: 51, Status: domain.RevisionStatusPending, EntityType: domain.EntityTypeEvent, Entity: &testRevisionEntity,
		Source: domain.RevisionSourceUser, Author: &testModerationAuthor, Summary: testMyRevision.Summary,
		CreatedAt: testNow, SubmittedAt: &testNow, BaseRevisionID: new(int64(46)),
		SubmittedBaseRevisionID: new(int64(45)), CurrentRevisionID: new(int64(47)), Stale: true,
		Document: testEventDocument, SubmittedDocument: testEventDocument, BaseDocument: testEventDocument,
		CurrentDocument: testEventDocument,
		LinkStates: []domain.LinkState{{URL: testLinkURL, LinkID: new(int64(7)), IsHidden: true,
			Liveness: new(domain.LivenessOK)}},
		Rebase: domain.RebasePreview{Status: domain.RebaseStatusConflicts, Conflicts: []domain.RebaseConflict{
			{Path: "/name", Base: json.RawMessage(`"Revel"`), Revision: json.RawMessage(`"Midwinter Revel"`)},
		}},
		PendingReferences: []domain.ModerationPendingReference{{RevisionID: 50, EntityType: domain.EntityTypeTerm,
			Entity: domain.RevisionEntity{Type: domain.EntityTypeTerm, ID: 3, Slug: "pending-term-3", IsNew: true},
			Document: domain.TermDocument{SchemaVersion: domain.DocumentSchemaVersion, Facet: domain.FacetTopic,
				Name: "Revels", DisplayName: "Revel songs"},
			Flags: []domain.Flag{{Code: domain.FlagPossibleDuplicate}}}},
		Flags:        []domain.Flag{{Code: domain.FlagHiddenAliasInText, Path: "/description_md"}},
		ChangedPaths: []string{"/description_md"},
	}
	testLinkURL        = "https://example.org/the-copper-kettle"
	testModerationLink = domain.ModerationLink{LinkID: 7, URL: testLinkURL, Liveness: domain.LivenessOK, IsHidden: true,
		Collection: &domain.CollectionRef{ID: 12, Name: testCollectionName},
		Attachments: []domain.ModerationLinkAttachment{{EntityType: domain.EntityTypePiece, Slug: "the-copper-kettle",
			Title: "The Copper Kettle", Public: true, Roles: []domain.LinkRole{domain.LinkRoleLyrics}}},
		OpenReportCount: 0}
	testLinkReport = domain.LinkReport{ID: 3, Link: testModerationLink, Reason: domain.LinkReportReasonBroken,
		Message: new("The video is gone."), Reporter: testModerationAuthor, Status: domain.LinkReportStatusResolved,
		CreatedAt: testNow, ResolvedAt: &testNow, ResolvedBy: &domain.ModerationAuthor{ID: "auth0|moderator",
			DisplayName: "Rowan", Role: domain.RoleModerator}, Note: new(domain.LinkHiddenNote)}
)

const (
	testModerationLinkBody = `{"link_id":7,"url":"https://example.org/the-copper-kettle","title":null,` +
		`"description":null,"platform":null,"place_type":null,` +
		`"collection":{"id":12,"name":"The Northmarch Songbook"},"locator":null,"liveness":"ok","is_hidden":true,` +
		`"attachments":[{"entity_type":"piece","slug":"the-copper-kettle","title":"The Copper Kettle",` +
		`"public":true,"roles":["lyrics"]}],"open_report_count":0}`
	approveBody         = `{"review_note":"Thanks.","summary":null,"acknowledged_flags":["hidden_alias_in_text"]}`
	rejectBody          = `{"review_note":"Already catalogued.","acknowledged_flags":[]}`
	rebaseBody          = `{"entity_type":"event","current_revision_id":47,"document":` + testEditorEventJSON + `,"summary":null}`
	moderatorRequiredBy = `{"code":"moderator_required","message":"this endpoint requires the moderator role"}`
	notPendingBody      = `{"code":"not_pending","message":"the revision is not pending"}`
	totalOne            = `"total":1`
	linkReportsPath     = "/v1/moderation/link-reports"
)

var moderatorRequiredCase = revisionCase{name: "moderator_required", err: domain.ErrModeratorRequired,
	wantStatus: http.StatusForbidden, wantBody: moderatorRequiredBy}

// moderationRevisionCases are the cases every moderation write of the revision at target shares: success, an
// anonymous request, an invalid ID, a body too large or not JSON, and the errors of every write.
func moderationRevisionCases(target, body string) (revisionCase, []revisionCase) {
	base := revisionCase{method: http.MethodPost, target: target, pathValues: map[string]string{revisionIDVar: "51"},
		body: body}
	ok := base
	ok.name, ok.wantStatus = "ok", http.StatusOK
	ok.wantContains = []string{`"id":51`, `"author":{"id":"` + principalUserID + `"`, `"link_states":[{"url":`,
		`"rebase":{"status":"conflicts","merged_document":null,"conflicts":[{"path":"/name",` +
			`"base_json":"\"Revel\"","revision_json":"\"Midwinter Revel\"","current_json":null}]}`,
		`"pending_references":[{"revision_id":50,"entity_type":"term"`}
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badID := base
	badID.name, badID.pathValues, badID.invalidRequest, badID.notCalled = badIDCase,
		map[string]string{revisionIDVar: "x"}, true, true
	badID.wantStatus, badID.wantBody = http.StatusBadRequest, invalidID(revisionIDVar)
	notJSON := base
	notJSON.name, notJSON.contentType, notJSON.invalidRequest, notJSON.notCalled = notJSONCase, plainText, true, true
	notJSON.wantStatus, notJSON.wantBody = http.StatusUnsupportedMediaType, unsupportedMediaTypeBody

	return base, append([]revisionCase{ok, anonymous, badID, notJSON}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
			wantBody: revisionNotFound},
		revisionCase{name: "not_pending", err: domain.ErrNotPending, wantStatus: http.StatusConflict,
			wantBody: notPendingBody},
		moderatorRequiredCase, unavailableCase, internalCase)...)
}

// badBodyCase is base with body, refused with 400 and message before the command runs.
func badBodyCase(base revisionCase, name, body, message string) revisionCase {
	tc := base
	tc.name, tc.body, tc.invalidRequest, tc.notCalled = name, body, true, true
	tc.wantStatus, tc.wantBody = http.StatusBadRequest, `{"code":"invalid_request","message":"`+message+`"}`

	return tc
}

func TestModerationQueueList_ServeHTTP(t *testing.T) {
	t.Parallel()

	queue := domain.ModerationQueue{Items: []domain.QueueItem{{ID: 51, EntityType: domain.EntityTypeEvent,
		Entity: &testRevisionEntity, Title: testEventName, Author: testModerationAuthor, SubmittedAt: testNow,
		Flags: []domain.FlagCode{domain.FlagPossibleDuplicate}}}, Total: 1}
	get := func(name, query string, status int, body string) revisionCase {
		tc := revisionCase{name: name, method: http.MethodGet, target: "/v1/moderation/queue" + query,
			wantStatus: status, wantBody: body}
		if status == http.StatusBadRequest {
			tc.invalidRequest, tc.notCalled = true, true
		}
		return tc
	}
	ok := get("ok", "?entity_type=event&author=auth0%7Ca&min_age_hours=24&max_age_hours=8760&include_blocked=true"+
		"&limit=5", http.StatusOK, "")
	ok.wantContains = []string{totalOne, `"flags":["possible_duplicate"]`, `"blocked":false`}
	anonymous := get(anonymousCase, "", http.StatusUnauthorized, unauthenticatedBody)
	anonymous.anonymous, anonymous.notCalled = true, true
	cases := []revisionCase{ok, anonymous,
		get("unknown_entity_type", "?entity_type=song", http.StatusBadRequest,
			`{"code":"invalid_request","message":"invalid entity_type: not one of its allowed values"}`),
		get("age_out_of_range", "?min_age_hours=8761", http.StatusBadRequest,
			`{"code":"invalid_request","message":"invalid min_age_hours: must be an integer from 0 to 8760"}`),
		get("repeated_author", "?author=a&author=b", http.StatusBadRequest,
			`{"code":"invalid_request","message":"query parameter author must be given at most once"}`),
		get("include_blocked_not_bool", "?include_blocked=yes", http.StatusBadRequest,
			`{"code":"invalid_request","message":"invalid include_blocked: must be true or false"}`),
		get("offset_out_of_range", "?offset=10001", http.StatusBadRequest,
			`{"code":"invalid_request","message":"invalid offset: must be an integer from 0 to 10000"}`),
		get(moderatorRequiredCase.name, "", http.StatusForbidden, moderatorRequiredBy),
		get(internalCase.name, "", http.StatusInternalServerError, internalErrorBody),
	}
	cases[len(cases)-2].err, cases[len(cases)-1].err = domain.ErrModeratorRequired, errCommand
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return ModerationQueueList{Command: commandFunc[command.ModerationQueueRequest, domain.ModerationQueue](
			func(_ context.Context, req command.ModerationQueueRequest) (domain.ModerationQueue, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				if tc.name == "ok" {
					assert.Equal(t, datasources.ModerationQueueQuery{EntityType: new(domain.EntityTypeEvent),
						AuthorUserID: new("auth0|a"), MinAgeHours: new(int32(24)), MaxAgeHours: new(int32(8760)),
						IncludeBlocked: true, Page: domain.Page{Limit: 5}}, req.Query)
				}
				return queue, tc.err
			})}
	})
}

func TestModerationRevisionGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	base, cases := moderationRevisionCases("/v1/moderation/revisions/51", "")
	base.method = http.MethodGet
	for i := range cases {
		cases[i].method = base.method
	}
	// A GET has no body to refuse, and no write to be unavailable.
	cases = filterCases(cases, notJSONCase, "not_pending", unavailableCase.name)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return ModerationRevisionGet{Command: returning[command.RevisionRequest](t, !tc.notCalled,
			testModerationRevision, tc.err)}
	})
}

func TestRevisionApprove_ServeHTTP(t *testing.T) {
	t.Parallel()

	base, cases := moderationRevisionCases("/v1/revisions/51/approve", approveBody)
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"summary":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	cases = append(cases, tooLarge,
		badBodyCase(base, "no_acknowledged_flags", `{}`, "acknowledged_flags is required"),
		badBodyCase(base, "unknown_flag", `{"acknowledged_flags":["x"]}`,
			"invalid acknowledged_flags: not one of its allowed values"),
		badBodyCase(base, "review_note_too_long",
			`{"review_note":"`+strings.Repeat("é", domain.MaxModeratorNoteLength+1)+`","acknowledged_flags":[]}`,
			"invalid review_note: must be at most 2000 characters"))
	cases = append(cases, errorCases(base,
		revisionCase{name: "rebase_required", err: domain.ErrRebaseRequired, wantStatus: http.StatusConflict,
			wantBody: `{"code":"rebase_required","message":"the entity changed since the base, and the changes ` +
				`conflict: edit the current version"}`},
		revisionCase{name: "duplicate_name", err: command.ErrDuplicateName, wantStatus: http.StatusConflict,
			wantBody: `{"code":"duplicate_name","message":"another term of the facet has this name"}`},
		revisionCase{name: "duplicate_blocked", err: domain.ErrDuplicateBlocked,
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   `{"code":"duplicate_blocked","message":"a person with this name has asked not to have a page"}`},
		entityGone, hiddenDataConflict, unacknowledgedFlags, pendingReferenceFlagged, invalidDocument)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return RevisionApprove{Command: commandFunc[command.ApproveRevisionRequest, domain.ModerationRevision](
			func(_ context.Context, req command.ApproveRevisionRequest) (domain.ModerationRevision, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				if tc.name == "ok" {
					assert.Equal(t, command.ApproveRevisionRequest{Principal: req.Principal, RevisionID: 51,
						Options: domain.ApprovalOptions{ReviewNote: "Thanks.",
							AcknowledgedFlags: []domain.FlagCode{domain.FlagHiddenAliasInText}}}, req)
				}
				return testModerationRevision, tc.err
			})}
	})
}

func TestRevisionReject_ServeHTTP(t *testing.T) {
	t.Parallel()

	base, cases := moderationRevisionCases("/v1/revisions/51/reject", rejectBody)
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"review_note":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	cases = append(cases, tooLarge,
		badBodyCase(base, "no_acknowledged_flags", `{"review_note":null}`, "acknowledged_flags is required"),
		badBodyCase(base, "review_note_too_long",
			`{"review_note":"`+strings.Repeat("a", domain.MaxModeratorNoteLength+1)+`","acknowledged_flags":[]}`,
			"invalid review_note: must be at most 2000 characters"))
	cases = append(cases, errorCases(base, revisionCase{name: "unacknowledged_review_note",
		err: &domain.UnacknowledgedFlagsError{Flags: []domain.Flag{
			{Code: domain.FlagHiddenAliasInReviewNote, Path: "/review_note"},
		}}, wantStatus: http.StatusConflict,
		wantBody: `{"code":"unacknowledged_flags","message":"the revision has flags that are not acknowledged",` +
			`"flags":[{"code":"hidden_alias_in_review_note","path":"/review_note"}]}`})...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return RevisionReject{Command: commandFunc[command.RejectRevisionRequest, domain.ModerationRevision](
			func(_ context.Context, req command.RejectRevisionRequest) (domain.ModerationRevision, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				if tc.name == "ok" {
					assert.Equal(t, "Already catalogued.", req.ReviewNote)
					assert.Equal(t, []domain.FlagCode{}, req.AcknowledgedFlags)
				}
				return testModerationRevision, tc.err
			})}
	})
}

func TestRevisionRebase_ServeHTTP(t *testing.T) {
	t.Parallel()

	base, cases := moderationRevisionCases("/v1/revisions/51/rebase", rebaseBody)
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"summary":"` + strings.Repeat("a", httpjson.MaxDocumentBodyBytes) + `"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	cases = append(cases, tooLarge,
		badBodyCase(base, "unknown_entity_type",
			`{"entity_type":"song","current_revision_id":null,"document":{}}`,
			"invalid entity_type: not one of its allowed values"),
		badBodyCase(base, "not_an_event_document",
			`{"entity_type":"event","current_revision_id":null,"document":{"title":"x"}}`,
			"field document has an unknown field"))
	cases = append(cases, errorCases(base,
		revisionCase{name: "stale", err: datasources.ErrStale, wantStatus: http.StatusConflict,
			wantBody: `{"code":"stale","message":"the entity's current revision has changed: resolve against the ` +
				`new current revision"}`},
		revisionCase{name: "wrong_document_type", err: datasources.ErrDocumentType,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"invalid_request","message":"invalid entity_type: not the draft's entity type"}`},
		entityGone, invalidDocument)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return RevisionRebase{Command: commandFunc[command.RebaseRevisionRequest, domain.ModerationRevision](
			func(_ context.Context, req command.RebaseRevisionRequest) (domain.ModerationRevision, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				if tc.name == "ok" {
					assert.Equal(t, new(int64(47)), req.CurrentRevisionID)
					assert.Equal(t, domain.EditorEventDocument{Name: testEventName, Kind: domain.EventKindEvent},
						req.Document)
				}
				return testModerationRevision, tc.err
			})}
	})
}

func TestLinkReportCreate_ServeHTTP(t *testing.T) {
	t.Parallel()

	receipt := domain.LinkReportReceipt{ID: 3, Status: domain.LinkReportStatusOpen, CreatedAt: testNow}
	receiptBody := `{"id":3,"status":"open","created_at":"2026-10-31T12:00:00Z"}`
	base := revisionCase{method: http.MethodPost, target: "/v1/link-reports",
		body: `{"url":"` + testLinkURL + `","reason":"broken","message":"The video is gone."}`}
	created := base
	created.name, created.wantStatus, created.wantBody = createdCase, http.StatusCreated, receiptBody
	existing := base
	existing.name, existing.wantStatus, existing.wantBody = "existing", http.StatusOK, receiptBody
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"message":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	notJSON := base
	notJSON.name, notJSON.contentType, notJSON.invalidRequest, notJSON.notCalled = notJSONCase, plainText, true, true
	notJSON.wantStatus, notJSON.wantBody = http.StatusUnsupportedMediaType, unsupportedMediaTypeBody
	cases := append([]revisionCase{created, existing, anonymous, tooLarge, notJSON,
		badBodyCase(base, "no_url", `{"reason":"broken"}`, "url is required"),
		badBodyCase(base, "url_too_long", `{"url":"https://example.org/`+strings.Repeat("a", 2048)+`","reason":"other"}`,
			"invalid url: must be at most 2048 characters"),
		badBodyCase(base, "unknown_reason", `{"url":"`+testLinkURL+`","reason":"spam"}`,
			"invalid reason: not one of its allowed values"),
		badBodyCase(base, "message_too_long",
			`{"url":"`+testLinkURL+`","reason":"other","message":"`+strings.Repeat("a", 2001)+`"}`,
			"invalid message: must be at most 2000 characters"),
	}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
			wantBody: `{"code":"not_found","message":"link not found"}`},
		revisionCase{name: "report_limit_reached", err: datasources.ErrReportLimitReached,
			wantStatus: http.StatusConflict,
			wantBody:   `{"code":"report_limit_reached","message":"a user may have at most 20 open link reports"}`},
		unavailableCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return LinkReportCreate{Command: commandFunc[command.ReportLinkRequest, command.ReportedLink](
			func(_ context.Context, req command.ReportLinkRequest) (command.ReportedLink, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				assert.Equal(t, principalUserID, req.Principal.User.ID)
				assert.Equal(t, domain.LinkReportReasonBroken, req.Reason)
				return command.ReportedLink{Receipt: receipt, Created: tc.name == createdCase}, tc.err
			})}
	})
}

func TestLinkReportsList_ServeHTTP(t *testing.T) {
	t.Parallel()

	list := domain.LinkReportList{Reports: []domain.LinkReport{testLinkReport}, Total: 1}
	cases := []revisionCase{
		{name: "ok", method: http.MethodGet, target: linkReportsPath + "?status=open&limit=5",
			wantStatus: http.StatusOK, wantContains: []string{totalOne, `"reason":"broken"`,
				`"resolved_by":{"id":"auth0|moderator","display_name":"Rowan","role":"moderator","blocked":false}`,
				`"note":"Link hidden."`, `"link":` + testModerationLinkBody}},
		{name: anonymousCase, method: http.MethodGet, target: linkReportsPath, anonymous: true,
			notCalled: true, wantStatus: http.StatusUnauthorized, wantBody: unauthenticatedBody},
		{name: "unknown_report_status", method: http.MethodGet, target: linkReportsPath + "?status=closed",
			invalidRequest: true, notCalled: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid status: not one of its allowed values"}`},
		{name: "limit_out_of_range", method: http.MethodGet, target: linkReportsPath + "?limit=0",
			invalidRequest: true, notCalled: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid limit: must be an integer from 1 to 100"}`},
		{name: moderatorRequiredCase.name, method: http.MethodGet, target: linkReportsPath,
			err: domain.ErrModeratorRequired, wantStatus: http.StatusForbidden, wantBody: moderatorRequiredBy},
		{name: internalCase.name, method: http.MethodGet, target: linkReportsPath, err: errCommand,
			wantStatus: http.StatusInternalServerError, wantBody: internalErrorBody},
	}
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return LinkReportsList{Command: commandFunc[command.LinkReportListRequest, domain.LinkReportList](
			func(_ context.Context, req command.LinkReportListRequest) (domain.LinkReportList, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				if tc.name == "ok" {
					assert.Equal(t, datasources.LinkReportQuery{Status: new(domain.LinkReportStatusOpen),
						Page: domain.Page{Limit: 5}}, req.Query)
				}
				return list, tc.err
			})}
	})
}

func TestLinkReportResolve_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodPost, target: "/v1/moderation/link-reports/3",
		pathValues: map[string]string{"report_id": "3"}, body: `{"status":"resolved","note":null,"hide_link":true}`}
	ok := base
	ok.name, ok.wantStatus, ok.wantContains = "ok", http.StatusOK, []string{`"id":3`, `"status":"resolved"`}
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badID := base
	badID.name, badID.pathValues, badID.invalidRequest, badID.notCalled = badIDCase,
		map[string]string{"report_id": "-1"}, true, true
	badID.wantStatus, badID.wantBody = http.StatusBadRequest, invalidID("report_id")
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"note":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	notJSON := base
	notJSON.name, notJSON.contentType, notJSON.invalidRequest, notJSON.notCalled = notJSONCase, plainText, true, true
	notJSON.wantStatus, notJSON.wantBody = http.StatusUnsupportedMediaType, unsupportedMediaTypeBody
	cases := append([]revisionCase{ok, anonymous, badID, tooLarge, notJSON,
		badBodyCase(base, "reopen", `{"status":"open"}`, "invalid status: must be resolved or dismissed"),
		badBodyCase(base, "note_too_long", `{"status":"dismissed","note":"`+strings.Repeat("a", 2001)+`"}`,
			"invalid note: must be at most 2000 characters"),
	}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
			wantBody: `{"code":"not_found","message":"link report not found"}`},
		revisionCase{name: "not_open", err: datasources.ErrReportNotOpen, wantStatus: http.StatusConflict,
			wantBody: `{"code":"conflict","message":"the link report is not open"}`},
		moderatorRequiredCase, unavailableCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return LinkReportResolve{Command: commandFunc[command.ResolveLinkReportRequest, domain.LinkReport](
			func(_ context.Context, req command.ResolveLinkReportRequest) (domain.LinkReport, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				if tc.name == "ok" {
					assert.Equal(t, command.ResolveLinkReportRequest{Principal: req.Principal, ReportID: 3,
						Status: domain.LinkReportStatusResolved, HideLink: true}, req)
				}
				return testLinkReport, tc.err
			})}
	})
}

func TestModerationLinkGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	get := func(name, query string, status int, body string) revisionCase {
		tc := revisionCase{name: name, method: http.MethodGet, target: "/v1/moderation/links" + query,
			wantStatus: status, wantBody: body}
		if status == http.StatusBadRequest {
			tc.invalidRequest, tc.notCalled = true, true
		}
		return tc
	}
	const found = "?url=https%3A%2F%2Fexample.org%2Fthe-copper-kettle"
	anonymous := get(anonymousCase, found, http.StatusUnauthorized, unauthenticatedBody)
	anonymous.anonymous, anonymous.notCalled = true, true
	notFound := get(notFoundCase, found, http.StatusNotFound, `{"code":"not_found","message":"link not found"}`)
	notFound.err = datasources.ErrNotFound
	forbidden := get(moderatorRequiredCase.name, found, http.StatusForbidden, moderatorRequiredBy)
	forbidden.err = domain.ErrModeratorRequired
	internal := get(internalCase.name, found, http.StatusInternalServerError, internalErrorBody)
	internal.err = errCommand
	cases := []revisionCase{
		get("ok", found, http.StatusOK, testModerationLinkBody), anonymous, notFound, forbidden, internal,
		get("no_url", "", http.StatusBadRequest,
			`{"code":"invalid_request","message":"query parameter url is required"}`),
		get("empty_url", "?url=", http.StatusBadRequest,
			`{"code":"invalid_request","message":"query parameter url is required"}`),
		get("repeated_url", found+"&url=x", http.StatusBadRequest,
			`{"code":"invalid_request","message":"query parameter url must be given at most once"}`),
		get("url_too_long", "?url="+strings.Repeat("a", 2049), http.StatusBadRequest,
			`{"code":"invalid_request","message":"invalid url: must be at most 2048 characters"}`),
	}
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return ModerationLinkGet{Command: commandFunc[command.ModerationLinkRequest, domain.ModerationLink](
			func(_ context.Context, req command.ModerationLinkRequest) (domain.ModerationLink, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				assert.Equal(t, testLinkURL, req.URL)
				return testModerationLink, tc.err
			})}
	})
}

func TestLinkHiddenSet_ServeHTTP(t *testing.T) {
	t.Parallel()

	base := revisionCase{method: http.MethodPut, target: "/v1/links/7/hidden",
		pathValues: map[string]string{"link_id": "7"}, body: `{"hidden":true}`}
	ok := base
	ok.name, ok.wantStatus, ok.wantBody = "ok", http.StatusOK, testModerationLinkBody
	anonymous := base
	anonymous.name, anonymous.anonymous, anonymous.notCalled = anonymousCase, true, true
	anonymous.wantStatus, anonymous.wantBody = http.StatusUnauthorized, unauthenticatedBody
	badID := base
	badID.name, badID.pathValues, badID.invalidRequest, badID.notCalled = badIDCase,
		map[string]string{"link_id": "seven"}, true, true
	badID.wantStatus, badID.wantBody = http.StatusBadRequest, invalidID("link_id")
	tooLarge := base
	tooLarge.name, tooLarge.invalidRequest, tooLarge.notCalled = tooLargeCase, true, true
	tooLarge.body = `{"x":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `"}`
	tooLarge.wantStatus, tooLarge.wantBody = http.StatusRequestEntityTooLarge, tooLargeBody
	notJSON := base
	notJSON.name, notJSON.contentType, notJSON.invalidRequest, notJSON.notCalled = notJSONCase, plainText, true, true
	notJSON.wantStatus, notJSON.wantBody = http.StatusUnsupportedMediaType, unsupportedMediaTypeBody
	cases := append([]revisionCase{ok, anonymous, badID, tooLarge, notJSON,
		badBodyCase(base, "no_hidden", `{}`, "hidden is required"),
	}, errorCases(base,
		revisionCase{name: notFoundCase, err: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
			wantBody: `{"code":"not_found","message":"link not found"}`},
		moderatorRequiredCase, unavailableCase, internalCase)...)
	runRevisionCases(t, cases, func(t *testing.T, tc revisionCase) http.Handler {
		return LinkHiddenSet{Command: commandFunc[command.SetLinkHiddenRequest, domain.ModerationLink](
			func(_ context.Context, req command.SetLinkHiddenRequest) (domain.ModerationLink, error) {
				if tc.notCalled {
					t.Error("command called unexpectedly")
				}
				assert.Equal(t, int64(7), req.LinkID)
				assert.True(t, req.Hidden)
				return testModerationLink, tc.err
			})}
	})
}

// filterCases returns cases without those named names.
func filterCases(cases []revisionCase, names ...string) []revisionCase {
	return slices.DeleteFunc(cases, func(tc revisionCase) bool { return slices.Contains(names, tc.name) })
}
