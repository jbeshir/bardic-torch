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
