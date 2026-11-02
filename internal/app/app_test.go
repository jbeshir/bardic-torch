package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoredTime_UTCWholeSeconds(t *testing.T) {
	t.Parallel()

	// Half a second or more past the second would round up if stored as is; storedTime truncates instead.
	fractional := time.Date(2026, 10, 31, 13, 0, 0, 734512345, time.FixedZone("CET", 60*60))

	got := storedTime(fractional)

	assert.Equal(t, time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), got)
	assert.Equal(t, time.UTC, got.Location())
}

func TestSetup_InvalidDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/bardic_torch")

	components, err := Setup(t.Context())

	require.ErrorContains(t, err, "parsing DATABASE_URL: database URL scheme must be mysql, got [postgres]")
	assert.Nil(t, components)
}
