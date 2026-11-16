package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeDisplayName(t *testing.T) {
	t.Parallel()

	const (
		// composedE is "é" as one code point; decomposedE is "e" followed by a combining acute accent, which NFC
		// composes into composedE.
		composedE   = "\u00e9"
		decomposedE = "e\u0301"
		plainName   = "Morwen"
		emptyError  = "must not be empty"
	)

	// Rows with clean set run through CleanDisplayName instead of NormalizeDisplayName.
	cases := []struct {
		name    string
		raw     string
		clean   bool
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
		{name: "truncated", raw: strings.Repeat("a", 65), want: strings.Repeat("a", 64)},
		{name: "truncated_multibyte", raw: strings.Repeat("\u00fc", 300), want: strings.Repeat("\u00fc", 64)},
		{
			// Two accents on one letter compose into one character with one accent left over, so 99 code points
			// become 66 characters, cut to 64.
			name: "truncated_after_composing",
			raw:  strings.Repeat("a\u0300\u0301", 33),
			want: strings.Repeat("\u00e0\u0301", 32),
		},
		{
			// The cut leaves a trailing space, which is trimmed.
			name: "truncated_then_trimmed",
			raw:  strings.Repeat("a", 63) + " Morwen",
			want: strings.Repeat("a", 63),
		},
		{name: "empty", raw: "", wantErr: emptyError},
		{name: "whitespace_only", raw: " \t\u3000 ", wantErr: emptyError},
		{name: "nul", raw: "Mor\x00wen", wantErr: disallowedCharacterError(0)},
		{name: "inner_newline", raw: "Mor\nwen", wantErr: disallowedCharacterError('\n')},
		{name: "right_to_left_override", raw: "Mor\u202ewen", wantErr: disallowedCharacterError(0x202e)},
		{name: "zero_width_space", raw: "Mor\u200bwen", wantErr: disallowedCharacterError(0x200b)},
		{name: "line_separator", raw: "Mor\u2028wen", wantErr: disallowedCharacterError(0x2028)},
		{name: "private_use", raw: "Mor\ue000wen", wantErr: disallowedCharacterError(0xe000)},
		{name: "unassigned", raw: "Mor\u0378wen", wantErr: disallowedCharacterError(0x378)},
		{name: "too_many_combining_marks", raw: "Zalgo" + strings.Repeat("\u0336", 5), wantErr: tooManyMarksError},
		{name: "invalid_utf8", raw: "Mor\xffwen", wantErr: "must be valid UTF-8"},
		{name: "clean_plain", raw: plainName, clean: true, want: plainName},
		{
			// A zero-width joiner makes a family emoji one glyph; without it the emoji are shown side by side.
			name:  "clean_zero_width_joiner",
			raw:   "Bard \U0001f468\u200d\U0001f469\u200d\U0001f467",
			clean: true,
			want:  "Bard \U0001f468\U0001f469\U0001f467",
		},
		{name: "clean_zero_width_non_joiner", raw: "می\u200cخواهم", clean: true,
			want: "میخواهم"},
		{name: "clean_right_to_left_mark", raw: "\u200fמרים", clean: true,
			want: "מרים"},
		{
			// Dropping characters frees room, and the cut still applies afterwards.
			name:  "clean_then_truncated",
			raw:   strings.Repeat("a\u200e", 65),
			clean: true,
			want:  strings.Repeat("a", 64),
		},
		{name: "clean_format_only", raw: "\u200d\u200c\u200e\u200f", clean: true, wantErr: emptyError},
		{name: "clean_format_and_spaces_only", raw: " \u200f ", clean: true, wantErr: emptyError},
		{
			name:    "clean_too_many_combining_marks",
			raw:     "Zalgo" + strings.Repeat("\u0336", 5),
			clean:   true,
			wantErr: tooManyMarksError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			normalize := NormalizeDisplayName
			if tc.clean {
				normalize = CleanDisplayName
			}
			got, err := normalize(tc.raw)
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
