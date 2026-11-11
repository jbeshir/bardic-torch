package mediawiki

import (
	"regexp"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// AliasSource is where a bard's alias was found.
type AliasSource string

// The alias sources.
const (
	// AliasFromH1Parenthetical is a name in parentheses in the page's H1: "= Bard Name (Other Name) =".
	AliasFromH1Parenthetical AliasSource = "h1_parenthetical"
	// AliasFromH1Lead is the H1's text before its parentheses, when a parenthetical is the page title.
	AliasFromH1Lead AliasSource = "h1_lead"
	// AliasFromTitleParenthetical is a name in parentheses at the end of the page title.
	AliasFromTitleParenthetical AliasSource = "title_parenthetical"
	// AliasFromLegalNameMarker follows "legal name" in the page's text.
	AliasFromLegalNameMarker AliasSource = "legal_name_marker"
	// AliasFromMundaneNameMarker follows "mundane name" in the page's text.
	AliasFromMundaneNameMarker AliasSource = "mundane_name_marker"
	// AliasFromAKAMarker follows "aka" or "a.k.a." in the page's text.
	AliasFromAKAMarker AliasSource = "aka_marker"
)

// AliasCandidate is a name a bard page gives its bard besides the page title.
type AliasCandidate struct {
	Name   string
	Source AliasSource
}

var (
	parenthetical      = regexp.MustCompile(`\(([^()]+)\)`)
	titleParenthetical = regexp.MustCompile(`\(([^)]+)\)[` + jsSpaces + `]*$`)
	aliasSeparator     = regexp.MustCompile(`,|/|;|\baka\b`)
)

// nameMarker matches "legal name", "mundane name", "real name", "aka" and "a.k.a." followed by a capitalised name.
var nameMarker = regexp.MustCompile(`(?:\b([Ll]egal|[Mm]undane|[Rr]eal)[ \t]+[Nn]ame|` +
	`\b[Aa]\.[Kk]\.[Aa]\.|\b[Aa][Kk][Aa]\b)` +
	`(?:[ \t]*:)?[ \t]*(?:(?:is|was)[ \t]+)?(` + capitalisedName + `)`)

// BardAliases returns the names a bard page gives besides its title: every parenthetical of its H1, split at
// ", / ; aka"; a parenthetical at the end of its title; and the names after "legal name", "mundane name", "aka"
// and "a.k.a." in its text. A name that folds (FoldName) to the page title is not an alias, but if an H1
// parenthetical is the title, the H1's lead is. Each name is given once, from its first source.
func BardAliases(p Page) []AliasCandidate {
	var out []AliasCandidate
	seen := map[string]bool{FoldName(p.Title): true}
	add := func(name string, source AliasSource) {
		name = trimSpace(name)
		if key := FoldName(name); name != "" && !seen[key] {
			seen[key] = true
			out = append(out, AliasCandidate{Name: name, Source: source})
		}
	}
	h1, lead, titleInH1 := h1Aliases(p)
	for _, name := range h1 {
		add(name, AliasFromH1Parenthetical)
	}
	if titleInH1 {
		add(lead, AliasFromH1Lead)
	}
	if m := titleParenthetical.FindStringSubmatch(p.Title); m != nil {
		add(m[1], AliasFromTitleParenthetical)
	}
	for _, m := range nameMarker.FindAllStringSubmatch(Plain(MaskNowiki(StripComments(p.Wikitext))), -1) {
		add(m[2], markerSource(m[1]))
	}

	return out
}

// h1Aliases returns the names in the parentheticals of the page's H1, the H1's lead (its text before the first
// parenthesis) and whether one of the names is the page title.
func h1Aliases(p Page) (names []string, lead string, titleInH1 bool) {
	parsed := ParsePage(p.Wikitext)
	if !parsed.HasH1 {
		return nil, "", false
	}
	h1 := Plain(parsed.H1)
	lead = h1
	if open := strings.IndexByte(h1, '('); open >= 0 {
		lead = h1[:open]
	}
	title := FoldName(p.Title)
	for _, m := range parenthetical.FindAllStringSubmatch(h1, -1) {
		for _, name := range aliasSeparator.Split(m[1], -1) {
			if name = trimSpace(name); name == "" {
				continue
			}
			if FoldName(name) == title {
				titleInH1 = true
			}
			names = append(names, name)
		}
	}

	return names, lead, titleInH1
}

func markerSource(kind string) AliasSource {
	switch strings.ToLower(kind) {
	case "legal":
		return AliasFromLegalNameMarker
	case "mundane":
		return AliasFromMundaneNameMarker
	}

	return AliasFromAKAMarker
}

// AliasClassification is what kind of name an alias is and whether the rules are sure of it: Confident decides
// whether a stub can be historical.
type AliasClassification struct {
	Kind      domain.PersonAliasKind
	Confident bool
}

var patronymicSuffixes = []string{"dottir", "datter"}

// ClassifyPersonAlias classifies a bard's alias. The first rule that applies wins:
//  1. a name after a "legal name" or "mundane name" marker is legal, confidently;
//  2. a name with "of" or "the" before a capitalised word, a patronymic marker word or a word ending in -dottir
//     or -datter is an SCA name, confidently;
//  3. a name with one of the particles legal names share (particleShared: de, la, van, mac…) is an SCA name,
//     but not confidently;
//  4. any other name is legal, not confidently.
func ClassifyPersonAlias(c AliasCandidate) AliasClassification {
	if c.Source == AliasFromLegalNameMarker || c.Source == AliasFromMundaneNameMarker {
		return AliasClassification{Kind: domain.PersonAliasKindLegal, Confident: true}
	}
	words := strings.FieldsFunc(c.Name, func(r rune) bool { return isSpace(r) || r == ',' })
	if confidentSCA(words) {
		return AliasClassification{Kind: domain.PersonAliasKindSCA, Confident: true}
	}
	for _, w := range words {
		if particleKindOf(w) == particleShared {
			return AliasClassification{Kind: domain.PersonAliasKindSCA}
		}
	}

	return AliasClassification{Kind: domain.PersonAliasKindLegal}
}

func confidentSCA(words []string) bool {
	for k, w := range words {
		// A capitalised "Of", "The" or "Ap" is not taken for a particle: only the lower-case word is confident.
		kind := particleKindOf(w)
		if w != strings.ToLower(w) {
			kind = particleNone
		}
		if kind == particleByname && k+1 < len(words) && isCapitalised(words[k+1]) {
			return true
		}
		if kind == particlePatronymic {
			return true
		}
		folded := domain.CollationKey(w)
		for _, suffix := range patronymicSuffixes {
			if len(folded) > len(suffix) && strings.HasSuffix(folded, suffix) {
				return true
			}
		}
	}

	return false
}

// FoldName is the key person names are matched by (never displayed): letters folded (domain.FoldLetters),
// apostrophes and their variants removed, lower case, white space collapsed.
func FoldName(s string) string {
	return collapseSpaces(strings.ToLower(withoutApostrophes(domain.FoldLetters(s))))
}
