package command

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testContactRequestID = int64(12)
	testTurnstileToken   = "XXXX.DUMMY.TOKEN.XXXX"
)

var (
	testContactName = "Wren Placeholder"
	testContactURL  = "https://example.org/the-copper-kettle"
	testSubmission  = ContactSubmission{
		Kind: domain.ContactRequestKindCorrection, Name: &testContactName, Email: "wren@example.org",
		Message: "The second verse is credited to the wrong person.", URL: &testContactURL,
		TurnstileToken: testTurnstileToken,
	}
	testNewContactRequest = datasources.NewContactRequest{
		Kind: testSubmission.Kind, Name: testSubmission.Name, Email: testSubmission.Email,
		Message: testSubmission.Message, URL: testSubmission.URL, Now: fixedNow,
	}
	testContactRequest = domain.ContactRequest{ID: testContactRequestID, Kind: domain.ContactRequestKindCorrection,
		Status: domain.ContactRequestStatusResolved}
)

func TestSubmitContactRequest_VerifiesThenStores(t *testing.T) {
	t.Parallel()

	verifier := mocks.NewTurnstileVerifier(t)
	creator := mocks.NewContactRequestCreator(t)
	verified := verifier.EXPECT().VerifyTurnstile(mock.Anything, testTurnstileToken).Return(nil).Once()
	creator.EXPECT().CreateContactRequest(mock.Anything, testNewContactRequest).
		Return(testContactRequestID, nil).Once().NotBefore(verified)

	id, err := (&SubmitContactRequest{Verifier: verifier, Creator: creator, Now: fixedClock}).
		Execute(t.Context(), testSubmission)

	require.NoError(t, err)
	assert.Equal(t, testContactRequestID, id)
}

// TestSubmitContactRequest_StoresAbsoluteURL checks that a URL typed without a scheme is stored with https, so
// that a moderator's link to it is never relative, and that a request without a URL stores none.
func TestSubmitContactRequest_StoresAbsoluteURL(t *testing.T) {
	t.Parallel()

	for typed, want := range map[string]*string{
		"www.example.org/the-copper-kettle": new("https://www.example.org/the-copper-kettle"),
		"//example.org/the-copper-kettle":   new("https://example.org/the-copper-kettle"),
		" " + testContactURL + " ":          &testContactURL,
		"":                                  nil,
	} {
		t.Run(typed, func(t *testing.T) {
			t.Parallel()

			submission, stored := testSubmission, testNewContactRequest
			submission.URL, stored.URL = new(typed), want
			if typed == "" {
				submission.URL = nil
			}
			verifier := mocks.NewTurnstileVerifier(t)
			verifier.EXPECT().VerifyTurnstile(mock.Anything, testTurnstileToken).Return(nil).Once()
			creator := mocks.NewContactRequestCreator(t)
			creator.EXPECT().CreateContactRequest(mock.Anything, stored).Return(testContactRequestID, nil).Once()

			_, err := (&SubmitContactRequest{Verifier: verifier, Creator: creator, Now: fixedClock}).
				Execute(t.Context(), submission)
			require.NoError(t, err)
		})
	}
}

func TestSubmitContactRequest_Errors(t *testing.T) {
	t.Parallel()

	errDatabase := errors.New("connection refused")
	cases := []struct {
		name string
		// verifyErr is what the verifier fails with; createErr, if the verifier passes, what the creator does.
		verifyErr, createErr error
		want                 []error
	}{
		{name: "rejected", verifyErr: datasources.ErrTurnstileRejected,
			want: []error{datasources.ErrTurnstileRejected}},
		{name: "turnstile_unavailable", verifyErr: datasources.ErrTurnstileUnavailable,
			want: []error{ErrContactUnavailable, datasources.ErrTurnstileUnavailable}},
		{name: "cap", createErr: datasources.ErrContactRequestLimitReached,
			want: []error{ErrContactUnavailable, datasources.ErrContactRequestLimitReached}},
		{name: "database_busy", createErr: datasources.ErrUnavailable, want: []error{datasources.ErrUnavailable}},
		{name: "database", createErr: errDatabase, want: []error{errDatabase}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			verifier := mocks.NewTurnstileVerifier(t)
			creator := mocks.NewContactRequestCreator(t)
			verifier.EXPECT().VerifyTurnstile(mock.Anything, testTurnstileToken).Return(tc.verifyErr).Once()
			if tc.verifyErr == nil {
				creator.EXPECT().CreateContactRequest(mock.Anything, testNewContactRequest).
					Return(0, tc.createErr).Once()
			}

			_, err := (&SubmitContactRequest{Verifier: verifier, Creator: creator, Now: fixedClock}).
				Execute(t.Context(), testSubmission)

			for _, want := range tc.want {
				require.ErrorIs(t, err, want)
			}
			if !errors.Is(tc.verifyErr, datasources.ErrTurnstileUnavailable) &&
				!errors.Is(tc.createErr, datasources.ErrContactRequestLimitReached) {
				require.NotErrorIs(t, err, ErrContactUnavailable)
			}
			for _, pii := range []string{testContactName, testSubmission.Email, testSubmission.Message,
				testContactURL, testTurnstileToken} {
				assert.NotContains(t, err.Error(), pii, "an error names a field's value")
			}
		})
	}
}

func TestListContactRequests_PurgesThenLists(t *testing.T) {
	t.Parallel()

	status := domain.ContactRequestStatusOpen
	query := datasources.ContactRequestQuery{Status: &status, Page: domain.Page{Limit: 5}}
	list := domain.ContactRequestList{Requests: []domain.ContactRequest{testContactRequest}, Total: 1}
	purger := mocks.NewContactRequestPurger(t)
	lister := mocks.NewContactRequestLister(t)
	purged := purger.EXPECT().PurgeContactRequests(mock.Anything, fixedNow).Return(2, nil).Once()
	lister.EXPECT().ListContactRequests(mock.Anything, query).Return(list, nil).Once().NotBefore(purged)
	cmd := &ListContactRequests{Purger: purger, Lister: lister, Now: fixedClock}

	got, err := cmd.Execute(t.Context(), ContactRequestListRequest{Principal: interactiveMod, Query: query})
	require.NoError(t, err)
	assert.Equal(t, list, got)

	// A failed purge is logged and does not fail the list; a failed list does.
	var logs bytes.Buffer
	ctx := domain.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))
	purger.EXPECT().PurgeContactRequests(mock.Anything, fixedNow).Return(0, datasources.ErrUnavailable).Once()
	lister.EXPECT().ListContactRequests(mock.Anything, query).Return(list, nil).Once()
	got, err = cmd.Execute(ctx, ContactRequestListRequest{Principal: interactiveMod, Query: query})
	require.NoError(t, err)
	assert.Equal(t, list, got)
	assert.Contains(t, logs.String(), "level=WARN msg=\"unable to purge contact requests\"")

	purger.EXPECT().PurgeContactRequests(mock.Anything, fixedNow).Return(0, nil).Once()
	lister.EXPECT().ListContactRequests(mock.Anything, query).
		Return(domain.ContactRequestList{}, datasources.ErrQueryTimeout).Once()
	_, err = cmd.Execute(t.Context(), ContactRequestListRequest{Principal: interactiveMod, Query: query})
	require.ErrorIs(t, err, datasources.ErrQueryTimeout)
}

func TestResolveContactRequest_ResolvesThenPurges(t *testing.T) {
	t.Parallel()

	note := "Credit corrected."
	request := datasources.ResolveContactRequestRequest{ModeratorUserID: moderatorUser.ID,
		RequestID: testContactRequestID, Status: domain.ContactRequestStatusResolved, Note: &note, Now: fixedNow}
	resolver := mocks.NewContactRequestResolver(t)
	purger := mocks.NewContactRequestPurger(t)
	resolved := resolver.EXPECT().ResolveContactRequest(mock.Anything, request).Return(testContactRequest, nil).Once()
	purger.EXPECT().PurgeContactRequests(mock.Anything, fixedNow).Return(1, nil).Once().NotBefore(resolved)
	cmd := &ResolveContactRequest{Resolver: resolver, Purger: purger, Now: fixedClock}
	req := ResolveContactRequestRequest{Principal: interactiveMod, RequestID: testContactRequestID,
		Status: domain.ContactRequestStatusResolved, Note: &note}

	got, err := cmd.Execute(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, testContactRequest, got)

	// A failed purge does not fail the resolution, which has committed.
	purger.EXPECT().PurgeContactRequests(mock.Anything, fixedNow).Return(0, datasources.ErrUnavailable).Once()
	resolver.EXPECT().ResolveContactRequest(mock.Anything, request).Return(testContactRequest, nil).Once()
	got, err = cmd.Execute(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, testContactRequest, got)

	// A failed resolution purges nothing.
	for _, want := range []error{datasources.ErrNotFound, datasources.ErrContactRequestNotOpen,
		datasources.ErrUnavailable} {
		resolver.EXPECT().ResolveContactRequest(mock.Anything, request).Return(domain.ContactRequest{}, want).Once()
		_, err := cmd.Execute(t.Context(), req)
		require.ErrorIs(t, err, want)
	}
}

func TestDismissContactRequests_DismissesThenPurges(t *testing.T) {
	t.Parallel()

	note := "Spam."
	ids := []int64{testContactRequestID, testContactRequestID + 1}
	request := datasources.DismissContactRequestsRequest{ModeratorUserID: moderatorUser.ID, RequestIDs: ids,
		Note: &note, Now: fixedNow}
	dismissal := domain.ContactRequestDismissal{Dismissed: ids[:1], NotOpen: ids[1:]}
	dismisser := mocks.NewContactRequestDismisser(t)
	purger := mocks.NewContactRequestPurger(t)
	dismissed := dismisser.EXPECT().DismissContactRequests(mock.Anything, request).Return(dismissal, nil).Once()
	purger.EXPECT().PurgeContactRequests(mock.Anything, fixedNow).Return(1, nil).Once().NotBefore(dismissed)
	cmd := &DismissContactRequests{Dismisser: dismisser, Purger: purger, Now: fixedClock}
	req := DismissContactRequestsRequest{Principal: interactiveMod, RequestIDs: ids, Note: &note}

	got, err := cmd.Execute(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, dismissal, got)

	// A failed purge does not fail the dismissal, which has committed.
	purger.EXPECT().PurgeContactRequests(mock.Anything, fixedNow).Return(0, datasources.ErrUnavailable).Once()
	dismisser.EXPECT().DismissContactRequests(mock.Anything, request).Return(dismissal, nil).Once()
	got, err = cmd.Execute(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, dismissal, got)

	// A failed dismissal purges nothing.
	for _, want := range []error{domain.ErrModeratorRequired, datasources.ErrUserBlocked, datasources.ErrUnavailable} {
		dismisser.EXPECT().DismissContactRequests(mock.Anything, request).
			Return(domain.ContactRequestDismissal{}, want).Once()
		_, err := cmd.Execute(t.Context(), req)
		require.ErrorIs(t, err, want)
	}
}

func TestPurgeContactRequests(t *testing.T) {
	t.Parallel()

	purger := mocks.NewContactRequestPurger(t)
	purger.EXPECT().PurgeContactRequests(mock.Anything, fixedNow).Return(3, nil).Once()
	_, err := (&PurgeContactRequests{Purger: purger, Now: fixedClock}).Execute(t.Context(), Empty{})
	require.NoError(t, err)

	purger.EXPECT().PurgeContactRequests(mock.Anything, fixedNow).Return(0, datasources.ErrUnavailable).Once()
	_, err = (&PurgeContactRequests{Purger: purger, Now: fixedClock}).Execute(t.Context(), Empty{})
	require.ErrorIs(t, err, datasources.ErrUnavailable)
}
