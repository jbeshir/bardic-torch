package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const ownerID = "google-oauth2|123"

// createWithCount runs CreateAPIToken for name with the user holding activeCount tokens, and returns the
// response along with what was stored.
func createWithCount(t *testing.T, name string, activeCount int64) (CreateAPITokenResponse, datasources.NewAPIToken) {
	t.Helper()

	counter := mocks.NewActiveAPITokenCounter(t)
	counter.EXPECT().CountUserActiveAPITokens(mock.Anything, ownerID).Return(activeCount, nil)

	var stored datasources.NewAPIToken
	creator := mocks.NewAPITokenCreator(t)
	creator.EXPECT().CreateAPIToken(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, token datasources.NewAPIToken) error {
			stored = token
			return nil
		})

	cmd := &CreateAPIToken{Counter: counter, Creator: creator, Now: fixedClock}
	res, err := cmd.Execute(t.Context(), CreateAPITokenRequest{UserID: ownerID, Name: name})
	require.NoError(t, err)

	return res, stored
}

func TestCreateAPIToken_CreatesToken(t *testing.T) {
	t.Parallel()

	res, stored := createWithCount(t, "  Laptop  ", 3)

	require.Regexp(t, `^user_api\|[0-9a-f]{64}$`, res.Secret)
	hash := sha256.Sum256([]byte(res.Secret))
	assert.Equal(t, hex.EncodeToString(hash[:]), stored.TokenHash, "the hash covers the whole secret")
	assert.Equal(t, strings.TrimPrefix(res.Secret, domain.APITokenPrefix)[:8], stored.TokenPrefix)
	require.NoError(t, uuid.Validate(stored.ID))

	name := "Laptop"
	assert.Equal(t, datasources.NewAPIToken{
		ID:          stored.ID,
		UserID:      ownerID,
		TokenHash:   stored.TokenHash,
		TokenPrefix: stored.TokenPrefix,
		Name:        &name,
		CreatedAt:   fixedNow,
	}, stored)
	assert.Equal(t, domain.APIToken{
		ID:        stored.ID,
		UserID:    ownerID,
		Prefix:    stored.TokenPrefix,
		Name:      &name,
		CreatedAt: fixedNow,
	}, res.Token)
}

func TestCreateAPIToken_SecretsAreUnique(t *testing.T) {
	t.Parallel()

	first, firstStored := createWithCount(t, "", 0)
	second, secondStored := createWithCount(t, "", 0)

	assert.NotEqual(t, first.Secret, second.Secret)
	assert.NotEqual(t, firstStored.TokenHash, secondStored.TokenHash)
	assert.NotEqual(t, firstStored.ID, secondStored.ID)
}

func TestCreateAPIToken_Names(t *testing.T) {
	t.Parallel()

	longest := strings.Repeat("\u00e9", 128)
	cases := []struct {
		name string
		raw  string
		want *string
	}{
		{name: "empty_is_no_name", raw: ""},
		{name: "whitespace_is_no_name", raw: " \t\n "},
		{name: "trimmed", raw: "\tCI runner\n", want: new("CI runner")},
		{name: "emoji", raw: "deploy \U0001f680", want: new("deploy \U0001f680")},
		{name: "longest_counts_characters_not_bytes", raw: longest, want: &longest},
		{name: "padding_does_not_count", raw: "  " + longest + "  ", want: &longest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			res, stored := createWithCount(t, tc.raw, 0)

			assert.Equal(t, tc.want, stored.Name)
			assert.Equal(t, tc.want, res.Token.Name)
		})
	}
}

func TestCreateAPIToken_InvalidNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "too_long",
			raw:     strings.Repeat("\u00e9", 129),
			wantErr: "invalid API token name: must be at most 128 characters",
		},
		{
			name: "escape_sequence",
			raw:  "CI \x1b[31mrunner",
			wantErr: "invalid API token name: must contain only letters, marks, numbers, punctuation, symbols and " +
				"spaces, not U+001B",
		},
		{
			name: "right_to_left_override",
			raw:  "CI \u202erunner",
			wantErr: "invalid API token name: must contain only letters, marks, numbers, punctuation, symbols and " +
				"spaces, not U+202E",
		},
		{
			name:    "too_many_combining_marks",
			raw:     "CI" + strings.Repeat("\u0336", 5),
			wantErr: "invalid API token name: must not stack so many combining marks on one character (at most 4)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mocks fail the test if anything is counted or stored.
			cmd := &CreateAPIToken{
				Counter: mocks.NewActiveAPITokenCounter(t),
				Creator: mocks.NewAPITokenCreator(t),
				Now:     fixedClock,
			}
			_, err := cmd.Execute(t.Context(), CreateAPITokenRequest{UserID: ownerID, Name: tc.raw})

			require.ErrorIs(t, err, ErrInvalidAPITokenName)
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

func TestCreateAPIToken_BelowLimitCreates(t *testing.T) {
	t.Parallel()

	res, _ := createWithCount(t, "", 9)

	assert.NotEmpty(t, res.Secret)
}

func TestCreateAPIToken_LimitReached(t *testing.T) {
	t.Parallel()

	counter := mocks.NewActiveAPITokenCounter(t)
	counter.EXPECT().CountUserActiveAPITokens(mock.Anything, ownerID).Return(10, nil)

	// The creator mock fails the test if a token is stored.
	cmd := &CreateAPIToken{Counter: counter, Creator: mocks.NewAPITokenCreator(t), Now: fixedClock}
	_, err := cmd.Execute(t.Context(), CreateAPITokenRequest{UserID: ownerID})

	require.ErrorIs(t, err, ErrAPITokenLimitReached)
}

func TestCreateAPIToken_DatasourceErrors(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")

	t.Run("count", func(t *testing.T) {
		t.Parallel()

		counter := mocks.NewActiveAPITokenCounter(t)
		counter.EXPECT().CountUserActiveAPITokens(mock.Anything, ownerID).Return(0, dbErr)

		cmd := &CreateAPIToken{Counter: counter, Creator: mocks.NewAPITokenCreator(t), Now: fixedClock}
		_, err := cmd.Execute(t.Context(), CreateAPITokenRequest{UserID: ownerID})

		require.ErrorIs(t, err, dbErr)
	})

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		counter := mocks.NewActiveAPITokenCounter(t)
		counter.EXPECT().CountUserActiveAPITokens(mock.Anything, ownerID).Return(0, nil)
		creator := mocks.NewAPITokenCreator(t)
		creator.EXPECT().CreateAPIToken(mock.Anything, mock.Anything).Return(dbErr)

		cmd := &CreateAPIToken{Counter: counter, Creator: creator, Now: fixedClock}
		_, err := cmd.Execute(t.Context(), CreateAPITokenRequest{UserID: ownerID})

		require.ErrorIs(t, err, dbErr)
	})
}
