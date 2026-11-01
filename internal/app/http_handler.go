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
	Pinger                 datasources.Pinger
	AuthValidators         []router.AuthValidator
	UserGetter             datasources.UserGetter
	UserEnsurer            datasources.UserEnsurer
	DisplayNameUpdater     datasources.DisplayNameUpdater
	UserBlocker            datasources.UserBlocker
	UserUnblocker          datasources.UserUnblocker
	APITokenCreator        datasources.APITokenCreator
	ActiveAPITokenCounter  datasources.ActiveAPITokenCounter
	ActiveAPITokenLister   datasources.ActiveAPITokenLister
	APITokenRevoker        datasources.APITokenRevoker
	PublicPieceLister      datasources.PublicPieceLister
	PublicPieceGetter      datasources.PublicPieceGetter
	PublicPersonLister     datasources.PublicPersonLister
	PublicPersonGetter     datasources.PublicPersonGetter
	PublicTermLister       datasources.PublicTermLister
	PublicTermGetter       datasources.PublicTermGetter
	PublicCollectionLister datasources.PublicCollectionLister
	PublicCollectionGetter datasources.PublicCollectionGetter
	PublicEventLister      datasources.PublicEventLister
	PublicEventGetter      datasources.PublicEventGetter
	Now                    func() time.Time
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
		APITokenLister:         deps.ActiveAPITokenLister,
		APITokenRevoker:        deps.APITokenRevoker,
		PublicPieceLister:      deps.PublicPieceLister,
		PublicPieceGetter:      deps.PublicPieceGetter,
		PublicPersonLister:     deps.PublicPersonLister,
		PublicPersonGetter:     deps.PublicPersonGetter,
		PublicTermLister:       deps.PublicTermLister,
		PublicTermGetter:       deps.PublicTermGetter,
		PublicCollectionLister: deps.PublicCollectionLister,
		PublicCollectionGetter: deps.PublicCollectionGetter,
		PublicEventLister:      deps.PublicEventLister,
		PublicEventGetter:      deps.PublicEventGetter,
		Now:                    deps.Now,
	})
}
