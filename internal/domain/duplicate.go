package domain

import (
	"slices"
	"strings"
	"unicode"
)

// duplicateApostrophes are the apostrophes DuplicateKey removes: straight, curly, modifier, grave and the spacing
// acute accent ("Wren´s"), which FoldLetters would otherwise turn into a space.
const duplicateApostrophes = "'’‘ʼ`´"

// DuplicateKey is the key names and titles are compared by when looking for a duplicate of a new entity: without
// apostrophes, letters folded (FoldLetters), lower case, and every run of white space and punctuation one space.
// "The Wren's Song", "the wrens song" and "The Wren’s  Sóng!" have one key.
func DuplicateKey(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(duplicateApostrophes, r) {
			return -1
		}

		return r
	}, s)
	s = strings.ToLower(FoldLetters(s))

	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) }),
		" ")
}

// duplicateKeys are the non-empty DuplicateKeys of names.
func duplicateKeys(names ...string) []string {
	keys := make([]string, 0, len(names))
	for _, n := range names {
		if key := DuplicateKey(n); key != "" {
			keys = append(keys, key)
		}
	}

	return keys
}

// DuplicateKeys are the keys of the piece's title and aliases.
func (d PieceDocument) DuplicateKeys() []string {
	names := []string{d.Title}
	for _, a := range d.Aliases {
		names = append(names, a.Text)
	}

	return duplicateKeys(names...)
}

// DuplicateKeys are the keys of the person's name and aliases.
func (d PersonDocument) DuplicateKeys() []string {
	names := []string{d.Name}
	for _, a := range d.Aliases {
		names = append(names, a.Name)
	}

	return duplicateKeys(names...)
}

// DuplicateKeys are the keys of the term's name and display name.
func (d TermDocument) DuplicateKeys() []string { return duplicateKeys(d.Name, d.DisplayName) }

// DuplicateKeys are the key of the collection's name.
func (d CollectionDocument) DuplicateKeys() []string { return duplicateKeys(d.Name) }

// DuplicateKeys are the key of the event's name.
func (d EventDocument) DuplicateKeys() []string { return duplicateKeys(d.Name) }

// DuplicateCandidate is an existing entity a new one may duplicate, by slug or, for a collection, by ID, with the
// DuplicateKeys of its document. A person who prefers no page may not be duplicated.
type DuplicateCandidate struct {
	EntityType    EntityType
	Slug          *string
	CollectionID  *int64
	Name          string
	Keys          []string
	PrefersNoPage bool
}

// MatchDuplicates returns the candidates that share a key with keys, in order. Candidates are of the new entity's
// type (and, for a term, of its facet).
func MatchDuplicates(keys []string, candidates []DuplicateCandidate) []DuplicateCandidate {
	var matches []DuplicateCandidate
	for _, c := range candidates {
		if slices.ContainsFunc(c.Keys, func(k string) bool { return slices.Contains(keys, k) }) {
			matches = append(matches, c)
		}
	}

	return matches
}
