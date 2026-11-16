package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRole(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    Role
		wantErr bool
	}{
		{in: "contributor", want: RoleContributor},
		{in: "moderator", want: RoleModerator},
		{in: "Moderator", wantErr: true},
		{in: "admin", wantErr: true},
		{in: "", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got, err := ParseRole(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
