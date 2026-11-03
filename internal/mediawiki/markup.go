package mediawiki

import (
	"regexp"
	"sort"
	"strings"
)

// MarkupIssueKind is a kind of broken markup.
type MarkupIssueKind string

// The markup issue kinds.
const (
	// MarkupDoubleBracketURL is an external URL in double brackets, [[https://… text]], which RepairMarkup turns
	// into an external link.
	MarkupDoubleBracketURL MarkupIssueKind = "double_bracket_url"
	// MarkupSingleBracketCategory is a category link with one opening bracket, [:Category:X|Y], which RepairMarkup
	// turns into [[:Category:X|Y]] (its "|" would otherwise split an infobox parameter).
	MarkupSingleBracketCategory MarkupIssueKind = "single_bracket_category"
	// MarkupUnbalancedWikilinks is a "[[" or "]]" without its pair.
	MarkupUnbalancedWikilinks MarkupIssueKind = "unbalanced_wikilinks"
	// MarkupUnbalancedTemplates is a "{{" or "}}" without its pair.
	MarkupUnbalancedTemplates MarkupIssueKind = "unbalanced_templates"
)

// MarkupIssue is one piece of broken markup at a byte offset of the wikitext it was found in.
type MarkupIssue struct {
	Kind   MarkupIssueKind
	Offset int
}

var (
	doubleBracketURL = regexp.MustCompile(`(?i)\[\[((?:https?:)?//[^\]|` + jsSpaces + `]+)(?:[` + jsSpaces +
		`]+([^\]]*))?\]\]`)
	singleBracketCategory = regexp.MustCompile(`(?i)\[:Category:[^\]]*\]\]?`)
)

// RepairMarkup returns wikitext with the two repairable mistakes the wiki has fixed: [[https://… text]] becomes
// [https://… text], and [:Category:X|Y] (or [:Category:X|Y]]) becomes [[:Category:X|Y]]. Each repair is an issue
// at its offset in wikitext.
func RepairMarkup(wikitext string) (string, []MarkupIssue) {
	type repair struct {
		start, end  int
		replacement string
		kind        MarkupIssueKind
	}
	var repairs []repair
	for _, m := range doubleBracketURL.FindAllStringSubmatchIndex(wikitext, -1) {
		r := "[" + wikitext[m[2]:m[3]]
		if m[4] >= 0 {
			r += " " + wikitext[m[4]:m[5]]
		}
		repairs = append(repairs, repair{start: m[0], end: m[1], replacement: r + "]", kind: MarkupDoubleBracketURL})
	}
	for _, m := range singleBracketCategory.FindAllStringIndex(wikitext, -1) {
		if m[0] > 0 && wikitext[m[0]-1] == '[' {
			continue
		}
		inner := strings.TrimRight(wikitext[m[0]+1:m[1]], "]")
		repairs = append(repairs, repair{
			start: m[0], end: m[1], replacement: "[[" + inner + "]]", kind: MarkupSingleBracketCategory,
		})
	}
	if len(repairs) == 0 {
		return wikitext, nil
	}
	sort.SliceStable(repairs, func(a, b int) bool { return repairs[a].start < repairs[b].start })
	var b strings.Builder
	var issues []MarkupIssue
	last := 0
	for _, r := range repairs {
		if r.start < last {
			continue
		}
		b.WriteString(wikitext[last:r.start])
		b.WriteString(r.replacement)
		last = r.end
		issues = append(issues, MarkupIssue{Kind: r.kind, Offset: r.start})
	}
	b.WriteString(wikitext[last:])

	return b.String(), issues
}

// FindMarkupIssues returns the first unpaired "[[" or "]]" and the first unpaired "{{" or "}}" of wikitext, with
// <nowiki> content masked.
func FindMarkupIssues(wikitext string) []MarkupIssue {
	masked := MaskNowiki(wikitext)
	var issues []MarkupIssue
	if at, ok := firstUnpaired(masked, "[[", "]]"); ok {
		issues = append(issues, MarkupIssue{Kind: MarkupUnbalancedWikilinks, Offset: at})
	}
	if at, ok := firstUnpaired(masked, "{{", "}}"); ok {
		issues = append(issues, MarkupIssue{Kind: MarkupUnbalancedTemplates, Offset: at})
	}

	return issues
}

// firstUnpaired returns the offset of the first open that is never closed or close that closes nothing.
func firstUnpaired(text, open, closer string) (int, bool) {
	var opens []int
	for k := 0; k+1 < len(text); k++ {
		switch text[k : k+2] {
		case open:
			opens = append(opens, k)
			k++
		case closer:
			if len(opens) == 0 {
				return k, true
			}
			opens = opens[:len(opens)-1]
			k++
		}
	}
	if len(opens) > 0 {
		return opens[0], true
	}

	return 0, false
}
