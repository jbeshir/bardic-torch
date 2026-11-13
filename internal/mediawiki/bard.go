package mediawiki

import (
	"regexp"
	"slices"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// PersonAlias is another name of a bard: where it was found and the alias kind it is imported as.
type PersonAlias struct {
	AliasCandidate
	Kind domain.PersonAliasKind
}

// WorkEntry is a work a bard page lists: the link target as written, the page it leads to through redirects and
// whether that page exists.
type WorkEntry struct {
	Title  string
	Target string
	Exists bool
}

// BardRecord is a bard page parsed for import. Wikitext is the page's wikitext without inline lyrics and with its
// markup repaired, and Parsed is that text parsed:
//   - Aliases: the H1 parentheticals and the prose markers (PersonAlias);
//   - Roles: from the performer-type categories;
//   - Affiliations: the categories that are affiliation terms (TermPlan);
//   - Status: deceased for In Memoriam;
//   - LicensingStatus and LicensingNotesMD: as for pieces; the "cannot find" sentence means unknown;
//   - NotesMD: the lead, Notes, unrecognised sections and the Affiliations section's prose, as Markdown;
//   - Works: the page links of the Songs & Works and Music sections;
//   - HeirOf: the persons the page makes the bard heir of ("The chosen heir of [[X]]"), by the title the page links
//     to (read through redirects), so that their works are not credited to the bard too.
type BardRecord struct {
	PageID           int64
	Title            string
	Wikitext         string
	Parsed           ParsedPage
	Aliases          []PersonAlias
	Roles            []domain.PersonRole
	Affiliations     []string
	Status           domain.PersonStatus
	LicensingStatus  domain.LicensingStatus
	LicensingNotesMD string
	NotesMD          string
	Works            []WorkEntry
	HeirOf           []string
}

// heirOf is the wording before a page link that makes the bard the linked person's heir.
var heirOf = regexp.MustCompile(`(?i)\bheir[` + jsSpaces + `]+(?:of|to)[` + jsSpaces + `]*$`)

// ParseBard parses a bard page. Like ParsePiece, it drops the page's inline lyrics and repairs its markup before
// anything reads it.
func ParseBard(p Page, s Site) BardRecord {
	dropped, _ := DropInlineLyrics(p.Wikitext)
	repaired := RepairMarkup(dropped)
	parsed := ParsePage(repaired)
	b := BardRecord{
		PageID: p.PageID, Title: p.Title, Wikitext: repaired, Parsed: parsed,
		Aliases: bardAliases(p), Status: domain.PersonStatusActive,
		LicensingStatus: domain.LicensingStatusUnknown,
	}
	b.Roles, b.Affiliations = bardCategories(p, s)
	if p.HasCategory(CategoryInMemoriam) {
		b.Status = domain.PersonStatusDeceased
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
		b.HeirOf = append(b.HeirOf, heirsOf(sec, repaired, s)...)
	}
	b.NotesMD = strings.Join(notes, "\n\n")

	return b
}

// bardAliases are the page's aliases (BardAliases), classified.
func bardAliases(p Page) []PersonAlias {
	var out []PersonAlias
	for _, c := range BardAliases(p) {
		out = append(out, PersonAlias{AliasCandidate: c, Kind: ClassifyPersonAlias(c).Kind})
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

// heirsOf are the persons a section makes the bard heir of: page links after heirOf's wording, read through
// redirects.
func heirsOf(sec PageSection, wikitext string, s Site) []string {
	masked := MaskNowiki(StripComments(sec.Body(wikitext)))
	var out []string
	last := 0
	for _, l := range FindWikilinks(masked) {
		before := masked[last:l.Start]
		last = l.End
		if l.Kind == WikilinkPage && heirOf.MatchString(before) {
			out = append(out, s.ResolveRedirect(l.Target))
		}
	}

	return out
}
