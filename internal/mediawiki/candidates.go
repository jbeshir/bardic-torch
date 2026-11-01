package mediawiki

import (
	"regexp"
	"sort"
	"strings"
)

// CandidateRule is the rule that found a possible person name.
type CandidateRule string

// The candidate rules.
const (
	// CandidateH1Parenthetical is a name in parentheses in an H1: "= Title (Other Name) =".
	CandidateH1Parenthetical CandidateRule = "h1_parenthetical"
	// CandidateParenPair is either name of "Capitalised Name (Capitalised Name)" anywhere in the text.
	CandidateParenPair CandidateRule = "paren_pair"
	// CandidateMarker is a name after "legal name", "mundane name", "aka" or "a.k.a.".
	CandidateMarker CandidateRule = "marker"
	// CandidateByFrom is two to four capitalised words after "by" or "from", outside links.
	CandidateByFrom CandidateRule = "by_from"
	// CandidateUploader is an uploader named by a performance item's wording (ParsePerformanceText).
	CandidateUploader CandidateRule = "uploader"
)

// NameCandidate is a string of a page that may be a person's name: wikitext[Offset:Offset+len(Text)] is Text,
// found by Rule in the section numbered Section (PageSections).
type NameCandidate struct {
	PageID  int64
	Section int
	Offset  int
	Text    string
	Rule    CandidateRule
}

var (
	parenPair = regexp.MustCompile(`(` + capitalisedName + `)[ \t]*\((` + capitalisedWord + `(?:[ \t]+(?:` +
		nameParticles + `[ \t]+)*` + capitalisedWord + `){1,3})\)`)
	byFrom = regexp.MustCompile(`\b(?:[Bb]y|[Ff]rom)[ \t]+(` + capitalisedWord + `(?:[ \t]+(?:` + nameParticles +
		`[ \t]+)*` + capitalisedWord + `){1,3})`)
)

// PersonNameCandidates returns every string of a page that may be a person's name, failing closed: the scrubber
// and the importer classify each (ClassifyPersonAlias) and treat any they cannot classify confidently as a legal
// name. It looks at the raw wikitext, comments and <nowiki> content included. The rules are the H1
// parentheticals, "Capitalised (Capitalised …)" pairs, the "legal name", "mundane name" and "aka" markers, "by" or
// "from" followed by two to four capitalised words outside links, and the uploaders of ParsePerformanceText on
// every line. Candidates are sorted by offset, then rule.
func PersonNameCandidates(p Page, maxYear int) []NameCandidate {
	text := p.Wikitext
	sections := PageSections(text)
	var out []NameCandidate
	add := func(offset int, name string, rule CandidateRule) {
		out = append(out, NameCandidate{
			PageID: p.PageID, Section: sectionAt(sections, offset), Offset: offset, Text: name, Rule: rule,
		})
	}
	for _, s := range sections {
		if s.Level == 1 {
			h1Candidates(text, s, add)
		}
	}
	for _, m := range parenPair.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], text[m[2]:m[3]], CandidateParenPair)
		add(m[4], text[m[4]:m[5]], CandidateParenPair)
	}
	for _, m := range nameMarker.FindAllStringSubmatchIndex(text, -1) {
		add(m[4], text[m[4]:m[5]], CandidateMarker)
	}
	unlinked := maskLinkTargets(text)
	for _, m := range byFrom.FindAllStringSubmatchIndex(unlinked, -1) {
		add(m[2], text[m[2]:m[3]], CandidateByFrom)
	}
	for lineStart := 0; lineStart < len(text); {
		lineEnd := strings.IndexByte(text[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text) - lineStart
		}
		if u := ParsePerformanceText(text[lineStart:lineStart+lineEnd], maxYear).Uploader; u.Text != "" {
			add(lineStart+u.Start, u.Text, CandidateUploader)
		}
		lineStart += lineEnd + 1
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Offset != out[b].Offset {
			return out[a].Offset < out[b].Offset
		}

		return out[a].Rule < out[b].Rule
	})

	return out
}

// h1Candidates adds each name of each parenthetical of the H1 heading s, split at ", / ; aka".
func h1Candidates(text string, s Section, add func(int, string, CandidateRule)) {
	line := text[s.Start:s.BodyStart]
	for _, m := range parenthetical.FindAllStringSubmatchIndex(line, -1) {
		group := line[m[2]:m[3]]
		at := 0
		for _, sep := range aliasSeparator.FindAllStringIndex(group, -1) {
			addTrimmed(s.Start+m[2]+at, group[at:sep[0]], add)
			at = sep[1]
		}
		addTrimmed(s.Start+m[2]+at, group[at:], add)
	}
}

func addTrimmed(offset int, name string, add func(int, string, CandidateRule)) {
	trimmed := strings.TrimLeftFunc(name, isSpace)
	offset += len(name) - len(trimmed)
	if trimmed = strings.TrimRightFunc(trimmed, isSpace); trimmed != "" {
		add(offset, trimmed, CandidateH1Parenthetical)
	}
}

// maskLinkTargets replaces every wikilink and the URL of every external link with spaces, keeping offsets, so
// that only text outside links is read.
func maskLinkTargets(text string) string {
	b := []byte(maskURLs(text))
	for _, l := range FindWikilinks(text) {
		for k := l.Start; k < l.End; k++ {
			b[k] = ' '
		}
	}

	return string(b)
}
