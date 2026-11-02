package importer

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// update rewrites the golden files: go test ./internal/importer -run Golden -update.
var update = flag.Bool("update", false, "rewrite the golden files")

// goldenPages are the tricky pages snapshotted from the scrubbed copy: the pages PLAN §7 names and the others
// whose parse has a rule of its own.
var goldenPages = []string{
	"Three Words", "My Thing is My Own", "Fight for the Midrealm", "Band of Brothers", "Black Alman",
	"The Burning of Tara", "Samantha Moore", "Lasair inghen ui h’Airt", "Auld Lang Syne", "Malkin Grey",
	"Dahrien Cordell", "Greensleeves", "Greensleeves (SCA Version)", "Voice of the Dragon", "Birthday Dirge",
	"Warrior's Wyrd",
}

// goldenPage is a golden snapshot: the entity imported from the page (a piece or a person) and its review items.
type goldenPage struct {
	Piece  *domain.ImportedPiece  `json:"piece,omitempty"`
	Person *domain.ImportedPerson `json:"person,omitempty"`
	Review []Item                 `json:"review"`
}

// goldenFor finds the entity imported from the page titled title.
func goldenFor(imp domain.CatalogueImport, review Review, title string) (goldenPage, bool) {
	g := goldenPage{Review: review.ItemsAbout(title)}
	for k, p := range imp.Pieces {
		if p.Legacy != nil && p.Legacy.PageID != nil && p.Legacy.Title == title {
			g.Piece = &imp.Pieces[k]
		}
	}
	for k, p := range imp.Persons {
		if p.Legacy != nil && p.Legacy.PageID != nil && p.Legacy.Title == title {
			g.Person = &imp.Persons[k]
		}
	}

	return g, g.Piece != nil || g.Person != nil
}

// compareGolden compares got with the golden file at path, or rewrites it with -update.
func compareGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, got, 0o600))

		return
	}
	want, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err, "run the test with -update to write it")
	assert.Equal(t, string(want), string(got))
}

func TestGoldenPages(t *testing.T) {
	imp, review := scrubbedImport(t)

	for _, title := range goldenPages {
		t.Run(title, func(t *testing.T) {
			g, ok := goldenFor(imp, review, title)
			require.True(t, ok, "the page is imported")
			got, err := json.MarshalIndent(g, "", "  ")
			require.NoError(t, err)
			compareGolden(t, filepath.Join("testdata", "golden", domain.Slugify(title)+".json"), append(got, '\n'))
		})
	}
}

func TestReviewGolden(t *testing.T) {
	_, review := scrubbedImport(t)

	compareGolden(t, filepath.Join("testdata", "import-review.golden.md"), RenderReview(review))
}
