package domain

import "regexp"

// MaxSlugLength is the longest slug, the length of the slug columns.
const MaxSlugLength = 191

// slugPattern is the form of a slug: runs of ASCII lowercase letters and digits joined by single hyphens.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// IsSlug reports whether s has the form of a slug, the address of a piece, person, term or event. It does not
// say whether any entity has it.
func IsSlug(s string) bool {
	return len(s) <= MaxSlugLength && slugPattern.MatchString(s)
}
