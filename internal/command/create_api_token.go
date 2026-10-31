package command

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

const (
	// MaxActiveAPITokensPerUser is how many non-revoked tokens a user may have.
	MaxActiveAPITokensPerUser = 10
	// MaxAPITokenNameLength is the most characters a token name may have after trimming.
	MaxAPITokenNameLength = 128

	apiTokenSecretBytes = 32
	apiTokenPrefixChars = 8
)

var (
	// ErrAPITokenLimitReached is returned when the user already has MaxActiveAPITokensPerUser active tokens.
	ErrAPITokenLimitReached = errors.New("user has reached the maximum number of active API tokens")
	// ErrInvalidAPITokenName is wrapped by the error returned for a name that is too long.
	ErrInvalidAPITokenName = errors.New("invalid API token name")
)

var _ Command[CreateAPITokenRequest, CreateAPITokenResponse] = (*CreateAPIToken)(nil)

// CreateAPITokenRequest is the request for the CreateAPIToken command.
type CreateAPITokenRequest struct {
	UserID string
	// Name is the label as the user sent it; it is trimmed, and empty means no name.
	Name string
}

// CreateAPITokenResponse is the created token and its secret, which is only ever available here.
type CreateAPITokenResponse struct {
	Token  domain.APIToken
	Secret string
}

// CreateAPIToken creates an API token for a user.
type CreateAPIToken struct {
	Counter datasources.ActiveAPITokenCounter
	Creator datasources.APITokenCreator
	Now     func() time.Time
}

// Execute creates a token for req.UserID. The limit is checked before the token is stored, so concurrent
// requests can take a user past it; that is accepted, as the limit only guards against accidental build-up.
func (c *CreateAPIToken) Execute(ctx context.Context, req CreateAPITokenRequest) (CreateAPITokenResponse, error) {
	name, err := apiTokenName(req.Name)
	if err != nil {
		return CreateAPITokenResponse{}, err
	}

	count, err := c.Counter.CountUserActiveAPITokens(ctx, req.UserID)
	if err != nil {
		return CreateAPITokenResponse{}, fmt.Errorf("counting active API tokens: %w", err)
	}
	if count >= MaxActiveAPITokensPerUser {
		return CreateAPITokenResponse{}, ErrAPITokenLimitReached
	}

	random := make([]byte, apiTokenSecretBytes)
	if _, err := rand.Read(random); err != nil {
		return CreateAPITokenResponse{}, fmt.Errorf("generating API token secret: %w", err)
	}
	randomHex := hex.EncodeToString(random)
	secret := domain.APITokenPrefix + randomHex

	token := domain.APIToken{
		ID:        uuid.NewString(),
		UserID:    req.UserID,
		Prefix:    randomHex[:apiTokenPrefixChars],
		Name:      name,
		CreatedAt: c.Now(),
	}
	if err := c.Creator.CreateAPIToken(ctx, datasources.NewAPIToken{
		ID:          token.ID,
		UserID:      token.UserID,
		TokenHash:   domain.HashAPITokenSecret(secret),
		TokenPrefix: token.Prefix,
		Name:        token.Name,
		CreatedAt:   token.CreatedAt,
	}); err != nil {
		return CreateAPITokenResponse{}, fmt.Errorf("storing API token: %w", err)
	}

	return CreateAPITokenResponse{Token: token, Secret: secret}, nil
}

// apiTokenName trims raw, returning nil if nothing is left.
func apiTokenName(raw string) (*string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(name) > MaxAPITokenNameLength {
		return nil, fmt.Errorf("%w: must be at most %d characters", ErrInvalidAPITokenName, MaxAPITokenNameLength)
	}

	return &name, nil
}
