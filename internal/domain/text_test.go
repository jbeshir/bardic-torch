package domain

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// disallowedCharacterError is the message CheckTextCharacters returns for r.
func disallowedCharacterError(r rune) string {
	return fmt.Sprintf("must contain only letters, marks, numbers, punctuation, symbols and spaces, not %U", r)
}

const tooManyMarksError = "must not stack so many combining marks on one character (at most 4)"

func TestCheckTextCharacters(t *testing.T) {
	t.Parallel()

	// marks are four combining accents (grave, acute, circumflex, tilde).
	const marks = "\u0300\u0301\u0302\u0303"

	cases := []struct {
		name    string
		text    string
		wantErr string
	}{
		{name: "letters_and_spaces", text: "Morwen of the Hills"},
		{name: "punctuation_symbols_numbers", text: "R2-D2 (\u00a9 2026) \u20ac5 #1!"},
		{name: "non_latin_scripts", text: "\u041c\u043e\u0440\u0432\u0435\u043d \u30e2\u30eb \u0645\u0648\u0631"},
		// The virama and vowel signs are combining marks.
		{name: "devanagari_with_marks", text: "\u0928\u092e\u0938\u094d\u0924\u0947"},
		{name: "no_break_and_ideographic_spaces", text: "Mor\u00a0wen\u3000Hills"},
		{name: "four_byte_emoji", text: "\U0001f3bb fiddler"},
		{name: "emoji_with_skin_tone", text: "\U0001f44b\U0001f3fd"},
		{name: "keycap_emoji", text: "1\ufe0f\u20e3"},
		{name: "max_combining_marks", text: "x" + marks},
		{name: "max_combining_marks_on_each_character", text: "x" + marks + "y" + marks},
		{name: "too_many_combining_marks", text: "x" + marks + "\u0304", wantErr: tooManyMarksError},
		{name: "long_mark_stack", text: "Zalgo" + strings.Repeat("\u0336", 40), wantErr: tooManyMarksError},
		{name: "nul", text: "Mor\x00wen", wantErr: disallowedCharacterError(0)},
		{name: "newline", text: "Mor\nwen", wantErr: disallowedCharacterError('\n')},
		{name: "tab", text: "Mor\twen", wantErr: disallowedCharacterError('\t')},
		{name: "c1_control", text: "Mor\u0085wen", wantErr: disallowedCharacterError(0x85)},
		{name: "right_to_left_override", text: "Mor\u202ewen", wantErr: disallowedCharacterError(0x202e)},
		{name: "zero_width_joiner", text: "Mor\u200dwen", wantErr: disallowedCharacterError(0x200d)},
		{name: "byte_order_mark", text: "\ufeffMorwen", wantErr: disallowedCharacterError(0xfeff)},
		{name: "line_separator", text: "Mor\u2028wen", wantErr: disallowedCharacterError(0x2028)},
		{name: "paragraph_separator", text: "Mor\u2029wen", wantErr: disallowedCharacterError(0x2029)},
		{name: "private_use", text: "Mor\ue000wen", wantErr: disallowedCharacterError(0xe000)},
		{name: "supplementary_private_use", text: "Mor\U000f0000wen", wantErr: disallowedCharacterError(0xf0000)},
		{name: "unassigned", text: "Mor\u0378wen", wantErr: disallowedCharacterError(0x378)},
		{name: "noncharacter", text: "Mor\ufffewen", wantErr: disallowedCharacterError(0xfffe)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := CheckTextCharacters(tc.text)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}
