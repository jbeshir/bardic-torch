package mediawiki

import (
	"regexp"
	"sort"
	"strings"
)

var (
	doubleBracketURL = regexp.MustCompile(`(?i)\[\[((?:https?:)?//[^\]|` + jsSpaces + `]+)(?:[` + jsSpaces +
		`]+([^\]]*))?\]\]`)
	singleBracketCategory = regexp.MustCompile(`(?i)\[:Category:[^\]]*\]\]?`)
)

// RepairMarkup returns wikitext with the two repairable mistakes the wiki has fixed: [[https://… text]] becomes
// [https://… text], and [:Category:X|Y] (or [:Category:X|Y]]) becomes [[:Category:X|Y]] (its "|" would otherwise
// split an infobox parameter).
func RepairMarkup(wikitext string) string {
	type repair struct {
		start, end  int
		replacement string
	}
	var repairs []repair
	for _, m := range doubleBracketURL.FindAllStringSubmatchIndex(wikitext, -1) {
		r := "[" + wikitext[m[2]:m[3]]
		if m[4] >= 0 {
			r += " " + wikitext[m[4]:m[5]]
		}
		repairs = append(repairs, repair{start: m[0], end: m[1], replacement: r + "]"})
	}
	for _, m := range singleBracketCategory.FindAllStringIndex(wikitext, -1) {
		if m[0] > 0 && wikitext[m[0]-1] == '[' {
			continue
		}
		inner := strings.TrimRight(wikitext[m[0]+1:m[1]], "]")
		repairs = append(repairs, repair{start: m[0], end: m[1], replacement: "[[" + inner + "]]"})
	}
	if len(repairs) == 0 {
		return wikitext
	}
	sort.SliceStable(repairs, func(a, b int) bool { return repairs[a].start < repairs[b].start })
	var b strings.Builder
	last := 0
	for _, r := range repairs {
		if r.start < last {
			continue
		}
		b.WriteString(wikitext[last:r.start])
		b.WriteString(r.replacement)
		last = r.end
	}
	b.WriteString(wikitext[last:])

	return b.String()
}
