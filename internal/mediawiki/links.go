package mediawiki

import (
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// infoboxSectionPrefix starts the section key of an external link in an infobox parameter ("infobox:tune_by").
const infoboxSectionPrefix = "infobox:"

// LinkOccurrence is one external link as a page writes it: its URL and text, whether it is bracketed, the trimmed
// line it is on and the key of its section ("infobox:<param>" for one in an infobox).
type LinkOccurrence struct {
	URL       string
	Text      string
	Bracketed bool
	Line      string
	Section   SectionKey
}

// LinkFields are a link's editor-owned fields: its URL as first entered, its normalised URL (its identity), its
// title (the link text), its description (the text of its line around it), the platform and place type it is on,
// the collection it belongs to (by normalised URL) and where in it the piece is (a locator such as "p. 63").
// Empty means none.
type LinkFields struct {
	URL           string
	NormalizedURL string
	Title         string
	Description   string
	Platform      string
	PlaceType     domain.LinkPlaceType
	CollectionURL string
	Locator       string
}

// LinkRecord is a link attached to a piece or person: its fields, its roles in first-seen order, the sections it
// occurs in, the collections tying for it (it then belongs to none, for review) and the performances it records.
type LinkRecord struct {
	LinkFields
	Roles          []domain.LinkRole
	Sections       []SectionKey
	CollectionTied []string
	Performances   []Performance
}

// PageLinks are the external links of a page: every occurrence in order (the sections', then the infoboxes'), the
// links attached to the page's piece or person (one per normalised URL; infobox links name persons, so they are
// not the piece's) and the occurrences whose URL does not normalise, which are dropped.
type PageLinks struct {
	Occurrences []LinkOccurrence
	Links       []LinkRecord
	Dropped     []LinkOccurrence
}

// sectionRoles are the roles of the links in each section; links in other sections have role other.
var sectionRoles = map[SectionKey]domain.LinkRole{
	SectionLyrics:                 domain.LinkRoleLyrics,
	SectionText:                   domain.LinkRoleText,
	SectionSheetMusic:             domain.LinkRoleSheetMusic,
	SectionDancingInstructions:    domain.LinkRoleDanceInstructions,
	SectionTextSources:            domain.LinkRoleTextSource,
	SectionTranslations:           domain.LinkRoleTranslation,
	SectionPerformancesRecordings: domain.LinkRolePerformance,
	SectionKnownPerformances:      domain.LinkRolePerformance,
	SectionLicensingEtiquette:     domain.LinkRoleLicensingSource,
	SectionWebsite:                domain.LinkRoleWebsite,
	SectionOtherResources:         domain.LinkRoleExternalProfile,
}

// IsInfobox reports whether the key is an infobox parameter's.
func (k SectionKey) IsInfobox() bool {
	return strings.HasPrefix(string(k), infoboxSectionPrefix)
}

// IsPerformanceSection reports whether the section lists performances and recordings.
func (k SectionKey) IsPerformanceSection() bool {
	return k == SectionPerformancesRecordings || k == SectionKnownPerformances
}

// The platforms named by host. Other links have their host as the platform, or pdf for a PDF file.
const (
	PlatformYouTube      = "youtube"
	PlatformSpotify      = "spotify"
	PlatformBandcamp     = "bandcamp"
	PlatformSoundCloud   = "soundcloud"
	PlatformReverbNation = "reverbnation"
	PlatformFacebook     = "facebook"
	PlatformWikipedia    = "wikipedia"
	PlatformGutenberg    = "gutenberg"
	PlatformPDF          = "pdf"
)

// platformHosts are the platforms by host; a host also has the platform of its parent domain.
var platformHosts = map[string]string{
	"youtube.com": PlatformYouTube, "youtu.be": PlatformYouTube, "spotify.com": PlatformSpotify,
	"bandcamp.com": PlatformBandcamp, "soundcloud.com": PlatformSoundCloud, "reverbnation.com": PlatformReverbNation,
	"facebook.com": PlatformFacebook, "fb.watch": PlatformFacebook, "wikipedia.org": PlatformWikipedia,
	"gutenberg.org": PlatformGutenberg,
}

// hostOrParent is the value in hosts of host or of its nearest parent domain ("youtube.com" for
// "m.youtube.com").
func hostOrParent[T any](host string, hosts map[string]T) (T, bool) {
	for h := host; h != ""; {
		if v, ok := hosts[h]; ok {
			return v, true
		}
		_, parent, ok := strings.Cut(h, ".")
		if !ok {
			break
		}
		h = parent
	}
	var none T

	return none, false
}

// linkPlatform is the platform of a link, by normalised URL: a named platform by host, pdf for a PDF file, or else
// the host.
func linkPlatform(normalized *url.URL) string {
	host := normalized.Hostname()
	if p, ok := hostOrParent(host, platformHosts); ok {
		return p
	}
	if strings.EqualFold(path.Ext(normalized.Path), ".pdf") {
		return PlatformPDF
	}

	return host
}

// bookCollectionKinds are the collection kinds that are books: a link in one is in a book.
var bookCollectionKinds = map[domain.CollectionKind]bool{
	domain.CollectionKindSongbookPDF: true, domain.CollectionKindGoogleDocSongbook: true,
	domain.CollectionKindHostedDocument: true, domain.CollectionKindArchivedSongbook: true,
}

// locatorPrefix starts a page locator ("p. 63").
const locatorPrefix = "p. "

var (
	locatorText     = regexp.MustCompile(`(?i)\b(?:p|pg|page)\.?[ \t]*(\d{1,5})\b`)
	locatorFragment = regexp.MustCompile(`(?i)(?:^|&)page=(\d{1,5})\b`)
)

// Locator is where in a document a link points, from the link's text ("p. 63", "pg 63", "page 63"), its URL's
// fragment ("#page=63") or its line, if the line has no other link; written "p. 63", or empty.
func Locator(text, fragment, line string) string {
	for _, m := range [][]string{
		locatorText.FindStringSubmatch(text), locatorFragment.FindStringSubmatch(fragment),
		locatorText.FindStringSubmatch(line),
	} {
		if m != nil {
			return locatorPrefix + m[1]
		}
	}

	return ""
}

// linkContext is what extracting a page's links needs from the rest of the wiki: the collections, the websites
// (normalised URLs) of the persons the page credits, which make its key links official, and of every bard, which
// are author sites.
type linkContext struct {
	collections   CollectionMatcher
	creditedSites []string
	bardSites     []string
}

// pageOccurrences are the external links of a parsed page in order: its sections', then its infoboxes' (each
// parameter once, as the last value given).
func pageOccurrences(parsed ParsedPage) []LinkOccurrence {
	var out []LinkOccurrence
	for _, sec := range parsed.Sections {
		for _, e := range sec.Content.ExternalLinks {
			out = append(out, LinkOccurrence{URL: e.URL, Text: e.Text, Bracketed: e.Bracketed, Line: e.Line,
				Section: sec.Key})
		}
	}
	for _, box := range parsed.Infoboxes {
		seen := map[string]bool{}
		for _, p := range box.Params {
			if seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			v, _ := box.Param(p.Name)
			for _, it := range v.Value.Items {
				if it.Type == ValueExternalLink {
					out = append(out, LinkOccurrence{URL: it.URL, Text: it.Label, Bracketed: true,
						Section: SectionKey(infoboxSectionPrefix + p.Name)})
				}
			}
		}
	}

	return out
}

// extractLinks returns a page's links: its occurrences, and the links of its sections deduplicated by normalised
// URL, each keeping the first URL as entered, the first text and locator and the union of the roles.
func extractLinks(parsed ParsedPage, ctx linkContext) PageLinks {
	out := PageLinks{Occurrences: pageOccurrences(parsed)}
	lines := map[string]int{}
	for _, o := range out.Occurrences {
		lines[string(o.Section)+"\n"+o.Line]++
	}
	byURL := map[string]int{}
	for _, o := range out.Occurrences {
		if o.Section.IsInfobox() {
			continue
		}
		line := ""
		if lines[string(o.Section)+"\n"+o.Line] == 1 {
			line = o.Line
		}
		rec, ok := linkOf(o, line, ctx)
		if !ok {
			out.Dropped = append(out.Dropped, o)

			continue
		}
		k, seen := byURL[rec.NormalizedURL]
		if !seen {
			byURL[rec.NormalizedURL] = len(out.Links)
			out.Links = append(out.Links, rec)

			continue
		}
		out.Links[k] = mergeLink(out.Links[k], rec)
	}
	for k, l := range out.Links {
		out.Links[k].PlaceType = placeType(l, ctx)
	}

	return out
}

// linkOf is one occurrence as a link, with the role of its section, or false if its URL does not normalise.
func linkOf(o LinkOccurrence, line string, ctx linkContext) (LinkRecord, bool) {
	normalized, err := domain.NormalizeURL(o.URL)
	if err != nil {
		return LinkRecord{}, false
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return LinkRecord{}, false
	}
	match := ctx.collections.Match(normalized)
	rec := LinkRecord{
		LinkFields: LinkFields{URL: o.URL, NormalizedURL: normalized, Title: Plain(o.Text),
			Description: linkDescription(o, line), Platform: linkPlatform(u), CollectionURL: match.URL,
			Locator: Locator(o.Text, u.Fragment, line)},
		Roles:          []domain.LinkRole{sectionRole(o.Section, normalized, ctx)},
		Sections:       []SectionKey{o.Section},
		CollectionTied: match.Tied,
	}

	return rec, true
}

// descriptionSeparators are what a link's description is trimmed of, with spaces: list markers and the separators
// between a link and its text.
const descriptionSeparators = "*#:-–—,;.|()"

// linkDescription is the text of a link's line around it, as plain text: given only when the line has no other
// link (line is not empty), and never for a performance item, whose description is the performance's.
func linkDescription(o LinkOccurrence, line string) string {
	if line == "" || o.Section.IsPerformanceSection() {
		return ""
	}
	var rest strings.Builder
	last := 0
	for _, e := range FindExternalLinks(line) {
		rest.WriteString(line[last:e.Start])
		rest.WriteByte(' ')
		last = e.End
	}
	rest.WriteString(line[last:])

	return strings.TrimFunc(collapseSpaces(Plain(rest.String())), func(r rune) bool {
		return isSpace(r) || strings.ContainsRune(descriptionSeparators, r)
	})
}

// sectionRole is the role of a link in a section. A key link is official when it is on the website of a person
// the page credits.
func sectionRole(key SectionKey, normalized string, ctx linkContext) domain.LinkRole {
	if role, ok := sectionRoles[key]; ok {
		return role
	}
	if key == SectionKeyLinks && slices.ContainsFunc(ctx.creditedSites, func(site string) bool {
		return isUnder(normalized, site)
	}) {
		return domain.LinkRoleOfficial
	}

	return domain.LinkRoleOther
}

// isUnder reports whether a normalised URL is a site's (a normalised URL) or under its path, whatever its scheme.
func isUnder(normalized, site string) bool {
	link, root := withoutScheme(normalized), withoutScheme(site)
	if !strings.HasPrefix(link, root) {
		return false
	}

	return len(link) == len(root) || strings.HasSuffix(root, "/") || strings.ContainsRune("/?#", rune(link[len(root)]))
}

func withoutScheme(u string) string {
	_, rest, _ := strings.Cut(u, "://")

	return rest
}

// mergeLink merges a later occurrence of a link into it: the first text, description and locator win, and roles
// and sections are added in first-seen order.
func mergeLink(l, later LinkRecord) LinkRecord {
	out := l
	if out.Title == "" {
		out.Title = later.Title
	}
	if out.Locator == "" {
		out.Locator = later.Locator
	}
	if out.Description == "" {
		out.Description = later.Description
	}
	out.Roles = appendNew(l.Roles, later.Roles...)
	out.Sections = appendNew(l.Sections, later.Sections...)

	return out
}

// appendNew returns s with the values of add it does not hold appended, in order; s itself is not changed.
func appendNew[T comparable](s []T, add ...T) []T {
	out := slices.Clone(s)
	for _, v := range add {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}

	return out
}

// placeType is where a link is: a recording platform (YouTube) or streaming service (Spotify), a book or a
// community archive (by the collection it belongs to), an author's site (a bard's website), or none.
func placeType(l LinkRecord, ctx linkContext) domain.LinkPlaceType {
	switch {
	case l.Platform == PlatformYouTube:
		return domain.LinkPlaceTypeRecordingPlatform
	case l.Platform == PlatformSpotify:
		return domain.LinkPlaceTypeStreaming
	case l.CollectionURL != "" && bookCollectionKinds[ctx.collections.kinds[l.CollectionURL]]:
		return domain.LinkPlaceTypeBook
	case l.CollectionURL != "":
		return domain.LinkPlaceTypeCommunityArchive
	case slices.Contains(l.Roles, domain.LinkRoleWebsite) ||
		slices.ContainsFunc(ctx.bardSites, func(site string) bool { return isUnder(l.NormalizedURL, site) }):
		return domain.LinkPlaceTypeAuthorSite
	}

	return ""
}

// trackingParameter is a query parameter that only tracks, as the reference's quality report finds them.
// RepeatedURLs are the URLs, as entered, that a page's occurrences hold more than once, in first-seen order.
func (p PageLinks) RepeatedURLs() []string {
	count := map[string]int{}
	for _, o := range p.Occurrences {
		count[o.URL]++
	}
	var out []string
	for _, o := range p.Occurrences {
		if count[o.URL] > 1 && !slices.Contains(out, o.URL) {
			out = append(out, o.URL)
		}
	}

	return out
}
