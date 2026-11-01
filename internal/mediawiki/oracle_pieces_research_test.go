package mediawiki

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

type oracleTaxonomyNode struct {
	Classification CategoryClass `json:"classification"`
	Facets         []string      `json:"facets"`
	Ancestors      []string      `json:"ancestors"`
	RedirectTo     *string       `json:"redirect_to"`
}

type oracleConsistency struct {
	InInfoboxNotTagged []string `json:"in_infobox_not_tagged"`
	TaggedNotInInfobox []string `json:"tagged_not_in_infobox"`
}

type oraclePieceRecord struct {
	PageID              int64             `json:"pageid"`
	Title               string            `json:"title"`
	Kind                domain.PieceKind  `json:"kind"`
	H1                  *string           `json:"h1"`
	TitleField          *string           `json:"title_field"`
	AlternateTitles     []string          `json:"alternate_titles"`
	SectionKeys         []SectionKey      `json:"section_keys"`
	BardNameCategories  []string          `json:"bard_name_categories"`
	CategoryConsistency oracleConsistency `json:"category_consistency"`
	ConservationStatus  []string          `json:"conservation_status"`
}

type oracleTitled struct {
	Title string `json:"title"`
}

type oracleQuality struct {
	InfoboxValueNotTagged      []oracleTitled `json:"infobox_value_not_tagged"`
	TaggedNotInInfobox         []oracleTitled `json:"tagged_not_in_infobox"`
	LinksToRedirectCategories  []string       `json:"links_to_redirect_categories"`
	InlineLyricsPages          []oracleTitled `json:"inline_lyrics_pages"`
	ConservationStatusAssigned []oracleTitled `json:"conservation_status_assigned"`
}

type pieceOracle struct {
	site     Site
	pieces   map[int64]PieceRecord
	expected []oraclePieceRecord
	taxonomy map[string]oracleTaxonomyNode
	quality  oracleQuality
}

func loadPieceOracle(t *testing.T) pieceOracle {
	t.Helper()
	dumpFS, analysisFS := wikidumptest.Research(t)
	dump, err := LoadDump(dumpFS)
	wikidumptest.NoError(t, "dump", err)
	o := pieceOracle{site: NewSite(dump), pieces: map[int64]PieceRecord{}}
	var pieces struct {
		Pieces []oraclePieceRecord `json:"pieces"`
	}
	wikidumptest.NoError(t, "pieces.json", decodeAnalysis(analysisFS, "pieces.json", &pieces))
	o.expected = pieces.Pieces
	var taxonomy struct {
		Nodes map[string]oracleTaxonomyNode `json:"nodes"`
	}
	wikidumptest.NoError(t, "taxonomy.json", decodeAnalysis(analysisFS, "taxonomy.json", &taxonomy))
	o.taxonomy = taxonomy.Nodes
	wikidumptest.NoError(t, "quality.json", decodeAnalysis(analysisFS, "quality.json", &o.quality))
	for _, p := range o.site.Pieces() {
		o.pieces[p.PageID] = p
	}

	return o
}

func TestResearchOracle_Taxonomy(t *testing.T) {
	o := loadPieceOracle(t)
	at := func(field string, k int) wikidumptest.Location {
		return wikidumptest.Location{Field: "taxonomy." + field, Offset: k}
	}
	names := sortedCategories(o.site.Taxonomy)
	wikidumptest.Equal(t, at("count", -1), len(o.taxonomy), len(names))
	for k, name := range names {
		want, ok := o.taxonomy[name]
		if !wikidumptest.Equal(t, at("present", k), true, ok) {
			continue
		}
		got := o.site.Taxonomy.Nodes[name]
		wikidumptest.Equal(t, at("classification", k), want.Classification, got.Class)
		wikidumptest.Equal(t, at("facets", k), nonNil(want.Facets), nonNil(got.Facets))
		wikidumptest.Equal(t, at("ancestors", k), nonNil(want.Ancestors), nonNil(got.Ancestors))
		wikidumptest.Equal(t, at("redirect_to", k), deref(want.RedirectTo), got.RedirectTo)
	}
}

func TestResearchOracle_Pieces(t *testing.T) {
	o := loadPieceOracle(t)
	wikidumptest.Equal(t, wikidumptest.Location{Field: "pieces.count", Offset: -1}, len(o.expected), len(o.pieces))
	for _, want := range o.expected {
		got, ok := o.pieces[want.PageID]
		at := func(field string) wikidumptest.Location {
			return wikidumptest.Location{PageID: want.PageID, Field: "pieces." + field, Offset: -1}
		}
		if !wikidumptest.Equal(t, at("present"), true, ok) {
			continue
		}
		wikidumptest.Equal(t, at("kind"), want.Kind, got.Kind)
		keys := make([]SectionKey, 0, len(got.Parsed.Sections))
		for _, s := range got.Parsed.Sections {
			keys = append(keys, s.Key)
		}
		wikidumptest.Equal(t, at("section_keys"), want.SectionKeys, keys)
		wikidumptest.Equal(t, at("h1"), deref(want.H1), got.Parsed.H1)
		wikidumptest.Equal(t, at("bard_name_categories"), nonNil(want.BardNameCategories),
			nonNil(got.BardNameCategories))
		wikidumptest.Equal(t, at("in_infobox_not_tagged"), nonNil(want.CategoryConsistency.InInfoboxNotTagged),
			driftCategories(got, DriftInInfoboxNotTagged))
		wikidumptest.Equal(t, at("tagged_not_in_infobox"), nonNil(want.CategoryConsistency.TaggedNotInInfobox),
			driftCategories(got, DriftTaggedNotInInfobox))
		wikidumptest.Equal(t, at("conservation_status"), conservationOf(want.ConservationStatus),
			nonNilStatuses(got.ConservationStatuses))
		compareAlternateTitles(t, want, got)
	}
}

func driftCategories(p PieceRecord, kind FacetDriftKind) []string {
	out := []string{}
	for _, d := range p.Drift {
		if d.Kind == kind {
			out = append(out, d.Category)
		}
	}

	return out
}

func conservationOf(categories []string) []domain.ConservationStatus {
	out := []domain.ConservationStatus{}
	for _, cs := range conservationStatuses {
		if slices.Contains(categories, cs.category) {
			out = append(out, cs.status)
		}
	}

	return out
}

func nonNilStatuses(s []domain.ConservationStatus) []domain.ConservationStatus {
	if s == nil {
		return []domain.ConservationStatus{}
	}

	return s
}

type titleSourceKey struct {
	key    string
	source TitleSource
}

var titleFragmentSeparators = regexp.MustCompile(`[()&/]`)

// titleFragmentKeys are the title keys of s's parts between parentheses, "&" and "/", without quotes.
func titleFragmentKeys(s string) []string {
	var out []string
	for _, f := range titleFragmentSeparators.Split(s, -1) {
		if key := titleKey(strings.Trim(f, titleQuotes)); key != "" {
			out = append(out, key)
		}
	}

	return out
}

// compareAlternateTitles checks that every alternate title the reference found is one of ours (imported or
// rejected) part by part, or is inside a line listed as unparsed, and that every title of ours comes from the
// reference's alternates, its infobox title or the H1.
func compareAlternateTitles(t *testing.T, want oraclePieceRecord, got PieceRecord) {
	t.Helper()
	deviation(t, "titles.parentheticals")
	deviation(t, "titles.h1_alternates")
	deviation(t, "titles.rejected")
	deviation(t, "titles.unparsed_prose")
	deviation(t, "titles.quotes_and_ampersands")
	deviation(t, "titles.redirects")
	ours := map[string]bool{titleKey(want.Title): true}
	var sources []titleSourceKey
	add := func(text string, source TitleSource) {
		ours[titleKey(text)] = true
		sources = append(sources, titleSourceKey{key: titleKey(text), source: source})
	}
	for _, a := range got.Titles.Alternates {
		add(a.Text, a.Source)
	}
	for _, r := range got.Titles.Rejected {
		add(r.Text, r.Source)
	}
	for k, alt := range want.AlternateTitles {
		covered := !slices.ContainsFunc(titleFragmentKeys(alt), func(key string) bool { return !ours[key] })
		for _, u := range got.Titles.Unparsed {
			covered = covered || strings.Contains(titleKey(u.Text), titleKey(alt))
		}
		wikidumptest.Equal(t, wikidumptest.Location{PageID: want.PageID, Field: "alternate_titles.covered", Offset: k},
			true, covered)
	}
	reference := map[TitleSource][]string{
		TitleFromInfobox:     append(titleFragmentKeys(deref(want.TitleField)), titleKey(Plain(deref(want.TitleField)))),
		TitleFromH1:          append(titleFragmentKeys(Plain(deref(want.H1))), titleKey(Plain(deref(want.H1)))),
		TitleFromOtherTitles: {},
		TitleFromAlsoKnownAs: {},
	}
	for _, alt := range want.AlternateTitles {
		keys := titleFragmentKeys(alt)
		reference[TitleFromOtherTitles] = append(reference[TitleFromOtherTitles], keys...)
		reference[TitleFromAlsoKnownAs] = append(reference[TitleFromAlsoKnownAs], keys...)
		reference[TitleFromInfobox] = append(reference[TitleFromInfobox], keys...)
	}
	for k, s := range sources {
		if s.source == TitleFromRedirect {
			continue
		}
		wikidumptest.Equal(t, wikidumptest.Location{PageID: want.PageID, Field: "alternate_titles.from", Offset: k},
			true, slices.Contains(reference[s.source], s.key))
	}
}

func TestResearchOracle_Quality(t *testing.T) {
	o := loadPieceOracle(t)
	byTitle := func(kind func(PieceRecord) bool) []string {
		out := []string{}
		for _, p := range o.pieces {
			if kind(p) {
				out = append(out, p.Title)
			}
		}
		sort.Strings(out)

		return out
	}
	titles := func(list []oracleTitled) []string {
		out := []string{}
		for _, x := range list {
			out = append(out, x.Title)
		}
		sort.Strings(out)

		return out
	}
	hasDrift := func(kind FacetDriftKind) func(PieceRecord) bool {
		return func(p PieceRecord) bool { return len(driftCategories(p, kind)) > 0 }
	}
	at := func(field string) wikidumptest.Location { return wikidumptest.Location{Field: field, Offset: -1} }
	wikidumptest.Equal(t, at("infobox_value_not_tagged"), titles(o.quality.InfoboxValueNotTagged),
		byTitle(hasDrift(DriftInInfoboxNotTagged)))
	wikidumptest.Equal(t, at("tagged_not_in_infobox"), titles(o.quality.TaggedNotInInfobox),
		byTitle(hasDrift(DriftTaggedNotInInfobox)))
	redirectLinks := append([]string(nil), o.quality.LinksToRedirectCategories...)
	sort.Strings(redirectLinks)
	wikidumptest.Equal(t, at("links_to_redirect_categories"), redirectLinks,
		byTitle(hasDrift(DriftRedirectCategoryLink)))
	wikidumptest.Equal(t, at("inline_lyrics_pages"), titles(o.quality.InlineLyricsPages),
		byTitle(func(p PieceRecord) bool { return p.Lyrics.HadInlineLyrics }))
	wikidumptest.Equal(t, at("conservation_status_assigned"), titles(o.quality.ConservationStatusAssigned),
		byTitle(func(p PieceRecord) bool { return len(p.ConservationStatuses) > 0 }))
}
