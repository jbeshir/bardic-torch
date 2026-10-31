package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestNormalizeDisplayName(t *testing.T) {
	t.Parallel()

	const (
		// composedE is "é" as one code point; decomposedE is "e" followed by a combining acute accent, which NFC
		// composes into composedE.
		composedE   = "\u00e9"
		decomposedE = "e\u0301"
		plainName   = "Morwen"
		formatError = "must not contain control or format characters"
	)

	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{name: "plain", raw: "Morwen", want: plainName},
		{name: "inner_spaces_kept", raw: "Morwen  of the Hills", want: "Morwen  of the Hills"},
		{name: "trimmed", raw: " \t " + plainName + " \n", want: plainName},
		{name: "unicode_whitespace_trimmed", raw: "\u3000" + plainName + "\u00a0", want: plainName},
		{name: "composed", raw: "Ren" + decomposedE + "e", want: "Ren" + composedE + "e"},
		{name: "already_composed", raw: "Ren" + composedE + "e", want: "Ren" + composedE + "e"},
		{name: "one_character", raw: "M", want: "M"},
		{name: "emoji", raw: "Bard \U0001f3bb", want: "Bard \U0001f3bb"},
		{name: "max_length", raw: strings.Repeat("a", 64), want: strings.Repeat("a", 64)},
		{name: "max_length_multibyte", raw: strings.Repeat("\u00fc", 64), want: strings.Repeat("\u00fc", 64)},
		{name: "max_length_padded", raw: "  " + strings.Repeat("a", 64) + "  ", want: strings.Repeat("a", 64)},
		{
			// 128 code points before normalisation, 64 after.
			name: "max_length_after_composing",
			raw:  strings.Repeat(decomposedE, 64),
			want: strings.Repeat(composedE, 64),
		},
		{name: "empty", raw: "", wantErr: "must not be empty"},
		{name: "whitespace_only", raw: " \t\u3000 ", wantErr: "must not be empty"},
		{name: "too_long", raw: strings.Repeat("a", 65), wantErr: "must be at most 64 characters"},
		{
			// Two accents on one letter compose into one character with one accent left over.
			name:    "too_long_after_composing",
			raw:     strings.Repeat("a\u0300\u0301", 33),
			wantErr: "must be at most 64 characters",
		},
		{name: "nul", raw: "Mor\x00wen", wantErr: formatError},
		{name: "inner_newline", raw: "Mor\nwen", wantErr: formatError},
		{name: "c1_control", raw: "Mor\u0085wen", wantErr: formatError},
		{name: "right_to_left_override", raw: "Mor\u202ewen", wantErr: formatError},
		{name: "zero_width_space", raw: "Mor\u200bwen", wantErr: formatError},
		{name: "byte_order_mark", raw: "\ufeffMorwen", wantErr: formatError},
		{name: "invalid_utf8", raw: "Mor\xffwen", wantErr: "must be valid UTF-8"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeDisplayName(tc.raw)
			if tc.wantErr != "" {
				require.ErrorIs(t, err, ErrInvalidDisplayName)
				require.EqualError(t, err, "invalid display name: "+tc.wantErr)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
