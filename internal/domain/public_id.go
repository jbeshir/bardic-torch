package domain

import (
	"crypto/rand"
	"encoding/base32"
)

// A user's public ID (docs/PLAN.md §4 "users") addresses their public repertoire, so that no identity provider's
// user ID is ever part of a public path or response: PublicIDLength lowercase base32 characters (a to z, 2 to 7)
// encoding publicIDBytes random bytes.
const (
	publicIDBytes = 10
	// PublicIDLength is the number of characters of a public ID.
	PublicIDLength = 16
)

// publicIDEncoding is RFC 4648 base32 in lowercase, without padding: publicIDBytes bytes are PublicIDLength
// characters.
var publicIDEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// PublicIDSource issues the public IDs of users: each call returns a fresh one.
type PublicIDSource func() string

// RandomPublicID is the PublicIDSource of the server: 80 bits from crypto/rand.
func RandomPublicID() string {
	random := make([]byte, publicIDBytes)
	// crypto/rand.Read never returns an error: it fills random or crashes the program.
	_, _ = rand.Read(random)

	return publicIDEncoding.EncodeToString(random)
}

// IsPublicID reports whether s has the form of a public ID: PublicIDLength characters, each a lowercase ASCII
// letter or a digit from 2 to 7.
func IsPublicID(s string) bool {
	if len(s) != PublicIDLength {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '2' || r > '7') {
			return false
		}
	}

	return true
}
