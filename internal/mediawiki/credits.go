package mediawiki

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// personParam is an infobox parameter that credits persons, with the role it credits.
type personParam struct {
	name string
	role domain.CreditRole
}

// personParams are the infobox parameters that credit persons, in the order credits are listed.
var personParams = []personParam{
	{"lyrics_by", domain.CreditRoleLyrics}, {"tune_by", domain.CreditRoleTune}, {"author", domain.CreditRoleAuthor},
	{"dance_by", domain.CreditRoleChoreography}, {"music_by", domain.CreditRoleMusic},
}

// NameForm is how a page names a person.
type NameForm string

// The name forms.
const (
	// NamePageLink is a page link ("[[Name]]"); its name is the link target read through redirects.
	NamePageLink NameForm = "page_link"
	// NameExternalLink is an external link to the person ("[https://… Name]"): a historical person or group with
	// no page on the wiki.
	NameExternalLink NameForm = "external_link"
	// NameText is a name in plain text, which is matched to the persons the wiki has.
	NameText NameForm = "text"
	// NameCategory is a piece's category named after a person who wrote it.
	NameCategory NameForm = "category"
	// NameRedLink is a page link to a person with no page, outside the infoboxes ("adapted by [[Name]]").
	NameRedLink NameForm = "red_link"
)

// PersonName is a person as a page names them, before it is matched to a person: the name as written (a link
// target read through redirects, a link label or text) and, for an external link, its URL.
type PersonName struct {
	Name string
	Form NameForm
	URL  string
}

// linked reports whether the name is a link or a category, which name a person exactly; text is matched more
// loosely, after the linked names have given their persons.
func (n PersonName) linked() bool {
	return n.Form != NameText
}

// CreditRecord is one credit of a piece, as its infobox gives it. A named credit has the Name of its person,
// which is matched to a person later; other attributions name no one. Part is the part of the piece the credit
// is for, from a qualifier ("(verses)") or inferred (PartInferred, listed for review). Note is the qualifiers'
// remaining text, which names no one; UnusedQualifiers are qualifiers dropped because they may be names (a
// capitalised word or a link), for review.
type CreditRecord struct {
	Role             domain.CreditRole
	Attribution      domain.Attribution
	Certainty        domain.Certainty
	Part             *domain.PiecePart
	PartInferred     bool
	Note             string
	Name             PersonName
	UnusedQualifiers []string
}

// certaintyWords are the words that qualify a credit's certainty, alone, before a name or in parentheses.
var certaintyWords = map[string]domain.Certainty{
	"possibly": domain.CertaintyPossible, "possible": domain.CertaintyPossible, "perhaps": domain.CertaintyPossible,
	"maybe": domain.CertaintyPossible, "probably": domain.CertaintyProbable, "probable": domain.CertaintyProbable,
	"likely": domain.CertaintyProbable, "disputed": domain.CertaintyDisputed,
}

// attributionWords are the texts that name no one: the wiki's pseudo-bards.
var attributionWords = map[string]domain.Attribution{
	"traditional": domain.AttributionTraditional, "trad": domain.AttributionTraditional,
	"folk": domain.AttributionTraditional, "traditional & folk": domain.AttributionTraditional,
	"traditional&folk": domain.AttributionTraditional, "unknown": domain.AttributionUnknown,
	"anonymous": domain.AttributionAnonymous, "anon": domain.AttributionAnonymous,
	"various": domain.AttributionVarious, "many": domain.AttributionVarious,
}

var certaintyPrefix = regexp.MustCompile(`(?i)^(possibly|perhaps|maybe|probably|likely|disputed)[` + jsSpaces +
	`]*:?[` + jsSpaces + `]+(.+)$`)

// pieceCredits are the credits of every person parameter of a piece's infoboxes, in parameter order.
func pieceCredits(boxes []Infobox, terms []PieceTerm, s Site) []CreditRecord {
	var out []CreditRecord
	for _, b := range boxes {
		for _, param := range personParams {
			if p, ok := b.Param(param.name); ok {
				out = append(out, scopeParts(ParseCredits(param.role, p.Value, s), terms)...)
			}
		}
	}

	return out
}

// ParseCredits reads the credits of one infobox person parameter, split at its separators (never inside a link):
//   - a page link credits the person it names, read through redirects; a link to the Traditional & Folk page,
//     a category link, "Folk" and "traditional" are a traditional attribution;
//   - an external link credits a person with no page (a historical person or a group);
//   - "Unknown", "anonymous", "Various" and "Many" are attributions with no person ("Unknown/Many" is unknown);
//   - other text is a name, matched to a person later;
//   - "possibly", "probably" and "disputed" set the certainty of the credit they qualify (as a prefix or in
//     parentheses) or, alone, of the credit before them (or after them, if they come first);
//   - a part qualifier in parentheses ("(verses)", "(chorus tune)") scopes the credit to a part.
func ParseCredits(role domain.CreditRole, v Value, s Site) []CreditRecord {
	var out []CreditRecord
	pending := domain.CertaintyCertain
	for k, it := range v.Items {
		if it.Qualifies != "" {
			continue
		}
		c, ok := creditOf(role, it, s)
		if !ok {
			if certainty, alone := certaintyWords[strings.ToLower(it.Label)]; alone {
				if n := len(out); n > 0 {
					out[n-1].Certainty = certainty
				} else {
					pending = certainty
				}
			}

			continue
		}
		if c.Certainty == domain.CertaintyCertain {
			c.Certainty = pending
		}
		pending = domain.CertaintyCertain
		out = append(out, qualify(c, qualifiersOf(v.Items, k)))
	}

	return collapseAttributions(out)
}

// creditOf reads one item as a credit. A certainty word alone is not a credit.
func creditOf(role domain.CreditRole, it ValueItem, s Site) (CreditRecord, bool) {
	c := CreditRecord{Role: role, Attribution: domain.AttributionNamed, Certainty: domain.CertaintyCertain}
	switch it.Type {
	case ValueCategoryLink, ValueCategoryTag:
		c.Attribution = domain.AttributionTraditional
	case ValuePageLink:
		target := s.ResolveRedirect(it.Target)
		if a, ok := attributionWords[strings.ToLower(target)]; ok {
			c.Attribution = a
		} else {
			c.Name = PersonName{Name: target, Form: NamePageLink}
		}
	case ValueExternalLink:
		c.Name = PersonName{Name: NormTitle(it.Label), Form: NameExternalLink, URL: it.URL}
	case ValueInterwiki, ValueText:
		text := trimSpace(strings.TrimSuffix(trimSpace(it.Label), "."))
		if m := certaintyPrefix.FindStringSubmatch(text); m != nil {
			c.Certainty = certaintyWords[strings.ToLower(m[1])]
			text = trimSpace(m[2])
		}
		if _, alone := certaintyWords[strings.ToLower(text)]; alone && c.Certainty == domain.CertaintyCertain {
			return CreditRecord{}, false
		}
		if a, ok := attributionWords[strings.ToLower(text)]; ok {
			c.Attribution = a
		} else {
			c.Name = PersonName{Name: text, Form: NameText}
		}
	}

	return c, true
}

// qualifiersOf are the items that qualify items[k]: those right after it, in parentheses.
func qualifiersOf(items []ValueItem, k int) []ValueItem {
	end := k + 1
	for end < len(items) && items[end].Qualifies != "" {
		end++
	}

	return items[k+1 : end]
}

// qualify applies qualifiers to a credit: a certainty word sets its certainty, a part word its part; other text
// in lower case is its note, and anything else (a capitalised word, which may be a name, or a link) is unused.
func qualify(c CreditRecord, qualifiers []ValueItem) CreditRecord {
	var notes []string
	for _, q := range qualifiers {
		label := trimSpace(q.Label)
		lower := strings.ToLower(label)
		if certainty, ok := certaintyWords[lower]; ok {
			c.Certainty = certainty

			continue
		}
		if part, ok := partQualifiers[lower]; ok && q.Type == ValueText {
			c.Part = &part

			continue
		}
		if q.Type == ValueText && !strings.ContainsFunc(label, unicode.IsUpper) {
			notes = append(notes, label)

			continue
		}
		c.UnusedQualifiers = append(c.UnusedQualifiers, label)
	}
	c.Note = strings.Join(notes, "; ")

	return c
}

// collapseAttributions keeps one credit per attribution naming no one and part, and drops "various" beside
// "unknown" ("Unknown/Many" is unknown).
func collapseAttributions(credits []CreditRecord) []CreditRecord {
	unknown := slices.ContainsFunc(credits, func(c CreditRecord) bool {
		return c.Attribution == domain.AttributionUnknown
	})
	var out []CreditRecord
	for _, c := range credits {
		if c.Attribution == domain.AttributionNamed {
			out = append(out, c)

			continue
		}
		duplicate := slices.ContainsFunc(out, func(o CreditRecord) bool {
			return o.Attribution == c.Attribution && samePart(o.Part, c.Part)
		})
		if !duplicate && (c.Attribution != domain.AttributionVarious || !unknown) {
			out = append(out, c)
		}
	}

	return out
}

func samePart(a, b *domain.PiecePart) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// scopeParts infers the parts of one parameter's credits when the piece's terms are scoped to its verses and its
// chorus and the parameter credits one named person beside attributions naming no one, none of them scoped: the
// named person wrote the verses' part and the others are the chorus's (Three Words: "[[X]] & anonymous", with
// SCA Pieces for the verses and Contrafacta for the chorus). The inference is listed for review (PartInferred).
func scopeParts(credits []CreditRecord, terms []PieceTerm) []CreditRecord {
	var named, unnamed int
	for _, c := range credits {
		if c.Part != nil {
			return credits
		}
		if c.Attribution == domain.AttributionNamed {
			named++
		} else {
			unnamed++
		}
	}
	if named != 1 || unnamed == 0 || !hasTermPart(terms, domain.PiecePartVerses) ||
		!hasTermPart(terms, domain.PiecePartChorus) {
		return credits
	}
	out := make([]CreditRecord, len(credits))
	for k, c := range credits {
		part := domain.PiecePartChorus
		if c.Attribution == domain.AttributionNamed {
			part = domain.PiecePartVerses
		}
		c.Part, c.PartInferred = &part, true
		out[k] = c
	}

	return out
}

func hasTermPart(terms []PieceTerm, part domain.PiecePart) bool {
	return slices.ContainsFunc(terms, func(t PieceTerm) bool { return t.Part != nil && *t.Part == part })
}
