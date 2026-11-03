package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxRawDisplayNameLength is the most characters a display name may have before normalisation, so that the
// work of normalising is bounded well below the request body limit.
const MaxRawDisplayNameLength = 256

// MaxDisplayNameLength is the most characters a display name may have after normalisation.
const MaxDisplayNameLength = 64

// ErrInvalidDisplayName is wrapped by the error NormalizeDisplayName returns for a name it rejects.
var ErrInvalidDisplayName = errors.New("invalid display name")

// ErrDisplayNameReserved is returned by CheckDisplayNameReserved for a name a user may not choose (D20).
var ErrDisplayNameReserved = errors.New("display name is reserved")

// reservedDisplayNameKeys are the DisplayNameKeys of names that claim an authority or a role no user has.
var reservedDisplayNameKeys = []string{
	"moderator", "mod", "admin", "administrator", "bardictorch", "system", "import", "wiki", "anonymous", "unknown",
	"traditional",
}

// defaultDisplayNameKey matches the DisplayNameKey of every DefaultDisplayName.
var defaultDisplayNameKey = regexp.MustCompile(`^bard[0-9a-f]{8}$`)

// CheckDisplayNameReserved returns ErrDisplayNameReserved if the user userID may not choose name: its
// DisplayNameKey is a reserved word, or has the shape of a default display name, so that no one can take another
// user's default. The user's own default display name is allowed, so that they may restore it.
func CheckDisplayNameReserved(userID, name string) error {
	key := DisplayNameKey(name)
	if key == DisplayNameKey(DefaultDisplayName(userID)) {
		return nil
	}
	if slices.Contains(reservedDisplayNameKeys, key) || defaultDisplayNameKey.MatchString(key) {
		return ErrDisplayNameReserved
	}

	return nil
}

// DefaultDisplayName is the display name a user starts with: "Bard " followed by the first eight hex digits of
// the SHA-256 of their ID. It is stable for an ID and does not reveal the ID.
func DefaultDisplayName(userID string) string {
	hash := sha256.Sum256([]byte(userID))
	return "Bard " + hex.EncodeToString(hash[:4])
}

// NormalizeDisplayName returns raw in Unicode NFC with surrounding whitespace trimmed, so that names which
// look the same are stored the same. raw may have at most MaxRawDisplayNameLength characters, and the result
// must have 1 to MaxDisplayNameLength characters that CheckTextCharacters allows; otherwise the error wraps
// ErrInvalidDisplayName.
func NormalizeDisplayName(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", fmt.Errorf("%w: must be valid UTF-8", ErrInvalidDisplayName)
	}
	if utf8.RuneCountInString(raw) > MaxRawDisplayNameLength {
		return "", fmt.Errorf("%w: must be at most %d characters before normalisation", ErrInvalidDisplayName,
			MaxRawDisplayNameLength)
	}

	name := strings.TrimSpace(norm.NFC.String(raw))
	if name == "" {
		return "", fmt.Errorf("%w: must not be empty", ErrInvalidDisplayName)
	}
	if utf8.RuneCountInString(name) > MaxDisplayNameLength {
		return "", fmt.Errorf("%w: must be at most %d characters", ErrInvalidDisplayName, MaxDisplayNameLength)
	}
	if err := CheckTextCharacters(name); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidDisplayName, err)
	}

	return name, nil
}
