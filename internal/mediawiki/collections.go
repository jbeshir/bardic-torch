package mediawiki

import (
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// CollectionsPage is the title of the page listing the wiki's collections: songbooks, archives and channels.
const CollectionsPage = "Collections"

// CollectionRecord is one entry of the Collections page: a "*" line under a letter heading whose first external
// link is the collection. Label is the link's text (or its URL), Note the line's text after the link, Depth the
// number of "*" and ParentURL the normalised URL of the depth-1 entry above a nested one. Kind is guessed from the
// URL and label (listed for review). IsBardicResource is false for an entry noted as "not a bardic community
// resource". RegionTerm is the category of the one kingdom the label names, if it names exactly one;
// RegionCandidates are the kingdoms of a label naming several, for review.
type CollectionRecord struct {
	Letter           string
	Label            string
	URL              string
	NormalizedURL    string
	Note             string
	Depth            int
	ParentURL        string
	Kind             domain.CollectionKind
	Description      string
	IsBardicResource bool
	RegionTerm       string
	RegionCandidates []string
}

// Name is the collection's name: its label up to the first " - ", the rest being its description.
func (c CollectionRecord) Name() string {
	name, _, _ := strings.Cut(c.Label, collectionLabelSeparator)

	return name
}

// collectionLabelSeparator separates a collection label's name from its description ("Songs of X - 2nd edition").
const collectionLabelSeparator = " - "

// Collections are the entries of the Collections page. Records has one entry per normalised URL, the first
// listed; Duplicates are the later entries with the URL of an earlier one, merged into it and listed for review;
// Dropped are the entries whose URL does not normalise, not imported.
type Collections struct {
	Records    []CollectionRecord
	Duplicates []CollectionRecord
	Dropped    []CollectionRecord
}

// Entries is the number of entries the page lists.
func (c Collections) Entries() int {
	return len(c.Records) + len(c.Duplicates) + len(c.Dropped)
}

var (
	collectionItem   = regexp.MustCompile(`^(\*+)[` + jsSpaces + `]*(.*)$`)
	leadingDash      = regexp.MustCompile(`^[` + jsSpaces + `]*-[` + jsSpaces + `]*`)
	notBardicNote    = regexp.MustCompile(`(?i)\bnot a bardic community resource\b`)
	youTubePlaylist  = regexp.MustCompile(`youtube\.com/playlist`)
	youTubeURL       = regexp.MustCompile(`youtube\.com|youtu\.be`)
	pdfURL           = regexp.MustCompile(`\.pdf($|\?)`)
	googleDocURL     = regexp.MustCompile(`docs\.google\.com/document`)
	hostedDocURL     = regexp.MustCompile(`yumpu\.com|scribd\.com`)
	wikiCategoryURL  = regexp.MustCompile(`(?i)[?&]title=category:|/category:|index\.php/category:`)
	wikiLabel        = regexp.MustCompile(`wiki`)
	songWikiLabel    = regexp.MustCompile(`song wiki`)
	wikiURL          = regexp.MustCompile(`song_wiki|tunearch\.org/wiki`)
	archivedURL      = regexp.MustCompile(`horntip\.com`)
	indexLabel       = regexp.MustCompile(`links|list of songbooks|directory|index`)
	songbookLabel    = regexp.MustCompile(`songbook|song book|snogbook|book of|songs`)
	blogURL          = regexp.MustCompile(`blogspot|wordpress`)
	kingdomOfTheName = regexp.MustCompile(`^(\S+) Kingdom$`)
)

// collectionRule gives a kind to a collection whose lower-case URL matches url, or whose lower-case label matches
// label but not notLabel.
type collectionRule struct {
	url      *regexp.Regexp
	label    *regexp.Regexp
	notLabel *regexp.Regexp
	kind     domain.CollectionKind
}

func (r collectionRule) matches(u, t string) bool {
	if r.url != nil && r.url.MatchString(u) {
		return true
	}

	return r.label != nil && r.label.MatchString(t) && (r.notLabel == nil || !r.notLabel.MatchString(t))
}

// collectionRules are the reference's classify rules, in order; a collection matching none is a website.
var collectionRules = []collectionRule{
	{url: youTubePlaylist, kind: domain.CollectionKindYouTubePlaylist},
	{url: youTubeURL, kind: domain.CollectionKindYouTubeChannel},
	{url: pdfURL, kind: domain.CollectionKindSongbookPDF},
	{url: googleDocURL, kind: domain.CollectionKindGoogleDocSongbook},
	{url: hostedDocURL, kind: domain.CollectionKindHostedDocument},
	{url: wikiCategoryURL, label: wikiLabel, notLabel: songWikiLabel, kind: domain.CollectionKindWikiCategory},
	{url: wikiURL, kind: domain.CollectionKindWiki},
	{url: archivedURL, kind: domain.CollectionKindArchivedSongbook},
	{label: indexLabel, kind: domain.CollectionKindIndexOrLinkList},
	{label: songbookLabel, kind: domain.CollectionKindSongbookWebsite},
	{url: blogURL, kind: domain.CollectionKindBlog},
}

// classifyCollection guesses a collection's kind from its URL and label, as the reference's classify does.
func classifyCollection(rawURL, label string) domain.CollectionKind {
	u, t := strings.ToLower(rawURL), strings.ToLower(label)
	for _, r := range collectionRules {
		if r.matches(u, t) {
			return r.kind
		}
	}

	return domain.CollectionKindWebsite
}

// ParseCollections reads the Collections page: every "*" line of a level-2 (letter) section with an external
// link is an entry, the line's first link being the collection. Entries with the normalised URL of an earlier one
// are merged into it. Nothing is fetched: whether a collection can be enumerated is not known from the wiki.
func ParseCollections(p Page, terms TermPlan) Collections {
	kingdoms := kingdomNames(terms)
	var out Collections
	byURL := map[string]bool{}
	for _, sec := range PageSections(p.Wikitext) {
		if sec.Level != 2 {
			continue
		}
		parent := ""
		for _, line := range strings.Split(sec.Body(p.Wikitext), "\n") {
			rec, ok := collectionEntry(sec.Heading, line)
			if !ok {
				continue
			}
			if rec.Depth > 1 {
				rec.ParentURL = parent
			}
			rec.RegionTerm, rec.RegionCandidates = region(rec.Label, kingdoms)
			normalized, err := domain.NormalizeURL(rec.URL)
			switch {
			case err != nil:
				out.Dropped = append(out.Dropped, rec)

				continue
			case byURL[normalized]:
				rec.NormalizedURL = normalized
				out.Duplicates = append(out.Duplicates, rec)
			default:
				rec.NormalizedURL = normalized
				byURL[normalized] = true
				out.Records = append(out.Records, rec)
			}
			if rec.Depth == 1 {
				parent = rec.NormalizedURL
			}
		}
	}

	return out
}

// collectionEntry reads one line of a letter section as an entry, if it is a "*" line with an external link.
func collectionEntry(letter, line string) (CollectionRecord, bool) {
	m := collectionItem.FindStringSubmatch(line)
	if m == nil {
		return CollectionRecord{}, false
	}
	links := FindExternalLinks(m[2])
	if len(links) == 0 {
		return CollectionRecord{}, false
	}
	l := links[0]
	after := m[2]
	if k := strings.IndexByte(after, ']'); k >= 0 {
		after = after[k+1:]
	}
	note := leadingDash.ReplaceAllString(Plain(after), "")
	label := l.URL
	if l.Text != "" {
		label = l.Text
	}
	label = DecodeEntities(label)
	rec := CollectionRecord{
		Letter: letter, Label: label, URL: l.URL, Note: note, Depth: len(m[1]),
		Kind: classifyCollection(l.URL, label), IsBardicResource: !notBardicNote.MatchString(note),
	}
	var description []string
	if _, rest, ok := strings.Cut(label, collectionLabelSeparator); ok && rest != "" {
		description = append(description, rest)
	}
	if note != "" {
		description = append(description, note)
	}
	rec.Description = strings.Join(description, "; ")

	return rec, true
}

// kingdomAlternateNames are other names a kingdom goes by, by its term's name.
var kingdomAlternateNames = map[string][]string{"Midrealm": {"Middle Kingdom"}}

// kingdomNames maps each kingdom term's category (an affiliation term under a root) to the folded names that
// name it: its name, "Kingdom of the West" for "West Kingdom", and its alternate names.
func kingdomNames(terms TermPlan) map[string][]string {
	roots := map[string]bool{}
	for _, t := range terms.Terms {
		if t.Facet == domain.FacetAffiliation && t.Parent == "" {
			roots[t.Category] = true
		}
	}
	out := map[string][]string{}
	for _, t := range terms.Terms {
		if !roots[t.Parent] {
			continue
		}
		names := append([]string{t.Name}, kingdomAlternateNames[t.Name]...)
		if m := kingdomOfTheName.FindStringSubmatch(t.Name); m != nil {
			names = append(names, "Kingdom of the "+m[1])
		}
		for _, n := range names {
			out[t.Category] = append(out[t.Category], FoldName(n))
		}
	}

	return out
}

// region returns the kingdom a label names, if it names exactly one, or the kingdoms it names, sorted.
func region(label string, kingdoms map[string][]string) (string, []string) {
	words := " " + strings.Join(strings.FieldsFunc(FoldName(label), isNameSeparator), " ") + " "
	var found []string
	for category, names := range kingdoms {
		if slices.ContainsFunc(names, func(n string) bool { return strings.Contains(words, " "+n+" ") }) {
			found = append(found, category)
		}
	}
	slices.Sort(found)
	if len(found) == 1 {
		return found[0], nil
	}

	return "", found
}

// isNameSeparator reports whether r separates the words of a folded name.
func isNameSeparator(r rune) bool {
	return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r <= 0x7f
}

// collectionPrefixKinds are the kinds of collection a link can belong to by being under its URL's path; links
// match other collections only by their exact URL.
var collectionPrefixKinds = map[domain.CollectionKind]bool{
	domain.CollectionKindWebsite: true, domain.CollectionKindSongbookWebsite: true, domain.CollectionKindWiki: true,
	domain.CollectionKindBlog: true,
}

// sharedDocumentHosts are hosts serving many people's documents and videos, so a path on them never makes a link
// part of a collection.
var sharedDocumentHosts = []string{"youtube.com", "youtu.be", "docs.google.com", "scribd.com", "yumpu.com"}

// CollectionMatch is the collection a link belongs to: the collection's normalised URL, or empty. Tied are the
// collections whose prefixes match equally well, when there are several; the link then belongs to none.
type CollectionMatch struct {
	URL  string
	Tied []string
}

// collectionPrefix is a collection's URL as a prefix links under it share: its host and path up to the last "/".
type collectionPrefix struct {
	url    string
	host   string
	prefix string
}

// CollectionMatcher matches links to the collections they belong to. kinds holds every collection by normalised
// URL, documents by normalised URL without its fragment.
type CollectionMatcher struct {
	kinds     map[string]domain.CollectionKind
	documents map[string][]string
	prefixes  []collectionPrefix
}

// NewCollectionMatcher indexes collections by their normalised URL and, for website, songbook website, wiki and
// blog collections not on a shared document host whose path has at least one segment, by their path prefix.
func NewCollectionMatcher(records []CollectionRecord) CollectionMatcher {
	m := CollectionMatcher{kinds: map[string]domain.CollectionKind{}, documents: map[string][]string{}}
	for _, c := range records {
		m.kinds[c.NormalizedURL] = c.Kind
		document := withoutFragment(c.NormalizedURL)
		m.documents[document] = append(m.documents[document], c.NormalizedURL)
		if !collectionPrefixKinds[c.Kind] {
			continue
		}
		u, err := url.Parse(c.NormalizedURL)
		if err != nil || isSharedDocumentHost(u.Host) {
			continue
		}
		prefix := u.EscapedPath()[:strings.LastIndexByte(u.EscapedPath(), '/')+1]
		if prefix != "/" {
			m.prefixes = append(m.prefixes, collectionPrefix{url: c.NormalizedURL, host: u.Host, prefix: prefix})
		}
	}

	return m
}

func isSharedDocumentHost(host string) bool {
	return slices.ContainsFunc(sharedDocumentHosts, func(h string) bool {
		return host == h || strings.HasSuffix(host, "."+h)
	})
}

// Match returns the collection a link (by normalised URL) belongs to: the one with its exact URL, or else with its
// URL but for the fragment (which locates a place in the document: "#page=63"), or else the one with the longest
// path prefix of the link on the same host. Several collections matching equally well tie, and match nothing.
func (m CollectionMatcher) Match(normalizedURL string) CollectionMatch {
	if _, ok := m.kinds[normalizedURL]; ok {
		return CollectionMatch{URL: normalizedURL}
	}
	switch same := m.documents[withoutFragment(normalizedURL)]; len(same) {
	case 0:
	case 1:
		return CollectionMatch{URL: same[0]}
	default:
		return CollectionMatch{Tied: same}
	}
	u, err := url.Parse(normalizedURL)
	if err != nil {
		return CollectionMatch{}
	}
	var best []string
	longest := 0
	for _, p := range m.prefixes {
		if p.host != u.Host || !strings.HasPrefix(u.EscapedPath(), p.prefix) {
			continue
		}
		switch {
		case len(p.prefix) > longest:
			best, longest = []string{p.url}, len(p.prefix)
		case len(p.prefix) == longest:
			best = append(best, p.url)
		}
	}
	switch len(best) {
	case 0:
		return CollectionMatch{}
	case 1:
		return CollectionMatch{URL: best[0]}
	}

	return CollectionMatch{Tied: best}
}

func withoutFragment(u string) string {
	before, _, _ := strings.Cut(u, "#")

	return before
}
