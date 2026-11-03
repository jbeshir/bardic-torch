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
			// 256 characters before trimming, 64 after.
			name: "max_raw_length",
			raw:  strings.Repeat(" ", 96) + strings.Repeat("a", 64) + strings.Repeat(" ", 96),
			want: strings.Repeat("a", 64),
		},
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
			name:    "raw_too_long",
			raw:     strings.Repeat(" ", 254) + "Bob",
			wantErr: "must be at most 256 characters before normalisation",
		},
		{
			// 257 code points before normalisation, which would compose into 129.
			name:    "raw_too_long_before_composing",
			raw:     strings.Repeat("e\u0301", 128) + "e",
			wantErr: "must be at most 256 characters before normalisation",
		},
		{
			// Two accents on one letter compose into one character with one accent left over.
			name:    "too_long_after_composing",
			raw:     strings.Repeat("a\u0300\u0301", 33),
			wantErr: "must be at most 64 characters",
		},
		{name: "nul", raw: "Mor\x00wen", wantErr: disallowedCharacterError(0)},
		{name: "inner_newline", raw: "Mor\nwen", wantErr: disallowedCharacterError('\n')},
		{name: "right_to_left_override", raw: "Mor\u202ewen", wantErr: disallowedCharacterError(0x202e)},
		{name: "zero_width_space", raw: "Mor\u200bwen", wantErr: disallowedCharacterError(0x200b)},
		{name: "line_separator", raw: "Mor\u2028wen", wantErr: disallowedCharacterError(0x2028)},
		{name: "private_use", raw: "Mor\ue000wen", wantErr: disallowedCharacterError(0xe000)},
		{name: "unassigned", raw: "Mor\u0378wen", wantErr: disallowedCharacterError(0x378)},
		{name: "too_many_combining_marks", raw: "Zalgo" + strings.Repeat("\u0336", 5), wantErr: tooManyMarksError},
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

func TestCheckDisplayNameReserved(t *testing.T) {
	t.Parallel()

	const userID = "google-oauth2|123"
	cases := []struct {
		name string
		want error
	}{
		{name: "Morwen of the Hills"},
		{name: "Moderator", want: ErrDisplayNameReserved},
		{name: "MOD", want: ErrDisplayNameReserved},
		{name: "Ad.min", want: ErrDisplayNameReserved},
		{name: "Bardic Torch", want: ErrDisplayNameReserved},
		{name: "Traditional", want: ErrDisplayNameReserved},
		{name: "Moderator Wren"},
		// The caller's own default name may be restored, in any spelling with its key.
		{name: DefaultDisplayName(userID)},
		{name: "bard-47B988F0"},
		// Another user's default name, or any name of that shape, may not be taken.
		{name: DefaultDisplayName("auth0|b"), want: ErrDisplayNameReserved},
		{name: "Bard 0123abcd", want: ErrDisplayNameReserved},
		{name: "Bard 0123abcde"},
		{name: "Bard 0123abcg"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.ErrorIs(t, CheckDisplayNameReserved(userID, tc.name), tc.want)
		})
	}
}
