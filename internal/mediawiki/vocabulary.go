package mediawiki

import (
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The vocabulary of person names that the alias rules and the importer share, each
// defined once.

// apostrophes are the apostrophe and quote variants names are written with, removed by FoldName.
const apostrophes = "'’‘ʼ`"

func isApostrophe(r rune) bool {
	return strings.ContainsRune(apostrophes, r)
}

func withoutApostrophes(s string) string {
	return strings.Map(func(r rune) rune {
		if isApostrophe(r) {
			return -1
		}

		return r
	}, s)
}

// capitalisedWordForm is a capitalised word: an upper-case letter, then letters, marks, apostrophes and hyphens.
const capitalisedWordForm = `\p{Lu}[\p{L}\p{M}` + apostrophes + `-]*`

// capitalisedWord is an initial ("J.") or a capitalised word.
const capitalisedWord = `(?:\p{Lu}\.|` + capitalisedWordForm + `)`

// particleKind is what a lower-case word between the capitalised words of a name tells of the name.
type particleKind int

const (
	particleNone particleKind = iota
	// particleByname is "of" and "the": before a capitalised word, an SCA byname ("Wren of the Vale").
	particleByname
	// particlePatronymic is a patronymic marker ("ap", "inghean"): an SCA name.
	particlePatronymic
	// particleShared is a particle legal names share ("de", "van"): perhaps an SCA name.
	particleShared
	// particleOther is a particle that tells nothing of the name ("na", "an").
	particleOther
)

// nameParticles are the particles of names as written, and what each tells.
var nameParticles = map[string]particleKind{
	"of": particleByname, "the": particleByname,
	"ap": particlePatronymic, "ab": particlePatronymic, "verch": particlePatronymic, "ferch": particlePatronymic,
	"inghen": particlePatronymic, "inghean": particlePatronymic, "ingen": particlePatronymic,
	"nic": particlePatronymic, "bint": particlePatronymic, "ibn": particlePatronymic,
	"de": particleShared, "del": particleShared, "della": particleShared, "di": particleShared,
	"du": particleShared, "la": particleShared, "le": particleShared, "van": particleShared, "von": particleShared,
	"zu": particleShared, "ní": particleShared, "ni": particleShared, "mac": particleShared, "mc": particleShared,
	"ui": particleShared, "ua": particleShared, "ó": particleShared,
	"na": particleOther, "an": particleOther,
}

// particleKinds are nameParticles by their collation key, so that a word is looked up folded.
var particleKinds = func() map[string]particleKind {
	out := map[string]particleKind{}
	for word, kind := range nameParticles {
		out[domain.CollationKey(word)] = kind
	}

	return out
}()

// particleKindOf is what word, folded (domain.CollationKey), tells as a particle of a name.
func particleKindOf(word string) particleKind {
	return particleKinds[domain.CollationKey(word)]
}

// nameParticle matches one of nameParticles as written.
var nameParticle = `(?:` + strings.Join(sortedByLengthDescending(slices.Collect(maps.Keys(nameParticles))), "|") + `)`

func sortedByLengthDescending(words []string) []string {
	sort.Slice(words, func(a, b int) bool {
		if len(words[a]) != len(words[b]) {
			return len(words[a]) > len(words[b])
		}

		return words[a] < words[b]
	})

	return words
}

// capitalisedName is one to four capitalised words or initials ("J."), which may have the lower-case particles
// of names between them.
var capitalisedName = capitalisedWord + `(?:[ \t]+(?:` + nameParticle + `[ \t]+)*` + capitalisedWord + `){0,3}`
