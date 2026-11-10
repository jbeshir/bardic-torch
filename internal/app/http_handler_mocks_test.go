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

// prefixValidator authenticates "Bearer <prefix><user ID>" as the user ID, and rejects
// "Bearer <prefix>invalid" and fails to check "Bearer <prefix>unverifiable".
func prefixValidator(prefix string) router.AuthValidator {
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
			return &router.Identity{UserID: userID}, nil
		}
	}
}

// handlerMocks are the datasources behind a handler built with NewHTTPHandler, authenticating with
// prefixValidator for Auth0 ("auth0|").
type handlerMocks struct {
	pinger            *mocks.Pinger
	users             *mocks.UserGetter
	displayNames      *mocks.DisplayNameUpdater
	blocker           *mocks.UserBlocker
	unblocker         *mocks.UserUnblocker
	pieceLister       *mocks.PublicPieceLister
	pieces            *mocks.PublicPieceGetter
	personLister      *mocks.PublicPersonLister
	persons           *mocks.PublicPersonGetter
	termLister        *mocks.PublicTermLister
	terms             *mocks.PublicTermGetter
	collectionLister  *mocks.PublicCollectionLister
	collections       *mocks.PublicCollectionGetter
	eventLister       *mocks.PublicEventLister
	events            *mocks.PublicEventGetter
	history           *mocks.PublicHistoryLister
	collectionHistory *mocks.PublicCollectionHistoryLister
	revisions         *mocks.PublicRevisionGetter
	needsWork         *mocks.PublicNeedsWorkLister
	handler           http.Handler
	validate          *openapitest.Validator
}

func newHandlerMocks(t *testing.T) handlerMocks {
	m := handlerMocks{
		pinger:            mocks.NewPinger(t),
		users:             mocks.NewUserGetter(t),
		displayNames:      mocks.NewDisplayNameUpdater(t),
		blocker:           mocks.NewUserBlocker(t),
		unblocker:         mocks.NewUserUnblocker(t),
		pieceLister:       mocks.NewPublicPieceLister(t),
		pieces:            mocks.NewPublicPieceGetter(t),
		personLister:      mocks.NewPublicPersonLister(t),
		persons:           mocks.NewPublicPersonGetter(t),
		termLister:        mocks.NewPublicTermLister(t),
		terms:             mocks.NewPublicTermGetter(t),
		collectionLister:  mocks.NewPublicCollectionLister(t),
		collections:       mocks.NewPublicCollectionGetter(t),
		eventLister:       mocks.NewPublicEventLister(t),
		events:            mocks.NewPublicEventGetter(t),
		history:           mocks.NewPublicHistoryLister(t),
		collectionHistory: mocks.NewPublicCollectionHistoryLister(t),
		revisions:         mocks.NewPublicRevisionGetter(t),
		needsWork:         mocks.NewPublicNeedsWorkLister(t),
		validate:          openapitest.New(t),
	}
	m.handler = NewHTTPHandler(HTTPDependencies{
		Pinger: m.pinger,
		AuthValidators: []router.AuthValidator{
			prefixValidator("auth0|"),
		},
		UserGetter:                    m.users,
		UserEnsurer:                   mocks.NewUserEnsurer(t),
		DisplayNameUpdater:            m.displayNames,
		UserBlocker:                   m.blocker,
		UserUnblocker:                 m.unblocker,
		PublicPieceLister:             m.pieceLister,
		PublicPieceGetter:             m.pieces,
		PublicPersonLister:            m.personLister,
		PublicPersonGetter:            m.persons,
		PublicTermLister:              m.termLister,
		PublicTermGetter:              m.terms,
		PublicCollectionLister:        m.collectionLister,
		PublicCollectionGetter:        m.collections,
		PublicEventLister:             m.eventLister,
		PublicEventGetter:             m.events,
		PublicHistoryLister:           m.history,
		PublicCollectionHistoryLister: m.collectionHistory,
		PublicRevisionGetter:          m.revisions,
		PublicNeedsWorkLister:         m.needsWork,
		Now: func() time.Time {
			return handlerNow
		},
	})
	return m
}

// storedUser is the user with userID as the datasource holds it: a contributor, unless userID is moderatorID
// or blockedID.
func storedUser(userID string) domain.User {
	user := domain.User{ID: userID, DisplayName: "Morwen", Role: domain.RoleContributor, CreatedAt: handlerNow}
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
