package app

import (
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
)

// HTTPDependencies are what the HTTP handler is built from: the datasource, the validators that authenticate
// requests, and the clock.
type HTTPDependencies struct {
	Repository     datasources.Repository
	AuthValidators []router.AuthValidator
	// Turnstile verifies the contact form's tokens.
	Turnstile datasources.TurnstileVerifier
	Now       func() time.Time
}

// NewHTTPHandler builds the production HTTP handler from deps. Setup and tests both use it, so tests exercise
// the same wiring as production.
func NewHTTPHandler(deps HTTPDependencies) http.Handler {
	return router.New(router.Dependencies{
		Repository:     deps.Repository,
		AuthValidators: deps.AuthValidators,
		ResolveUser:    &command.ResolveUser{Users: deps.Repository, Now: deps.Now},
		UpdateDisplayName: &command.UpdateDisplayName{
			Users: deps.Repository,
		},
		SetUserBlocked:   &command.SetUserBlocked{Users: deps.Repository, Now: deps.Now},
		Now:              deps.Now,
		CreateLinkReport: &command.CreateLinkReport{Reports: deps.Repository, Now: deps.Now},
		SubmitRevision:   &command.SubmitRevision{Revisions: deps.Repository, Now: deps.Now},
		CreateContactRequest: &command.CreateContactRequest{
			Verifier: deps.Turnstile, Contacts: deps.Repository, Now: deps.Now,
		},
	})
}
