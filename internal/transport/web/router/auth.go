package router

import (
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// ErrInvalidCredentials is wrapped by validator errors caused by the credentials themselves, such as a bad
// signature, as opposed to a failure to check them.
var ErrInvalidCredentials = errors.New("invalid credentials")

// Identity is who a validator authenticated a request as.
type Identity struct {
	UserID string
}

// AuthValidator authenticates a request from its credentials.
//
// It returns nil, nil if the request does not carry credentials of its kind, so the next validator can try.
// It returns an error wrapping ErrInvalidCredentials if the credentials are of its kind but invalid, and any
// other error if it could not check them.
type AuthValidator func(r *http.Request) (*Identity, error)

// authMiddleware authenticates each request with the first validator its credentials apply to, resolves the
// user and stores them as the request's principal. Requests no validator applies to continue anonymously; the
// require* middleware decides whether a route accepts them.
//
// Invalid credentials get 401, and a failure to check credentials or resolve the user gets 500, with the
// details logged rather than returned.
func authMiddleware(
	validators []AuthValidator,
	resolveUser command.Command[string, domain.User],
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			logger := domain.LoggerFromContext(ctx)

			identity, err := authenticate(validators, r)
			switch {
			case errors.Is(err, ErrInvalidCredentials):
				logger.WarnContext(ctx, "authentication failed", "error", err)
				httpjson.WriteError(ctx, w, http.StatusUnauthorized, httpjson.CodeUnauthenticated,
					"invalid credentials")
				return
			case err != nil:
				logger.ErrorContext(ctx, "unable to check credentials", "error", err)
				httpjson.WriteError(ctx, w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
				return
			case identity == nil:
				next.ServeHTTP(w, r)
				return
			}

			user, err := resolveUser.Execute(ctx, identity.UserID)
			if err != nil {
				logger.ErrorContext(ctx, "unable to resolve user", "user_id", identity.UserID, "error", err)
				httpjson.WriteError(ctx, w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
				return
			}

			recordUserID(ctx, user.ID)
			ctx = domain.ContextWithLogger(ctx, logger.With("user_id", user.ID))
			ctx = domain.ContextWithPrincipal(ctx, domain.Principal{User: user})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// authenticate returns the identity from the first validator that applies to r, or nil if none does.
func authenticate(validators []AuthValidator, r *http.Request) (*Identity, error) {
	for _, validate := range validators {
		if identity, err := validate(r); identity != nil || err != nil {
			return identity, err
		}
	}

	return nil, nil
}
