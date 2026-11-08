package router

import (
	"net/http"
	"strings"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
)

// Dependencies are what the routes are built from.
type Dependencies struct {
	Pinger datasources.Pinger
	// AuthValidators authenticate requests to authenticated routes; with none, every request is anonymous.
	AuthValidators                 []AuthValidator
	ResolveUser                    command.Command[string, domain.User]
	UpdateDisplayName              command.Command[command.UpdateDisplayNameRequest, domain.User]
	SetUserBlocked                 command.Command[command.SetUserBlockedRequest, domain.User]
	CreateAPIToken                 command.Command[command.CreateAPITokenRequest, command.CreateAPITokenResponse]
	APITokenLister                 datasources.ActiveAPITokenLister
	APITokenRevoker                datasources.APITokenRevoker
	PublicPieceLister              datasources.PublicPieceLister
	PublicPieceGetter              datasources.PublicPieceGetter
	PublicPersonLister             datasources.PublicPersonLister
	PublicPersonGetter             datasources.PublicPersonGetter
	PublicTermLister               datasources.PublicTermLister
	PublicTermGetter               datasources.PublicTermGetter
	PublicCollectionLister         datasources.PublicCollectionLister
	PublicCollectionGetter         datasources.PublicCollectionGetter
	PublicEventLister              datasources.PublicEventLister
	PublicEventGetter              datasources.PublicEventGetter
	PublicHistoryLister            datasources.PublicHistoryLister
	PublicCollectionHistoryLister  datasources.PublicCollectionHistoryLister
	PublicRevisionGetter           datasources.PublicRevisionGetter
	PublicNeedsWorkLister          datasources.PublicNeedsWorkLister
	PublicPersonPerformanceLister  datasources.PublicPersonPerformanceLister
	PublicCollectionCitationLister datasources.PublicCollectionCitationLister
	PublicEventPerformanceLister   datasources.PublicEventPerformanceLister
	PublicRepertoireLister         datasources.PublicRepertoireLister
	GetEditable                    command.Command[command.EntityRequest, domain.Editable]
	GetPendingVersion              command.Command[command.EntityRequest, domain.MyRevision]
	SaveDraft                      command.Command[command.SaveDraftRequest, command.SavedDraft]
	UpdateDraft                    command.Command[command.UpdateDraftRequest, command.DraftResult]
	GetDraft                       command.Command[command.RevisionRequest, command.DraftResult]
	DiscardDraft                   command.Command[command.RevisionRequest, command.Empty]
	SubmitRevision                 command.Command[command.SubmitRevisionRequest, command.SubmittedRevision]
	WithdrawRevision               command.Command[command.RevisionRequest, domain.MyRevision]
	RevertTo                       command.Command[command.RevertRequest, command.SubmittedRevision]
	ListMyRevisions                command.Command[command.ListMyRevisionsRequest, domain.MyRevisionList]
	GetMyRevision                  command.Command[command.RevisionRequest, domain.MyRevision]
	ReportLink                     command.Command[command.ReportLinkRequest, command.ReportedLink]
	ListModerationQueue            command.Command[command.ModerationQueueRequest, domain.ModerationQueue]
	GetModerationRevision          command.Command[command.RevisionRequest, domain.ModerationRevision]
	ApproveRevision                command.Command[command.ApproveRevisionRequest, domain.ModerationRevision]
	RejectRevision                 command.Command[command.RejectRevisionRequest, domain.ModerationRevision]
	RebaseRevision                 command.Command[command.RebaseRevisionRequest, domain.ModerationRevision]
	ListLinkReports                command.Command[command.LinkReportListRequest, domain.LinkReportList]
	ResolveLinkReport              command.Command[command.ResolveLinkReportRequest, domain.LinkReport]
	GetModerationLink              command.Command[command.ModerationLinkRequest, domain.ModerationLink]
	SetLinkHidden                  command.Command[command.SetLinkHiddenRequest, domain.ModerationLink]
	SubmitContactRequest           command.Command[command.ContactSubmission, int64]
	ListContactRequests            command.Command[command.ContactRequestListRequest, domain.ContactRequestList]
	ResolveContactRequest          command.Command[command.ResolveContactRequestRequest, domain.ContactRequest]
	DismissContactRequests         command.Command[command.DismissContactRequestsRequest, domain.ContactRequestDismissal]
	ListRepertoire                 command.Command[command.RepertoireListRequest, domain.RepertoireList]
	GetRepertoireEntry             command.Command[command.RepertoireEntryRequest, domain.RepertoireEntry]
	PutRepertoireEntry             command.Command[command.PutRepertoireEntryRequest, command.PutRepertoireEntryResult]
	RemoveRepertoireEntry          command.Command[command.RepertoireEntryRequest, command.Empty]
	// RateLimits limit the routes docs/PLAN.md §5.1 "Rate limits" lists; ClientIP says how the contact form tells its
	// clients apart.
	RateLimits RateLimits
	ClientIP   ClientIPConfig
	Now        func() time.Time
}

// route is one operation the router serves.
type route struct {
	method string
	// path is a net/http.ServeMux path pattern, such as /v1/users/{user_id}/blocked. It must start but not end in
	// "/", and must not contain a {name...} wildcard; newRouter panics if it does, or if method is empty.
	path    string
	handler http.Handler
	// authenticated routes run behind the auth middleware, which sets the request's principal; handler then
	// applies the route's require* middleware. Other routes answer whatever credentials a request carries.
	authenticated bool
}

// routes returns every route the API serves. Each method and path pair is a separate route.
func routes(deps Dependencies) []route {
	return []route{
		{method: http.MethodGet, path: "/healthz", handler: controller.Healthz{}},
		{method: http.MethodGet, path: "/readyz", handler: controller.Readyz{Pinger: deps.Pinger}},

		// Public catalogue reads. The ServeMux routes a request to the most specific matching pattern, whatever the
		// order of this table, so a later authenticated route with a literal segment where these have a wildcard,
		// such as GET /v1/pieces/mine or /v1/revisions/pending, is routed to its own handler and not answered by the
		// public {slug} or {revision_id} route. Two patterns that overlap with neither more specific than the other,
		// such as /v1/pieces/{slug} and /v1/{collection}/mine, make newRouter panic. The literal segment must be
		// sent as is: one sent with an encoded letter, such as /v1/pieces/min%65, would match no route.
		// TestRouter_AuthMatrix fails for an authenticated operation a public route answers.
		{method: http.MethodGet, path: "/v1/pieces", handler: controller.PieceListGet{Lister: deps.PublicPieceLister}},
		{method: http.MethodGet, path: "/v1/pieces/{slug}", handler: controller.PieceGet{Getter: deps.PublicPieceGetter}},
		{method: http.MethodGet, path: "/v1/persons",
			handler: controller.PersonListGet{Lister: deps.PublicPersonLister}},
		{method: http.MethodGet, path: "/v1/persons/{slug}",
			handler: controller.PersonGet{Getter: deps.PublicPersonGetter}},
		{method: http.MethodGet, path: "/v1/terms", handler: controller.TermListGet{Lister: deps.PublicTermLister}},
		{method: http.MethodGet, path: "/v1/terms/{slug}", handler: controller.TermGet{Getter: deps.PublicTermGetter}},
		{method: http.MethodGet, path: "/v1/collections",
			handler: controller.CollectionListGet{Lister: deps.PublicCollectionLister}},
		{method: http.MethodGet, path: "/v1/collections/{collection_id}",
			handler: controller.CollectionGet{Getter: deps.PublicCollectionGetter}},
		{method: http.MethodGet, path: "/v1/events", handler: controller.EventListGet{Lister: deps.PublicEventLister}},
		{method: http.MethodGet, path: "/v1/events/{slug}", handler: controller.EventGet{Getter: deps.PublicEventGetter}},
		// History, of the entities addressed by slug and of collections.
		{method: http.MethodGet, path: "/v1/pieces/{slug}/history", handler: controller.HistoryGet{
			Lister: deps.PublicHistoryLister, EntityType: domain.SlugEntityTypePiece,
		}},
		{method: http.MethodGet, path: "/v1/persons/{slug}/history", handler: controller.HistoryGet{
			Lister: deps.PublicHistoryLister, EntityType: domain.SlugEntityTypePerson,
		}},
		{method: http.MethodGet, path: "/v1/terms/{slug}/history", handler: controller.HistoryGet{
			Lister: deps.PublicHistoryLister, EntityType: domain.SlugEntityTypeTerm,
		}},
		{method: http.MethodGet, path: "/v1/events/{slug}/history", handler: controller.HistoryGet{
			Lister: deps.PublicHistoryLister, EntityType: domain.SlugEntityTypeEvent,
		}},
		{method: http.MethodGet, path: "/v1/collections/{collection_id}/history",
			handler: controller.CollectionHistoryGet{Lister: deps.PublicCollectionHistoryLister}},
		// The paged lists of what the pages above cap.
		{method: http.MethodGet, path: "/v1/persons/{slug}/performances",
			handler: controller.PersonPerformancesGet{Lister: deps.PublicPersonPerformanceLister}},
		{method: http.MethodGet, path: "/v1/collections/{collection_id}/citations",
			handler: controller.CollectionCitationsGet{Lister: deps.PublicCollectionCitationLister}},
		{method: http.MethodGet, path: "/v1/events/{slug}/performances",
			handler: controller.EventPerformancesGet{Lister: deps.PublicEventPerformanceLister}},
		{method: http.MethodGet, path: "/v1/revisions/{revision_id}",
			handler: controller.RevisionGet{Getter: deps.PublicRevisionGetter}},
		{method: http.MethodGet, path: "/v1/needs-work",
			handler: controller.NeedsWorkListGet{Lister: deps.PublicNeedsWorkLister}},
		// A user's public repertoire is addressed by their public ID, never their user ID, and lists only the entries
		// they made public.
		{method: http.MethodGet, path: "/v1/users/{public_id}/repertoire",
			handler: controller.PublicRepertoireGet{Lister: deps.PublicRepertoireLister}},
		// The contact form needs no account: like the catalogue reads, it answers whatever credentials a request
		// carries. Turnstile, not authentication, keeps automated senders out. Its rate limit, by client IP, comes before
		// the body is read and Turnstile is asked.
		{method: http.MethodPost, path: "/v1/contact",
			handler: limitedByClientIP(deps.RateLimits, deps.ClientIP, deps.Now,
				controller.ContactSubmit{Command: deps.SubmitContactRequest})},

		{method: http.MethodGet, path: "/v1/me", handler: requireAuth(controller.MeGet{}), authenticated: true},
		{method: http.MethodPatch, path: "/v1/me", authenticated: true, handler: requireAuth(
			limitedByUser(deps.RateLimits.ProfileWrite, controller.MePatch{Command: deps.UpdateDisplayName}),
		)},

		// Moderation, for interactive moderators only. User IDs contain "|", which clients send percent-encoded
		// as %7C. The approve, reject and rebase routes coexist with the public GET /v1/revisions/{revision_id},
		// which has another method and path depth.
		{method: http.MethodPut, path: "/v1/users/{user_id}/blocked", authenticated: true,
			handler: requireInteractiveModerator(controller.UserBlockedSet{Command: deps.SetUserBlocked})},
		{method: http.MethodGet, path: "/v1/moderation/queue", authenticated: true,
			handler: requireInteractiveModerator(controller.ModerationQueueList{Command: deps.ListModerationQueue})},
		{method: http.MethodGet, path: "/v1/moderation/revisions/{revision_id}", authenticated: true,
			handler: requireInteractiveModerator(controller.ModerationRevisionGet{Command: deps.GetModerationRevision})},
		{method: http.MethodPost, path: "/v1/revisions/{revision_id}/approve", authenticated: true,
			handler: requireInteractiveModerator(controller.RevisionApprove{Command: deps.ApproveRevision})},
		{method: http.MethodPost, path: "/v1/revisions/{revision_id}/reject", authenticated: true,
			handler: requireInteractiveModerator(controller.RevisionReject{Command: deps.RejectRevision})},
		{method: http.MethodPost, path: "/v1/revisions/{revision_id}/rebase", authenticated: true,
			handler: requireInteractiveModerator(controller.RevisionRebase{Command: deps.RebaseRevision})},
		{method: http.MethodGet, path: "/v1/moderation/link-reports", authenticated: true,
			handler: requireInteractiveModerator(controller.LinkReportsList{Command: deps.ListLinkReports})},
		{method: http.MethodPost, path: "/v1/moderation/link-reports/{report_id}", authenticated: true,
			handler: requireInteractiveModerator(controller.LinkReportResolve{Command: deps.ResolveLinkReport})},
		{method: http.MethodGet, path: "/v1/moderation/links", authenticated: true,
			handler: requireInteractiveModerator(controller.ModerationLinkGet{Command: deps.GetModerationLink})},
		{method: http.MethodPut, path: "/v1/links/{link_id}/hidden", authenticated: true,
			handler: requireInteractiveModerator(controller.LinkHiddenSet{Command: deps.SetLinkHidden})},
		// Contact requests hold personal data, which only interactive moderators see.
		{method: http.MethodGet, path: "/v1/moderation/contact-requests", authenticated: true,
			handler: requireInteractiveModerator(controller.ContactRequestsList{Command: deps.ListContactRequests})},
		{method: http.MethodPost, path: "/v1/moderation/contact-requests/{request_id}", authenticated: true,
			handler: requireInteractiveModerator(controller.ContactRequestResolve{Command: deps.ResolveContactRequest})},
		// The ServeMux prefers this literal path to the {request_id} wildcard beside it.
		{method: http.MethodPost, path: "/v1/moderation/contact-requests/dismissals", authenticated: true,
			handler: requireInteractiveModerator(controller.ContactRequestsDismiss{Command: deps.DismissContactRequests})},

		// API tokens cannot manage API tokens, so a leaked token cannot be used to mint or revoke others.
		{method: http.MethodPost, path: "/v1/tokens", authenticated: true,
			handler: requireNonAPIToken(controller.APITokenCreate{Command: deps.CreateAPIToken})},
		{method: http.MethodGet, path: "/v1/tokens", authenticated: true,
			handler: requireNonAPIToken(controller.APITokenList{Lister: deps.APITokenLister})},
		{method: http.MethodDelete, path: "/v1/tokens/{token_id}", authenticated: true,
			handler: requireNonAPIToken(controller.APITokenRevoke{Revoker: deps.APITokenRevoker, Now: deps.Now})},

		// Revisions: the caller's own editable documents, drafts, submissions and revisions. API tokens act with
		// contributor powers, which the commands apply. The editable and pending documents of each entity
		// type have literal last segments, so they coexist with the public {slug} and history routes; placeholder
		// slugs address the caller's own pending new entities.
		editableRoute("pieces", "{slug}", domain.EntityTypePiece, deps),
		editableRoute("persons", "{slug}", domain.EntityTypePerson, deps),
		editableRoute("terms", "{slug}", domain.EntityTypeTerm, deps),
		editableRoute("events", "{slug}", domain.EntityTypeEvent, deps),
		editableRoute("collections", "{collection_id}", domain.EntityTypeCollection, deps),
		pendingRoute("pieces", "{slug}", domain.EntityTypePiece, deps),
		pendingRoute("persons", "{slug}", domain.EntityTypePerson, deps),
		pendingRoute("terms", "{slug}", domain.EntityTypeTerm, deps),
		pendingRoute("events", "{slug}", domain.EntityTypeEvent, deps),
		pendingRoute("collections", "{collection_id}", domain.EntityTypeCollection, deps),
		{method: http.MethodPost, path: "/v1/drafts", authenticated: true,
			handler: requireAuth(limitedByUser(deps.RateLimits.DraftWrite, controller.DraftCreate{Command: deps.SaveDraft}))},
		{method: http.MethodGet, path: draftPath, authenticated: true,
			handler: requireAuth(controller.DraftGet{Command: deps.GetDraft})},
		{method: http.MethodPut, path: draftPath, authenticated: true,
			handler: requireAuth(limitedByUser(deps.RateLimits.DraftWrite, controller.DraftUpdate{Command: deps.UpdateDraft}))},
		{method: http.MethodDelete, path: draftPath, authenticated: true,
			handler: requireAuth(controller.DraftDelete{Command: deps.DiscardDraft})},
		{method: http.MethodPost, path: "/v1/revisions", authenticated: true,
			handler: requireAuth(
				limitedByUser(deps.RateLimits.RevisionWrite, controller.RevisionSubmit{Command: deps.SubmitRevision}),
			)},
		{method: http.MethodPost, path: "/v1/revisions/{revision_id}/withdraw", authenticated: true,
			handler: requireAuth(
				limitedByUser(deps.RateLimits.RevisionWrite, controller.RevisionWithdraw{Command: deps.WithdrawRevision}),
			)},
		{method: http.MethodPost, path: "/v1/revisions/{revision_id}/revert", authenticated: true,
			handler: requireAuth(
				limitedByUser(deps.RateLimits.RevisionWrite, controller.RevisionRevert{Command: deps.RevertTo}),
			)},
		{method: http.MethodGet, path: "/v1/me/revisions", authenticated: true,
			handler: requireAuth(controller.MyRevisionsList{Command: deps.ListMyRevisions})},
		{method: http.MethodGet, path: "/v1/me/revisions/{revision_id}", authenticated: true,
			handler: requireAuth(controller.MyRevisionGet{Command: deps.GetMyRevision})},
		// Link reports address a link by the URL a public page shows: link IDs are not public.
		{method: http.MethodPost, path: "/v1/link-reports", authenticated: true,
			handler: requireAuth(
				limitedByUser(deps.RateLimits.LinkReport, controller.LinkReportCreate{Command: deps.ReportLink}),
			)},
		// The caller's own repertoire, with either credential.
		{method: http.MethodGet, path: "/v1/me/repertoire", authenticated: true,
			handler: requireAuth(controller.MyRepertoireList{Command: deps.ListRepertoire})},
		{method: http.MethodGet, path: repertoireEntryPath, authenticated: true,
			handler: requireAuth(controller.MyRepertoireEntryGet{Command: deps.GetRepertoireEntry})},
		{method: http.MethodPut, path: repertoireEntryPath, authenticated: true,
			handler: requireAuth(limitedByUser(
				deps.RateLimits.RepertoireWrite, controller.MyRepertoireEntryPut{Command: deps.PutRepertoireEntry},
			))},
		{method: http.MethodDelete, path: repertoireEntryPath, authenticated: true,
			handler: requireAuth(limitedByUser(
				deps.RateLimits.RepertoireWrite, controller.MyRepertoireEntryDelete{Command: deps.RemoveRepertoireEntry},
			))},
	}
}

// New returns the handler for every route. Each request passes through the request logger, then the security
// headers, then CORS, which answers preflight requests itself, before any authentication.
//
// Authenticated routes pass through the auth middleware and then the require* middleware for their route. The
// health checks and the public catalogue reads are not authenticated: they answer whatever credentials a request
// carries, as they would an anonymous request, so a client with an expired token, or a blocked user, can still
// read the catalogue. Requests no route matches get a JSON 404, or 405 if a route matches the path but not the
// method, without authenticating.
func New(deps Dependencies) http.Handler {
	r := newRouter(routes(deps), authMiddleware(deps.AuthValidators, deps.ResolveUser))
	return requestLogger(r.lookup)(securityHeaders(cors(strings.Join(r.methods, ", "))(r)))
}

// editableRoute is the route of GET /v1/<collection>/<key>/editable, the caller's editable document of an entity of
// entityType.
func editableRoute(collection, key string, entityType domain.EntityType, deps Dependencies) route {
	return route{method: http.MethodGet, path: "/v1/" + collection + "/" + key + "/editable", authenticated: true,
		handler: requireAuth(controller.EditableGet{Command: deps.GetEditable, EntityType: entityType})}
}

// pendingRoute is the route of GET /v1/<collection>/<key>/pending, the caller's pending revision of an entity of
// entityType.
func pendingRoute(collection, key string, entityType domain.EntityType, deps Dependencies) route {
	return route{method: http.MethodGet, path: "/v1/" + collection + "/" + key + "/pending", authenticated: true,
		handler: requireAuth(controller.PendingGet{Command: deps.GetPendingVersion, EntityType: entityType})}
}

// draftPath is the path of a draft, which three routes share.
const draftPath = "/v1/drafts/{draft_id}"

// repertoireEntryPath is the path of the caller's repertoire entry for a public piece, which three routes share.
const repertoireEntryPath = "/v1/me/repertoire/{slug}"
