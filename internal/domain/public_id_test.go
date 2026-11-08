package domain

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPublicID_Format(t *testing.T) {
	t.Parallel()

	format := regexp.MustCompile(`^[a-z2-7]{16}$`)
	seen := map[string]bool{}
	for range 100 {
		id := RandomPublicID()
		assert.Regexp(t, format, id)
		assert.True(t, IsPublicID(id), id)
		assert.False(t, seen[id], "a public ID is issued twice")
		seen[id] = true
	}

	for _, tc := range []struct {
		name string
		id   string
		want bool
	}{
		{name: "valid", id: "abcdefghijklmn27", want: true},
		{name: "every letter and digit", id: "qrstuvwxyz234567", want: true},
		{name: "too short", id: "abcdefghijklmn2"},
		{name: "too long", id: "abcdefghijklmn234"},
		{name: "no characters"},
		{name: "uppercase", id: "ABCDEFGHIJKLMNOP"},
		{name: "digit 1", id: "abcdefghijklmno1"},
		{name: "digit 8", id: "abcdefghijklmno8"},
		{name: "digit 0", id: "abcdefghijklmno0"},
		{name: "user ID", id: "google-oauth2|12"},
		{name: "non-ASCII", id: "abcdefghijklmné"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IsPublicID(tc.id))
		})
	}
}
