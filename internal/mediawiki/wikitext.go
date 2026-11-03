package mediawiki

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxNestingDepth caps how deeply templates may nest before FindTemplates gives up on one, so hostile input cannot
// make parsing slower than linear. Real pages nest two deep.
const MaxNestingDepth = 32

// jsSpaces is the class of characters JavaScript's \s matches (Go's \s is ASCII only), for use inside a regexp
// character class.
const jsSpaces = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// isSpace reports whether r is white space as JavaScript's \s and String.prototype.trim define it, which the
// research scripts rely on.
func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}

	return r >= 0x2000 && r <= 0x200a
}

func trimSpace(s string) string {
	return strings.TrimFunc(s, isSpace)
}

// collapseSpaces replaces every run of white space with one space.
func collapseSpaces(s string) string {
	return strings.Join(strings.FieldsFunc(s, isSpace), " ")
}

// NormTitle is a page title as MediaWiki stores it: entities decoded, underscores as spaces, white space collapsed
// and trimmed, and the first letter upper-cased.
func NormTitle(t string) string {
	if t == "" {
		return t
	}
	s := trimSpace(spaceRun.ReplaceAllString(strings.ReplaceAll(DecodeEntities(t), "_", " "), " "))
	first, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}

	return strings.ToUpper(string(first)) + s[size:]
}

var spaceRun = regexp.MustCompile(`[` + jsSpaces + `]+`)

var namedEntities = map[string]string{
	"amp": "&", "lt": "<", "gt": ">", "quot": `"`, "apos": "'", "nbsp": " ", "ndash": "–", "mdash": "—",
	"rsquo": "’", "lsquo": "‘", "hellip": "…",
}

var (
	decimalEntity = regexp.MustCompile(`&#(\d+);`)
	hexEntity     = regexp.MustCompile(`(?i)&#x([0-9a-f]+);`)
	namedEntity   = regexp.MustCompile(`(?i)&([a-z]+);`)
)

// DecodeEntities decodes numeric character references and the named entities the wiki uses, in the reference's
// order: decimal, then hexadecimal, then named. A reference to something that is not a Unicode scalar value is
// left as written.
func DecodeEntities(s string) string {
	s = decimalEntity.ReplaceAllStringFunc(s, func(m string) string {
		return codePoint(m, m[2:len(m)-1], 10)
	})
	s = hexEntity.ReplaceAllStringFunc(s, func(m string) string {
		return codePoint(m, m[3:len(m)-1], 16)
	})

	return namedEntity.ReplaceAllStringFunc(s, func(m string) string {
		if v, ok := namedEntities[strings.ToLower(m[1:len(m)-1])]; ok {
			return v
		}

		return m
	})
}

func codePoint(match, digits string, base int) string {
	n, err := strconv.ParseInt(digits, base, 32)
	if err != nil || !utf8.ValidRune(rune(n)) {
		return match
	}

	return string(rune(n))
}

var (
	comment = regexp.MustCompile(`(?s)<!--.*?-->`)
	nowiki  = regexp.MustCompile(`(?is)<nowiki>.*?</nowiki>`)
)

// StripComments removes every closed HTML comment.
func StripComments(s string) string {
	return comment.ReplaceAllString(s, "")
}

// MaskComments replaces every closed HTML comment with spaces of the same byte length, so that offsets into the
// result are offsets into s.
func MaskComments(s string) string {
	return comment.ReplaceAllStringFunc(s, blank)
}

// MaskNowiki replaces every <nowiki>…</nowiki> element with spaces of the same byte length, so that the links
// inside it are not parsed.
func MaskNowiki(s string) string {
	return nowiki.ReplaceAllStringFunc(s, blank)
}

func blank(m string) string {
	return strings.Repeat(" ", len(m))
}

// Template is one top-level template call {{Name|…}}. Start and End are byte offsets of Raw in the text.
type Template struct {
	Name   string
	Params []TemplateParam
	Start  int
	End    int
	Raw    string
}

// TemplateParam is one parameter of a template call. A positional parameter is named by its position, from "1".
// Value is Raw's value part, trimmed.
type TemplateParam struct {
	Name  string
	Value string
	Raw   string
}

// FindTemplates returns the top-level template calls in text, in order. A template parameter {{{x}}} is not a
// call. A call with no closing braces runs to the end of the text, as in the reference; one nested more than
// MaxNestingDepth deep is not a call, and scanning resumes after its opening braces.
func FindTemplates(text string) []Template {
	var out []Template
	for i := 0; ; {
		next := strings.Index(text[i:], "{{")
		if next < 0 {
			return out
		}
		i += next
		if i+2 < len(text) && text[i+2] == '{' {
			i += 3

			continue
		}
		end, ok := templateEnd(text, i)
		if !ok {
			i += 2

			continue
		}
		out = append(out, parseTemplate(text, i, end))
		i = end
	}
}

// templateEnd returns the end of the template call opening at start: after its matching "}}", or the end of the
// text if it never closes. It reports false if the call nests deeper than MaxNestingDepth.
func templateEnd(text string, start int) (int, bool) {
	depth := 0
	j := start
	for ; j < len(text)-1; j++ {
		switch {
		case text[j] == '{' && text[j+1] == '{':
			depth++
			j++
			if depth > MaxNestingDepth {
				return 0, false
			}
		case text[j] == '}' && text[j+1] == '}':
			depth--
			j++
			if depth == 0 {
				return j + 1, true
			}
		}
	}

	return min(j+1, len(text)), true
}

func parseTemplate(text string, start, end int) Template {
	raw := text[start:end]
	inner := ""
	if len(raw) >= 4 {
		inner = raw[2 : len(raw)-2]
	}
	parts := SplitTopLevel(inner, '|')
	params := make([]TemplateParam, 0, len(parts)-1)
	position := 1
	for _, part := range parts[1:] {
		eq := indexTopLevel(part, '=')
		if eq < 0 {
			params = append(params, TemplateParam{Name: strconv.Itoa(position), Value: trimSpace(part), Raw: part})
			position++

			continue
		}
		params = append(params, TemplateParam{
			Name: trimSpace(part[:eq]), Value: trimSpace(part[eq+1:]), Raw: part,
		})
	}

	return Template{Name: trimSpace(parts[0]), Params: params, Start: start, End: end, Raw: raw}
}

// scanTopLevel calls found for each byte of s outside [[…]] and {{…}} pairs (found's argument) and stops at the
// first for which it returns true, returning that offset, or -1.
func scanTopLevel(s string, found func(k int) bool) int {
	links, braces := 0, 0
	for k := 0; k < len(s); k++ {
		if k+1 < len(s) {
			pair := s[k : k+2]
			switch pair {
			case "[[":
				links++
				k++

				continue
			case "]]":
				links = max(0, links-1)
				k++

				continue
			case "{{":
				braces++
				k++

				continue
			case "}}":
				braces = max(0, braces-1)
				k++

				continue
			}
		}
		if links == 0 && braces == 0 && found(k) {
			return k
		}
	}

	return -1
}

// SplitTopLevel splits s at each sep that is outside [[…]] and {{…}}.
func SplitTopLevel(s string, sep byte) []string {
	var parts []string
	last := 0
	scanTopLevel(s, func(k int) bool {
		if s[k] == sep {
			parts = append(parts, s[last:k])
			last = k + 1
		}

		return false
	})

	return append(parts, s[last:])
}

func indexTopLevel(s string, ch byte) int {
	return scanTopLevel(s, func(k int) bool { return s[k] == ch })
}

// WikilinkKind is what a [[…]] link points at.
type WikilinkKind string

// The wikilink kinds.
const (
	WikilinkPage         WikilinkKind = "page"
	WikilinkCategoryTag  WikilinkKind = "category_tag"
	WikilinkCategoryLink WikilinkKind = "category_link"
	WikilinkFile         WikilinkKind = "file"
	WikilinkFileLink     WikilinkKind = "file_link"
	WikilinkInterwiki    WikilinkKind = "interwiki"
	WikilinkNamespace    WikilinkKind = "ns_link"
)

// The namespaces of the wiki that links name.
const (
	NSMain     = 0
	NSUser     = 2
	NSProject  = 4
	NSFile     = 6
	NSTemplate = 10
	NSCategory = 14
	// NSSpecial is the namespace of the special pages, which have no page in the dump.
	NSSpecial = -1
)

var namespacePrefixes = map[string]int{
	"category": NSCategory, "file": NSFile, "image": NSFile, "template": NSTemplate, "help": 12, "project": NSProject,
	"user": NSUser, "special": NSSpecial, "mediawiki": 8, "module": 828, "bardic torch": NSProject,
}

var interwikiPrefixes = map[string]bool{"w": true, "wikipedia": true, "wikt": true, "mw": true, "meta": true,
	"commons": true}

// Wikilink is one [[…]] link. Target is the normalised title without its namespace (an interwiki target is kept
// as written, prefix included). Piped is whether the link has a display part, which may be empty. Start and End
// are byte offsets of Raw.
type Wikilink struct {
	Raw          string
	Target       string
	Display      string
	Piped        bool
	Kind         WikilinkKind
	NS           int
	Fragment     string
	LeadingColon bool
	Start        int
	End          int
}

// Label is the text a link shows: its display part if it has one, otherwise its target.
func (l Wikilink) Label() string {
	if l.Piped {
		return l.Display
	}

	return l.Target
}

var wikilink = regexp.MustCompile(`\[\[([^\[\]]*?)\]\]`)

// FindWikilinks returns every [[…]] link in text, in order.
func FindWikilinks(text string) []Wikilink {
	matches := wikilink.FindAllStringSubmatchIndex(text, -1)
	out := make([]Wikilink, 0, len(matches))
	for _, m := range matches {
		out = append(out, parseWikilink(text[m[0]:m[1]], text[m[2]:m[3]], m[0]))
	}

	return out
}

func parseWikilink(raw, inner string, start int) Wikilink {
	l := Wikilink{Raw: raw, Start: start, End: start + len(raw)}
	target := inner
	if pipe := strings.IndexByte(inner, '|'); pipe >= 0 {
		target = inner[:pipe]
		l.Display = trimSpace(inner[pipe+1:])
		l.Piped = true
	}
	target = trimSpace(target)
	if strings.HasPrefix(target, ":") {
		l.LeadingColon = true
		target = trimSpace(target[1:])
	}
	if hash := strings.IndexByte(target, '#'); hash >= 0 {
		l.Fragment = target[hash+1:]
		target = trimSpace(target[:hash])
	}
	name, interwiki := target, false
	if colon := strings.IndexByte(target, ':'); colon > 0 {
		prefix := strings.ToLower(trimSpace(target[:colon]))
		if ns, ok := namespacePrefixes[prefix]; ok {
			l.NS = ns
			name = trimSpace(target[colon+1:])
		} else {
			interwiki = interwikiPrefixes[prefix]
		}
	}
	l.Kind = wikilinkKind(l.NS, l.LeadingColon, interwiki)
	if interwiki {
		l.Target = target
	} else {
		l.Target = NormTitle(name)
	}

	return l
}

func wikilinkKind(ns int, leadingColon, interwiki bool) WikilinkKind {
	switch {
	case interwiki:
		return WikilinkInterwiki
	case ns == NSCategory && leadingColon:
		return WikilinkCategoryLink
	case ns == NSCategory:
		return WikilinkCategoryTag
	case ns == NSFile && leadingColon:
		return WikilinkFileLink
	case ns == NSFile:
		return WikilinkFile
	case ns != NSMain:
		return WikilinkNamespace
	}

	return WikilinkPage
}

// ExternalLink is one external link: bracketed ([url text]) or a bare URL. Text is the bracketed link's text,
// trimmed, or empty. Start and End are the byte offsets of the link in the text.
type ExternalLink struct {
	URL       string
	Text      string
	Bracketed bool
	Start     int
	End       int
}

var bracketedLink = regexp.MustCompile(`(?i)\[((?:https?:)?//[^\]` + jsSpaces + `]+|www\.[^\]` + jsSpaces +
	`]+)(?:[` + jsSpaces + `]+([^\]]*))?\]`)

// FindExternalLinks returns every bracketed external link and every bare http(s) URL outside one, in order of
// position. A bare URL directly after one of [ = / " ' or a word character is not a link (it is a parameter value
// or part of a longer string), and trailing . , ; : ) are not part of it.
func FindExternalLinks(text string) []ExternalLink {
	var out []ExternalLink
	for _, m := range bracketedLink.FindAllStringSubmatchIndex(text, -1) {
		link := ExternalLink{URL: text[m[2]:m[3]], Bracketed: true, Start: m[0], End: m[1]}
		if m[4] >= 0 {
			link.Text = trimSpace(text[m[4]:m[5]])
		}
		out = append(out, link)
	}
	bracketed := len(out)
	for _, bare := range bareURLs(text) {
		if !insideLink(out[:bracketed], bare.Start) {
			out = append(out, bare)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Start < out[b].Start })

	return out
}

// insideLink reports whether offset is inside one of links, which are sorted and do not overlap.
func insideLink(links []ExternalLink, offset int) bool {
	k := sort.Search(len(links), func(k int) bool { return links[k].End > offset })

	return k < len(links) && links[k].Start <= offset
}

// bareURLs scans text for bare URLs as the reference's /(?<![\[=\/"'])\bhttps?:\/\/[^\s\]\[<>|{}"]+/gi does,
// which RE2 cannot express: the matches do not overlap, and each is trimmed of trailing punctuation.
func bareURLs(text string) []ExternalLink {
	var out []ExternalLink
	for i := 0; i+4 <= len(text); {
		end, ok := bareURLAt(text, i)
		if !ok {
			i++

			continue
		}
		url := strings.TrimRight(text[i:end], ".,;:)")
		out = append(out, ExternalLink{URL: url, Start: i, End: i + len(url)})
		i = end
	}

	return out
}

// bareURLAt returns the end of the bare URL starting at i, before trimming, if one does.
func bareURLAt(text string, i int) (int, bool) {
	if !strings.EqualFold(text[i:i+4], "http") || !bareURLMayStart(text, i) {
		return 0, false
	}
	rest := i + 4
	if rest < len(text) && (text[rest] == 's' || text[rest] == 'S') && strings.HasPrefix(text[rest+1:], "://") {
		rest++
	}
	if !strings.HasPrefix(text[rest:], "://") {
		return 0, false
	}
	end := rest + 3
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if isSpace(r) || strings.ContainsRune(`][<>|{}"`, r) {
			break
		}
		end += size
	}

	return end, end > rest+3
}

// bareURLMayStart is the reference's (?<![\[=\/"'])\b before the "h" at i.
func bareURLMayStart(text string, i int) bool {
	if i == 0 {
		return true
	}
	prev := text[i-1]

	return !isASCIIWord(prev) && !strings.ContainsRune(`[=/"'`, rune(prev))
}

func isASCIIWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// Section is one section of a page: the lead (Level 0, no heading) or a heading of level 1 to 6 and the text up to
// the next heading. Start is the byte offset of the heading line; the body is text[BodyStart:End].
type Section struct {
	Level     int
	Heading   string
	Start     int
	BodyStart int
	End       int
}

// Body returns the section's body in text, the text Sections was given.
func (s Section) Body(text string) string {
	return text[s.BodyStart:s.End]
}

// Sections splits text at its headings, as the reference's /^(={1,6})\s*(.*?)\s*\1\s*$/gm does (RE2 has no
// backreference, so the match is hand-coded): a heading is a line that starts and ends with the same run of 1 to 6
// "=", the longest run that fits, and its text is what is between them, trimmed, so "== a = b ==" is level 2
// "a = b" and "=x=" is level 1 "x". As in the reference, the blank lines after a heading are not part of its
// body. The first section is the lead, which is empty if the text starts with a heading. Unlike the reference, a
// heading never spans two lines.
func Sections(text string) []Section {
	var heads []Section
	for lineStart := 0; lineStart < len(text); {
		lineEnd := strings.IndexByte(text[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text)
		} else {
			lineEnd += lineStart
		}
		level, heading, closeEnd, ok := parseHeading(text, lineStart, lineEnd)
		if !ok {
			lineStart = lineEnd + 1

			continue
		}
		bodyStart := headingBodyStart(text, closeEnd)
		heads = append(heads, Section{Level: level, Heading: heading, Start: lineStart, BodyStart: bodyStart})
		lineStart = bodyStart + 1
	}
	out := make([]Section, 0, len(heads)+1)
	leadEnd := len(text)
	if len(heads) > 0 {
		leadEnd = heads[0].Start
	}
	out = append(out, Section{End: leadEnd})
	for k, h := range heads {
		h.End = len(text)
		if k+1 < len(heads) {
			h.End = heads[k+1].Start
		}
		out = append(out, h)
	}

	return out
}

// parseHeading parses text[lineStart:lineEnd] as a heading line, returning its level, its text and the offset
// after its closing "=" run.
func parseHeading(text string, lineStart, lineEnd int) (level int, heading string, closeEnd int, ok bool) {
	line := strings.TrimRightFunc(text[lineStart:lineEnd], isSpace)
	leading := len(line) - len(strings.TrimLeft(line, "="))
	if leading == 0 {
		return 0, "", 0, false
	}
	trailing := len(line) - len(strings.TrimRight(line, "="))
	level = min(leading, trailing, 6, len(line)/2)
	if level == 0 {
		return 0, "", 0, false
	}
	heading = trimSpace(line[level : len(line)-level])
	if strings.ContainsAny(heading, "\r\u2028\u2029") {
		return 0, "", 0, false
	}

	return level, heading, lineStart + len(line), true
}

// headingBodyStart is where the body after a heading starts: the reference's greedy \s*$ takes the white space
// after the heading up to its last line break, or to the end of the text if only white space follows.
func headingBodyStart(text string, closeEnd int) int {
	runEnd := closeEnd
	for runEnd < len(text) {
		r, size := utf8.DecodeRuneInString(text[runEnd:])
		if !isSpace(r) {
			break
		}
		runEnd += size
	}
	if runEnd == len(text) {
		return len(text)
	}

	lastBreak := strings.LastIndexByte(text[closeEnd:runEnd], '\n')
	if lastBreak < 0 {
		return runEnd
	}

	return closeEnd + lastBreak
}

var (
	plainWikilink = regexp.MustCompile(`\[\[(?:[^\]|]*\|)?([^\]]*)\]\]`)
	plainExternal = regexp.MustCompile(`\[(?:https?:)?//[^\]` + jsSpaces + `]+[` + jsSpaces + `]*([^\]]*)\]`)
	boldItalic    = regexp.MustCompile(`'{2,}`)
	htmlTag       = regexp.MustCompile(`<[^>]+>`)
)

// Plain is s as plain text: links replaced by their labels, bold and italic quotes and HTML tags removed,
// entities decoded, trimmed.
func Plain(s string) string {
	s = plainWikilink.ReplaceAllString(s, "$1")
	s = plainExternal.ReplaceAllString(s, "$1")
	s = boldItalic.ReplaceAllString(s, "")
	s = htmlTag.ReplaceAllString(s, "")

	return trimSpace(DecodeEntities(s))
}
