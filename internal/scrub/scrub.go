// Package scrub turns the private research crawl of the legacy wiki into the copy committed under
// testdata/wiki-dump. The repository is public, so the copy holds no legal or mundane name, email address, phone
// number, uploader or wiki user name and no lyrics, while keeping every structure the importer reads: titles,
// sections, links, templates, categories and the inline-lyrics classification. Scrub is pure and deterministic:
// scrubbing the same crawl gives the same bytes, and scrubbing its output changes nothing.
package scrub

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// The placeholder forms. A name is "Legal Name 001" in prose and titles, and "Legal_Name_001", "legal-name-001" or
// "legalname001" inside a URL, following how the name was written there.
const (
	legalNameFormat   = "Legal Name %03d"
	wikiUserFormat    = "Wiki User %02d"
	emailFormat       = "email-%03d@example.invalid"
	phoneFormat       = "000-000-%04d"
	placeholderDomain = "@example.invalid"
)

// KeptNamespaces are the namespaces whose pages the scrubbed copy keeps: the main namespace and categories. User,
// template, module, file and MediaWiki pages are dropped.
var KeptNamespaces = map[int]bool{mediawiki.NSMain: true, mediawiki.NSCategory: true}

// Result is a scrubbed dump, the deny list of the lyrics it dropped, what was replaced, and the decision taken on
// every possible person name, which only the private candidates list shows.
type Result struct {
	Dump       mediawiki.Dump
	Denylist   Denylist
	Stats      Stats
	Candidates []CandidateDecision
}

// Stats counts what Scrub replaced. It holds no replaced text.
type Stats struct {
	PagesKept     int
	PagesDropped  int
	Names         int
	Honorifics    int
	Emails        int
	Phones        int
	WikiUsers     int
	PoemBlocks    int
	ProseBlocks   int
	CommentBlocks int
}

// Scrub returns the scrubbed copy of d:
//   - only pages, category members and activity of KeptNamespaces are kept;
//   - every inline lyrics block (mediawiki.InlineLyricsBlocks) is replaced with LyricPlaceholderLine lines, at most
//     InlineLyricsMinLines of them, so the inline-lyrics classification of every section is kept;
//   - every deny phrase (CandidateDecision.Deny) is replaced with its numbered placeholder everywhere it occurs, as
//     mediawiki.NameMatcher finds it: in wikitext, titles, category names and members, redirects and activity; a
//     legal name also with an honorific before its surname ("Mr. Surname") and with its last word before its first;
//   - any other honorific followed by a capitalised word, emails, and phone numbers outside links are replaced;
//   - wiki user names are replaced in activity and in links to user pages.
func Scrub(d mediawiki.Dump) (Result, error) {
	var pages []mediawiki.Page
	for _, p := range d.Pages {
		if KeptNamespaces[p.NS] {
			pages = append(pages, p)
		}
	}
	sort.SliceStable(pages, func(a, b int) bool { return pages[a].PageID < pages[b].PageID })

	s := scrubber{emails: map[string]string{}, phones: map[string]string{}, honorifics: map[string]string{},
		users: map[string]string{}}
	s.stats.PagesKept, s.stats.PagesDropped = len(pages), len(d.Pages)-len(pages)

	unlyriced := make([]mediawiki.Page, len(pages))
	var bodies, outside []string
	for k, p := range pages {
		unlyriced[k] = p
		unlyriced[k].Wikitext, bodies, outside = s.lyrics(p.Wikitext, bodies, outside)
		outside = append(outside, p.Title)
	}
	descriptions := map[string]string{}
	for _, key := range sortedKeys(d.Categories) {
		descriptions[key], bodies, outside = s.lyrics(d.Categories[key].DescriptionWikitext, bodies, outside)
	}
	denylist := BuildDenylist(bodies, outside)

	phrases, decisions := collectPhrases(unlyriced, d.Redirects)
	s.names = newNameReplacer(phrases)
	s.numberUsers(d.Activity, pages)

	out := mediawiki.Dump{Categories: map[string]mediawiki.Category{}}
	for _, p := range unlyriced {
		out.Pages = append(out.Pages, s.page(p))
	}
	for _, key := range sortedKeys(d.Categories) {
		c := d.Categories[key]
		c.Name = s.title(c.Name)
		c.Parents = s.titles(c.Parents)
		c.Subcategories = s.titles(c.Subcategories)
		c.Pages = s.members(c.Pages)
		c.DescriptionWikitext = s.wikitext(descriptions[key])
		out.Categories[s.title(key)] = c
	}
	for _, r := range d.Redirects {
		if KeptNamespaces[r.NS] {
			out.Redirects = append(out.Redirects, mediawiki.Redirect{From: s.title(r.From), NS: r.NS, To: s.title(r.To)})
		}
	}
	for _, a := range d.Activity {
		if KeptNamespaces[a.NS] {
			out.Activity = append(out.Activity, s.activity(a))
		}
	}
	if len(out.Categories) != len(d.Categories) {
		return Result{}, ErrPlaceholderCollision
	}
	s.stats.Emails, s.stats.Phones, s.stats.WikiUsers = len(s.emails), len(s.phones), len(s.users)

	return Result{Dump: out, Denylist: denylist, Stats: s.stats, Candidates: decisions}, nil
}

// ErrPlaceholderCollision is returned when replacing names would make two category names one.
var ErrPlaceholderCollision = errors.New("scrubbing makes two category names equal")

// scrubber holds the numbers given so far to emails, phones, honorific names and wiki users. Scrub numbers them in
// the order it meets them, which is fixed (pages by page ID, then categories by name, redirects and activity).
type scrubber struct {
	names      nameReplacer
	emails     map[string]string
	phones     map[string]string
	honorifics map[string]string
	users      map[string]string
	stats      Stats
}

func (s *scrubber) page(p mediawiki.Page) mediawiki.Page {
	title := s.title(p.Title)
	out := p
	out.Title = title
	out.Wikitext = s.wikitext(p.Wikitext)
	out.Categories = s.titles(p.Categories)
	out.HiddenCategories = s.titles(p.HiddenCategories)
	if title != p.Title {
		out.URL = pageURL(p.URL, title)
	}

	return out
}

// pageURL returns the URL of the page titled title on the wiki of url, a page URL of the form …/wiki/Title.
func pageURL(pageURL, title string) string {
	k := strings.LastIndex(pageURL, "/wiki/")
	if k < 0 {
		return ""
	}
	path := (&url.URL{Path: strings.ReplaceAll(title, " ", "_")}).EscapedPath()

	return pageURL[:k+len("/wiki/")] + path
}

func (s *scrubber) title(t string) string {
	out, n := s.names.replace(t, nil)
	s.stats.Names += n

	return out
}

func (s *scrubber) titles(ts []string) []string {
	if ts == nil {
		return nil
	}
	out := make([]string, len(ts))
	for k, t := range ts {
		out[k] = s.title(t)
	}

	return out
}

func (s *scrubber) members(ms []mediawiki.CategoryMember) []mediawiki.CategoryMember {
	if ms == nil {
		return nil
	}
	out := []mediawiki.CategoryMember{}
	for _, m := range ms {
		if KeptNamespaces[m.NS] {
			out = append(out, mediawiki.CategoryMember{Title: s.title(m.Title), NS: m.NS})
		}
	}

	return out
}

func (s *scrubber) activity(a mediawiki.PageActivity) mediawiki.PageActivity {
	out := a
	out.Title = s.title(a.Title)
	if a.Users != nil {
		out.Users = map[string]int{}
		for name, n := range a.Users {
			out.Users[s.user(name)] += n
		}
	}

	return out
}

// wikitext scrubs the names, emails, phones and user links of a page's wikitext, its lyrics already replaced.
func (s *scrubber) wikitext(text string) string {
	var n int
	text, n = s.names.replace(text, urlSpans(text))
	s.stats.Names += n
	text = emailPattern.ReplaceAllStringFunc(text, s.email)
	text = s.prose(text, phonePattern, s.phone)
	text = s.prose(text, honorificPattern, s.honorific)

	return s.userLinks(text)
}

var (
	emailPattern = regexp.MustCompile(`[\p{L}\p{N}._%+-]+@[\p{L}\p{N}-]+(?:\.[\p{L}\p{N}-]+)+`)
	phonePattern = regexp.MustCompile(`\+?\d[\d ().-]{7,}\d`)
	// honorificPattern is an honorific and the capitalised word after it.
	honorificPattern = regexp.MustCompile(`\b(?:Mr|Mrs|Ms|Mx|Dr|Miss)\.?[ \t]+(\p{Lu}[\p{L}\p{M}'’-]*)`)
	// urlPattern is anything that looks like a URL, also where wikitext does not make it a link (a template
	// parameter's value), so that names in it get the URL form of their placeholder.
	urlPattern     = regexp.MustCompile(`(?i)(?:https?:)?//[^\s\[\]<>|{}"]+|\bwww\.[^\s\[\]<>|{}"]+`)
	phoneDigits    = regexp.MustCompile(`\d`)
	phoneMinDigits = 9
)

func (s *scrubber) email(e string) string {
	if strings.HasSuffix(strings.ToLower(e), placeholderDomain) {
		return e
	}

	return numbered(s.emails, strings.ToLower(e), emailFormat)
}

func (s *scrubber) phone(m []int, text string) string {
	p := text[m[0]:m[1]]
	digits := phoneDigits.FindAllString(p, -1)
	if len(digits) < phoneMinDigits || isPhonePlaceholder(p) {
		return p
	}

	return numbered(s.phones, strings.Join(digits, ""), phoneFormat)
}

var phonePlaceholder = regexp.MustCompile(`^000-000-\d{4}$`)

func isPhonePlaceholder(p string) bool {
	return phonePlaceholder.MatchString(p)
}

// honorific replaces the word after an honorific with a placeholder of its own, unless it is one already.
func (s *scrubber) honorific(m []int, text string) string {
	word := text[m[2]:m[3]]
	if startsWithPlaceholder(text[m[2]:]) {
		return text[m[0]:m[1]]
	}
	s.stats.Honorifics++
	key := mediawiki.FoldName(word)
	if _, ok := s.honorifics[key]; !ok {
		s.honorifics[key] = s.names.fresh()
	}

	return text[m[0]:m[2]] + s.honorifics[key]
}

// prose replaces each match of re outside URLs and link targets with what replace returns for it.
func (s *scrubber) prose(text string, re *regexp.Regexp, replace func(m []int, text string) string) string {
	masked := []byte(text)
	for _, span := range append(urlSpans(text), linkTargetSpans(text)...) {
		for k := span[0]; k < span[1]; k++ {
			masked[k] = ' '
		}
	}
	var b strings.Builder
	last := 0
	for _, m := range re.FindAllStringSubmatchIndex(string(masked), -1) {
		b.WriteString(text[last:m[0]])
		b.WriteString(replace(m, text))
		last = m[1]
	}
	b.WriteString(text[last:])

	return b.String()
}

func urlSpans(text string) [][2]int {
	var out [][2]int
	for _, m := range urlPattern.FindAllStringIndex(text, -1) {
		out = append(out, [2]int{m[0], m[1]})
	}

	return out
}

// linkTargetSpans are the spans of each wikilink up to its display part.
func linkTargetSpans(text string) [][2]int {
	var out [][2]int
	for _, l := range mediawiki.FindWikilinks(text) {
		end := l.End
		if pipe := strings.IndexByte(l.Raw, '|'); pipe >= 0 {
			end = l.Start + pipe
		}
		out = append(out, [2]int{l.Start, end})
	}

	return out
}

// numbered returns the placeholder of key in seen, giving it the next number of format if it has none.
func numbered(seen map[string]string, key, format string) string {
	if p, ok := seen[key]; ok {
		return p
	}
	seen[key] = fmt.Sprintf(format, len(seen)+1)

	return seen[key]
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	return keys
}
