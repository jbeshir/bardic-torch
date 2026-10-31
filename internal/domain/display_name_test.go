package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultDisplayName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		userID string
		want   string
	}{
		// The expected names are the first eight hex digits of `printf '<id>' | sha256sum`.
		{userID: "google-oauth2|123", want: "Bard 47b988f0"},
		{userID: "auth0|b", want: "Bard 38cb9489"},
		{userID: "auth0|B", want: "Bard e773376e"},
	}

	for _, tc := range cases {
		t.Run(tc.userID, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, DefaultDisplayName(tc.userID))
		})
	}
}
