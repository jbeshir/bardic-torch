package app

import (
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
)

// HTTPDependencies are what the HTTP handler is built from: the datasources, the validators that authenticate
// requests, and the clock.
type HTTPDependencies struct {
	Pinger                        datasources.Pinger
	AuthValidators                []router.AuthValidator
	UserGetter                    datasources.UserGetter
	UserEnsurer                   datasources.UserEnsurer
	DisplayNameUpdater            datasources.DisplayNameUpdater
	UserBlocker                   datasources.UserBlocker
	UserUnblocker                 datasources.UserUnblocker
	APITokenCreator               datasources.APITokenCreator
	ActiveAPITokenCounter         datasources.ActiveAPITokenCounter
	ActiveAPITokenLister          datasources.ActiveAPITokenLister
	APITokenRevoker               datasources.APITokenRevoker
	PublicPieceLister             datasources.PublicPieceLister
	PublicPieceGetter             datasources.PublicPieceGetter
	PublicPersonLister            datasources.PublicPersonLister
	PublicPersonGetter            datasources.PublicPersonGetter
	PublicTermLister              datasources.PublicTermLister
	PublicTermGetter              datasources.PublicTermGetter
	PublicCollectionLister        datasources.PublicCollectionLister
	PublicCollectionGetter        datasources.PublicCollectionGetter
	PublicEventLister             datasources.PublicEventLister
	PublicEventGetter             datasources.PublicEventGetter
	PublicHistoryLister           datasources.PublicHistoryLister
	PublicCollectionHistoryLister datasources.PublicCollectionHistoryLister
	PublicRevisionGetter          datasources.PublicRevisionGetter
	PublicNeedsWorkLister         datasources.PublicNeedsWorkLister
	EditorRefResolver             datasources.EditorRefResolver
	EditableDocumentGetter        datasources.EditableDocumentGetter
	DraftSaver                    datasources.DraftSaver
	DraftUpdater                  datasources.DraftUpdater
	DraftGetter                   datasources.DraftGetter
	DraftDiscarder                datasources.DraftDiscarder
	DuplicateFinder               datasources.DuplicateFinder
	RevisionSubmitter             datasources.RevisionSubmitter
	RevisionWithdrawer            datasources.RevisionWithdrawer
	RevisionReverter              datasources.RevisionReverter
	MyRevisionLister              datasources.MyRevisionLister
	MyRevisionGetter              datasources.MyRevisionGetter
	PendingVersionGetter          datasources.PendingVersionGetter
	ModerationQueueLister         datasources.ModerationQueueLister
	ModerationRevisionGetter      datasources.ModerationRevisionGetter
	ModerationLinkGetter          datasources.ModerationLinkGetter
	RevisionApprover              datasources.RevisionApprover
	RevisionRejecter              datasources.RevisionRejecter
	RevisionRebaser               datasources.RevisionRebaser
	LinkReporter                  datasources.LinkReporter
	LinkReportLister              datasources.LinkReportLister
	LinkReportResolver            datasources.LinkReportResolver
	LinkHiddenSetter              datasources.LinkHiddenSetter
	Now                           func() time.Time
}

// NewHTTPHandler builds the production HTTP handler from deps. Setup and tests both use it, so tests exercise
// the same wiring as production.
func NewHTTPHandler(deps HTTPDependencies) http.Handler {
	return router.New(router.Dependencies{
		Pinger:         deps.Pinger,
		AuthValidators: deps.AuthValidators,
		ResolveUser: &command.ResolveUser{
			Getter:  deps.UserGetter,
			Ensurer: deps.UserEnsurer,
			Now:     deps.Now,
		},
		UpdateDisplayName: &command.UpdateDisplayName{
			Updater: deps.DisplayNameUpdater,
			Getter:  deps.UserGetter,
		},
		SetUserBlocked: &command.SetUserBlocked{
			Blocker:   deps.UserBlocker,
			Unblocker: deps.UserUnblocker,
			Getter:    deps.UserGetter,
			Now:       deps.Now,
		},
		CreateAPIToken: &command.CreateAPIToken{
			Counter: deps.ActiveAPITokenCounter,
			Creator: deps.APITokenCreator,
			Now:     deps.Now,
		},
		APITokenLister:                deps.ActiveAPITokenLister,
		APITokenRevoker:               deps.APITokenRevoker,
		PublicPieceLister:             deps.PublicPieceLister,
		PublicPieceGetter:             deps.PublicPieceGetter,
		PublicPersonLister:            deps.PublicPersonLister,
		PublicPersonGetter:            deps.PublicPersonGetter,
		PublicTermLister:              deps.PublicTermLister,
		PublicTermGetter:              deps.PublicTermGetter,
		PublicCollectionLister:        deps.PublicCollectionLister,
		PublicCollectionGetter:        deps.PublicCollectionGetter,
		PublicEventLister:             deps.PublicEventLister,
		PublicEventGetter:             deps.PublicEventGetter,
		PublicHistoryLister:           deps.PublicHistoryLister,
		PublicCollectionHistoryLister: deps.PublicCollectionHistoryLister,
		PublicRevisionGetter:          deps.PublicRevisionGetter,
		PublicNeedsWorkLister:         deps.PublicNeedsWorkLister,
		GetEditable:                   &command.GetEditable{Getter: deps.EditableDocumentGetter},
		GetPendingVersion:             &command.GetPendingVersion{Getter: deps.PendingVersionGetter},
		SaveDraft: &command.SaveDraft{
			Resolver: deps.EditorRefResolver, Saver: deps.DraftSaver, Duplicates: deps.DuplicateFinder, Now: deps.Now,
		},
		UpdateDraft: &command.UpdateDraft{
			Resolver: deps.EditorRefResolver, Updater: deps.DraftUpdater, Duplicates: deps.DuplicateFinder,
			Now: deps.Now,
		},
		GetDraft:     &command.GetDraft{Getter: deps.DraftGetter, Duplicates: deps.DuplicateFinder},
		DiscardDraft: &command.DiscardDraft{Discarder: deps.DraftDiscarder},
		SubmitRevision: &command.SubmitRevision{
			Drafts: deps.DraftGetter, Duplicates: deps.DuplicateFinder, Submitter: deps.RevisionSubmitter,
			Revisions: deps.MyRevisionGetter, Now: deps.Now,
		},
		WithdrawRevision: &command.WithdrawRevision{
			Withdrawer: deps.RevisionWithdrawer, Revisions: deps.MyRevisionGetter,
		},
		RevertTo: &command.RevertTo{
			Reverter: deps.RevisionReverter, Revisions: deps.MyRevisionGetter, Now: deps.Now,
		},
		ListMyRevisions:       &command.ListMyRevisions{Lister: deps.MyRevisionLister},
		GetMyRevision:         &command.GetMyRevision{Getter: deps.MyRevisionGetter},
		ReportLink:            &command.ReportLink{Reporter: deps.LinkReporter, Now: deps.Now},
		ListModerationQueue:   &command.ListModerationQueue{Lister: deps.ModerationQueueLister, Now: deps.Now},
		GetModerationRevision: &command.GetModerationRevision{Getter: deps.ModerationRevisionGetter},
		ApproveRevision: &command.ApproveRevision{
			Revisions: deps.ModerationRevisionGetter, Approver: deps.RevisionApprover, Now: deps.Now,
		},
		RejectRevision: &command.RejectRevision{
			Revisions: deps.ModerationRevisionGetter, Rejecter: deps.RevisionRejecter, Now: deps.Now,
		},
		RebaseRevision: &command.RebaseRevision{
			Resolver: deps.EditorRefResolver, Revisions: deps.ModerationRevisionGetter, Rebaser: deps.RevisionRebaser,
			Now: deps.Now,
		},
		ListLinkReports:   &command.ListLinkReports{Lister: deps.LinkReportLister},
		ResolveLinkReport: &command.ResolveLinkReport{Resolver: deps.LinkReportResolver, Now: deps.Now},
		GetModerationLink: &command.GetModerationLink{Getter: deps.ModerationLinkGetter},
		SetLinkHidden:     &command.SetLinkHidden{Setter: deps.LinkHiddenSetter, Now: deps.Now},
		Now:               deps.Now,
	})
}
