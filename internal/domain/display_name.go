package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

// DefaultDisplayName is the display name a user starts with: "Bard " followed by the first eight hex digits of
// the SHA-256 of their ID. It is stable for an ID and does not reveal the ID.
func DefaultDisplayName(userID string) string {
	hash := sha256.Sum256([]byte(userID))
	return "Bard " + hex.EncodeToString(hash[:4])
}
