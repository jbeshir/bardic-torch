package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const testEnvName = "BARDIC_TORCH_TEST_ENV"

func TestMustGetEnvAsString_Set(t *testing.T) {
	t.Setenv(testEnvName, "value")
	assert.Equal(t, "value", MustGetEnvAsString(t.Context(), testEnvName))
}

func TestMustGetEnvAsString_EmptyCountsAsSet(t *testing.T) {
	t.Setenv(testEnvName, "")
	assert.Empty(t, MustGetEnvAsString(t.Context(), testEnvName))
}

func TestMustGetEnvAsString_MissingPanics(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "missing environment variable [BARDIC_TORCH_TEST_ENV_UNSET]", func() {
		MustGetEnvAsString(t.Context(), "BARDIC_TORCH_TEST_ENV_UNSET")
	})
}

func TestMustGetEnvAsInt_Valid(t *testing.T) {
	t.Setenv(testEnvName, "5000")
	assert.Equal(t, 5000, MustGetEnvAsInt(t.Context(), testEnvName))
}

func TestMustGetEnvAsInt_InvalidPanics(t *testing.T) {
	t.Setenv(testEnvName, "50x")
	assert.PanicsWithValue(t, "unable to parse environment variable as integer [BARDIC_TORCH_TEST_ENV]: 50x", func() {
		MustGetEnvAsInt(t.Context(), testEnvName)
	})
}

func TestMustGetEnvAsStrings_TrimsAndDropsEmpty(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty", value: "", want: nil},
		{name: "only_separators", value: " , ,", want: nil},
		{name: "single", value: auth0Driver, want: []string{auth0Driver}},
		{name: "trims_and_drops_empty", value: " auth0 ,, moderator ,", want: []string{"auth0", "moderator"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(testEnvName, tc.value)
			assert.Equal(t, tc.want, MustGetEnvAsStrings(t.Context(), testEnvName))
		})
	}
}
