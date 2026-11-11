package mediawiki

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// AliasFromRedirect is a redirect page that leads to the bard's page ("James Treebull the Stubborn").
const AliasFromRedirect AliasSource = "redirect"

// PersonAlias is another name of a bard: where it was found, how ClassifyPersonAlias classified it and the alias
// kind it is imported as (a redirect alias is of kind redirect).
type PersonAlias struct {
	AliasCandidate
	Classification AliasClassification
	Kind           domain.PersonAliasKind
}

// WorkEntry is a work a bard page lists: the link target as written, the page it leads to through redirects and
// whether that page exists.
type WorkEntry struct {
	Title  string
	Target string
	Exists bool
}

// PersonRelation is a relation of a bard to another person, named by the title the page links to (read through
// redirects): "The chosen heir of [[X]]" makes the bard heir_of X.
type PersonRelation struct {
	Type domain.PersonRelationType
	To   string
}

// BardRecord is a bard page parsed for import. Wikitext is the page's wikitext without inline lyrics and with its
// markup repaired, and Parsed is that text parsed:
//   - Aliases: the H1 parentheticals, the prose markers and the redirects to the page (PersonAlias);
//   - Roles: from the performer-type categories;
//   - Affiliations: the categories that are affiliation terms (TermPlan);
//   - Status: deceased for In Memoriam, with StatusDate from a "Deceased <date>" line and StatusSourceURL the URL
//     of the external link on that line (an obituary), if it has one;
//   - LicensingStatus and LicensingNotesMD: as for pieces; the "cannot find" sentence means unknown;
//   - NotesMD: the lead, Notes, unrecognised sections and the Affiliations section's prose, as Markdown;
//   - Works: the page links of the Songs & Works and Music sections;
//   - Relations: "heir of [[X]]".
type BardRecord struct {
	PageID           int64
	Title            string
	URL              string
	Wikitext         string
	Parsed           ParsedPage
	Aliases          []PersonAlias
	Roles            []domain.PersonRole
	Affiliations     []string
	Status           domain.PersonStatus
	StatusDate       *domain.Date
	StatusSourceURL  string
	LicensingStatus  domain.LicensingStatus
	LicensingNotesMD string
	NotesMD          string
	Works            []WorkEntry
	Relations        []PersonRelation
}

// relationPatterns are the wording before a page link that relates the bard to the linked person.
var relationPatterns = []struct {
	before   *regexp.Regexp
	relation domain.PersonRelationType
}{
	{regexp.MustCompile(`(?i)\bheir[` + jsSpaces + `]+(?:of|to)[` + jsSpaces + `]*$`), domain.PersonRelationTypeHeirOf},
}

var deceasedDate = regexp.MustCompile(`(?i)\bdeceased[` + jsSpaces + `]*:?[` + jsSpaces + `]*(?:on[` + jsSpaces +
	`]+)?(\p{L}+[` + jsSpaces + `]+\d{1,2},?[` + jsSpaces + `]+\d{4}|\d{1,2}[` + jsSpaces + `]+\p{L}+[` + jsSpaces +
	`]+\d{4}|\d{4}-\d{2}-\d{2})`)

// deceasedLayouts are the date layouts a "Deceased <date>" line is read with.
var deceasedLayouts = []string{"January 2, 2006", "January 2 2006", "2 January 2006", time.DateOnly}

// ParseBard parses a bard page. Like ParsePiece, it drops the page's inline lyrics and repairs its markup before
// anything reads it.
func ParseBard(p Page, s Site) BardRecord {
	lyrics := InlineLyricsBlocks(p.Wikitext)
	repaired := RepairMarkup(WithoutInlineLyrics(p.Wikitext, lyrics.Blocks))
	parsed := ParsePage(repaired)
	b := BardRecord{
		PageID: p.PageID, Title: p.Title, URL: p.URL, Wikitext: repaired, Parsed: parsed,
		Aliases: bardAliases(p, s), Status: domain.PersonStatusActive,
		LicensingStatus: domain.LicensingStatusUnknown,
	}
	b.Roles, b.Affiliations = bardCategories(p, s)
	if p.HasCategory(CategoryInMemoriam) {
		b.Status = domain.PersonStatusDeceased
		b.StatusDate = statusDate(Plain(repaired))
		b.StatusSourceURL = statusSource(repaired)
	}
	var notes []string
	for _, sec := range parsed.Sections {
		body := proseOf(sec, repaired)
		switch {
		case sec.Key == SectionLead || sec.Key == SectionTitle || sec.Key == SectionNotes:
			notes = appendNonEmpty(notes, WikitextToMarkdown(body))
		case sec.Key == SectionAffiliations:
			notes = appendSection(notes, sec, affiliationProse(body))
		case sec.Key.IsOther():
			notes = appendSection(notes, sec, body)
		case sec.Key == SectionLicensingEtiquette:
			b.LicensingNotesMD, b.LicensingStatus = licensingSection(sec, body)
		case sec.Key == SectionWorks || sec.Key == SectionWorksMusic:
			b.Works = append(b.Works, works(sec, s)...)
		}
		b.Relations = append(b.Relations, relations(sec, repaired, s)...)
	}
	b.NotesMD = strings.Join(notes, "\n\n")

	return b
}

// bardAliases are the page's aliases (BardAliases) and the redirects to it, classified.
func bardAliases(p Page, s Site) []PersonAlias {
	var out []PersonAlias
	for _, c := range BardAliases(p) {
		class := ClassifyPersonAlias(c)
		out = append(out, PersonAlias{AliasCandidate: c, Classification: class, Kind: class.Kind})
	}
	for _, from := range s.RedirectsTo(p.Title) {
		c := AliasCandidate{Name: from, Source: AliasFromRedirect}
		class := ClassifyPersonAlias(c)
		out = append(out, PersonAlias{AliasCandidate: c, Classification: class, Kind: domain.PersonAliasKindRedirect})
	}

	return out
}

// bardCategories sorts a bard's categories into roles and affiliation terms; the others give the bard nothing.
func bardCategories(p Page, s Site) (roles []domain.PersonRole, affiliations []string) {
	for _, c := range p.Categories {
		category := s.Taxonomy.Resolve(c)
		if role, ok := performerRoles[category]; ok {
			if !slices.Contains(roles, role) {
				roles = append(roles, role)
			}

			continue
		}
		if spec, ok := s.Terms.Term(category); ok && spec.Facet == domain.FacetAffiliation {
			affiliations = appendUnique(affiliations, category)
		}
	}

	return roles, affiliations
}

// statusDate is the date of the first "Deceased <date>" in text, if it is a date.
func statusDate(text string) *domain.Date {
	m := deceasedDate.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	value := collapseSpaces(m[1])
	for _, layout := range deceasedLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return &domain.Date{Time: t}
		}
	}

	return nil
}

// statusSource is the URL of the first external link on a "Deceased <date>" line of a wikitext, or empty.
func statusSource(wikitext string) string {
	for _, line := range strings.Split(wikitext, "\n") {
		if links := FindExternalLinks(line); len(links) > 0 && deceasedDate.MatchString(Plain(line)) {
			return links[0].URL
		}
	}

	return ""
}

// appendSection appends a section's prose to notes under its heading, if it has any.
func appendSection(notes []string, sec PageSection, body string) []string {
	md := WikitextToMarkdown(body)
	if md == "" {
		return notes
	}

	return append(notes, notesSectionMD+markdownEscapes.Replace(Plain(sec.Heading))+"\n\n"+md)
}

// affiliationProse is an Affiliations section's lines that are prose: those without category links (the
// affiliation terms) or external links (imported as links).
func affiliationProse(body string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if len(FindExternalLinks(line)) > 0 || slices.ContainsFunc(FindWikilinks(line), func(l Wikilink) bool {
			return l.Kind == WikilinkCategoryLink || l.Kind == WikilinkCategoryTag
		}) {
			continue
		}
		kept = append(kept, line)
	}

	return strings.Join(kept, "\n")
}

// works are the page links a works section lists.
func works(sec PageSection, s Site) []WorkEntry {
	var out []WorkEntry
	for _, l := range sec.Content.Wikilinks {
		if l.Kind == WikilinkPage {
			out = append(out, WorkEntry{Title: l.Target, Target: s.ResolveRedirect(l.Target), Exists: s.HasPage(l.Target)})
		}
	}

	return out
}

// relations are the persons a section relates the bard to: page links after a relation's wording, read through
// redirects.
func relations(sec PageSection, wikitext string, s Site) []PersonRelation {
	masked := MaskNowiki(StripComments(sec.Body(wikitext)))
	var out []PersonRelation
	last := 0
	for _, l := range FindWikilinks(masked) {
		before := masked[last:l.Start]
		last = l.End
		if l.Kind != WikilinkPage {
			continue
		}
		for _, r := range relationPatterns {
			if r.before.MatchString(before) {
				out = append(out, PersonRelation{Type: r.relation, To: s.ResolveRedirect(l.Target)})
			}
		}
	}

	return out
}
