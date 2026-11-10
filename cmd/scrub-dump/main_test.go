package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
	"github.com/jbeshir/bardic-torch/internal/scrub"
)

func TestRun_WritesLyricScrubbedFiles(t *testing.T) {
	in, out := t.TempDir(), filepath.Join(t.TempDir(), "copy")
	page := `{"ns":0,"title":"Wren Placeholder","pageid":1,"categories":[],"hidden_categories":[],` +
		`"wikitext":"== Lyrics ==\n<poem>\nline one\nline two\n</poem>\n","url":"https://wiki.example/wiki/Wren",` +
		`"revid":5,"last_rev_timestamp":"2025-01-02T03:04:05Z","last_rev_user":"Fern Handle"}` + "\n"
	for name, content := range map[string]string{
		mediawiki.PagesFile:      page,
		mediawiki.CategoriesFile: `{"categories":{}}`,
		mediawiki.RedirectsFile:  `{"redirect_pages":[]}`,
		mediawiki.ActivityFile:   `{"per_page":[]}`,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(in, name), []byte(content), 0o600))
	}
	var stdout bytes.Buffer

	require.NoError(t, run([]string{"--in", in, "--out", out}, &stdout))

	d, err := mediawiki.LoadDump(os.DirFS(out))
	require.NoError(t, err)
	require.Len(t, d.Pages, 1)
	assert.Equal(t, "== Lyrics ==\n<poem>\n"+scrub.LyricPlaceholderLine+"\n"+scrub.LyricPlaceholderLine+"\n</poem>\n",
		d.Pages[0].Wikitext)
	assert.FileExists(t, filepath.Join(out, scrub.DenylistFile))
	assert.Contains(t, stdout.String(), "poem 1")
	require.ErrorIs(t, run(nil, &bytes.Buffer{}), ErrUsage)
}
