package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxDisplayNameLength is the most characters a display name may have after normalisation.
const MaxDisplayNameLength = 64

// ErrInvalidDisplayName is wrapped by the error NormalizeDisplayName returns for a name it rejects.
var ErrInvalidDisplayName = errors.New("invalid display name")

// NormalizeDisplayName returns raw in Unicode NFC with surrounding whitespace trimmed, so that names which
// look the same are stored the same. A longer name is cut to its first MaxDisplayNameLength characters and
// trimmed again. The result must not be empty and may have only characters that CheckTextCharacters allows;
// otherwise the error wraps ErrInvalidDisplayName.
func NormalizeDisplayName(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", fmt.Errorf("%w: must be valid UTF-8", ErrInvalidDisplayName)
	}

	name := strings.TrimSpace(norm.NFC.String(raw))
	if runes := []rune(name); len(runes) > MaxDisplayNameLength {
		name = strings.TrimSpace(string(runes[:MaxDisplayNameLength]))
	}
	if name == "" {
		return "", fmt.Errorf("%w: must not be empty", ErrInvalidDisplayName)
	}
	if err := CheckTextCharacters(name); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidDisplayName, err)
	}

	return name, nil
}

// CleanDisplayName is NormalizeDisplayName for a name from the identity provider, which the user cannot change
// here. It first drops every character CheckTextCharacters disallows by its category, such as the zero-width
// joiner in an emoji sequence, the zero-width non-joiner in a Persian name or a left-to-right mark, rather than
// rejecting the name. A name left empty, or with too many combining marks, is still rejected.
func CleanDisplayName(raw string) (string, error) {
	return NormalizeDisplayName(strings.Map(func(r rune) rune {
		if !isAllowedTextCharacter(r) {
			return -1
		}
		return r
	}, raw))
}
