package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxDisplayNameLength is the most characters a display name may have after normalisation.
const MaxDisplayNameLength = 64

// ErrInvalidDisplayName is wrapped by the error NormalizeDisplayName returns for a name it rejects.
var ErrInvalidDisplayName = errors.New("invalid display name")

// DefaultDisplayName is the display name a user starts with: "Bard " followed by the first eight hex digits of
// the SHA-256 of their ID. It is stable for an ID and does not reveal the ID.
func DefaultDisplayName(userID string) string {
	hash := sha256.Sum256([]byte(userID))
	return "Bard " + hex.EncodeToString(hash[:4])
}

// NormalizeDisplayName returns raw in Unicode NFC with surrounding whitespace trimmed, so that names which
// look the same are stored the same. The result must have 1 to MaxDisplayNameLength characters and no control
// or format characters, such as U+202E, which reverses the text after it; otherwise the error wraps
// ErrInvalidDisplayName.
func NormalizeDisplayName(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", fmt.Errorf("%w: must be valid UTF-8", ErrInvalidDisplayName)
	}

	name := strings.TrimSpace(norm.NFC.String(raw))
	if name == "" {
		return "", fmt.Errorf("%w: must not be empty", ErrInvalidDisplayName)
	}
	if utf8.RuneCountInString(name) > MaxDisplayNameLength {
		return "", fmt.Errorf("%w: must be at most %d characters", ErrInvalidDisplayName, MaxDisplayNameLength)
	}
	if strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) {
		return "", fmt.Errorf("%w: must not contain control or format characters", ErrInvalidDisplayName)
	}

	return name, nil
}
