package app

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/mock"
)

// The moderation operations' auth matrix rows: every one is for interactive moderators only (D25), and a link
// report is any user's.

// matrixLinkID and matrixReportID are the link and link report the moderation operations act on.
const (
	matrixLinkID   = 7
	matrixReportID = 3
	matrixLinkURL  = "https://example.org/the-copper-kettle"
	// matrixDisplayName is the display name of the users the moderation operations show.
	matrixDisplayName = "Morwen"
)

var (
	matrixModerationRevision = domain.ModerationRevision{
		ID: matrixRevisionID, Status: domain.RevisionStatusPending, EntityType: domain.EntityTypeEvent,
		Entity: &matrixEventEntity, Source: domain.RevisionSourceUser,
		Author: &domain.ModerationAuthor{
			ID: contributorID, DisplayName: matrixDisplayName, Role: domain.RoleContributor,
		},
		CreatedAt: handlerNow, SubmittedAt: &handlerNow, Document: matrixEventDocument,
		Rebase: domain.RebasePreview{Status: domain.RebaseStatusNotNeeded},
	}
	matrixModerationLink = domain.ModerationLink{
		LinkID: matrixLinkID, URL: matrixLinkURL, Liveness: domain.LivenessOK,
	}
	matrixLinkReport = domain.LinkReport{
		ID: matrixReportID, Link: matrixModerationLink, Reason: domain.LinkReportReasonBroken,
		Reporter: domain.ModerationAuthor{ID: contributorID, DisplayName: matrixDisplayName, Role: domain.RoleContributor},
		Status:   domain.LinkReportStatusResolved, CreatedAt: handlerNow, ResolvedAt: &handlerNow,
		ResolvedBy: &domain.ModerationAuthor{ID: moderatorID, DisplayName: matrixDisplayName, Role: domain.RoleModerator},
	}
)

// moderationMatrix are the rows of the moderation operations and of reportLink, added to authMatrix.
func moderationMatrix() map[openapitest.Operation]matrixRow {
	revisionParams := map[string]string{revisionIDParam: strconv.Itoa(matrixRevisionID)}

	return map[openapitest.Operation]matrixRow{
		{Method: http.MethodPost, Path: "/v1/link-reports"}: {
			access:        authenticatedUser,
			body:          `{"url":"` + matrixLinkURL + `","reason":"broken","message":null}`,
			successStatus: http.StatusCreated,
			expectSuccess: func(m handlerMocks, userID string) {
				m.reporter.EXPECT().InsertLinkReport(mock.Anything, datasources.NewLinkReport{
					ReporterUserID: userID, URL: matrixLinkURL, Reason: domain.LinkReportReasonBroken, Now: handlerNow,
				}).Return(domain.LinkReportReceipt{
					ID: matrixReportID, Status: domain.LinkReportStatusOpen, CreatedAt: handlerNow,
				}, true, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/moderation/queue"}: {
			access:        interactiveModerator,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.queue.EXPECT().ListModerationQueue(mock.Anything,
					datasources.ModerationQueueQuery{Page: matrixPage, Now: handlerNow}).
					Return(domain.ModerationQueue{}, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/moderation/revisions/{revision_id}"}: {
			access:        interactiveModerator,
			pathParams:    revisionParams,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				expectModerationRevision(m, 1)
			},
		},
		{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/approve"}: {
			access:        interactiveModerator,
			pathParams:    revisionParams,
			body:          `{"acknowledged_flags":[]}`,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.approver.EXPECT().ApproveRevision(mock.Anything, mock.MatchedBy(func(r datasources.ApproveRequest) bool {
					return r.Actor.UserID == userID && r.RevisionID == matrixRevisionID
				})).Return(nil)
				expectModerationRevision(m, 2)
			},
		},
		{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/reject"}: {
			access:        interactiveModerator,
			pathParams:    revisionParams,
			body:          `{"review_note":"Already catalogued.","acknowledged_flags":[]}`,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.rejecter.EXPECT().RejectRevision(mock.Anything, mock.MatchedBy(func(r datasources.RejectRequest) bool {
					return r.Actor.UserID == userID && r.RevisionID == matrixRevisionID
				})).Return(nil)
				expectModerationRevision(m, 1)
			},
		},
		{Method: http.MethodPost, Path: "/v1/revisions/{revision_id}/rebase"}: {
			access:     interactiveModerator,
			pathParams: revisionParams,
			body: `{"entity_type":"event","current_revision_id":41,"document":` + matrixEditorEvent +
				`,"summary":null}`,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.refs.EXPECT().ResolveEditorRefs(mock.Anything, matrixActor(userID), domain.EditorRefs{}).
					Return(domain.RefMap{}, nil)
				m.rebaser.EXPECT().RebaseRevision(mock.Anything, mock.MatchedBy(func(r datasources.RebaseRequest) bool {
					return r.Actor.UserID == userID && r.RevisionID == matrixRevisionID
				})).Return(nil)
				expectModerationRevision(m, 1)
			},
		},
		{Method: http.MethodGet, Path: "/v1/moderation/link-reports"}: {
			access:        interactiveModerator,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.reportLister.EXPECT().ListLinkReports(mock.Anything, datasources.LinkReportQuery{Page: matrixPage}).
					Return(domain.LinkReportList{}, nil)
			},
		},
		{Method: http.MethodPost, Path: "/v1/moderation/link-reports/{report_id}"}: {
			access:        interactiveModerator,
			pathParams:    map[string]string{"report_id": strconv.Itoa(matrixReportID)},
			body:          `{"status":"resolved","note":null,"hide_link":true}`,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.reportResolver.EXPECT().ResolveLinkReport(mock.Anything, datasources.ResolveLinkReportRequest{
					ModeratorUserID: userID, ReportID: matrixReportID, Status: domain.LinkReportStatusResolved,
					HideLink: true, Now: handlerNow,
				}).Return(matrixLinkReport, nil)
			},
		},
		{Method: http.MethodGet, Path: "/v1/moderation/links"}: {
			access:        interactiveModerator,
			query:         "?url=" + url.QueryEscape(matrixLinkURL),
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.moderationLinks.EXPECT().GetModerationLink(mock.Anything, matrixLinkURL).Return(matrixModerationLink, nil)
			},
		},
		{Method: http.MethodPut, Path: "/v1/links/{link_id}/hidden"}: {
			access:        interactiveModerator,
			pathParams:    map[string]string{"link_id": strconv.Itoa(matrixLinkID)},
			body:          `{"hidden":true}`,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.linkHider.EXPECT().SetLinkHidden(mock.Anything, datasources.SetLinkHiddenRequest{
					ModeratorUserID: userID, LinkID: matrixLinkID, Hidden: true, Now: handlerNow,
				}).Return(matrixModerationLink, nil)
			},
		},
	}
}

// expectModerationRevision expects matrixModerationRevision to be read times times.
func expectModerationRevision(m handlerMocks, times int) {
	m.moderation.EXPECT().GetModerationRevision(mock.Anything, int64(matrixRevisionID)).
		Return(matrixModerationRevision, nil).Times(times)
}

// matrixContactRequestID is the contact request the moderation operations act on.
const matrixContactRequestID = 12

// matrixTurnstileToken is the dummy token Cloudflare's test site keys produce.
const matrixTurnstileToken = "XXXX.DUMMY.TOKEN.XXXX"

var matrixContactRequest = domain.ContactRequest{
	ID: matrixContactRequestID, Kind: domain.ContactRequestKindGeneral, Email: new("wren@example.org"),
	Message: new("Hello."), Status: domain.ContactRequestStatusResolved, CreatedAt: handlerNow, ResolvedAt: &handlerNow,
	ResolvedBy: &domain.ModerationAuthor{ID: moderatorID, DisplayName: matrixDisplayName, Role: domain.RoleModerator},
}

// contactMatrix are the rows of the contact form, which is public, and of the contact request inbox, which is for
// interactive moderators only (D25), added to authMatrix.
func contactMatrix() map[openapitest.Operation]matrixRow {
	return map[openapitest.Operation]matrixRow{
		{Method: http.MethodPost, Path: "/v1/contact"}: {
			access: public,
			body: `{"kind":"general","name":null,"email":"wren@example.org","message":"Hello.","url":null,` +
				`"turnstile_token":"` + matrixTurnstileToken + `"}`,
			successStatus: http.StatusAccepted,
			expectSuccess: func(m handlerMocks, _ string) {
				m.turnstile.EXPECT().VerifyTurnstile(mock.Anything, matrixTurnstileToken).Return(nil).Once()
				m.contactCreator.EXPECT().CreateContactRequest(mock.Anything, datasources.NewContactRequest{
					Kind: domain.ContactRequestKindGeneral, Email: "wren@example.org", Message: "Hello.", Now: handlerNow,
				}).Return(matrixContactRequestID, nil).Once()
			},
		},
		{Method: http.MethodGet, Path: "/v1/moderation/contact-requests"}: {
			access:        interactiveModerator,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, _ string) {
				m.contactPurger.EXPECT().PurgeContactRequests(mock.Anything, handlerNow).Return(0, nil).Once()
				m.contactLister.EXPECT().ListContactRequests(mock.Anything,
					datasources.ContactRequestQuery{Page: matrixPage}).
					Return(domain.ContactRequestList{Requests: []domain.ContactRequest{matrixContactRequest}, Total: 1},
						nil).Once()
			},
		},
		{Method: http.MethodPost, Path: "/v1/moderation/contact-requests/{request_id}"}: {
			access:        interactiveModerator,
			pathParams:    map[string]string{"request_id": strconv.Itoa(matrixContactRequestID)},
			body:          `{"status":"resolved","note":null}`,
			successStatus: http.StatusOK,
			expectSuccess: func(m handlerMocks, userID string) {
				m.contactResolver.EXPECT().ResolveContactRequest(mock.Anything, datasources.ResolveContactRequestRequest{
					ModeratorUserID: userID, RequestID: matrixContactRequestID,
					Status: domain.ContactRequestStatusResolved, Now: handlerNow,
				}).Return(matrixContactRequest, nil).Once()
				m.contactPurger.EXPECT().PurgeContactRequests(mock.Anything, handlerNow).Return(0, nil).Once()
			},
		},
	}
}
