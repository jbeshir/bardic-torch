package importer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
)

// lyricWindow is the n-gram length of the dropped-lyrics check, and minLyricLine the fewest words a whole lyric
// line must have to be checked on its own (shorter lines, "Chorus" or "Repeat", are common words).
const (
	lyricWindow  = 5
	minLyricLine = 3
)

// researchImport builds the import of the real crawl, or skips without BARDIC_RESEARCH_DIR.
func researchImport(t *testing.T) (mediawiki.Corpus, mediawiki.Dump) {
	t.Helper()
	dump, _ := wikidumptest.Research(t)
	d, err := mediawiki.LoadDump(dump)
	wikidumptest.NoError(t, "loading the dump", err)

	return mediawiki.ParseCorpus(d), d
}

func TestBuild_ResearchCountsOnRealDump(t *testing.T) {
	c, _ := researchImport(t)
	imp, review := Build(c, BuildOptions{Now: buildTime})

	wikidumptest.Equal(t, wikidumptest.Location{Field: "import counts", Offset: -1}, researchCounts,
		importCounts(c, imp, review))
	wikidumptest.Equal(t, wikidumptest.Location{Field: "import validates", Offset: -1}, true, imp.Validate() == nil)
}

// TestBuild_NoPrivateNameInPublicFieldsOnRealDump scans the public fields of the real import, in clear: no legal
// name of the crawl (the scrubber's legal names and every hidden alias, in every form) is in any of them, no other
// private name (an unsure one) is in any free-text field, and no email address is anywhere. An unsure name may
// name an entity the wiki itself names (a credited person, a title, a collection, an event), which the review lists.
func TestBuild_NoPrivateNameInPublicFieldsOnRealDump(t *testing.T) {
	c, _ := researchImport(t)
	imp, _ := Build(c, BuildOptions{Now: buildTime})
	var legal, all []mediawiki.PrivateName
	for _, n := range c.PrivateNames {
		all = append(all, n)
		if n.Legal {
			legal = append(legal, n)
		}
	}
	for _, b := range c.People.Bards {
		for _, a := range b.Aliases {
			if !a.IsPublic {
				legal = append(legal, mediawiki.PrivateName{Text: a.Name, Legal: true})
			}
		}
	}
	all = append(all, legal...)
	wikidumptest.Equal(t, wikidumptest.Location{Field: "legal names found", Offset: -1}, true, len(legal) > 0)
	wikidumptest.Equal(t, wikidumptest.Location{Field: "unsure names found", Offset: -1}, true, len(all) > len(legal))

	public := publicJSON(t, imp)
	for _, m := range mediawiki.NewPrivateNameMatcher(legal).Find(public) {
		wikidumptest.Equal(t, wikidumptest.Location{Field: "public import", Offset: m.Start}, "no legal name",
			"legal name")
	}
	for _, m := range mediawiki.EmailAddress.FindAllStringIndex(public, -1) {
		wikidumptest.Equal(t, wikidumptest.Location{Field: "public import", Offset: m[0]}, "no email", "email")
	}
	matcher := mediawiki.NewPrivateNameMatcher(all)
	for k, text := range publicFreeText(t, imp) {
		if len(matcher.Find(text)) > 0 {
			wikidumptest.Equal(t, wikidumptest.Location{Field: "public free text", Offset: k}, "no private name",
				"private name")
		}
	}
}

// TestBuild_NoRealLyricsAnywhere checks that no 5-word window and no whole line (of at least minLyricLine words)
// of any inline lyrics of the crawl is in any string of the real import or in its review, unless the crawl's text
// outside the lyrics has it too (a title sung in its song). The lyrics are found independently of the detector
// under test (wikidumptest.OracleLyrics) in the sections the reference analysis lists as holding inline text.
func TestBuild_NoRealLyricsAnywhere(t *testing.T) {
	c, d := researchImport(t)
	_, analysis := wikidumptest.Research(t)
	imp, review := Build(c, BuildOptions{Now: buildTime})
	encoded, err := json.Marshal(imp)
	wikidumptest.NoError(t, "encoding the import", err)
	var decoded any
	wikidumptest.NoError(t, "decoding the import", json.Unmarshal(encoded, &decoded))
	texts := append(jsonStrings(decoded, nil), reviewFields(RenderReview(review))...)
	imported := foldedTexts(texts)

	sections := wikidumptest.InlineTextSections(t, analysis)

	type pageLyrics struct {
		page   mediawiki.Page
		blocks [][]int
	}
	var all []pageLyrics
	var outsideTexts []string
	for _, p := range d.Pages {
		blocks := wikidumptest.OracleLyrics(p.Wikitext, sections[p.PageID])
		all = append(all, pageLyrics{page: p, blocks: blocks})
		outsideTexts = append(append(outsideTexts, wikidumptest.OutsideSpans(p.Wikitext, blocks)...), p.Title)
	}
	outside := foldedTexts(outsideTexts)

	found, lyricPages := 0, 0
	for _, pl := range all {
		if len(pl.blocks) > 0 && len(sections[pl.page.PageID]) > 0 {
			lyricPages++
		}
		for _, block := range pl.blocks {
			found++
			at := func(field string) wikidumptest.Location {
				return wikidumptest.Location{PageID: pl.page.PageID, Field: field, Offset: block[0]}
			}
			body := pl.page.Wikitext[block[0]:block[1]]
			words := foldedWords(body)
			for k := 0; k+lyricWindow <= len(words); k++ {
				gram := " " + strings.Join(words[k:k+lyricWindow], " ") + " "
				if !strings.Contains(outside, gram) {
					wikidumptest.Equal(t, at("lyric window"), false, strings.Contains(imported, gram))
				}
			}
			for _, line := range strings.Split(body, "\n") {
				lw := foldedWords(line)
				if len(lw) < minLyricLine {
					continue
				}
				whole := " " + strings.Join(lw, " ") + " "
				if !strings.Contains(outside, whole) {
					wikidumptest.Equal(t, at("lyric line"), false, strings.Contains(imported, whole))
				}
			}
		}
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "lyrics blocks found", Offset: -1}, true, found > 0)
	wikidumptest.Equal(t, wikidumptest.Location{Field: "reference inline-text pages with lyrics", Offset: -1},
		researchCounts.InlineLyrics, lyricPages)
}

// jsonStrings appends every string of a decoded JSON value to out.
func jsonStrings(v any, out []string) []string {
	switch v := v.(type) {
	case string:
		out = append(out, v)
	case []any:
		for _, e := range v {
			out = jsonStrings(e, out)
		}
	case map[string]any:
		for _, e := range v {
			out = jsonStrings(e, out)
		}
	}

	return out
}

// foldedWords are the folded words of text, as the name matcher reads them.
func foldedWords(text string) []string {
	var out []string
	for _, w := range mediawiki.Words(text) {
		out = append(out, w.Folded)
	}

	return out
}

// foldedTexts joins the folded words of each text, each text between " | " separators, so that a word sequence
// " a b c " is found only within one text.
func foldedTexts(texts []string) string {
	var b strings.Builder
	for _, s := range texts {
		b.WriteString(" | ")
		b.WriteString(strings.Join(foldedWords(s), " "))
	}
	b.WriteString(" | ")

	return b.String()
}

// reviewFields are the review's lines cut at the punctuation that separates an item's fields (its page in
// brackets, quoted values, "kind:"), so that a word sequence is found only within one field: a page title followed
// by an item kind is not a sentence of the page.
func reviewFields(review []byte) []string {
	return strings.FieldsFunc(string(review), func(r rune) bool {
		return r == '\n' || r == '[' || r == ']' || r == '"' || r == ':'
	})
}
