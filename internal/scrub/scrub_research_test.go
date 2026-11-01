package scrub

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

// These tests read the private research crawl. They report only through wikidumptest (page, field and offset,
// with digests in place of values), so no name or lyric of the crawl reaches the test output.

func researchDump(t *testing.T) mediawiki.Dump {
	t.Helper()
	dumpFS, _ := wikidumptest.Research(t)
	d, err := mediawiki.LoadDump(dumpFS)
	wikidumptest.NoError(t, "loading the crawl", err)

	return d
}

func researchScrub(t *testing.T, d mediawiki.Dump) Result {
	t.Helper()
	res, err := Scrub(d)
	wikidumptest.NoError(t, "scrubbing the crawl", err)

	return res
}

// TestScrubbedDumpIsUpToDate: scrubbing the crawl gives the committed copy and deny list byte for byte. After a
// change to the scrubber or to anything it uses, regenerate the copy with cmd/scrub-dump (docs/TESTING.md).
func TestScrubbedDumpIsUpToDate(t *testing.T) {
	res := researchScrub(t, researchDump(t))
	files, err := mediawiki.EncodeDump(res.Dump)
	wikidumptest.NoError(t, "encoding the scrubbed crawl", err)
	files[DenylistFile] = res.Denylist.Encode()

	dir := filepath.Join(wikidumptest.RepositoryRoot(t), wikidumptest.ScrubbedDir)
	entries, err := os.ReadDir(dir)
	wikidumptest.NoError(t, "listing the committed copy", err)
	var committed []string
	for _, e := range entries {
		committed = append(committed, e.Name())
	}
	var want []string
	for name := range files {
		want = append(want, name)
	}
	sort.Strings(want)
	wikidumptest.Equal(t, wikidumptest.Location{Field: "files", Offset: -1}, want, committed)
	for _, name := range want {
		content, err := os.ReadFile(filepath.Clean(filepath.Join(dir, name)))
		wikidumptest.NoError(t, "reading the committed copy", err)
		wikidumptest.Equal(t, wikidumptest.Location{Field: name, Offset: -1}, files[name], content)
	}
}

// pageCounts is what the importer reads of one page, as counts and keys that hold no name: the page type, the
// sections' levels and keys (other headings only as "other"), the links and templates outside inline lyrics,
// the infoboxes, the inline-lyrics flag and the performance items with linked performers.
type pageCounts struct {
	Type             mediawiki.PageType
	Sections         []string
	Wikilinks        int
	ExternalLinks    int
	Templates        int
	Infoboxes        int
	InlineLyrics     bool
	PerformanceItems int
	WithPerformers   int
	Performers       int
}

func countPage(p mediawiki.Page, maxYear int) pageCounts {
	lyrics := mediawiki.InlineLyricsBlocks(p.Wikitext)
	text := mediawiki.WithoutInlineLyrics(p.Wikitext, lyrics.Blocks)
	parsed := mediawiki.ParsePage(text)
	c := pageCounts{
		Type: mediawiki.TypePage(p), Wikilinks: len(mediawiki.FindWikilinks(text)),
		ExternalLinks: len(mediawiki.FindExternalLinks(text)), Templates: len(mediawiki.FindTemplates(text)),
		Infoboxes: len(parsed.Infoboxes), InlineLyrics: lyrics.HadInlineLyrics,
	}
	for _, s := range parsed.Sections {
		key := string(s.Key)
		if s.Key.IsOther() {
			key = "other"
		}
		c.Sections = append(c.Sections, strings.Repeat("=", s.Level)+key)
		if s.Key != mediawiki.SectionPerformancesRecordings {
			continue
		}
		for _, item := range s.Content.Items {
			c.PerformanceItems++
			if n := len(mediawiki.ParsePerformanceText(item.Raw, maxYear).Performers); n > 0 {
				c.WithPerformers++
				c.Performers += n
			}
		}
	}

	return c
}

// corpusCounts are the counts of a whole dump that chunk tests assert.
type corpusCounts struct {
	PagesByType        map[mediawiki.PageType]int
	InlineLyricsPages  int
	RedirectsByNS      map[int]int
	Categories         int
	CategoryMembers    int
	ActivityPages      int
	CollectionLinks    int
	CollectionURLs     int
	PerformanceItems   int
	WithPerformers     int
	Sections           int
	Wikilinks          int
	ExternalLinks      int
	pagesByID          map[int64]pageCounts
	collectionPageSeen bool
}

func countCorpus(d mediawiki.Dump) corpusCounts {
	c := corpusCounts{PagesByType: map[mediawiki.PageType]int{}, RedirectsByNS: map[int]int{},
		pagesByID: map[int64]pageCounts{}}
	maxYear := mediawiki.LatestYear(d.Pages)
	for _, p := range d.Pages {
		if KeptNamespaces[p.NS] {
			c.addPage(p, countPage(p, maxYear))
		}
	}
	for _, r := range d.Redirects {
		c.RedirectsByNS[r.NS]++
	}
	for _, cat := range d.Categories {
		c.Categories++
		for _, m := range cat.Pages {
			if KeptNamespaces[m.NS] {
				c.CategoryMembers++
			}
		}
	}
	for _, a := range d.Activity {
		if KeptNamespaces[a.NS] {
			c.ActivityPages++
		}
	}

	return c
}

func (c *corpusCounts) addPage(p mediawiki.Page, pc pageCounts) {
	c.pagesByID[p.PageID] = pc
	c.PagesByType[pc.Type]++
	if pc.InlineLyrics && pc.Type == mediawiki.PageTypePiece {
		c.InlineLyricsPages++
	}
	c.PerformanceItems += pc.PerformanceItems
	c.WithPerformers += pc.WithPerformers
	c.Sections += len(pc.Sections)
	c.Wikilinks += pc.Wikilinks
	c.ExternalLinks += pc.ExternalLinks
	if p.Title != mediawiki.CollectionsPage {
		return
	}
	c.collectionPageSeen = true
	urls := map[string]bool{}
	for _, l := range mediawiki.FindExternalLinks(p.Wikitext) {
		c.CollectionLinks++
		if u, err := domain.NormalizeURL(l.URL); err == nil {
			urls[u] = true
		}
	}
	c.CollectionURLs = len(urls)
}

// TestScrubPreservesResearchCounts: every count the importer's tests assert is the same in the committed copy as
// in the crawl, and so is every page's structure (pageCounts).
func TestScrubPreservesResearchCounts(t *testing.T) {
	crawl := countCorpus(researchDump(t))
	scrubbedCopy, err := mediawiki.LoadDump(wikidumptest.Scrubbed(t))
	wikidumptest.NoError(t, "loading the committed copy", err)
	committed := countCorpus(scrubbedCopy)

	at := func(field string) wikidumptest.Location { return wikidumptest.Location{Field: field, Offset: -1} }
	wikidumptest.Equal(t, at("collections page present"), true, crawl.collectionPageSeen)
	wikidumptest.Equal(t, at("corpus counts"), crawl, committed)
	wikidumptest.Equal(t, at("pieces"), 142, crawl.PagesByType[mediawiki.PageTypePiece])
	wikidumptest.Equal(t, at("bards"), 48, crawl.PagesByType[mediawiki.PageTypeBard])
	wikidumptest.Equal(t, at("inline lyrics pieces"), 11, committed.InlineLyricsPages)
	wikidumptest.Equal(t, at("page redirects"), 6, committed.RedirectsByNS[mediawiki.NSMain])
	wikidumptest.Equal(t, at("category redirects"), 6, committed.RedirectsByNS[mediawiki.NSCategory])
	wikidumptest.Equal(t, at("collection URLs"), 61, committed.CollectionURLs)
	wikidumptest.Equal(t, at("pages"), len(crawl.pagesByID), len(committed.pagesByID))
	for id, want := range crawl.pagesByID {
		wikidumptest.Equal(t, wikidumptest.Location{PageID: id, Field: "page counts", Offset: -1}, want,
			committed.pagesByID[id])
	}
}

// TestScrubbedCopyHasNoResearchLyrics checks the committed copy against the crawl's inline lyrics found
// independently of the detector the scrubber uses (wikidumptest.OracleLyrics, in the sections the reference
// analysis lists as holding inline text): no 5-word window of them is in the copy unless the crawl's text outside
// the lyrics has it too. A detector that misses a block fails here once the copy is regenerated.
func TestScrubbedCopyHasNoResearchLyrics(t *testing.T) {
	_, analysis := wikidumptest.Research(t)
	d := researchDump(t)
	sections := wikidumptest.InlineTextSections(t, analysis)
	var outside []string
	type pageBlocks struct {
		page   mediawiki.Page
		blocks [][]int
	}
	var all []pageBlocks
	for _, p := range d.Pages {
		blocks := wikidumptest.OracleLyrics(p.Wikitext, sections[p.PageID])
		all = append(all, pageBlocks{page: p, blocks: blocks})
		outside = append(append(outside, wikidumptest.OutsideSpans(p.Wikitext, blocks)...), p.Title)
	}
	outsideGrams := lyricGrams(outside)
	var committed []string
	for _, p := range loadScrubbed(t).Pages {
		committed = append(committed, p.Wikitext)
	}
	committedGrams := lyricGrams(committed)

	checked := 0
	for _, pb := range all {
		for _, block := range pb.blocks {
			for _, gram := range lyricGramList(pb.page.Wikitext[block[0]:block[1]]) {
				if outsideGrams[gram] {
					continue
				}
				checked++
				wikidumptest.Equal(t, wikidumptest.Location{PageID: pb.page.PageID, Field: "lyric window",
					Offset: block[0]}, false, committedGrams[gram])
			}
		}
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "lyric windows checked", Offset: -1}, true, checked > 0)
}

// scrubLyricWindow is the word count of a lyric window.
const scrubLyricWindow = 5

// lyricGramList are the windows of scrubLyricWindow folded words of text.
func lyricGramList(text string) []string {
	words := mediawiki.Words(text)
	var out []string
	for k := 0; k+scrubLyricWindow <= len(words); k++ {
		gram := make([]string, 0, scrubLyricWindow)
		for _, w := range words[k : k+scrubLyricWindow] {
			gram = append(gram, w.Folded)
		}
		out = append(out, strings.Join(gram, " "))
	}

	return out
}

// lyricGrams are the windows of every text, each text on its own.
func lyricGrams(texts []string) map[string]bool {
	out := map[string]bool{}
	for _, text := range texts {
		for _, g := range lyricGramList(text) {
			out[g] = true
		}
	}

	return out
}

// TestRepositoryHasNoResearchPrivateText scans every text file of the repository, in clear, for every deny phrase
// of the crawl (with its honorific forms, its last word before its first, and every form mediawiki.NameMatcher
// finds) and for every email and phone number of the crawl. It is the check the deny list cannot be: names are
// never hashed into the repository.
func TestRepositoryHasNoResearchPrivateText(t *testing.T) {
	d := researchDump(t)
	phrases := denyPhrases(researchScrub(t, d).Candidates)
	wikidumptest.Equal(t, wikidumptest.Location{Field: "deny phrases found", Offset: -1}, true, len(phrases) > 0)
	names := newNameReplacer(phrases)
	private := contactDetails(d)

	for _, f := range repositoryTextFiles(t) {
		for _, m := range names.matcher.Find(f.text) {
			wikidumptest.Equal(t, wikidumptest.Location{Field: "repository file " + f.path, Offset: m.Start},
				"no deny phrase", "deny phrase")
		}
		lower := strings.ToLower(f.text)
		for text := range private {
			if k := strings.Index(lower, text); k >= 0 {
				wikidumptest.Equal(t, wikidumptest.Location{Field: "repository file " + f.path, Offset: k},
					"no email or phone", "email or phone")
			}
		}
	}
}

// denyPhrases are the private names of decisions, in placeholder order, legal if any decision on one says so.
func denyPhrases(decisions []CandidateDecision) []mediawiki.PrivateName {
	numbers := map[string]int{}
	var phrases []mediawiki.PrivateName
	for _, c := range decisions {
		if !c.Private {
			continue
		}
		if numbers[c.Placeholder] == 0 {
			phrases = append(phrases, mediawiki.PrivateName{Text: c.Text})
			numbers[c.Placeholder] = len(phrases)
		}
		if c.Reason == mediawiki.PrivacyHiddenAlias || c.Reason == mediawiki.PrivacyLegalMarker {
			phrases[numbers[c.Placeholder]-1].Legal = true
		}
	}

	return phrases
}

// contactDetails are the emails and the phone numbers in prose of the crawl, in lower case.
func contactDetails(d mediawiki.Dump) map[string]bool {
	private := map[string]bool{}
	s := scrubber{}
	for _, p := range d.Pages {
		for _, e := range mediawiki.EmailAddress.FindAllString(p.Wikitext, -1) {
			private[strings.ToLower(e)] = true
		}
		s.prose(p.Wikitext, phonePattern, func(m []int, text string) string {
			if digits := phoneDigits.FindAllString(text[m[0]:m[1]], -1); len(digits) >= phoneMinDigits {
				private[strings.ToLower(text[m[0]:m[1]])] = true
			}

			return text[m[0]:m[1]]
		})
	}

	return private
}
