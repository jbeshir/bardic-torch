package mediawiki

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// AltTitle is an alternate title of a piece, imported as an alias of Kind.
type AltTitle struct {
	Text string
	Kind domain.PieceAliasKind
}

// PieceTitles are a piece's other titles. The page title is canonical; the infobox title, the H1 (its
// parentheticals included, which name the piece, never a person), other_titles, "Also known as" lead lines and the
// redirects to the page are alternates, each kept once and only if it differs from the title and from each other
// beyond case and punctuation. A title that is another page's (a copy-paste error: Fight for the Midrealm's infobox
// says Voice of the Dragon) or a category or facet word (Birthday Dirge's infobox says Miscellaneous) is not one.
type PieceTitles struct {
	Alternates []AltTitle
}

var (
	trailingParenthetical = regexp.MustCompile(`^(.*[^` + jsSpaces + `])[` + jsSpaces + `]*\(([^()]+)\)$`)
	alsoKnownAsLine       = regexp.MustCompile(`(?i)^also known as\b:?[` + jsSpaces + `]*(.+)$`)
	titleListSeparator    = regexp.MustCompile(`(?i),|[` + jsSpaces + `]&[` + jsSpaces + `]|\bor\b`)
	otherTitlesSeparator  = regexp.MustCompile(`[,;]`)
)

// titleQuotes are the quotes and closing punctuation around a title in prose.
const titleQuotes = "\"'“”‘’. " + "\t"

// genericTitleWords are words a title field holds that are not titles: placeholders and facet names.
var genericTitleWords = map[string]bool{
	"miscellaneous": true, "misc": true, "untitled": true, "unknown": true, "various": true, "other": true,
	"none": true, "title": true, "song": true, "poem": true, "story": true, "mood": true, "topic": true,
	"culture": true, "language": true, "origin": true, "source": true, "genre": true, "form": true,
}

// titleKey is what titles are compared by: letters folded, lower case, punctuation as space, spaces collapsed.
func titleKey(s string) string {
	return collapseSpaces(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}

		return ' '
	}, domain.FoldLetters(s)))
}

func pieceTitles(p Page, parsed ParsedPage, s Site) PieceTitles {
	t := titleCollector{page: p, site: s, seen: map[string]bool{titleKey(p.Title): true}}
	for _, r := range s.RedirectsTo(p.Title) {
		t.seen[titleKey(r)] = true
		t.titles.Alternates = append(t.titles.Alternates, AltTitle{
			Text: r, Kind: domain.PieceAliasKindRedirect,
		})
	}
	var box Infobox
	if len(parsed.Infoboxes) > 0 {
		box = parsed.Infoboxes[0]
	}
	if param, ok := box.Param(titleParam); ok {
		t.addSplit(Plain(param.Raw))
	}
	if parsed.HasH1 {
		t.addSplit(Plain(parsed.H1))
	}
	if param, ok := box.Param(otherTitlesParam); ok {
		for _, part := range otherTitlesSeparator.Split(param.Raw, -1) {
			t.add(Plain(part), domain.PieceAliasKindAltTitle)
		}
	}
	for _, sec := range parsed.Sections {
		if sec.Key == SectionLead || sec.Key == SectionTitle {
			t.addLead(sec)
		}
	}

	return t.titles
}

// addLead adds the titles of a lead section's "Also known as" lines.
func (t *titleCollector) addLead(sec PageSection) {
	for _, line := range strings.Split(sec.Content.PlainText, "\n") {
		line = trimSpace(line)
		if m := alsoKnownAsLine.FindStringSubmatch(line); m != nil {
			for _, part := range titleListSeparator.Split(m[1], -1) {
				t.add(strings.Trim(part, titleQuotes), domain.PieceAliasKindAltTitle)
			}
		}
	}
}

// The infobox parameters holding titles.
const (
	titleParam       = "title"
	otherTitlesParam = "other_titles"
)

type titleCollector struct {
	page   Page
	site   Site
	seen   map[string]bool
	titles PieceTitles
}

// addSplit adds a title field's titles: its parts between "/" outside parentheses, each kept whole unless it is
// the page title followed by a parenthetical, whose parts are then the titles: a subtitle when there is one
// ("Wren's Song (The Vale Sings Softly)") and alternate titles when there are several ("Rise (We Shall Rise /
// Rise Ealdormere)").
func (t *titleCollector) addSplit(value string) {
	title := titleKey(t.page.Title)
	if titleKey(value) == title {
		return
	}
	for _, part := range splitOutsideParentheses(value) {
		if t.seen[titleKey(part)] {
			continue
		}
		m := trailingParenthetical.FindStringSubmatch(trimSpace(part))
		if m == nil || titleKey(m[1]) != title {
			t.add(part, domain.PieceAliasKindAltTitle)

			continue
		}
		inner := strings.Split(m[2], "/")
		kind := domain.PieceAliasKindAltTitle
		if len(inner) == 1 {
			kind = domain.PieceAliasKindSubtitle
		}
		for _, p := range inner {
			t.add(p, kind)
		}
	}
}

// splitOutsideParentheses splits s at the "/" that are outside parentheses.
func splitOutsideParentheses(s string) []string {
	var parts []string
	depth, last := 0, 0
	for k, r := range s {
		switch {
		case r == '(':
			depth++
		case r == ')':
			depth = max(0, depth-1)
		case r == '/' && depth == 0:
			parts = append(parts, s[last:k])
			last = k + 1
		}
	}

	return append(parts, s[last:])
}

func (t *titleCollector) add(text string, kind domain.PieceAliasKind) {
	text = trimSpace(text)
	key := titleKey(text)
	if key == "" || t.seen[key] {
		return
	}
	t.seen[key] = true
	if !t.isOtherPage(key) && !t.isCategoryWord(key) {
		t.titles.Alternates = append(t.titles.Alternates, AltTitle{Text: text, Kind: kind})
	}
}

// isOtherPage reports whether key is the title of another page than the piece's (a redirect to the piece aside).
func (t *titleCollector) isOtherPage(key string) bool {
	for title := range t.site.pages {
		if titleKey(title) == key && title != t.page.Title && t.site.ResolveRedirect(title) != t.page.Title {
			return true
		}
	}

	return false
}

func (t *titleCollector) isCategoryWord(key string) bool {
	if genericTitleWords[key] {
		return true
	}
	for name, node := range t.site.Taxonomy.Nodes {
		if node.Class != ClassBardName && titleKey(name) == key {
			return true
		}
	}

	return false
}
