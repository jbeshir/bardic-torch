package mediawiki

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// PieceContent is a piece's prose, as Markdown, taken from its wikitext after the inline lyrics are dropped:
//   - NotesMD: the lead (without the H1, infobox and "Also known as" lines), Notes and unrecognised sections, each
//     of the latter under its heading, and the prose of the link sections under theirs (none when it is as long
//     as inline lyrics: InlineLyricsMinLines lines or more could be lyrics);
//   - VersionNotesMD: the version commentary of the Lyrics and Text sections (their prose when it is under
//     InlineLyricsMinLines lines);
//   - LicensingNotesMD: Licensing & Etiquette without its hints and the "cannot find" sentence;
//   - HistoricalSourceMD: Timeline of Text Sources.
//
// LicensingStatus is known when the licensing section has text and a source link. The licence itself is never
// inferred (it stays null). PeriodStart and PeriodEnd span the piece's century origin terms. Sections nested under
// a Lyrics or Text section go nowhere.
type PieceContent struct {
	NotesMD            string
	VersionNotesMD     string
	LicensingNotesMD   string
	HistoricalSourceMD string
	LicensingStatus    domain.LicensingStatus
	PeriodStart        *int16
	PeriodEnd          *int16
}

// linkSections are the sections of a piece's links whose prose goes to the notes. A line with an external link
// is a link, and its text the link's description; performance items are performances.
var linkSections = map[SectionKey]bool{
	SectionKeyLinks: true, SectionSheetMusic: true, SectionDancingInstructions: true, SectionTranslations: true,
}

// DroppedLyrics is what InlineLyricsBlocks dropped from a piece: never the text. HadInlineLyrics is
// legacy_had_inline_lyrics.
type DroppedLyrics struct {
	HadInlineLyrics bool
}

var centuryOrigin = regexp.MustCompile(`^(\d{1,2})(?:st|nd|rd|th) Century$`)

// maxCentury is the last century a period can be in (the 99th: its end, 9900, fits a SMALLINT).
const maxCentury = 99

// notesSectionMD is the Markdown heading prefix of an unrecognised section in the notes.
const notesSectionMD = "### "

// versionNotes is the version commentary of a page's Lyrics and Text sections, as wikitext, in page order: their
// prose lines when there are fewer than InlineLyricsMinLines (InlineLyrics.Commentary) and, in a section without
// prose lyrics, the prose lines that carry a link (as Belt and Chain's "There are two versions … [url …]"). List
// items, which are the section's links, are not commentary.
func versionNotes(wikitext string, lyrics InlineLyrics) string {
	masked := MaskComments(wikitext)
	commentary := map[int]bool{}
	for _, c := range lyrics.Commentary {
		commentary[c.Start] = true
	}
	proseSections := map[int]bool{}
	for _, b := range lyrics.Blocks {
		if b.Kind == LyricsProse {
			proseSections[b.Section] = true
		}
	}
	var lines []string
	for k, s := range PageSections(wikitext) {
		if !KeyOf(s).IsLyrics() {
			continue
		}
		for _, l := range lineSpans(masked, s.BodyStart, s.End) {
			line := masked[l.start:l.end]
			linkProse := !proseSections[k] && carriesLink(line) && !listLine.MatchString(line) &&
				!overlapsBlock(lyrics.Blocks, l.start, l.end)
			if commentary[l.start] || linkProse {
				lines = append(lines, StripComments(wikitext[l.start:l.end]))
			}
		}
	}

	return strings.Join(lines, "\n")
}

// lineSpans are the lines of text[start:end], as offsets into text.
func lineSpans(text string, start, end int) []lineSpan {
	var out []lineSpan
	for start < end {
		next := strings.IndexByte(text[start:end], '\n')
		if next < 0 {
			next = end
		} else {
			next += start
		}
		out = append(out, lineSpan{start: start, end: next})
		start = next + 1
	}

	return out
}

func pieceContent(r PieceRecord, s Site, versionWikitext string) PieceContent {
	c := PieceContent{VersionNotesMD: WikitextToMarkdown(versionWikitext), LicensingStatus: domain.LicensingStatusUnknown}
	var notes []string
	lyricsLevel := 0
	for _, sec := range r.Parsed.Sections {
		if lyricsLevel > 0 && sec.Level > lyricsLevel {
			continue
		}
		lyricsLevel = 0
		body := proseOf(sec, r.Wikitext)
		switch {
		case sec.Key.IsLyrics():
			lyricsLevel = sec.Level
		case sec.Key == SectionLead || sec.Key == SectionTitle || sec.Key == SectionNotes:
			notes = appendNonEmpty(notes, WikitextToMarkdown(body))
		case sec.Key.IsOther():
			notes = appendSection(notes, sec, body)
		case linkSections[sec.Key]:
			notes = appendSection(notes, sec, linkSection(body))
		case sec.Key == SectionLicensingEtiquette:
			c.LicensingNotesMD, c.LicensingStatus = licensingSection(sec, body)
		case sec.Key == SectionTextSources:
			c.HistoricalSourceMD = WikitextToMarkdown(body)
		}
	}
	c.NotesMD = strings.Join(notes, "\n\n")
	c.period(r.Terms, s.Terms)

	return c
}

// licensingSection reads a Licensing & Etiquette section's prose: its text without the "cannot find a central
// page" sentence, as Markdown, and the licensing status, known only when the section has text and a source link
// and does not say that the preferences could not be found.
func licensingSection(sec PageSection, body string) (md string, status domain.LicensingStatus) {
	var kept []string
	unknown := false
	for _, line := range strings.Split(body, "\n") {
		if IsLicensingUnknown(line) {
			unknown = true

			continue
		}
		kept = append(kept, line)
	}
	wikitext := strings.Join(kept, "\n")
	md = WikitextToMarkdown(wikitext)
	status = domain.LicensingStatusUnknown
	if !unknown && md != "" && len(sec.Content.ExternalLinks) > 0 {
		status = domain.LicensingStatusKnown
	}

	return md, status
}

// period spans the origin terms that are centuries ("13th Century" is 1201–1300).
func (c *PieceContent) period(terms []PieceTerm, plan TermPlan) {
	for _, t := range terms {
		spec, ok := plan.Term(t.Category)
		m := centuryOrigin.FindStringSubmatch(t.Category)
		if !ok || spec.Facet != domain.FacetOrigin || m == nil {
			continue
		}
		n, err := strconv.ParseInt(m[1], 10, 16)
		if err != nil || n < 1 || n > maxCentury {
			continue
		}
		start, end := int16((n-1)*100+1), int16(n*100)
		if c.PeriodStart == nil || start < *c.PeriodStart {
			c.PeriodStart = &start
		}
		if c.PeriodEnd == nil || end > *c.PeriodEnd {
			c.PeriodEnd = &end
		}
	}
}

// linkSection is the prose of a link section to keep in the notes, as wikitext (linkSectionProse): none if it is
// as long as inline lyrics.
func linkSection(body string) string {
	prose, lines := linkSectionProse(body)
	if lines >= InlineLyricsMinLines {
		return ""
	}

	return prose
}

// ruleLine is a horizontal rule, or a list marker with nothing after it.
var ruleLine = regexp.MustCompile(`^(?:-{4,}|[*#:]+)$`)

// linkSectionProse is the prose of a link section's body (proseOf), as wikitext, and its number of lines: the lines
// that carry no external link and are not rules or empty, each counted as the lines it reads as (TextLines).
func linkSectionProse(body string) (string, int) {
	var kept []string
	n := 0
	for _, line := range strings.Split(body, "\n") {
		t := trimSpace(line)
		if t == "" || ruleLine.MatchString(t) || len(FindExternalLinks(MaskNowiki(t))) > 0 {
			continue
		}
		kept = append(kept, line)
		n += TextLines(t)
	}

	return strings.Join(kept, "\n"), n
}

// proseOf is a section's body without its infobox call and the lines that are not prose: hints, category tags
// and "Also known as" lines (which are alternate titles).
func proseOf(sec PageSection, wikitext string) string {
	body := sec.Body(wikitext)
	if loc := infoboxCall.FindStringIndex(body); loc != nil {
		body = body[:loc[0]] + body[loc[1]:]
	}
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		t := trimSpace(line)
		if (t != "" && IsHint(line)) || categoryTagLine.MatchString(t) || alsoKnownAsLine.MatchString(Plain(t)) {
			continue
		}
		kept = append(kept, line)
	}

	return strings.Join(kept, "\n")
}

func appendNonEmpty(list []string, s string) []string {
	if s == "" {
		return list
	}

	return append(list, s)
}
