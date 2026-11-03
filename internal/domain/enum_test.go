package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnums_ValidAndParse(t *testing.T) {
	t.Parallel()

	for _, reason := range NeedsWorkReasons() {
		assert.True(t, reason.Valid(), reason)
		parsed, err := ParseNeedsWorkReason(string(reason))
		require.NoError(t, err)
		assert.Equal(t, reason, parsed)
	}
	_, err := ParseNeedsWorkReason("orphaned")
	require.EqualError(t, err, "unknown needs-work reason [orphaned]")

	for _, kind := range pieceKindValues {
		assert.True(t, kind.Valid(), kind)
	}
	assert.False(t, PieceKind("ballad").Valid())
	assert.False(t, PieceKind("").Valid())
	assert.False(t, Facet("Mood").Valid(), "values are case-sensitive")
	_, err = ParseFacet("kind")
	require.EqualError(t, err, "unknown facet [kind]")
}

func TestEnums_ModerationVocabularies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		values  []string
		valid   func(string) bool
		parse   func(string) (string, error)
		unknown string
	}{
		{"revision action", strs(revisionActionValues),
			func(s string) bool { return RevisionAction(s).Valid() },
			func(s string) (string, error) { v, err := ParseRevisionAction(s); return string(v), err }, "publish"},
		{"link report reason", strs(linkReportReasonValues),
			func(s string) bool { return LinkReportReason(s).Valid() },
			func(s string) (string, error) { v, err := ParseLinkReportReason(s); return string(v), err }, "spam"},
		{"link report status", strs(linkReportStatusValues),
			func(s string) bool { return LinkReportStatus(s).Valid() },
			func(s string) (string, error) { v, err := ParseLinkReportStatus(s); return string(v), err }, "closed"},
		{"contact request kind", strs(contactRequestKindValues),
			func(s string) bool { return ContactRequestKind(s).Valid() },
			func(s string) (string, error) { v, err := ParseContactRequestKind(s); return string(v), err }, "sales"},
		{"contact request status", strs(contactRequestStatusValues),
			func(s string) bool { return ContactRequestStatus(s).Valid() },
			func(s string) (string, error) { v, err := ParseContactRequestStatus(s); return string(v), err }, "closed"},
		{"entity type", strs(entityTypeValues),
			func(s string) bool { return EntityType(s).Valid() },
			func(s string) (string, error) { v, err := ParseEntityType(s); return string(v), err }, "song"},
		{"revision status", strs(revisionStatusValues),
			func(s string) bool { return RevisionStatus(s).Valid() },
			func(s string) (string, error) { v, err := ParseRevisionStatus(s); return string(v), err }, "deleted"},
		{"flag code", strs(flagCodeValues),
			func(s string) bool { return FlagCode(s).Valid() },
			func(s string) (string, error) { v, err := ParseFlagCode(s); return string(v), err }, "suspicious"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, tc.values)
			for _, value := range tc.values {
				assert.True(t, tc.valid(value), value)
				parsed, err := tc.parse(value)
				require.NoError(t, err)
				assert.Equal(t, value, parsed)
			}
			assert.False(t, tc.valid(tc.unknown))
			assert.False(t, tc.valid(""))
			assert.False(t, tc.valid(strings.ToUpper(tc.values[0])), "values are case-sensitive")
			_, err := tc.parse(tc.unknown)
			require.EqualError(t, err, "unknown "+tc.name+" ["+tc.unknown+"]")
		})
	}
}
