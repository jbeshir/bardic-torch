package scrub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const plantedLyric = "the quiet heron sings beneath a borrowed moon tonight"

func TestScan_FindsPlantedLyric(t *testing.T) {
	d := BuildDenylist([]string{plantedLyric}, nil)
	require.Equal(t, 2, d.Len(), "9 words are 2 windows of 8")

	for _, text := range []string{
		"… The QUIET  heron sings beneath a borrowed moon …",
		"the-quiet_heron.sings beneath a borrowed moon",
		"the quiet heron\nsings beneath\na borrowed moon",
		`{"wikitext":"the quiet heron\nsings beneath a\nborrowed moon"}`,
		"thé quiet héron sings beneath a borrowed moon",
	} {
		assert.NotEmpty(t, d.Scan(text), text)
	}
	text := "x the quiet heron sings beneath a borrowed moon y"
	assert.Equal(t, []Hit{{Start: 2, End: 2 + len("the quiet heron sings beneath a borrowed moon")}}, d.Scan(text))
	assert.Empty(t, d.Scan("the quiet heron sings beneath a borrowed sun"))
}

func TestBuildDenylist_ExcludesWindowsFoundOutsideTheLyrics(t *testing.T) {
	d := BuildDenylist([]string{plantedLyric}, []string{"Title: The Quiet Heron Sings Beneath A Borrowed Moon"})

	assert.Equal(t, 1, d.Len())
	assert.Empty(t, d.Scan("the quiet heron sings beneath a borrowed moon"))
	assert.NotEmpty(t, d.Scan("quiet heron sings beneath a borrowed moon tonight"))
}

func TestBuildDenylist_IgnoresShortBodies(t *testing.T) {
	assert.Zero(t, BuildDenylist([]string{"only seven words in this one line"}, nil).Len())
}

func TestDenylist_EncodeParse(t *testing.T) {
	d := BuildDenylist([]string{plantedLyric}, nil)
	file := d.Encode()

	assert.True(t, strings.HasPrefix(string(file), "# hmac-sha256/128 v1\n# window: 8 words\n"))
	assert.NotContains(t, string(file), "heron")
	parsed, err := ParseDenylist(file)
	require.NoError(t, err)
	assert.Equal(t, d, parsed)

	for _, bad := range []string{"", "# other\n", denylistHeader + "zz\n", denylistHeader + "abcd\n"} {
		_, err := ParseDenylist([]byte(bad))
		require.ErrorIs(t, err, ErrMalformedDenylist, bad)
	}
}

// TestRepositoryHasNoDenylistedText scans every text file of the repository, the scrubbed copy, tests, golden
// files and docs included, for the n-grams of the lyrics the scrubber dropped.
func TestRepositoryHasNoDenylistedText(t *testing.T) {
	file, err := os.ReadFile(filepath.Join(wikidumptest.RepositoryRoot(t), wikidumptest.ScrubbedDir, DenylistFile))
	require.NoError(t, err)
	d, err := ParseDenylist(file)
	require.NoError(t, err)
	require.Positive(t, d.Len())

	for _, f := range repositoryTextFiles(t) {
		for _, hit := range d.Scan(f.text) {
			t.Errorf("%s: deny-listed lyric n-gram at byte %d", f.path, hit.Start)
		}
	}
}
