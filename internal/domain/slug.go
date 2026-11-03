package domain

import (
	"regexp"
	"strconv"
	"strings"
)

// MaxSlugLength is the longest slug, the length of the slug columns.
const MaxSlugLength = 191

// slugPattern is the form of a slug: runs of ASCII lowercase letters and digits joined by single hyphens.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// IsSlug reports whether s has the form of a slug, the address of a piece, person, term or event. It does not
// say whether any entity has it.
func IsSlug(s string) bool {
	return len(s) <= MaxSlugLength && slugPattern.MatchString(s)
}

// Slugify returns the slug of a name: the name with its letters folded (FoldLetters) and lower-cased, each run of
// characters other than ASCII letters and digits turned into one hyphen, without leading or trailing hyphens and
// cut to at most MaxSlugLength characters at the last hyphen that fits (or within a word longer than that). The
// result is a slug, or "" for a name with no letter or digit Slugify keeps.
func Slugify(name string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(FoldLetters(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			hyphen = false
		} else {
			hyphen = true
		}
	}

	return truncateSlug(b.String(), MaxSlugLength)
}

// truncateSlug returns the slug s cut to at most n characters, at the last hyphen that fits if s is longer.
func truncateSlug(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if s[n] == '-' {
		return s[:n]
	}
	cut := s[:n]
	if i := strings.LastIndexByte(cut, '-'); i > 0 {
		return cut[:i]
	}

	return cut
}

// SlugAllocator gives entities unique slugs, per entity type, in the order it is asked. The zero value is ready to
// use.
type SlugAllocator struct {
	taken map[SlugEntityType]map[string]bool
}

// Allocate returns a slug for the entity id of type t named name that no earlier Allocate gave an entity of type
// t: the first of EntitySlug(name, t, id, 1), EntitySlug(name, t, id, 2)… that is free.
func (a *SlugAllocator) Allocate(name string, t SlugEntityType, id int64) string {
	if a.taken == nil {
		a.taken = make(map[SlugEntityType]map[string]bool)
	}
	taken := a.taken[t]
	if taken == nil {
		taken = make(map[string]bool)
		a.taken[t] = taken
	}

	slug := EntitySlug(name, t, id, 1)
	for n := 2; taken[slug]; n++ {
		slug = EntitySlug(name, t, id, n)
	}
	taken[slug] = true

	return slug
}

// EntitySlug returns the n-th slug (counting from 1) to try for the entity id of type t named name: Slugify(name),
// or "<entity type>-<id>" if that is empty, and from the second on with the suffix "-<n>", the base cut (at a
// hyphen where it can) so that the slug stays within MaxSlugLength.
func EntitySlug(name string, t SlugEntityType, id int64, n int) string {
	base := Slugify(name)
	if base == "" {
		base = t.String() + "-" + strconv.FormatInt(id, 10)
	}
	if n < 2 {
		return base
	}
	suffix := "-" + strconv.Itoa(n)

	return truncateSlug(base, MaxSlugLength-len(suffix)) + suffix
}
