package command

import (
	"context"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type turnstileStub bool

func (s turnstileStub) VerifyTurnstile(context.Context, string) (bool, error) { return bool(s), nil }

func TestCreateContactRequest_RefusesFailedTurnstileCheck(t *testing.T) {
	t.Parallel()

	req := CreateContactRequestRequest{Kind: domain.ContactKindGeneral, Email: "wren@example.org",
		Message: "Hello.", Token: "tok"}
	now := func() time.Time { return time.Unix(0, 0) }

	repo := &fakerepo.Repo{}
	_, err := (&CreateContactRequest{Verifier: turnstileStub(false), Contacts: repo, Now: now}).
		Execute(t.Context(), req)
	require.ErrorIs(t, err, ErrTurnstileFailed)
	assert.Nil(t, repo.ContactRequest, "a request that failed the check was stored")

	_, err = (&CreateContactRequest{Verifier: turnstileStub(true), Contacts: repo, Now: now}).
		Execute(t.Context(), req)
	require.NoError(t, err)
	assert.NotNil(t, repo.ContactRequest)
}
