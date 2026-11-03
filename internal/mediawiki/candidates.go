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
	// CandidateSlashPair is either name of "Capitalised Name / Capitalised Name", which pairs an SCA name with
	// another name of the same person; a side whose partner is public is a legal name (PrivacyPairedName).
	CandidateSlashPair CandidateRule = "slash_pair"
	// CandidateCredited is a name after "credited to", "attributed to", "copyright (to)" or "©", a year before
	// it allowed: "attributed to \"2008 Name Name\"".
	CandidateCredited CandidateRule = "credited"
	// CandidatePossessive is a name before "'s" and a word naming what a person owns or made ("Name Name's
	// YouTube", "… blog", "… songbook").
	CandidatePossessive CandidateRule = "possessive"
	// CandidatePerformer is a name before a verb of singing, performing or writing ("Name Name singing").
	CandidatePerformer CandidateRule = "performer"
	// CandidateConjunction is either name of "Name Name and Name Name" or "Name Name & Name Name".
	CandidateConjunction CandidateRule = "conjunction"
	// CandidateAlsoKnownAs is each name of a comma-separated list after "also known as".
	CandidateAlsoKnownAs CandidateRule = "also_known_as"
	// CandidateSiteName is the two-word label of an external link whose host is the label's words joined: a
	// website named after a person ("[https://wren-placeholder.example Wren Placeholder]").
	CandidateSiteName CandidateRule = "site_name"
)

// NameCandidate is a string of a page that may be a person's name: wikitext[Offset:Offset+len(Text)] is Text,
// found by Rule in the section numbered Section (PageSections).
// For CandidateSlashPair, Partner is the other side of the pair.
type NameCandidate struct {
	PageID  int64
	Section int
	Offset  int
	Text    string
	Rule    CandidateRule
	Partner string
}

var (
	parenPair = regexp.MustCompile(`(` + capitalisedName + `)[ \t]*\((` + multiwordName + `)\)`)
	byFrom    = regexp.MustCompile(`\b(?:[Bb]y|[Ff]rom)[ \t]+(` + multiwordName + `)`)
	slashPair = regexp.MustCompile(`(` + multiwordName + `)[ \t]*/[ \t]*(` + multiwordName + `)`)
	credited  = regexp.MustCompile(`(?i:\b(?:credited|attributed)[ \t]+to|\bcopyright(?:ed)?(?:[ \t]+to)?|©)[ \t]*` +
		`["'‘“]?(?:\d{4},?[ \t]+)?(` + multiwordName + `)`)
	possessive = regexp.MustCompile(`(` + multiwordName + `)[` + apostrophes + `]s?[ \t]+` +
		`(?i:youtube|blog|channel|page|site|website|` +
		`homepage|songbook|collection|version|album|recording|book)\b`)
	performer = regexp.MustCompile(`(` + multiwordName + `)[ \t]+(?:sings|singing|sang|performs|performing|` +
		`performed|wrote|writes|composed|claims)\b`)
	conjunction   = regexp.MustCompile(`(` + multiwordName + `)[ \t]*(?:&|\band\b)[ \t]*(` + multiwordName + `)`)
	alsoKnownAs   = regexp.MustCompile(`(?i:\balso[ \t]+known[ \t]+as)[ \t]*:?[ \t]*([^\n]*)`)
	alsoKnownName = regexp.MustCompile(`^` + capitalisedName + `$`)
	siteLabel     = regexp.MustCompile(`\[https?://([^/\s\]]+)[^\s\]]*[ \t]+(` + capitalisedWord + `[ \t]+` +
		capitalisedWord + `)\]`)
)

// PersonNameCandidates returns every string of a page that may be a person's name, failing closed: the scrubber
// and the importer classify each (ClassifyPersonAlias) and treat any they cannot classify confidently as a legal
// name. It looks at the raw wikitext, comments and <nowiki> content included. The rules are the H1
// parentheticals, "Capitalised (Capitalised …)" pairs, the "legal name", "mundane name", "real name" and "aka"
// markers, the uploaders of ParsePerformanceText on every line, the two-word label of a link to a website named by
// those words, and, outside links (proseCandidates), two to four capitalised words after "by" or "from", on either
// side of "/", "and" or "&", after "credited to", "attributed to" or "copyright", before a possessive naming what a
// person owns ("'s YouTube") or a verb of singing or writing, and in an "also known as" list (CandidateRule).
// Candidates are sorted by offset, then rule.
func PersonNameCandidates(p Page, maxYear int) []NameCandidate {
	text := p.Wikitext
	sections := PageSections(text)
	var out []NameCandidate
	add := func(offset int, name string, rule CandidateRule, partner string) {
		out = append(out, NameCandidate{PageID: p.PageID, Section: sectionAt(sections, offset), Offset: offset,
			Text: name, Rule: rule, Partner: partner})
	}
	for _, s := range sections {
		if s.Level == 1 {
			h1Candidates(text, s, add)
		}
	}
	for _, m := range parenPair.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], text[m[2]:m[3]], CandidateParenPair, "")
		add(m[4], text[m[4]:m[5]], CandidateParenPair, "")
	}
	for _, m := range nameMarker.FindAllStringSubmatchIndex(text, -1) {
		add(m[4], text[m[4]:m[5]], CandidateMarker, "")
	}
	for _, m := range siteLabel.FindAllStringSubmatchIndex(text, -1) {
		if hostSpells(text[m[2]:m[3]], text[m[4]:m[5]]) {
			add(m[4], text[m[4]:m[5]], CandidateSiteName, "")
		}
	}
	proseCandidates(text, add)
	for lineStart := 0; lineStart < len(text); {
		lineEnd := strings.IndexByte(text[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text) - lineStart
		}
		if u := ParsePerformanceText(text[lineStart:lineStart+lineEnd], maxYear).Uploader; u.Text != "" {
			add(lineStart+u.Start, u.Text, CandidateUploader, "")
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

// addCandidate adds a candidate at offset; partner is the other side of a CandidateSlashPair, or "".
type addCandidate func(offset int, name string, rule CandidateRule, partner string)

// proseCandidates adds the candidates of the rules that read text outside links: "by" and "from" anywhere, and the
// others outside headings and infobox title fields too (maskTitles). A name led by an article is a title.
func proseCandidates(text string, add addCandidate) {
	unlinked := maskLinkTargets(text)
	for _, m := range byFrom.FindAllStringSubmatchIndex(unlinked, -1) {
		add(m[2], text[m[2]:m[3]], CandidateByFrom, "")
	}
	prose := maskTitles(unlinked)
	for _, m := range slashPair.FindAllStringSubmatchIndex(prose, -1) {
		if left, right := text[m[2]:m[3]], text[m[4]:m[5]]; !titleLed(left) && !titleLed(right) {
			add(m[2], left, CandidateSlashPair, right)
			add(m[4], right, CandidateSlashPair, left)
		}
	}
	for _, m := range conjunction.FindAllStringSubmatchIndex(prose, -1) {
		if left, right := text[m[2]:m[3]], text[m[4]:m[5]]; !titleLed(left) && !titleLed(right) {
			add(m[2], left, CandidateConjunction, "")
			add(m[4], right, CandidateConjunction, "")
		}
	}
	for _, r := range []struct {
		re   *regexp.Regexp
		rule CandidateRule
	}{{credited, CandidateCredited}, {possessive, CandidatePossessive}, {performer, CandidatePerformer}} {
		for _, m := range r.re.FindAllStringSubmatchIndex(prose, -1) {
			if !titleLed(text[m[2]:m[3]]) {
				add(m[2], text[m[2]:m[3]], r.rule, "")
			}
		}
	}
	for _, m := range alsoKnownAs.FindAllStringSubmatchIndex(prose, -1) {
		alsoKnownAsCandidates(text, m[2], m[3], add)
	}
}

// alsoKnownAsCandidates adds each name of the list text[start:end] after "also known as", split at "," ";" "/"
// and "or", that is a capitalised name.
func alsoKnownAsCandidates(text string, start, end int, add addCandidate) {
	list := text[start:end]
	at := 0
	for _, sep := range alsoKnownSeparator.FindAllStringIndex(list+",", -1) {
		part := list[at:min(sep[0], len(list))]
		trimmed := strings.TrimLeftFunc(part, isSpace)
		offset := start + at + len(part) - len(trimmed)
		trimmed = strings.TrimRightFunc(trimmed, func(r rune) bool { return isSpace(r) || r == '.' })
		if alsoKnownName.MatchString(trimmed) {
			add(offset, trimmed, CandidateAlsoKnownAs, "")
		}
		at = min(sep[1], len(list))
	}
}

var alsoKnownSeparator = regexp.MustCompile(`[,;/]|\bor\b`)

// titleLed reports whether name starts with an article, as titles do and person names do not ("The Fyne").
func titleLed(name string) bool {
	first, _, _ := strings.Cut(name, " ")

	return first == "The" || first == "A" || first == "An"
}

// maskTitles replaces every heading line and every infobox title and other_titles value with spaces, keeping
// offsets: section names ("Other Pages & External Resources") are not prose, an H1's names are read by
// h1Candidates, and a piece's titles ("Title (Other / Another)") name the piece, never a person.
func maskTitles(text string) string {
	b := []byte(text)
	blank := func(start, end int) {
		for k := start; k < end; k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
	}
	for _, s := range Sections(text) {
		blank(s.Start, s.BodyStart)
	}
	for _, m := range titleField.FindAllStringIndex(text, -1) {
		blank(m[0], m[1])
	}

	return string(b)
}

// titleField is an infobox's title or other_titles line.
var titleField = regexp.MustCompile(`(?m)^[ \t]*\|[ \t]*(?:` + titleParam + `|` + otherTitlesParam +
	`)[ \t]*=.*$`)

// hostSpells reports whether host, without "www." and its top-level domain, is the words of label joined, with
// or without "-" or "_" between them.
func hostSpells(host, label string) bool {
	host = strings.ToLower(strings.TrimPrefix(strings.ToLower(host), "www."))
	if dot := strings.LastIndexByte(host, '.'); dot > 0 {
		host = host[:dot]
	}
	words := foldedWords(label)
	for _, sep := range []string{"", "-", "_"} {
		if host == strings.Join(words, sep) {
			return true
		}
	}

	return false
}

// h1Candidates adds each name of each parenthetical of the H1 heading s, split at ", / ; aka".
func h1Candidates(text string, s Section, add addCandidate) {
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

func addTrimmed(offset int, name string, add addCandidate) {
	trimmed := strings.TrimLeftFunc(name, isSpace)
	offset += len(name) - len(trimmed)
	if trimmed = strings.TrimRightFunc(trimmed, isSpace); trimmed != "" {
		add(offset, trimmed, CandidateH1Parenthetical, "")
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
