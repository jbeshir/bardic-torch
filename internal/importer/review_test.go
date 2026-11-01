package importer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

func TestRenderReview_SectionsInOrder(t *testing.T) {
	out := string(RenderReview(Review{}))

	assert.True(t, strings.HasPrefix(out, "# Import review\n"))
	assert.Contains(t, out, "John and Fabian")
	assert.Contains(t, out, "Private: do not publish or commit this file.")
	last := strings.Index(out, "## Counts")
	require.GreaterOrEqual(t, last, 0)
	r := Review{}
	for k, s := range r.sections() {
		heading := fmt.Sprintf("\n## %d. %s\n", k+1, s.title)
		at := strings.Index(out, heading)
		require.Greater(t, at, last, "section %q is present and in order", s.title)
		last = at
	}
	assert.Equal(t, len(r.sections()), strings.Count(out, "\nNone.\n"), "an empty section says so")
	for _, title := range []string{"Hidden names withheld from public text", "Contact details withheld"} {
		assert.Contains(t, out, ". "+title+"\n")
	}
}

func TestRenderReview_Deterministic(t *testing.T) {
	c := scrubbedCorpus(t)
	_, review := Build(c, BuildOptions{Now: buildTime})
	_, again := Build(c, BuildOptions{Now: buildTime})

	assert.Equal(t, RenderReview(review), RenderReview(again))
	ids := map[string]bool{}
	id := regexp.MustCompile(`^[A-Z]+-\d{3}$`)
	for _, it := range review.Items() {
		assert.Regexp(t, id, it.ID)
		assert.False(t, ids[it.ID], "%s is unique", it.ID)
		ids[it.ID] = true
	}
}

func TestRenderReview_NeverContainsLyrics(t *testing.T) {
	const marker = "quietheronlyric"
	d := syntheticDump()
	poem := "\n== Lyrics ==\n<poem>\n" + strings.Repeat(marker+" sings beneath a borrowed moon\n", 6) + "</poem>\n"
	d.Pages[2].Wikitext += poem
	d.Pages[0].Wikitext += "\n== Notes ==\n<poem style=\"x\">\n" + marker + " in a bard's notes\n</poem>\n"

	imp, review := Build(mediawiki.ParseCorpus(d), BuildOptions{Now: buildTime})

	encoded, err := json.Marshal(imp)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), marker)
	assert.NotContains(t, string(RenderReview(review)), marker)
	assert.True(t, pieceTitled(t, imp, songTitle).LegacyHadInlineLyrics)
	require.NotEmpty(t, review.Lyrics)
	assert.Contains(t, review.Lyrics[0].Text, "6 line(s)", "the review gives the count, never the text")
}

func TestReview_ListsEveryAmbiguity(t *testing.T) {
	_, review := scrubbedImport(t)

	var texts []string
	for _, it := range review.Ambiguities {
		texts = append(texts, it.Page+" "+it.Text)
	}
	all := strings.Join(texts, "\n")
	for _, want := range []string{"Fight for the Midrealm", "My Thing Is My Own", "Marian of Heatherdale",
		"Samantha Moore"} {
		assert.Contains(t, all, want, "PLAN §2.9 ambiguity")
	}
}

func TestReview_ListsEveryAliasClassification(t *testing.T) {
	imp, review := scrubbedImport(t)

	aliases := 0
	for _, p := range imp.Persons {
		aliases += len(p.Doc.Aliases)
	}
	assert.Len(t, review.Aliases, aliases)
	seenSure := false
	for _, it := range review.Aliases {
		sure := strings.HasSuffix(it.Text, "confident).")
		assert.False(t, seenSure && !sure, "unsure classifications come first: %s", it.ID)
		seenSure = seenSure || sure
	}
}

func TestPersonNameShaped(t *testing.T) {
	for u, want := range map[string]bool{"Wren Placeholder": true, "Wren Q Placeholder": true, "Wren": false,
		"WrenP": false, "Wiki User 07": false, "wren placeholder": false, "Wren_Placeholder": false} {
		assert.Equal(t, want, personNameShaped(u), u)
	}
}

func TestReview_FlagsNameShapedContributors(t *testing.T) {
	d := syntheticDump()
	d.Activity = append(d.Activity, mediawiki.PageActivity{Title: songTitle, NS: mediawiki.NSMain, Revisions: 1,
		Users: map[string]int{"Hazel Placeholder": 1, "HazelP": 1}})

	_, review := Build(mediawiki.ParseCorpus(d), BuildOptions{Now: buildTime})

	flagged := map[string]bool{}
	for _, it := range review.Contributors {
		flagged[strings.SplitN(it.Text, "\"", 3)[1]] = strings.Contains(it.Text, "shaped like a personal name")
	}
	assert.True(t, flagged["Hazel Placeholder"])
	assert.False(t, flagged["HazelP"])
}
