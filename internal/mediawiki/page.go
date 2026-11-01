package mediawiki

import (
	"regexp"
	"strings"
)

// PageType is what a page of the main namespace is for the importer.
type PageType string

// The page types. Only pieces and bards are imported; the others are skipped.
const (
	PageTypePiece    PageType = "piece"
	PageTypeBard     PageType = "bard"
	PageTypeMeta     PageType = "meta"
	PageTypeRedirect PageType = "redirect"
	PageTypeVenue    PageType = "venue"
	PageTypeTest     PageType = "test"
	PageTypeOther    PageType = "other"
)

// The categories that type pages.
const (
	CategoryBards  = "Bards"
	CategoryPieces = "Pieces"
	CategoryVenue  = "Venue"
)

// metaPages are the site's pages about the site itself.
var metaPages = map[string]bool{
	"Main Page": true, "Categories": true, "Collections": true, "Conservation Status": true, "FAQ for Bards": true,
	"Get Started Contributing": true, "Easy Start Add a Song": true, "Bard Preference Checklist": true,
}

// testPage is the one main-namespace page that only tests the wiki's templates.
const testPage = "Roses of the East"

// TypePage returns the type of p. Pages outside the main namespace are PageTypeOther.
func TypePage(p Page) PageType {
	switch {
	case p.NS != NSMain:
		return PageTypeOther
	case p.Redirect:
		return PageTypeRedirect
	case metaPages[p.Title]:
		return PageTypeMeta
	case p.Title == testPage:
		return PageTypeTest
	case p.HasCategory(CategoryBards):
		return PageTypeBard
	case p.HasCategory(CategoryPieces) || hasInfobox(p.Wikitext):
		return PageTypePiece
	case p.HasCategory(CategoryVenue):
		return PageTypeVenue
	}

	return PageTypeOther
}

func hasInfobox(wikitext string) bool {
	for _, t := range FindTemplates(wikitext) {
		if isInfobox(t.Name) {
			return true
		}
	}

	return false
}

func isInfobox(templateName string) bool {
	return strings.HasPrefix(strings.ToLower(templateName), "infobox")
}

// SectionKey is the normalised name of a section heading. Headings with no known meaning are "other:" and their
// plain text.
type SectionKey string

// The section keys.
const (
	SectionLead                   SectionKey = "lead"
	SectionTitle                  SectionKey = "title"
	SectionKeyLinks               SectionKey = "key_links"
	SectionLyrics                 SectionKey = "lyrics"
	SectionText                   SectionKey = "text"
	SectionLicensingEtiquette     SectionKey = "licensing_etiquette"
	SectionPerformancesRecordings SectionKey = "performances_recordings"
	SectionSheetMusic             SectionKey = "sheet_music"
	SectionDancingInstructions    SectionKey = "dancing_instructions"
	SectionTextSources            SectionKey = "text_sources"
	SectionWebsite                SectionKey = "website"
	SectionAffiliations           SectionKey = "affiliations"
	SectionWorks                  SectionKey = "works"
	SectionWorksMusic             SectionKey = "works_music"
	SectionOtherResources         SectionKey = "other_resources"
	SectionNotes                  SectionKey = "notes"
	SectionOtherCategories        SectionKey = "other_categories"
	SectionKnownPerformances      SectionKey = "known_performances"
	SectionTranslations           SectionKey = "translations"
)

const otherSectionPrefix = "other:"

// IsOther reports whether the heading has no known meaning.
func (k SectionKey) IsOther() bool {
	return strings.HasPrefix(string(k), otherSectionPrefix)
}

// IsLyrics reports whether the section is one where the wiki put lyrics or a poem's text.
func (k SectionKey) IsLyrics() bool {
	return k == SectionLyrics || k == SectionText
}

var sectionKeys = []struct {
	heading *regexp.Regexp
	key     SectionKey
}{
	{regexp.MustCompile(`(?i)^key links?$`), SectionKeyLinks},
	{regexp.MustCompile(`(?i)^lyrics$`), SectionLyrics},
	{regexp.MustCompile(`(?i)^text$`), SectionText},
	{regexp.MustCompile(`(?i)^licen[cs]ing (&|and) etiquette$`), SectionLicensingEtiquette},
	{regexp.MustCompile(`(?i)^performances? (&|and) recordings$`), SectionPerformancesRecordings},
	{regexp.MustCompile(`(?i)^sheet music$`), SectionSheetMusic},
	{regexp.MustCompile(`(?i)^dancing instructions$`), SectionDancingInstructions},
	{regexp.MustCompile(`(?i)^timeline of text sources$`), SectionTextSources},
	{regexp.MustCompile(`(?i)^bard[’']s websites?$`), SectionWebsite},
	{regexp.MustCompile(`(?i)^affiliations$`), SectionAffiliations},
	{regexp.MustCompile(`(?i)^songs( (&|and) works)?$`), SectionWorks},
	{regexp.MustCompile(`(?i)^music$`), SectionWorksMusic},
	{regexp.MustCompile(`(?i)^other pages (&|and) external resources$`), SectionOtherResources},
	{regexp.MustCompile(`(?i)^notes$`), SectionNotes},
	{regexp.MustCompile(`(?i)^other categories$`), SectionOtherCategories},
	{regexp.MustCompile(`(?i)^known performances$`), SectionKnownPerformances},
	{regexp.MustCompile(`(?i)^translations$`), SectionTranslations},
}

// KeyOf returns the key of a section: the lead, the title (level 1) or the normalised heading.
func KeyOf(s Section) SectionKey {
	switch s.Level {
	case 0:
		return SectionLead
	case 1:
		return SectionTitle
	}
	t := trimSpace(Plain(s.Heading))
	for _, k := range sectionKeys {
		if k.heading.MatchString(t) {
			return k.key
		}
	}

	return SectionKey(otherSectionPrefix + t)
}

var hintPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\*?[` + jsSpaces + `]*$`),
	regexp.MustCompile(`(?i)^\*[` + jsSpaces + `]*link[` + jsSpaces + `]*$`),
	regexp.MustCompile(`(?i)^\*[` + jsSpaces + `]*\(add sourced information here\)[` + jsSpaces + `]*$`),
	regexp.MustCompile(`(?i)^information about licensing, permissions, and performance etiquette should always be ` +
		`sourced from the author\.?$`),
	regexp.MustCompile(`(?i)^'''note:'''[` + jsSpaces + `]*please ensure (lyrics|poems' texts) are only included here`),
	regexp.MustCompile(`^-{4,}[` + jsSpaces + `]*$`),
}

// IsHint reports whether line is one of the boilerplate hints the page skeletons leave ("* Link", an empty bullet,
// the licensing and lyrics notes, a "----" rule), which are not content.
func IsHint(line string) bool {
	t := trimSpace(line)
	for _, re := range hintPatterns {
		if re.MatchString(t) {
			return true
		}
	}

	return false
}

var licensingUnknown = regexp.MustCompile(`(?i)cannot find a central page`)

// IsLicensingUnknown reports whether a licensing section's text is the boilerplate saying that the bard's
// preferences could not be found, which is a status (licensing unknown), not text.
func IsLicensingUnknown(text string) bool {
	return licensingUnknown.MatchString(text)
}

// SectionContent is what one section holds: its list items, external links, wikilinks (category tags excluded)
// and its text without hints, as plain text. MeaningfulLines counts the lines that are not blank, hints or
// category tags; a section without any is empty.
type SectionContent struct {
	MeaningfulLines int
	Items           []ListItem
	ExternalLinks   []SectionExternalLink
	Wikilinks       []Wikilink
	PlainText       string
}

// Empty reports whether the section has no content.
func (c SectionContent) Empty() bool {
	return c.MeaningfulLines == 0
}

// ListItem is one "*" or "#" line: Depth is the number of leading list markers, Raw the wikitext after them and
// Text its plain text.
type ListItem struct {
	Depth int
	Raw   string
	Text  string
}

// SectionExternalLink is an external link with its text decoded and the trimmed line it is on.
type SectionExternalLink struct {
	ExternalLink
	Line string
}

var (
	categoryTagLine = regexp.MustCompile(`(?i)^\[\[Category:[^\]]+\]\][` + jsSpaces + `]*$`)
	listLine        = regexp.MustCompile(`^[` + jsSpaces + `]*[*#]`)
	listMarkers     = regexp.MustCompile(`^[` + jsSpaces + `]*([*#:]+)`)
	listPrefix      = regexp.MustCompile(`^[` + jsSpaces + `]*[*#:]+[` + jsSpaces + `]*`)
)

// AnalyzeSection returns what a section body holds, ignoring comments and <nowiki> content.
func AnalyzeSection(content string) SectionContent {
	masked := MaskNowiki(StripComments(content))
	lines := strings.Split(masked, "\n")
	var meaningful []string
	var items []ListItem
	for _, l := range lines {
		t := trimSpace(l)
		if t != "" && !IsHint(l) && !categoryTagLine.MatchString(t) {
			meaningful = append(meaningful, l)
		}
		if listLine.MatchString(l) && trimSpace(strings.TrimLeftFunc(l, isListMarkerOrSpace)) != "" && !IsHint(l) {
			raw := listPrefix.ReplaceAllString(l, "")
			items = append(items, ListItem{Depth: len(listMarkers.FindStringSubmatch(l)[1]), Raw: raw, Text: Plain(raw)})
		}
	}
	var external []SectionExternalLink
	for _, e := range FindExternalLinks(masked) {
		e.Text = DecodeEntities(e.Text)
		external = append(external, SectionExternalLink{ExternalLink: e, Line: lineAt(masked, e.Start)})
	}
	var wikilinks []Wikilink
	for _, l := range FindWikilinks(masked) {
		if l.Kind != WikilinkCategoryTag {
			wikilinks = append(wikilinks, l)
		}
	}

	return SectionContent{
		MeaningfulLines: len(meaningful),
		Items:           items,
		ExternalLinks:   external,
		Wikilinks:       wikilinks,
		PlainText:       Plain(strings.Join(meaningful, "\n")),
	}
}

// isListMarkerOrSpace is the reference's [\s*#:].
func isListMarkerOrSpace(r rune) bool {
	return isSpace(r) || r == '*' || r == '#' || r == ':'
}

func lineAt(s string, offset int) string {
	start := strings.LastIndexByte(s[:offset], '\n') + 1
	end := strings.IndexByte(s[offset:], '\n')
	if end < 0 {
		return trimSpace(s[start:])
	}

	return trimSpace(s[start : offset+end])
}

// ParsedPage is a page's wikitext parsed: its infoboxes, its level-1 heading (H1, the display title) and its
// sections with their keys and content.
type ParsedPage struct {
	Infoboxes []Infobox
	H1        string
	HasH1     bool
	Sections  []PageSection
}

// PageSection is one section of a parsed page. Its offsets are offsets into the page's wikitext.
type PageSection struct {
	Section
	Key     SectionKey
	Content SectionContent
}

// Infobox is one infobox template call with its parameters in order, each value parsed.
type Infobox struct {
	Template string
	Params   []InfoboxParam
}

// InfoboxParam is one infobox parameter: its name, its value as written (trimmed) and the value parsed.
type InfoboxParam struct {
	Name  string
	Raw   string
	Value Value
}

// Param returns the parameter called name, the last one if it is given more than once.
func (b Infobox) Param(name string) (InfoboxParam, bool) {
	for k := len(b.Params) - 1; k >= 0; k-- {
		if b.Params[k].Name == name {
			return b.Params[k], true
		}
	}

	return InfoboxParam{}, false
}

var infoboxCall = regexp.MustCompile(`(?is)\{\{[` + jsSpaces + `]*infobox.*?\n\}\}`)

// ParsePage parses a page's wikitext. Sections are found with comments and <nowiki> content masked, so their
// offsets are offsets into wikitext; a section's content is analysed without the first infobox call in it.
func ParsePage(wikitext string) ParsedPage {
	var page ParsedPage
	for _, t := range FindTemplates(MaskNowiki(StripComments(wikitext))) {
		if !isInfobox(t.Name) {
			continue
		}
		box := Infobox{Template: t.Name, Params: make([]InfoboxParam, 0, len(t.Params))}
		for _, p := range t.Params {
			box.Params = append(box.Params, InfoboxParam{Name: p.Name, Raw: p.Value, Value: ParseValue(p.Value)})
		}
		page.Infoboxes = append(page.Infoboxes, box)
	}
	for _, s := range PageSections(wikitext) {
		if s.Level == 1 && !page.HasH1 {
			page.H1, page.HasH1 = s.Heading, true
		}
		body := s.Body(wikitext)
		if loc := infoboxCall.FindStringIndex(body); loc != nil {
			body = body[:loc[0]] + body[loc[1]:]
		}
		page.Sections = append(page.Sections, PageSection{Section: s, Key: KeyOf(s), Content: AnalyzeSection(body)})
	}

	return page
}

// PageSections returns the sections of a page's wikitext, found with comments and <nowiki> content masked; their
// offsets are offsets into wikitext.
func PageSections(wikitext string) []Section {
	return Sections(MaskNowiki(MaskComments(wikitext)))
}
