package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxRawDisplayNameLength is the most characters a display name may have before normalisation, so that the
// work of normalising is bounded well below the request body limit.
const MaxRawDisplayNameLength = 256

// MaxDisplayNameLength is the most characters a display name may have after normalisation.
const MaxDisplayNameLength = 64

// MaxDisplayNameKeyRunes is the most characters a DisplayNameKey may have: the width of the users.display_name_key
// column. Folding can lengthen a name, so NormalizeDisplayName checks it.
const MaxDisplayNameKeyRunes = 255

// ErrInvalidDisplayName is wrapped by the error NormalizeDisplayName returns for a name it rejects.
var ErrInvalidDisplayName = errors.New("invalid display name")

// ErrDisplayNameReserved is returned by CheckDisplayNameReserved for a name a user may not choose (D20).
var ErrDisplayNameReserved = errors.New("display name is reserved")

// reservedDisplayNameKeys are the DisplayNameKeys of names that claim an authority or a role no user has.
var reservedDisplayNameKeys = []string{"mod", "system", "import", "wiki", "anonymous", "unknown", "traditional"}

// reservedDisplayNameKeyParts may appear nowhere in a DisplayNameKey, and reservedDisplayNameKeyPrefix not at its
// start, so that "Moderator Team" and "Admin Wren" claim no role either. "mod" and "admin" are not refused
// anywhere in a key, which would refuse ordinary names such as "Modesty" and "Badminton".
var reservedDisplayNameKeyParts = []string{"moderator", "bardictorch"}

const reservedDisplayNameKeyPrefix = "admin"

// defaultDisplayNameKey matches the DisplayNameKey of every DefaultDisplayName.
var defaultDisplayNameKey = regexp.MustCompile(`^bard[0-9a-f]{8}$`)

// CheckDisplayNameReserved returns ErrDisplayNameReserved if the user userID may not choose name: its
// DisplayNameKey is a reserved word, contains or starts with one, or has the shape of a default display name, so
// that no one can take another user's default. The user's own default display name is allowed, so that they may
// restore it.
func CheckDisplayNameReserved(userID, name string) error {
	key := DisplayNameKey(name)
	if key == DisplayNameKey(DefaultDisplayName(userID)) {
		return nil
	}
	containsPart := func(part string) bool { return strings.Contains(key, part) }
	if slices.Contains(reservedDisplayNameKeys, key) || slices.ContainsFunc(reservedDisplayNameKeyParts, containsPart) ||
		strings.HasPrefix(key, reservedDisplayNameKeyPrefix) || defaultDisplayNameKey.MatchString(key) {
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
// ErrInvalidDisplayName. So that a name cannot pass for another, it must hold no blank letter (IsBlankLetter),
// must not mix the letters of two of the Latin, Cyrillic and Greek scripts, and must have a letter or a digit, so
// that its DisplayNameKey is not empty; that key must have at most MaxDisplayNameKeyRunes characters. No error
// quotes the name.
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
	// Compatibility decomposition leaves every blank letter one (U+3164 and U+FFA0 become U+1160).
	if strings.ContainsFunc(norm.NFKD.String(name), IsBlankLetter) {
		return "", fmt.Errorf("%w: must not contain a blank letter (a Hangul filler, the braille blank or a "+
			"default-ignorable character)", ErrInvalidDisplayName)
	}
	if mixesConfusableScripts(name) {
		return "", fmt.Errorf("%w: must not mix Latin, Cyrillic and Greek letters", ErrInvalidDisplayName)
	}
	key := DisplayNameKey(name)
	if key == "" {
		return "", fmt.Errorf("%w: must contain a letter or a digit", ErrInvalidDisplayName)
	}
	if utf8.RuneCountInString(key) > MaxDisplayNameKeyRunes {
		return "", fmt.Errorf("%w: must be at most %d characters once folded for comparison", ErrInvalidDisplayName,
			MaxDisplayNameKeyRunes)
	}

	return name, nil
}

// confusableScripts are the scripts whose letters look alike: a name mixing two of them may pass for another.
var confusableScripts = []*unicode.RangeTable{unicode.Latin, unicode.Cyrillic, unicode.Greek}

// mixesConfusableScripts reports whether name has letters of two or more of confusableScripts.
func mixesConfusableScripts(name string) bool {
	seen := map[*unicode.RangeTable]bool{}
	for _, r := range name {
		for _, script := range confusableScripts {
			if unicode.Is(script, r) {
				seen[script] = true
			}
		}
	}

	return len(seen) > 1
}
