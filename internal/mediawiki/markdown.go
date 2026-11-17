package mediawiki

import (
	"regexp"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	markdownListPrefix = regexp.MustCompile(`^([*#:]+)[` + jsSpaces + `]*`)
	markdownBlankRuns  = regexp.MustCompile(`\n{3,}`)
	markdownLineStart  = regexp.MustCompile(`^(?:[-+]|\d+[.)])[` + jsSpaces + `]`)
	nowikiTag          = regexp.MustCompile(`(?i)</?nowiki[` + jsSpaces + `]*/?>`)
)

// markdownEscapes escapes the characters that are Markdown syntax anywhere in a line of text.
var markdownEscapes = strings.NewReplacer(`\`, `\\`, "`", "\\`", `*`, `\*`, `_`, `\_`, `[`, `\[`, `]`, `\]`,
	`<`, `\<`, `>`, `\>`)

// WikitextToMarkdown converts the wikitext of a piece of prose to Markdown: bold and italic quotes become ** and
// *, [url text] becomes [text](url) and a bare or textless URL <url>, a wikilink becomes its label, a category
// tag (membership) is removed, "*" and "#" lists become "- " and "1. " lists, <br> becomes a line break and other
// HTML tags, comments and template calls are removed. Markdown syntax in the text is escaped, so the only links
// are the converted ones (never images or raw HTML), and their URLs are absolute http(s) URLs (a "www." or "//"
// URL gets https) without tracking parameters (domain.WithoutTrackingParameters). The result is trimmed and has no
// runs of more than one blank line.
func WikitextToMarkdown(s string) string {
	s = nowikiTag.ReplaceAllString(StripComments(s), "")
	s = withoutTemplates(s)
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, markdownLine(line))
	}

	return trimSpace(markdownBlankRuns.ReplaceAllString(strings.Join(out, "\n"), "\n\n"))
}

// withoutTemplates returns s without its template calls.
func withoutTemplates(s string) string {
	var b strings.Builder
	last := 0
	for _, t := range FindTemplates(s) {
		b.WriteString(s[last:t.Start])
		last = t.End
	}
	b.WriteString(s[last:])

	return b.String()
}

func markdownLine(line string) string {
	prefix := ""
	if m := markdownListPrefix.FindStringSubmatch(line); m != nil {
		markers := m[1]
		indent := strings.Repeat("  ", len(markers)-1)
		switch markers[len(markers)-1] {
		case '*':
			prefix = indent + "- "
		case '#':
			prefix = indent + "1. "
		default:
			prefix = indent
		}
		line = line[len(m[0]):]
	}
	body := trimSpace(markdownInlines(line))
	if body == "" {
		return ""
	}
	if prefix == "" {
		body = escapeLineStart(body)
	}

	return prefix + body
}

// escapeLineStart escapes what would make a line of text a heading, list item or quote.
func escapeLineStart(s string) string {
	switch {
	case strings.HasPrefix(s, "#"):
		return `\` + s
	case markdownLineStart.MatchString(s):
		if s[0] == '-' || s[0] == '+' {
			return `\` + s
		}
		k := strings.IndexAny(s, ".)")

		return s[:k] + `\` + s[k:]
	}

	return s
}

// markdownInlines converts the inline tokens and the text between them. A "!" of the text directly before a
// converted link is escaped, since "![text](url)" would be an image.
func markdownInlines(line string) string {
	var pieces []string
	add := func(piece string) {
		if piece == "" {
			return
		}
		if n := len(pieces); n > 0 && strings.HasPrefix(piece, "[") && strings.HasSuffix(pieces[n-1], "!") {
			pieces[n-1] = strings.TrimSuffix(pieces[n-1], "!") + `\!`
		}
		pieces = append(pieces, piece)
	}
	last := 0
	for _, m := range markdownInlineTokens(line) {
		add(markdownText(line[last:m[0]]))
		add(markdownToken(line[m[0]:m[1]]))
		last = m[1]
	}
	add(markdownText(line[last:]))

	return strings.Join(pieces, "")
}

// markdownInlineTokens returns the offsets of the line's inline tokens, left to right without overlap: wikilinks,
// bracketed external links, runs of two to five quotes, <br> and other HTML tags.
func markdownInlineTokens(line string) [][2]int {
	var out [][2]int
	sc := newTokenScanner(line)
	for i := 0; i < len(line); {
		end, ok := markdownInlineAt(sc, i)
		if !ok {
			i++

			continue
		}
		out = append(out, [2]int{i, end})
		i = end
	}

	return out
}

func markdownInlineAt(sc *tokenScanner, i int) (int, bool) {
	switch sc.s[i] {
	case '[':
		if end, ok := sc.wikilinkAt(i); ok {
			return end, true
		}

		return sc.externalLinkAt(i)
	case '\'':
		n := 0
		for i+n < len(sc.s) && sc.s[i+n] == '\'' && n < 5 {
			n++
		}

		return i + n, n >= 2
	case '<':
		if end, ok := sc.lineBreakAt(i); ok {
			return end, true
		}

		return sc.tagAt(i)
	}

	return 0, false
}

// markdownText converts plain text, which may hold bare URLs.
func markdownText(s string) string {
	var b strings.Builder
	last := 0
	for _, l := range FindExternalLinks(s) {
		b.WriteString(markdownEscapes.Replace(DecodeEntities(s[last:l.Start])))
		b.WriteString(markdownURL(markdownLinkURL(l.URL)))
		last = l.End
	}
	b.WriteString(markdownEscapes.Replace(DecodeEntities(s[last:])))

	return b.String()
}

func markdownToken(tok string) string {
	lower := strings.ToLower(tok)
	switch {
	case strings.HasPrefix(tok, "[["):
		l := FindWikilinks(tok)[0]
		switch l.Kind {
		case WikilinkCategoryTag, WikilinkFile:
			return ""
		case WikilinkPage, WikilinkCategoryLink, WikilinkFileLink, WikilinkInterwiki, WikilinkNamespace:
		}

		return markdownText(l.Label())
	case strings.HasPrefix(tok, "["):
		links := FindExternalLinks(tok)
		if len(links) == 0 {
			return markdownEscapes.Replace(tok)
		}
		u := markdownLinkURL(links[0].URL)
		if links[0].Text == "" {
			return markdownURL(u)
		}

		return "[" + markdownEscapes.Replace(DecodeEntities(links[0].Text)) + "](" + markdownDestination(u) + ")"
	case strings.HasPrefix(tok, "''"):
		switch len(tok) {
		case 2:
			return "*"
		case 3:
			return "**"
		case 5:
			return "***"
		}

		return markdownEscapes.Replace(tok)
	case strings.HasPrefix(lower, "<br"):
		return "\\\n"
	}

	return ""
}

// markdownLinkURL is the URL a converted link points to: absolute, and without tracking parameters.
func markdownLinkURL(u string) string {
	return domain.WithoutTrackingParameters(domain.WithScheme(u))
}

// markdownURL is an autolink, or the URL as escaped text if it cannot be one.
func markdownURL(u string) string {
	if strings.ContainsAny(u, "<> ") {
		return markdownEscapes.Replace(u)
	}

	return "<" + u + ">"
}

// markdownDestination is a link destination, in angle brackets if it holds parentheses.
func markdownDestination(u string) string {
	if strings.ContainsAny(u, "()") && !strings.ContainsAny(u, "<>") {
		return "<" + u + ">"
	}

	return strings.NewReplacer(" ", "%20", "<", "%3C", ">", "%3E").Replace(u)
}
