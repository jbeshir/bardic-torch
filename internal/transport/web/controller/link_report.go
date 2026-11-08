package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// linkNotFoundMessage is the message of a 404 for a link, the same whether the link is hidden or unknown.
const linkNotFoundMessage = "link not found"

// LinkReportCreateRequest is the JSON body of a link report: the URL a public page shows, why, and an optional
// message.
type LinkReportCreateRequest struct {
	URL     string                  `json:"url"`
	Reason  domain.LinkReportReason `json:"reason"`
	Message *string                 `json:"message"`
}

// LinkReportReceipt is the JSON representation of what a reporter learns of their report.
type LinkReportReceipt struct {
	ID        int64                   `json:"id"`
	Status    domain.LinkReportStatus `json:"status"`
	CreatedAt time.Time               `json:"created_at"`
}

// LinkReportCreate handles POST /v1/link-reports, reporting a visible link: 201 with the new report, or 200 with
// the caller's open report of the link. A hidden link is not found, exactly like an unknown URL.
type LinkReportCreate struct {
	Command command.Command[command.ReportLinkRequest, command.ReportedLink]
}

func (c LinkReportCreate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	var body LinkReportCreateRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	switch {
	case body.URL == "":
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest, "url is required")
		return
	case !textOrBadRequest(w, r, "url", &body.URL, domain.MaxURLLength):
		return
	case !body.Reason.Valid():
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid reason: not one of its allowed values")
		return
	case !textOrBadRequest(w, r, "message", body.Message, domain.MaxLinkReportMessageLength):
		return
	}

	reported, err := c.Command.Execute(ctx, command.ReportLinkRequest{
		Principal: principal, URL: body.URL, Reason: body.Reason, Message: body.Message,
	})
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, linkNotFoundMessage)
	case err != nil:
		writeRevisionError(w, r, "link report", err)
	default:
		status := http.StatusOK
		if reported.Created {
			status = http.StatusCreated
		}
		receipt := reported.Receipt
		httpjson.WriteJSON(ctx, w, status, LinkReportReceipt{
			ID: receipt.ID, Status: receipt.Status, CreatedAt: receipt.CreatedAt,
		})
	}
}

// LinkReportsList handles GET /v1/moderation/link-reports, one page of the link reports, oldest first, optionally
// of one status.
type LinkReportsList struct {
	Command command.Command[command.LinkReportListRequest, domain.LinkReportList]
}

func (c LinkReportsList) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	status, ok := enumOrBadRequest(w, r, query, "status", domain.ParseLinkReportStatus)
	if !ok {
		return
	}
	page, ok := pageOrBadRequest(w, r, query)
	if !ok {
		return
	}

	list, err := c.Command.Execute(r.Context(), command.LinkReportListRequest{
		Principal: principal, Query: datasources.LinkReportQuery{Status: status, Page: page},
	})
	if err != nil {
		writeRevisionError(w, r, "link reports", err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, LinkReportList{
		Data: mapSlice(list.Reports, linkReportFromDomain), Total: list.Total,
	})
}

// LinkReportResolveRequest is the JSON body of a link report's resolution: resolved or dismissed, an optional note,
// and whether to hide the link too.
type LinkReportResolveRequest struct {
	Status   domain.LinkReportStatus `json:"status"`
	Note     *string                 `json:"note"`
	HideLink bool                    `json:"hide_link"`
}

// LinkReportResolve handles POST /v1/moderation/link-reports/{report_id}, resolving or dismissing an open report.
type LinkReportResolve struct {
	Command command.Command[command.ResolveLinkReportRequest, domain.LinkReport]
}

func (c LinkReportResolve) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	reportID, ok := idOrBadRequest(w, r, "report_id")
	if !ok {
		return
	}
	var body LinkReportResolveRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	if body.Status != domain.LinkReportStatusResolved && body.Status != domain.LinkReportStatusDismissed {
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid status: must be resolved or dismissed")
		return
	}
	if !textOrBadRequest(w, r, "note", body.Note, domain.MaxModeratorNoteLength) {
		return
	}

	report, err := c.Command.Execute(ctx, command.ResolveLinkReportRequest{
		Principal: principal, ReportID: reportID, Status: body.Status, Note: body.Note, HideLink: body.HideLink,
	})
	if err != nil {
		writeRevisionError(w, r, "link report", err)
		return
	}
	httpjson.WriteJSON(ctx, w, http.StatusOK, linkReportFromDomain(report))
}

// ModerationLinkGet handles GET /v1/moderation/links?url=, any link by URL as moderators see it.
type ModerationLinkGet struct {
	Command command.Command[command.ModerationLinkRequest, domain.ModerationLink]
}

func (c ModerationLinkGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	url, set, ok := singleOrBadRequest(w, r, query, "url")
	if !ok {
		return
	}
	if !set || url == "" {
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"query parameter url is required")
		return
	}
	if !textOrBadRequest(w, r, "url", &url, domain.MaxURLLength) {
		return
	}

	link, err := c.Command.Execute(ctx, command.ModerationLinkRequest{Principal: principal, URL: url})
	if err != nil {
		writeRevisionError(w, r, "link", err)
		return
	}
	httpjson.WriteJSON(ctx, w, http.StatusOK, moderationLinkFromDomain(link))
}

// LinkHiddenSetRequest is the JSON body of a request to hide or show a link. Hidden is a pointer so that a missing
// value is rejected rather than read as false, which would show the link.
type LinkHiddenSetRequest struct {
	Hidden *bool `json:"hidden"`
}

// LinkHiddenSet handles PUT /v1/links/{link_id}/hidden, hiding or showing a link; hiding it resolves its open
// reports.
type LinkHiddenSet struct {
	Command command.Command[command.SetLinkHiddenRequest, domain.ModerationLink]
}

func (c LinkHiddenSet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	linkID, ok := idOrBadRequest(w, r, "link_id")
	if !ok {
		return
	}
	var body LinkHiddenSetRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	if body.Hidden == nil {
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest, "hidden is required")
		return
	}

	link, err := c.Command.Execute(ctx, command.SetLinkHiddenRequest{
		Principal: principal, LinkID: linkID, Hidden: *body.Hidden,
	})
	if err != nil {
		writeRevisionError(w, r, "link", err)
		return
	}
	domain.LoggerFromContext(ctx).InfoContext(ctx, "set link hidden", "link_id", link.LinkID, "hidden", *body.Hidden)
	httpjson.WriteJSON(ctx, w, http.StatusOK, moderationLinkFromDomain(link))
}
