package domain

import (
	"strings"
)

// LegacyPage is where an imported entity was on the wiki (legacy_wiki_pageid, legacy_title): a page, or,
// the title a red link gave.
type LegacyPage struct {
	Title string `json:"title"`
}

// CollationKey is the key text columns compare by (utf8mb4_0900_ai_ci): letters folded (FoldLetters) and lower
// case, so that two names a UNIQUE index would take for one have the same key. It folds at least as much as the
// collation does, so that a value Validate accepts is never a duplicate entry.
func CollationKey(s string) string {
	return strings.ToLower(FoldLetters(s))
}
