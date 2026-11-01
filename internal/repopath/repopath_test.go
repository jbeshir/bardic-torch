package repopath

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInside(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module "+ModulePath+"\n\ngo 1.27\n"), 0o600))
	other := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(other, "go.mod"), []byte("module example.org/other\n"), 0o600))

	for path, want := range map[string]bool{
		filepath.Join(repo, "review.md"):               true,
		filepath.Join(repo, "deep", "down", "x.md"):    true,
		filepath.Join(other, "review.md"):              false,
		filepath.Join(t.TempDir(), "review.md"):        false,
		filepath.Join(repo, "..", "outside-review.md"): false,
	} {
		got, err := Inside(path)
		require.NoError(t, err)
		assert.Equal(t, want, got, path)
	}
}
