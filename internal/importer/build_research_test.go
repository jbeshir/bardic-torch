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
	imp, _ := Build(c, BuildOptions{Now: buildTime})

	wikidumptest.Equal(t, wikidumptest.Location{Field: "import counts", Offset: -1}, researchCounts,
		importCounts(c, imp))
	wikidumptest.Equal(t, wikidumptest.Location{Field: "import validates", Offset: -1}, true, imp.Validate() == nil)
}

// TestBuild_NoPrivateNameInPublicFieldsOnRealDump scans the public fields of the real import, in clear, for every
// private name of the crawl (the scrubber's list, in every form) and every hidden alias, and for email addresses.
func TestBuild_NoPrivateNameInPublicFieldsOnRealDump(t *testing.T) {
	c, _ := researchImport(t)
	imp, _ := Build(c, BuildOptions{Now: buildTime})
	names := append([]mediawiki.PrivateName(nil), c.PrivateNames...)
	for _, b := range c.People.Bards {
		for _, a := range b.Aliases {
			if !a.IsPublic {
				names = append(names, mediawiki.PrivateName{Text: a.Name, Legal: true})
			}
		}
	}
	wikidumptest.Equal(t, wikidumptest.Location{Field: "private names found", Offset: -1}, true, len(names) > 0)

	public := publicJSON(t, imp)
	for _, m := range mediawiki.NewPrivateNameMatcher(names).Find(public) {
		wikidumptest.Equal(t, wikidumptest.Location{Field: "public import", Offset: m.Start}, "no private name",
			"private name")
	}
	for _, m := range mediawiki.EmailAddress.FindAllStringIndex(public, -1) {
		wikidumptest.Equal(t, wikidumptest.Location{Field: "public import", Offset: m[0]}, "no email", "email")
	}
}

// TestBuild_NoRealLyricsAnywhere checks that no 5-word window and no whole line (of at least minLyricLine words)
// of any inline lyrics block of the crawl is in any string of the real import or in its review, unless the
// crawl's text outside the lyrics has it too (a title sung in its song).
func TestBuild_NoRealLyricsAnywhere(t *testing.T) {
	c, d := researchImport(t)
	imp, review := Build(c, BuildOptions{Now: buildTime})
	encoded, err := json.Marshal(imp)
	wikidumptest.NoError(t, "encoding the import", err)
	var decoded any
	wikidumptest.NoError(t, "decoding the import", json.Unmarshal(encoded, &decoded))
	texts := append(jsonStrings(decoded, nil), reviewFields(RenderReview(review))...)
	imported := foldedTexts(texts)

	var outsideTexts []string
	for _, p := range d.Pages {
		outsideTexts = append(outsideTexts, mediawiki.WithoutInlineLyrics(p.Wikitext,
			mediawiki.InlineLyricsBlocks(p.Wikitext).Blocks), p.Title)
	}
	outside := foldedTexts(outsideTexts)

	blocks := 0
	for _, p := range d.Pages {
		for _, block := range mediawiki.InlineLyricsBlocks(p.Wikitext).Blocks {
			blocks++
			at := func(field string) wikidumptest.Location {
				return wikidumptest.Location{PageID: p.PageID, Field: field, Offset: block.Start}
			}
			body := p.Wikitext[block.BodyStart:block.BodyEnd]
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
	wikidumptest.Equal(t, wikidumptest.Location{Field: "lyrics blocks found", Offset: -1}, true, blocks > 0)
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
