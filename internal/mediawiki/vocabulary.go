package mediawiki

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The vocabulary of person names that the name rules, the scrubber (internal/scrub) and the importer share, each
// defined once.

// apostrophes are the apostrophe and quote variants names are written with: a separator or a joiner in a name
// (NameMatcher), removed by FoldName.
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

// multiwordName is two to four capitalised words or initials, which may have the particles of names between them.
var multiwordName = capitalisedWord + `(?:[ \t]+(?:` + nameParticle + `[ \t]+)*` + capitalisedWord + `){1,3}`

// PossessiveName matches a text that starts with what may be a person's name in the possessive ("First Last's
// Songbook").
var PossessiveName = regexp.MustCompile(`^` + multiwordName + `[` + apostrophes + `]s\b`)

// honorifics are the honorifics whose forms with a legal name's surname ("Mr. Surname") are private too.
var honorifics = []string{"Mr", "Mrs", "Ms", "Mx", "Dr", "Miss"}

// HonorificName matches an honorific and the capitalised word after it ("Mr. Surname"): in prose, a person's
// surname, which the scrubber replaces and the importer withholds whoever it names.
var HonorificName = regexp.MustCompile(`\b(?:` + strings.Join(honorifics, "|") + `)\.?[ \t]+(` +
	capitalisedWordForm + `)`)

// The placeholders the scrubber writes for private names, as formats of the name's number: a legal name is "Legal
// Name 001" in prose and titles, "LegalName001" for a name of several words written as one, and "Legal_Name_001",
// "legal-name-001" or "legalname001" inside a URL, following how the name was written there; a wiki user is "Wiki
// User 01".
const (
	LegalNameFormat               = "Legal Name %03d"
	LegalNameJoinedFormat         = "LegalName%03d"
	LegalNameURLUnderscoresFormat = "Legal_Name_%03d"
	LegalNameURLHyphensFormat     = "legal-name-%03d"
	LegalNameURLWordFormat        = "legalname%03d"
	WikiUserFormat                = "Wiki User %02d"
)

// placeholderWords are the folded words of the prose placeholders without their number ("legal name", "wiki
// user").
var placeholderWords = [][]string{
	foldedWords(fmt.Sprintf(LegalNameFormat, 0)), foldedWords(fmt.Sprintf(WikiUserFormat, 0)),
}

// PlaceholderPrefixes are the prose placeholders without their number ("Legal Name ", "Wiki User ").
var PlaceholderPrefixes = []string{
	strings.TrimSuffix(LegalNameFormat, "%03d"), strings.TrimSuffix(WikiUserFormat, "%02d"),
}

// PlaceholderNumber matches the number that ends a placeholder (after its words, which NameMatcher finds): "001",
// " 01", "_001" or "-001".
var PlaceholderNumber = regexp.MustCompile(`^[ _-]?[0-9]{2,3}\b`)

// IsPlaceholderName reports whether text holds a placeholder the scrubber writes, in any of its forms. A
// placeholder is never decided on again, so scrubbing the scrubbed copy changes nothing.
func IsPlaceholderName(text string) bool {
	joined := strings.Join(placeholderWords[0][:2], "")
	words := Words(text)
	for k, w := range words {
		if strings.HasPrefix(w.Folded, joined) {
			return true
		}
		for _, p := range placeholderWords {
			if k+1 < len(words) && w.Folded == p[0] && words[k+1].Folded == p[1] {
				return true
			}
		}
	}

	return false
}

// IsContentNamespace reports whether pages of namespace ns are content the catalogue is read from, and so may name
// people: the main namespace and categories. User, template, module, file and MediaWiki pages are not.
func IsContentNamespace(ns int) bool {
	return ns == NSMain || ns == NSCategory
}

// ContentPages are the pages of content namespaces (IsContentNamespace), in a new slice sorted by page ID: the
// pages private names are decided on.
func ContentPages(pages []Page) []Page {
	var out []Page
	for _, p := range pages {
		if IsContentNamespace(p.NS) {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].PageID < out[b].PageID })

	return out
}
