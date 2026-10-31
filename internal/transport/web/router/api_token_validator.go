package router

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

const apiTokenHeaderPrefix = "Bearer " + domain.APITokenPrefix

// NewAPITokenValidator returns a validator for "Bearer user_api|<secret>" credentials. The token is looked up by
// the SHA-256 of the whole secret, prefix included, and must not be revoked.
//
// Each successful use also records the token's last use at now(); the datasource skips the write if the last
// recorded use is recent. A failure to record it is logged, and the request is still authenticated.
func NewAPITokenValidator(
	tokens datasources.APITokenByHashGetter,
	lastUsed datasources.APITokenLastUsedUpdater,
	now func() time.Time,
) AuthValidator {
	return func(r *http.Request) (*Identity, error) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, apiTokenHeaderPrefix) {
			return nil, nil
		}

		ctx := r.Context()
		hash := sha256.Sum256([]byte(strings.TrimPrefix(header, "Bearer ")))
		token, err := tokens.GetAPITokenByHash(ctx, hex.EncodeToString(hash[:]))
		if errors.Is(err, datasources.ErrNotFound) {
			return nil, fmt.Errorf("%w: unknown API token", ErrInvalidCredentials)
		}
		if err != nil {
			return nil, fmt.Errorf("getting API token: %w", err)
		}

		if !token.IsActive() {
			return nil, fmt.Errorf("%w: API token [%s] is revoked", ErrInvalidCredentials, token.ID)
		}

		if err := lastUsed.UpdateAPITokenLastUsed(ctx, token.ID, now()); err != nil {
			domain.LoggerFromContext(ctx).WarnContext(ctx, "unable to record API token use",
				"token_id", token.ID,
				"error", err,
			)
		}

		return &Identity{UserID: token.UserID, Method: domain.AuthMethodAPIToken}, nil
	}
}
