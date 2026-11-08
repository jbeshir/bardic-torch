package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// A performance's key (docs/PLAN.md §5.1, "Restore on submit") is an opaque, stable identity the editor round-trips,
// so that a submitted performance matches the base performance it edits even when its description or year changed.
// The server issues a new performance freshKeyBytes random bytes as lowercase hex. A stored performance without a key
// is given a derived key instead: derivedKeyPrefix and the first derivedKeyDigits hex digits of the SHA-256 of what
// the caller is shown of it. Both are 16 lowercase hex digits.
const (
	freshKeyBytes     = 8
	performanceKeyLen = 2 * freshKeyBytes
	derivedKeyPrefix  = "c"
	derivedKeyDigits  = performanceKeyLen - len(derivedKeyPrefix)
)

// KeySource issues the keys of new performances: each call returns a fresh one.
type KeySource func() string

// RandomPerformanceKey is the KeySource of the server: 16 hex digits from crypto/rand.
func RandomPerformanceKey() string {
	random := make([]byte, freshKeyBytes)
	// crypto/rand.Read never returns an error: it fills random or crashes the program.
	_, _ = rand.Read(random)

	return hex.EncodeToString(random)
}

// derivedPerformanceKey is the key of a stored performance without one, of the link with URL url: a hash of its
// description, year and piece. pieceID is the piece only if the caller is shown it, else 0, so the key hashes only
// what the caller receives and cannot be tested against a piece they may not see.
func derivedPerformanceKey(url string, p DocPerformance, pieceID *int64) string {
	sum := sha256.Sum256([]byte(performanceKey(url, p, pieceID)))

	return derivedKeyPrefix + hex.EncodeToString(sum[:])[:derivedKeyDigits]
}

// validPerformanceKey is whether key has the form of an issued or a derived key: 16 lowercase hex digits.
func validPerformanceKey(key string) bool {
	if len(key) != performanceKeyLen {
		return false
	}
	for _, r := range key {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}

	return true
}
