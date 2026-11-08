package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// errDatabaseUnavailable is what a datasource or command fails with when the database cannot serve the request.
var errDatabaseUnavailable = fmt.Errorf("x: %w", datasources.ErrUnavailable)

// unavailableRow is one operation's request, with the controller whose datasource or command fails with
// errDatabaseUnavailable.
type unavailableRow struct {
	op      openapitest.Operation
	body    string
	query   string
	handler func(t *testing.T) http.Handler
}

// failing is a command that fails with errDatabaseUnavailable.
func failing[Req, Res any](t *testing.T) commandFunc[Req, Res] {
	var zero Res
	return returning[Req](t, true, zero, errDatabaseUnavailable)
}

// unavailablePathValues are the path values of the rows' requests.
var unavailablePathValues = map[string]string{
	slugVar: testPieceSlug, collectionIDVar: "12", revisionIDVar: "51", draftIDVar: "51", reportIDVar: "3",
	requestIDVar: "12", linkIDVar: "7", "user_id": "google-oauth2|456", "token_id": revokedUUID,
}

// The path variables of the moderation inboxes and links, and the UUID of the API token revoked.
const (
	reportIDVar  = "report_id"
	requestIDVar = "request_id"
	linkIDVar    = "link_id"
	revokedUUID  = "0b0e5a4c-6a43-4f8e-9d3f-1c2b3a4d5e6f"
)

// unavailableExempt are the operations without a row: those that read no database, and GET /v1/me, which reads
// it only in the auth middleware (TestRouter_UserLookupUnavailableIs503). /readyz has its own 503 test.
var unavailableExempt = []openapitest.Operation{
	{Method: http.MethodGet, Path: "/healthz"},
	{Method: http.MethodGet, Path: "/readyz"},
	{Method: http.MethodGet, Path: "/v1/me"},
}

func op(method, path string) openapitest.Operation {
	return openapitest.Operation{Method: method, Path: path}
}

func unavailableCatalogueRows() []unavailableRow {
	history := func(path string, entityType domain.SlugEntityType) unavailableRow {
		return unavailableRow{op: op(http.MethodGet, path), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicHistoryLister(t)
			l.EXPECT().ListPublicHistory(mock.Anything, entityType, mock.Anything, mock.Anything).
				Return(domain.RevisionHistory{}, errDatabaseUnavailable)
			return HistoryGet{Lister: l, EntityType: entityType}
		}}
	}

	return []unavailableRow{
		{op: op(http.MethodGet, "/v1/pieces"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicPieceLister(t)
			l.EXPECT().ListPublicPieces(mock.Anything, mock.Anything).Return(domain.PieceList{}, errDatabaseUnavailable)
			return PieceListGet{Lister: l}
		}},
		{op: op(http.MethodGet, "/v1/pieces/{slug}"), handler: func(t *testing.T) http.Handler {
			g := mocks.NewPublicPieceGetter(t)
			g.EXPECT().GetPublicPiece(mock.Anything, mock.Anything).Return(domain.Piece{}, errDatabaseUnavailable)
			return PieceGet{Getter: g}
		}},
		{op: op(http.MethodGet, "/v1/persons"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicPersonLister(t)
			l.EXPECT().ListPublicPersons(mock.Anything, mock.Anything).Return(domain.PersonList{}, errDatabaseUnavailable)
			return PersonListGet{Lister: l}
		}},
		{op: op(http.MethodGet, "/v1/persons/{slug}"), handler: func(t *testing.T) http.Handler {
			g := mocks.NewPublicPersonGetter(t)
			g.EXPECT().GetPublicPerson(mock.Anything, mock.Anything).Return(domain.Person{}, errDatabaseUnavailable)
			return PersonGet{Getter: g}
		}},
		{op: op(http.MethodGet, "/v1/terms"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicTermLister(t)
			l.EXPECT().ListPublicTerms(mock.Anything, mock.Anything).Return(nil, errDatabaseUnavailable)
			return TermListGet{Lister: l}
		}},
		{op: op(http.MethodGet, "/v1/terms/{slug}"), handler: func(t *testing.T) http.Handler {
			g := mocks.NewPublicTermGetter(t)
			g.EXPECT().GetPublicTerm(mock.Anything, mock.Anything, mock.Anything).
				Return(domain.Term{}, errDatabaseUnavailable)
			return TermGet{Getter: g}
		}},
		{op: op(http.MethodGet, "/v1/collections"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicCollectionLister(t)
			l.EXPECT().ListPublicCollections(mock.Anything, mock.Anything).
				Return(domain.CollectionList{}, errDatabaseUnavailable)
			return CollectionListGet{Lister: l}
		}},
		{op: op(http.MethodGet, "/v1/collections/{collection_id}"), handler: func(t *testing.T) http.Handler {
			g := mocks.NewPublicCollectionGetter(t)
			g.EXPECT().GetPublicCollection(mock.Anything, mock.Anything).
				Return(domain.Collection{}, errDatabaseUnavailable)
			return CollectionGet{Getter: g}
		}},
		{op: op(http.MethodGet, "/v1/events"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicEventLister(t)
			l.EXPECT().ListPublicEvents(mock.Anything, mock.Anything, mock.Anything).
				Return(domain.EventList{}, errDatabaseUnavailable)
			return EventListGet{Lister: l}
		}},
		{op: op(http.MethodGet, "/v1/events/{slug}"), handler: func(t *testing.T) http.Handler {
			g := mocks.NewPublicEventGetter(t)
			g.EXPECT().GetPublicEvent(mock.Anything, mock.Anything).Return(domain.Event{}, errDatabaseUnavailable)
			return EventGet{Getter: g}
		}},
		history("/v1/pieces/{slug}/history", domain.SlugEntityTypePiece),
		history("/v1/persons/{slug}/history", domain.SlugEntityTypePerson),
		history("/v1/terms/{slug}/history", domain.SlugEntityTypeTerm),
		history("/v1/events/{slug}/history", domain.SlugEntityTypeEvent),
		{op: op(http.MethodGet, "/v1/collections/{collection_id}/history"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicCollectionHistoryLister(t)
			l.EXPECT().ListPublicCollectionHistory(mock.Anything, mock.Anything, mock.Anything).
				Return(domain.RevisionHistory{}, errDatabaseUnavailable)
			return CollectionHistoryGet{Lister: l}
		}},
		{op: op(http.MethodGet, "/v1/persons/{slug}/performances"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicPersonPerformanceLister(t)
			l.EXPECT().ListPublicPersonPerformances(mock.Anything, mock.Anything, mock.Anything).
				Return(domain.PersonPerformanceList{}, errDatabaseUnavailable)
			return PersonPerformancesGet{Lister: l}
		}},
		{op: op(http.MethodGet, "/v1/collections/{collection_id}/citations"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicCollectionCitationLister(t)
			l.EXPECT().ListPublicCollectionCitations(mock.Anything, mock.Anything, mock.Anything).
				Return(domain.CitationList{}, errDatabaseUnavailable)
			return CollectionCitationsGet{Lister: l}
		}},
		{op: op(http.MethodGet, "/v1/events/{slug}/performances"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicEventPerformanceLister(t)
			l.EXPECT().ListPublicEventPerformances(mock.Anything, mock.Anything, mock.Anything).
				Return(domain.EventPerformanceList{}, errDatabaseUnavailable)
			return EventPerformancesGet{Lister: l}
		}},
		{op: op(http.MethodGet, "/v1/revisions/{revision_id}"), handler: func(t *testing.T) http.Handler {
			g := mocks.NewPublicRevisionGetter(t)
			g.EXPECT().GetPublicRevision(mock.Anything, mock.Anything).Return(nil, errDatabaseUnavailable)
			return RevisionGet{Getter: g}
		}},
		{op: op(http.MethodGet, "/v1/needs-work"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewPublicNeedsWorkLister(t)
			l.EXPECT().ListPublicNeedsWork(mock.Anything, mock.Anything, mock.Anything).
				Return(domain.NeedsWorkList{}, errDatabaseUnavailable)
			return NeedsWorkListGet{Lister: l}
		}},
	}
}

func unavailableAccountRows() []unavailableRow {
	return []unavailableRow{
		{op: op(http.MethodPost, contactPath), body: contactBody, handler: func(t *testing.T) http.Handler {
			return ContactSubmit{Command: failing[command.ContactSubmission, int64](t)}
		}},
		{op: op(http.MethodPatch, "/v1/me"), body: displayNameBody("Morwen"), handler: func(t *testing.T) http.Handler {
			return MePatch{Command: failing[command.UpdateDisplayNameRequest, domain.User](t)}
		}},
		{op: op(http.MethodPut, "/v1/users/{user_id}/blocked"), body: `{"blocked":true}`,
			handler: func(t *testing.T) http.Handler {
				return UserBlockedSet{Command: failing[command.SetUserBlockedRequest, domain.User](t)}
			}},
		{op: op(http.MethodPost, "/v1/tokens"), body: `{"name":"CI runner"}`, handler: func(t *testing.T) http.Handler {
			return APITokenCreate{Command: failing[command.CreateAPITokenRequest, command.CreateAPITokenResponse](t)}
		}},
		{op: op(http.MethodGet, "/v1/tokens"), handler: func(t *testing.T) http.Handler {
			l := mocks.NewActiveAPITokenLister(t)
			l.EXPECT().ListUserActiveAPITokens(mock.Anything, mock.Anything).Return(nil, errDatabaseUnavailable)
			return APITokenList{Lister: l}
		}},
		{op: op(http.MethodDelete, "/v1/tokens/{token_id}"), handler: func(t *testing.T) http.Handler {
			r := mocks.NewAPITokenRevoker(t)
			r.EXPECT().RevokeAPIToken(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return(errDatabaseUnavailable)
			return APITokenRevoke{Revoker: r, Now: testClock}
		}},
	}
}

func unavailableRevisionRows() []unavailableRow {
	var rows []unavailableRow
	for _, collection := range []struct {
		path       string
		entityType domain.EntityType
	}{
		{"/v1/pieces/{slug}", domain.EntityTypePiece}, {"/v1/persons/{slug}", domain.EntityTypePerson},
		{"/v1/terms/{slug}", domain.EntityTypeTerm}, {"/v1/events/{slug}", domain.EntityTypeEvent},
		{"/v1/collections/{collection_id}", domain.EntityTypeCollection},
	} {
		rows = append(rows,
			unavailableRow{op: op(http.MethodGet, collection.path+"/editable"),
				handler: func(t *testing.T) http.Handler {
					return EditableGet{Command: failing[command.EntityRequest, domain.Editable](t),
						EntityType: collection.entityType}
				}},
			unavailableRow{op: op(http.MethodGet, collection.path+"/pending"),
				handler: func(t *testing.T) http.Handler {
					return PendingGet{Command: failing[command.EntityRequest, domain.MyRevision](t),
						EntityType: collection.entityType}
				}})
	}

	return append(rows,
		unavailableRow{op: op(http.MethodPost, "/v1/drafts"), body: createDraftBody,
			handler: func(t *testing.T) http.Handler {
				return DraftCreate{Command: failing[command.SaveDraftRequest, command.SavedDraft](t)}
			}},
		unavailableRow{op: op(http.MethodGet, "/v1/drafts/{draft_id}"), handler: func(t *testing.T) http.Handler {
			return DraftGet{Command: failing[command.RevisionRequest, command.DraftResult](t)}
		}},
		unavailableRow{op: op(http.MethodPut, "/v1/drafts/{draft_id}"), body: updateDraftBody,
			handler: func(t *testing.T) http.Handler {
				return DraftUpdate{Command: failing[command.UpdateDraftRequest, command.DraftResult](t)}
			}},
		unavailableRow{op: op(http.MethodDelete, "/v1/drafts/{draft_id}"), handler: func(t *testing.T) http.Handler {
			return DraftDelete{Command: failing[command.RevisionRequest, command.Empty](t)}
		}},
		unavailableRow{op: op(http.MethodPost, "/v1/revisions"), body: submitBody,
			handler: func(t *testing.T) http.Handler {
				return RevisionSubmit{Command: failing[command.SubmitRevisionRequest, command.SubmittedRevision](t)}
			}},
		unavailableRow{op: op(http.MethodPost, "/v1/revisions/{revision_id}/withdraw"),
			handler: func(t *testing.T) http.Handler {
				return RevisionWithdraw{Command: failing[command.RevisionRequest, domain.MyRevision](t)}
			}},
		unavailableRow{op: op(http.MethodPost, "/v1/revisions/{revision_id}/revert"), body: revertBody,
			handler: func(t *testing.T) http.Handler {
				return RevisionRevert{Command: failing[command.RevertRequest, command.SubmittedRevision](t)}
			}},
		unavailableRow{op: op(http.MethodGet, "/v1/me/revisions"), handler: func(t *testing.T) http.Handler {
			return MyRevisionsList{Command: failing[command.ListMyRevisionsRequest, domain.MyRevisionList](t)}
		}},
		unavailableRow{op: op(http.MethodGet, "/v1/me/revisions/{revision_id}"),
			handler: func(t *testing.T) http.Handler {
				return MyRevisionGet{Command: failing[command.RevisionRequest, domain.MyRevision](t)}
			}},
		unavailableRow{op: op(http.MethodPost, "/v1/link-reports"),
			body: `{"url":"` + testLinkURL + `","reason":"broken","message":null}`,
			handler: func(t *testing.T) http.Handler {
				return LinkReportCreate{Command: failing[command.ReportLinkRequest, command.ReportedLink](t)}
			}},
	)
}

func unavailableModerationRows() []unavailableRow {
	return []unavailableRow{
		{op: op(http.MethodGet, "/v1/moderation/queue"), handler: func(t *testing.T) http.Handler {
			return ModerationQueueList{Command: failing[command.ModerationQueueRequest, domain.ModerationQueue](t)}
		}},
		{op: op(http.MethodGet, "/v1/moderation/revisions/{revision_id}"), handler: func(t *testing.T) http.Handler {
			return ModerationRevisionGet{Command: failing[command.RevisionRequest, domain.ModerationRevision](t)}
		}},
		{op: op(http.MethodPost, "/v1/revisions/{revision_id}/approve"), body: approveBody,
			handler: func(t *testing.T) http.Handler {
				return RevisionApprove{Command: failing[command.ApproveRevisionRequest, domain.ModerationRevision](t)}
			}},
		{op: op(http.MethodPost, "/v1/revisions/{revision_id}/reject"), body: rejectBody,
			handler: func(t *testing.T) http.Handler {
				return RevisionReject{Command: failing[command.RejectRevisionRequest, domain.ModerationRevision](t)}
			}},
		{op: op(http.MethodPost, "/v1/revisions/{revision_id}/rebase"), body: rebaseBody,
			handler: func(t *testing.T) http.Handler {
				return RevisionRebase{Command: failing[command.RebaseRevisionRequest, domain.ModerationRevision](t)}
			}},
		{op: op(http.MethodGet, linkReportsPath), handler: func(t *testing.T) http.Handler {
			return LinkReportsList{Command: failing[command.LinkReportListRequest, domain.LinkReportList](t)}
		}},
		{op: op(http.MethodPost, linkReportsPath+"/{report_id}"), body: `{"status":"resolved","note":null,"hide_link":true}`,
			handler: func(t *testing.T) http.Handler {
				return LinkReportResolve{Command: failing[command.ResolveLinkReportRequest, domain.LinkReport](t)}
			}},
		{op: op(http.MethodGet, "/v1/moderation/links"), query: "url=" + testLinkURL,
			handler: func(t *testing.T) http.Handler {
				return ModerationLinkGet{Command: failing[command.ModerationLinkRequest, domain.ModerationLink](t)}
			}},
		{op: op(http.MethodPut, "/v1/links/{link_id}/hidden"), body: `{"hidden":true}`,
			handler: func(t *testing.T) http.Handler {
				return LinkHiddenSet{Command: failing[command.SetLinkHiddenRequest, domain.ModerationLink](t)}
			}},
		{op: op(http.MethodGet, "/v1/moderation/contact-requests"), handler: func(t *testing.T) http.Handler {
			return ContactRequestsList{
				Command: failing[command.ContactRequestListRequest, domain.ContactRequestList](t),
			}
		}},
		{op: op(http.MethodPost, "/v1/moderation/contact-requests/{request_id}"),
			body: `{"status":"resolved","note":"Credit corrected."}`, handler: func(t *testing.T) http.Handler {
				return ContactRequestResolve{
					Command: failing[command.ResolveContactRequestRequest, domain.ContactRequest](t),
				}
			}},
	}
}

func unavailableRows() []unavailableRow {
	return slices.Concat(unavailableCatalogueRows(), unavailableAccountRows(), unavailableRevisionRows(),
		unavailableModerationRows())
}

// pathVariable matches a path template's variables.
var pathVariable = regexp.MustCompile(`\{([a-z_]+)\}`)

// TestControllers_DatabaseUnavailableIs503 checks that every operation that reads or writes the database answers
// 503 unavailable, as the spec documents for it, when its datasource or command fails because the database cannot
// serve the request, and that every such operation has a row.
func TestControllers_DatabaseUnavailableIs503(t *testing.T) {
	t.Parallel()

	rows := unavailableRows()
	var covered []openapitest.Operation
	for _, row := range rows {
		covered = append(covered, row.op)
		t.Run(row.op.Method+" "+row.op.Path, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{}
			target := pathVariable.ReplaceAllStringFunc(row.op.Path, func(v string) string {
				name := v[1 : len(v)-1]
				vars[name] = unavailablePathValues[name]
				return url.PathEscape(unavailablePathValues[name])
			})
			if row.query != "" {
				target += "?" + row.query
			}
			serveRevisionCase(t, row.handler(t), revisionCase{method: row.op.Method, target: target,
				pathValues: vars, body: row.body, anonymous: openapitest.New(t).Public(row.op),
				wantStatus: http.StatusServiceUnavailable, wantBody: unavailableBody})
		})
	}

	for _, documented := range openapitest.New(t).Operations() {
		if !slices.Contains(unavailableExempt, documented) {
			assert.Contains(t, covered, documented, "%s %s has no row", documented.Method, documented.Path)
		}
	}
}
