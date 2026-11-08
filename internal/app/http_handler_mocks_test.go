package app

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
	"github.com/stretchr/testify/mock"
)

var handlerNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// Users the fake validators authenticate as, by the ID after their credential prefix.
const (
	contributorID  = "google-oauth2|contributor"
	moderatorID    = "google-oauth2|moderator"
	blockedID      = "google-oauth2|blocked"
	unresolvableID = "google-oauth2|unresolvable"
	invalidSecret  = "invalid"
	// unverifiableSecret is a credential the validator cannot check, as when Auth0 keys cannot be fetched.
	unverifiableSecret = "unverifiable"
)

// prefixValidator authenticates "Bearer <prefix><user ID>" as the user ID with method, and rejects
// "Bearer <prefix>invalid" and fails to check "Bearer <prefix>unverifiable".
func prefixValidator(prefix string, method domain.AuthMethod) router.AuthValidator {
	return func(r *http.Request) (*router.Identity, error) {
		userID, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "+prefix)
		switch {
		case !ok:
			return nil, nil
		case userID == invalidSecret:
			return nil, fmt.Errorf("%w: test credential", router.ErrInvalidCredentials)
		case userID == unverifiableSecret:
			return nil, errors.New("signing keys unavailable")
		default:
			return &router.Identity{UserID: userID, Method: method}, nil
		}
	}
}

// handlerMocks are the datasources behind a handler built with NewHTTPHandler, authenticating with
// prefixValidator for both Auth0 ("auth0|") and API tokens.
type handlerMocks struct {
	pinger              *mocks.Pinger
	users               *mocks.UserGetter
	displayNames        *mocks.DisplayNameUpdater
	blocker             *mocks.UserBlocker
	unblocker           *mocks.UserUnblocker
	counter             *mocks.ActiveAPITokenCounter
	creator             *mocks.APITokenCreator
	lister              *mocks.ActiveAPITokenLister
	revoker             *mocks.APITokenRevoker
	pieceLister         *mocks.PublicPieceLister
	pieces              *mocks.PublicPieceGetter
	personLister        *mocks.PublicPersonLister
	persons             *mocks.PublicPersonGetter
	termLister          *mocks.PublicTermLister
	terms               *mocks.PublicTermGetter
	collectionLister    *mocks.PublicCollectionLister
	collections         *mocks.PublicCollectionGetter
	eventLister         *mocks.PublicEventLister
	events              *mocks.PublicEventGetter
	history             *mocks.PublicHistoryLister
	collectionHistory   *mocks.PublicCollectionHistoryLister
	revisions           *mocks.PublicRevisionGetter
	needsWork           *mocks.PublicNeedsWorkLister
	personPerformances  *mocks.PublicPersonPerformanceLister
	collectionCitations *mocks.PublicCollectionCitationLister
	eventPerformances   *mocks.PublicEventPerformanceLister
	publicRepertoire    *mocks.PublicRepertoireLister
	repertoire          *mocks.RepertoireLister
	repertoireEntries   *mocks.RepertoireEntryGetter
	repertoirePutter    *mocks.RepertoireEntryPutter
	repertoireDeleter   *mocks.RepertoireEntryDeleter
	refs                *mocks.EditorRefResolver
	editable            *mocks.EditableDocumentGetter
	draftSaver          *mocks.DraftSaver
	draftUpdater        *mocks.DraftUpdater
	drafts              *mocks.DraftGetter
	draftDiscarder      *mocks.DraftDiscarder
	duplicates          *mocks.DuplicateFinder
	submitter           *mocks.RevisionSubmitter
	withdrawer          *mocks.RevisionWithdrawer
	reverter            *mocks.RevisionReverter
	myRevisionLister    *mocks.MyRevisionLister
	myRevisions         *mocks.MyRevisionGetter
	pending             *mocks.PendingVersionGetter
	queue               *mocks.ModerationQueueLister
	moderation          *mocks.ModerationRevisionGetter
	moderationLinks     *mocks.ModerationLinkGetter
	approver            *mocks.RevisionApprover
	rejecter            *mocks.RevisionRejecter
	rebaser             *mocks.RevisionRebaser
	reporter            *mocks.LinkReporter
	reportLister        *mocks.LinkReportLister
	reportResolver      *mocks.LinkReportResolver
	linkHider           *mocks.LinkHiddenSetter
	contactCreator      *mocks.ContactRequestCreator
	contactLister       *mocks.ContactRequestLister
	contactResolver     *mocks.ContactRequestResolver
	contactPurger       *mocks.ContactRequestPurger
	turnstile           *mocks.TurnstileVerifier
	handler             http.Handler
	validate            *openapitest.Validator
}

func newHandlerMocks(t *testing.T) handlerMocks {
	m := handlerMocks{
		pinger:              mocks.NewPinger(t),
		users:               mocks.NewUserGetter(t),
		displayNames:        mocks.NewDisplayNameUpdater(t),
		blocker:             mocks.NewUserBlocker(t),
		unblocker:           mocks.NewUserUnblocker(t),
		counter:             mocks.NewActiveAPITokenCounter(t),
		creator:             mocks.NewAPITokenCreator(t),
		lister:              mocks.NewActiveAPITokenLister(t),
		revoker:             mocks.NewAPITokenRevoker(t),
		pieceLister:         mocks.NewPublicPieceLister(t),
		pieces:              mocks.NewPublicPieceGetter(t),
		personLister:        mocks.NewPublicPersonLister(t),
		persons:             mocks.NewPublicPersonGetter(t),
		termLister:          mocks.NewPublicTermLister(t),
		terms:               mocks.NewPublicTermGetter(t),
		collectionLister:    mocks.NewPublicCollectionLister(t),
		collections:         mocks.NewPublicCollectionGetter(t),
		eventLister:         mocks.NewPublicEventLister(t),
		events:              mocks.NewPublicEventGetter(t),
		history:             mocks.NewPublicHistoryLister(t),
		collectionHistory:   mocks.NewPublicCollectionHistoryLister(t),
		revisions:           mocks.NewPublicRevisionGetter(t),
		needsWork:           mocks.NewPublicNeedsWorkLister(t),
		personPerformances:  mocks.NewPublicPersonPerformanceLister(t),
		collectionCitations: mocks.NewPublicCollectionCitationLister(t),
		eventPerformances:   mocks.NewPublicEventPerformanceLister(t),
		publicRepertoire:    mocks.NewPublicRepertoireLister(t),
		repertoire:          mocks.NewRepertoireLister(t),
		repertoireEntries:   mocks.NewRepertoireEntryGetter(t),
		repertoirePutter:    mocks.NewRepertoireEntryPutter(t),
		repertoireDeleter:   mocks.NewRepertoireEntryDeleter(t),
		refs:                mocks.NewEditorRefResolver(t),
		editable:            mocks.NewEditableDocumentGetter(t),
		draftSaver:          mocks.NewDraftSaver(t),
		draftUpdater:        mocks.NewDraftUpdater(t),
		drafts:              mocks.NewDraftGetter(t),
		draftDiscarder:      mocks.NewDraftDiscarder(t),
		duplicates:          mocks.NewDuplicateFinder(t),
		submitter:           mocks.NewRevisionSubmitter(t),
		withdrawer:          mocks.NewRevisionWithdrawer(t),
		reverter:            mocks.NewRevisionReverter(t),
		myRevisionLister:    mocks.NewMyRevisionLister(t),
		myRevisions:         mocks.NewMyRevisionGetter(t),
		pending:             mocks.NewPendingVersionGetter(t),
		queue:               mocks.NewModerationQueueLister(t),
		moderation:          mocks.NewModerationRevisionGetter(t),
		moderationLinks:     mocks.NewModerationLinkGetter(t),
		approver:            mocks.NewRevisionApprover(t),
		rejecter:            mocks.NewRevisionRejecter(t),
		rebaser:             mocks.NewRevisionRebaser(t),
		reporter:            mocks.NewLinkReporter(t),
		reportLister:        mocks.NewLinkReportLister(t),
		reportResolver:      mocks.NewLinkReportResolver(t),
		linkHider:           mocks.NewLinkHiddenSetter(t),
		contactCreator:      mocks.NewContactRequestCreator(t),
		contactLister:       mocks.NewContactRequestLister(t),
		contactResolver:     mocks.NewContactRequestResolver(t),
		contactPurger:       mocks.NewContactRequestPurger(t),
		turnstile:           mocks.NewTurnstileVerifier(t),
		validate:            openapitest.New(t),
	}
	m.handler = NewHTTPHandler(HTTPDependencies{
		Pinger:     m.pinger,
		RateLimits: unlimitedRateLimits(),
		AuthValidators: []router.AuthValidator{
			prefixValidator("auth0|", domain.AuthMethodAuth0),
			prefixValidator(domain.APITokenPrefix, domain.AuthMethodAPIToken),
		},
		UserGetter:                     m.users,
		UserEnsurer:                    mocks.NewUserEnsurer(t),
		DisplayNameUpdater:             m.displayNames,
		UserBlocker:                    m.blocker,
		UserUnblocker:                  m.unblocker,
		APITokenCreator:                m.creator,
		ActiveAPITokenCounter:          m.counter,
		ActiveAPITokenLister:           m.lister,
		APITokenRevoker:                m.revoker,
		PublicPieceLister:              m.pieceLister,
		PublicPieceGetter:              m.pieces,
		PublicPersonLister:             m.personLister,
		PublicPersonGetter:             m.persons,
		PublicTermLister:               m.termLister,
		PublicTermGetter:               m.terms,
		PublicCollectionLister:         m.collectionLister,
		PublicCollectionGetter:         m.collections,
		PublicEventLister:              m.eventLister,
		PublicEventGetter:              m.events,
		PublicHistoryLister:            m.history,
		PublicCollectionHistoryLister:  m.collectionHistory,
		PublicRevisionGetter:           m.revisions,
		PublicNeedsWorkLister:          m.needsWork,
		PublicPersonPerformanceLister:  m.personPerformances,
		PublicCollectionCitationLister: m.collectionCitations,
		PublicEventPerformanceLister:   m.eventPerformances,
		PublicRepertoireLister:         m.publicRepertoire,
		RepertoireLister:               m.repertoire,
		RepertoireEntryGetter:          m.repertoireEntries,
		RepertoireEntryPutter:          m.repertoirePutter,
		RepertoireEntryDeleter:         m.repertoireDeleter,
		EditorRefResolver:              m.refs,
		EditableDocumentGetter:         m.editable,
		DraftSaver:                     m.draftSaver,
		DraftUpdater:                   m.draftUpdater,
		DraftGetter:                    m.drafts,
		DraftDiscarder:                 m.draftDiscarder,
		DuplicateFinder:                m.duplicates,
		RevisionSubmitter:              m.submitter,
		RevisionWithdrawer:             m.withdrawer,
		RevisionReverter:               m.reverter,
		MyRevisionLister:               m.myRevisionLister,
		MyRevisionGetter:               m.myRevisions,
		PendingVersionGetter:           m.pending,
		ModerationQueueLister:          m.queue,
		ModerationRevisionGetter:       m.moderation,
		ModerationLinkGetter:           m.moderationLinks,
		RevisionApprover:               m.approver,
		RevisionRejecter:               m.rejecter,
		RevisionRebaser:                m.rebaser,
		LinkReporter:                   m.reporter,
		LinkReportLister:               m.reportLister,
		LinkReportResolver:             m.reportResolver,
		LinkHiddenSetter:               m.linkHider,
		ContactRequestCreator:          m.contactCreator,
		ContactRequestLister:           m.contactLister,
		ContactRequestResolver:         m.contactResolver,
		ContactRequestPurger:           m.contactPurger,
		TurnstileVerifier:              m.turnstile,
		Now: func() time.Time {
			return handlerNow
		},
	})
	return m
}

// storedUser is the user with userID as the datasource holds it: a contributor, unless userID is moderatorID
// or blockedID.
func storedUser(userID string) domain.User {
	user := domain.User{ID: userID, PublicID: matrixPublicID, DisplayName: "Morwen", Role: domain.RoleContributor,
		CreatedAt: handlerNow}
	switch userID {
	case moderatorID:
		user.Role = domain.RoleModerator
	case blockedID:
		blockedAt := handlerNow.Add(-time.Hour)
		user.BlockedAt = &blockedAt
	}
	return user
}

// expectUser expects the auth middleware to resolve userID, failing for unresolvableID.
func (m handlerMocks) expectUser(userID string) {
	if userID == unresolvableID {
		m.users.EXPECT().GetUser(mock.Anything, userID).Return(domain.User{}, errors.New("connection refused")).Once()
		return
	}
	m.users.EXPECT().GetUser(mock.Anything, userID).Return(storedUser(userID), nil).Once()
}

// errorResponse is the JSON body of an error response.
type errorResponse struct {
	Code httpjson.ErrorCode `json:"code"`
}
