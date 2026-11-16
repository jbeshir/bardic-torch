package mediawiki

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MinPerformanceYear is the earliest year a performance can have: the SCA was founded in 1966.
const MinPerformanceYear = 1966

// TextSpan is a piece of a text, text[Start:End], whose text is Text (for a link, its label). The zero value is
// no span.
type TextSpan struct {
	Text  string
	Start int
	End   int
}

// PerformanceText is what the wording of one performance item says, as written: the linked performers, the event,
// the year, the piece sung and every page link, in order. Offsets are offsets into the item. Who uploaded a
// recording is not read: the wording names the uploader, the artist and the singer alike ("by X on YouTube"), and
// the description keeps it as written.
type PerformanceText struct {
	Performers []Wikilink
	Event      TextSpan
	Year       int
	Piece      string
	PageLinks  []Wikilink
}

var (
	performerBefore = regexp.MustCompile(`(?i)(?:\bof|\bby|,|&|\band)[ \t]*$`)
	tuneBefore      = regexp.MustCompile(`(?i)\btune[ \t]+by[ \t]*$`)
	eventStart      = regexp.MustCompile(`(?i)\bat[ \t]+(?:the[ \t]+)?`)
	eventEnd        = regexp.MustCompile(`(?i)[,.;\]\[(\n]|[ \t]+(?:singing|performing|sung|performed|recorded)\b|` +
		`[ \t]+-[ \t]`)
	trailingYear = regexp.MustCompile(`(?:[ \t]+(?:in[ \t]+)?\d{4})+[ \t]*$`)
	leadingYear  = regexp.MustCompile(`^\d{4}[ \t]+`)
	yearWord     = regexp.MustCompile(`\b\d{4}\b`)
	pieceSung    = regexp.MustCompile(`(?i)\bsinging[ \t]+$`)
	eventPlace   = regexp.MustCompile(`[ \t]+(?:at|for|on)[ \t]+`)
)

// ParsePerformanceText reads a performance item's wikitext (without its bullet):
//   - the performers: the page links after "of", "by", or a list separator, but not after "tune by";
//   - the event after "at": a linked page, or text up to a comma, full stop, "singing" or a further "at", "for"
//     or "on", without a year, and only if it is capitalised ("at around the 10:52 mark" is no event);
//   - the year: the first four-digit number from MinPerformanceYear to maxYear outside URLs;
//   - the piece: the page link after "singing".
func ParsePerformanceText(item string, maxYear int) PerformanceText {
	masked := maskURLs(item)
	var res PerformanceText
	links := FindWikilinks(masked)
	last := 0
	for _, l := range links {
		before := masked[last:l.Start]
		last = l.End
		if l.Kind != WikilinkPage {
			continue
		}
		res.PageLinks = append(res.PageLinks, l)
		switch {
		case pieceSung.MatchString(before):
			res.Piece = l.Target
		case performerBefore.MatchString(before) && !tuneBefore.MatchString(before):
			res.Performers = append(res.Performers, l)
		}
	}
	res.Event = event(masked, links)
	for _, m := range yearWord.FindAllString(masked, -1) {
		if y, err := strconv.Atoi(m); err == nil && y >= MinPerformanceYear && y <= maxYear {
			res.Year = y

			break
		}
	}

	return res
}

// maskURLs replaces the URL of every external link with spaces, keeping offsets.
func maskURLs(item string) string {
	masked := []byte(item)
	for _, e := range FindExternalLinks(item) {
		at := e.Start + strings.Index(item[e.Start:e.End], e.URL)
		for k := at; k < at+len(e.URL); k++ {
			masked[k] = ' '
		}
	}

	return string(masked)
}

func event(masked string, links []Wikilink) TextSpan {
	loc := eventStart.FindStringIndex(masked)
	if loc == nil {
		return TextSpan{}
	}
	start := loc[1]
	for _, l := range links {
		if l.Start == start {
			return TextSpan{Text: l.Label(), Start: l.Start, End: l.End}
		}
	}
	end := len(masked)
	if cut := eventEnd.FindStringIndex(masked[start:]); cut != nil {
		end = start + cut[0]
	}
	if y := leadingYear.FindStringIndex(masked[start:end]); y != nil {
		start += y[1]
	}
	text := masked[start:end]
	if cut := eventPlace.FindStringIndex(text); cut != nil {
		text = text[:cut[0]]
	}
	if y := trailingYear.FindStringIndex(text); y != nil {
		text = text[:y[0]]
	}
	text = strings.TrimRightFunc(text, isSpace)
	if first, _ := utf8.DecodeRuneInString(text); !unicode.IsUpper(first) {
		return TextSpan{}
	}

	return TextSpan{Text: text, Start: start, End: start + len(text)}
}

// LatestYear is the latest year a performance on pages can be from: the year of their latest revision, and at least
// MinPerformanceYear.
func LatestYear(pages []Page) int {
	year := MinPerformanceYear
	for _, p := range pages {
		year = max(year, p.LastRevTimestamp.Year())
	}

	return year
}

// PerformanceRecord is what one performance item says: the item as written without its list markers
// (DescriptionVerbatim, kept as the performance's description), and as ParsePerformanceText reads it its event,
// year, the targets of its performer links and the piece it says is sung.
type PerformanceRecord struct {
	DescriptionVerbatim string
	EventText           string
	Year                int
	Performers          []string
	Piece               string
}

// ParsePerformance reads one item of a performances section. A page link to a work (isWork) never names a
// performer, whatever comes before it ("his original piece, [[W]]"); if the wording names no piece sung, the first
// link to a work is the piece ("her song [[W]]", "performing [[W]] by …").
func ParsePerformance(item ListItem, maxYear int, isWork func(target string) bool) PerformanceRecord {
	verbatim := strings.TrimRightFunc(item.Raw, isSpace)
	text := ParsePerformanceText(verbatim, maxYear)
	rec := PerformanceRecord{DescriptionVerbatim: verbatim, EventText: text.Event.Text, Year: text.Year,
		Piece: text.Piece}
	for _, l := range text.Performers {
		if !isWork(l.Target) {
			rec.Performers = append(rec.Performers, l.Target)
		}
	}
	if rec.Piece == "" {
		for _, l := range text.PageLinks {
			if isWork(l.Target) {
				rec.Piece = l.Target

				break
			}
		}
	}

	return rec
}

// workTitles are the titles of the wiki's works: its piece pages and the red links of the bards' works lists.
type workTitles struct {
	site   Site
	titles map[string]bool
}

func newWorkTitles(s Site, pieces []PieceRecord, bards []BardRecord) workTitles {
	w := workTitles{site: s, titles: map[string]bool{}}
	for _, p := range pieces {
		w.titles[p.Title] = true
	}
	for _, b := range bards {
		for _, e := range b.Works {
			if !e.Exists {
				w.titles[e.Target] = true
			}
		}
	}

	return w
}

// has reports whether a page link's target, read through redirects, is a work.
func (w workTitles) has(target string) bool {
	return w.titles[w.site.ResolveRedirect(target)]
}

// performanceItems are the items of a parsed page's performances sections, with the key of their section.
func performanceItems(parsed ParsedPage) []sectionItem {
	var out []sectionItem
	for _, sec := range parsed.Sections {
		if sec.Key.IsPerformanceSection() {
			for _, it := range sec.Content.Items {
				out = append(out, sectionItem{ListItem: it, Section: sec.Key})
			}
		}
	}

	return out
}

// sectionItem is a list item with the key of its section.
type sectionItem struct {
	ListItem
	Section SectionKey
}
