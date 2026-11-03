package mediawiki

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

type oracleLinkOccurrence struct {
	URL        string     `json:"url"`
	Text       *string    `json:"text"`
	Bracketed  bool       `json:"bracketed"`
	SectionKey SectionKey `json:"section_key"`
}

type oracleLinksPage struct {
	PageID        int64                  `json:"pageid"`
	Title         string                 `json:"title"`
	ExternalLinks []oracleLinkOccurrence `json:"external_links"`
	AllExternal   []oracleLinkOccurrence `json:"all_external_links"`
}

type oracleCollection struct {
	Letter      string                `json:"letter"`
	Label       string                `json:"label"`
	URL         string                `json:"url"`
	Host        *string               `json:"host"`
	Note        *string               `json:"note"`
	Depth       int                   `json:"depth"`
	Parent      *string               `json:"parent"`
	Kind        domain.CollectionKind `json:"kind"`
	Description *string               `json:"description"`
}

type oracleRepeatedURLs struct {
	Title string `json:"title"`
	Dups  []struct {
		URL string `json:"url"`
		N   int    `json:"n"`
	} `json:"dups"`
}

type oracleLinkQuality struct {
	BareURLs []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	} `json:"bare_urls"`
	TrackingParams int                  `json:"tracking_params_in_urls"`
	RepeatedURLs   []oracleRepeatedURLs `json:"duplicate_urls_within_page"`
}

type linksOracle struct {
	site        Site
	pieces      []PieceRecord
	people      People
	links       Links
	refPieces   []oracleLinksPage
	refBards    []oracleLinksPage
	collections []oracleCollection
	quality     oracleLinkQuality
}

func loadLinksOracle(t *testing.T) linksOracle {
	t.Helper()
	dumpFS, analysisFS := wikidumptest.Research(t)
	dump, err := LoadDump(dumpFS)
	wikidumptest.NoError(t, "dump", err)
	o := linksOracle{site: NewSite(dump)}
	o.pieces = o.site.Pieces()
	o.people = ParsePeople(o.site, o.pieces)
	o.links = ParseLinks(o.site, o.pieces, o.people)
	var pieces struct {
		Pieces []oracleLinksPage `json:"pieces"`
	}
	wikidumptest.NoError(t, "pieces.json", decodeAnalysis(analysisFS, "pieces.json", &pieces))
	o.refPieces = pieces.Pieces
	var bards struct {
		Bards []oracleLinksPage `json:"bards"`
	}
	wikidumptest.NoError(t, "bards.json", decodeAnalysis(analysisFS, "bards.json", &bards))
	o.refBards = bards.Bards
	var collections struct {
		Collections []oracleCollection `json:"collections"`
	}
	wikidumptest.NoError(t, "collections.json", decodeAnalysis(analysisFS, "collections.json", &collections))
	o.collections = collections.Collections
	wikidumptest.NoError(t, "quality.json", decodeAnalysis(analysisFS, "quality.json", &o.quality))

	return o
}

func occurrenceProjection(occurrences []LinkOccurrence) []oracleLinkOccurrence {
	out := []oracleLinkOccurrence{}
	for _, o := range occurrences {
		out = append(out, oracleLinkOccurrence{URL: o.URL, Text: optional(o.Text), Bracketed: o.Bracketed,
			SectionKey: o.Section})
	}

	return out
}

func nonNilOccurrences(s []oracleLinkOccurrence) []oracleLinkOccurrence {
	if s == nil {
		return []oracleLinkOccurrence{}
	}

	return s
}

func TestResearchOracle_ExternalLinks(t *testing.T) {
	o := loadLinksOracle(t)
	deviation(t, "links.repaired_markup")
	pieces := map[int64]PieceLinks{}
	for _, pl := range o.links.Pieces {
		pieces[pl.PageID] = pl
	}
	persons := map[int64]PersonLinks{}
	for _, pl := range o.links.Persons {
		persons[pl.PageID] = pl
	}
	repaired := map[int64]bool{}
	for _, p := range o.pieces {
		repaired[p.PageID] = hasRepairedURL(p.MarkupIssues)
	}
	for _, b := range o.people.Bards {
		repaired[b.PageID] = hasRepairedURL(b.MarkupIssues)
	}
	for _, want := range o.refPieces {
		at := wikidumptest.Location{PageID: want.PageID, Field: "external_links", Offset: -1}
		if !repaired[want.PageID] {
			wikidumptest.Equal(t, at, nonNilOccurrences(want.ExternalLinks),
				occurrenceProjection(pieces[want.PageID].Occurrences))
		}
	}
	for _, want := range o.refBards {
		at := wikidumptest.Location{PageID: want.PageID, Field: "all_external_links", Offset: -1}
		if !repaired[want.PageID] {
			wikidumptest.Equal(t, at, nonNilOccurrences(want.AllExternal),
				occurrenceProjection(persons[want.PageID].Occurrences))
		}
	}
}

func hasRepairedURL(issues []MarkupIssue) bool {
	return slices.ContainsFunc(issues, func(m MarkupIssue) bool { return m.Kind == MarkupDoubleBracketURL })
}

// TestResearchOracle_LinksDedupe groups the reference's section links of each piece by normalised URL: one link
// per URL, with the first URL as entered and the sections in first-seen order.
func TestResearchOracle_LinksDedupe(t *testing.T) {
	o := loadLinksOracle(t)
	deviation(t, "links.dedupe")
	type grouped struct {
		URL      string
		Sections []SectionKey
	}
	byID := map[int64]PieceRecord{}
	for _, p := range o.pieces {
		byID[p.PageID] = p
	}
	for _, ref := range o.refPieces {
		want := []grouped{}
		index := map[string]int{}
		for _, e := range ref.ExternalLinks {
			normalized, err := domain.NormalizeURL(e.URL)
			if err != nil || e.SectionKey.IsInfobox() {
				continue
			}
			k, ok := index[normalized]
			if !ok {
				index[normalized] = len(want)
				want = append(want, grouped{URL: e.URL})
				k = len(want) - 1
			}
			want[k].Sections = appendNew(want[k].Sections, e.SectionKey)
		}
		got := []grouped{}
		for _, l := range extractLinks(byID[ref.PageID].Parsed, linkContext{}).Links {
			got = append(got, grouped{URL: l.URL, Sections: l.Sections})
		}
		wikidumptest.Equal(t, wikidumptest.Location{PageID: ref.PageID, Field: "links", Offset: -1}, want, got)
	}
}

func TestResearchOracle_Collections(t *testing.T) {
	o := loadLinksOracle(t)
	c := o.links.Collections
	wikidumptest.Equal(t, wikidumptest.Location{Field: "collections.count", Offset: -1}, len(o.collections),
		c.Entries())
	labels := map[string]string{}
	for _, r := range c.Records {
		labels[r.NormalizedURL] = r.Label
	}
	seen := map[string]bool{}
	var firsts, repeats []oracleCollection
	for _, want := range o.collections {
		if seen[want.URL] {
			repeats = append(repeats, want)
		} else {
			firsts = append(firsts, want)
		}
		seen[want.URL] = true
	}
	project := func(records []CollectionRecord) []oracleCollection {
		out := []oracleCollection{}
		for _, r := range records {
			rec := oracleCollection{Letter: r.Letter, Label: r.Label, URL: r.URL, Host: optional(referenceHost(r.URL)),
				Note: optional(r.Note), Depth: r.Depth, Kind: r.Kind, Description: optional(r.Description)}
			if r.ParentURL != "" {
				parent := labels[r.ParentURL]
				rec.Parent = &parent
			}
			out = append(out, rec)
		}

		return out
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "collections", Offset: -1}, firsts, project(c.Records))
	wikidumptest.Equal(t, wikidumptest.Location{Field: "collections.duplicates", Offset: -1}, repeats,
		project(c.Duplicates))
}

func TestResearchOracle_LinkQuality(t *testing.T) {
	o := loadLinksOracle(t)
	type pageURL struct{ Title, URL string }
	wantBare := []pageURL{}
	for _, b := range o.quality.BareURLs {
		wantBare = append(wantBare, pageURL(b))
	}
	gotBare := []pageURL{}
	for _, b := range o.links.BareURLs() {
		gotBare = append(gotBare, pageURL{Title: b.Page, URL: b.URL})
	}
	byPage := func(a, b pageURL) int { return strings.Compare(a.Title+"\n"+a.URL, b.Title+"\n"+b.URL) }
	slices.SortFunc(wantBare, byPage)
	slices.SortFunc(gotBare, byPage)
	wikidumptest.Equal(t, wikidumptest.Location{Field: "bare_urls", Offset: -1}, wantBare, gotBare)
	wikidumptest.Equal(t, wikidumptest.Location{Field: "tracking_params_in_urls", Offset: -1},
		o.quality.TrackingParams, o.links.TrackingParameterURLs())
	want := map[string][]string{}
	for _, r := range o.quality.RepeatedURLs {
		for _, d := range r.Dups {
			want[r.Title] = append(want[r.Title], d.URL)
		}
	}
	got := map[string][]string{}
	for _, pl := range o.links.Pieces {
		if urls := pl.RepeatedURLs(); len(urls) > 0 {
			got[pl.Title] = urls
		}
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "duplicate_urls_within_page", Offset: -1}, want, got)
}

// referenceHost is a URL's host as the reference reads it: in lower case without a leading "www.", or empty
// if it does not parse.
func referenceHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		return ""
	}

	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}
