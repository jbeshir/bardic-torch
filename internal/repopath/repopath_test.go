package repopath

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRepository is a directory whose go.mod declares ModulePath.
func fakeRepository(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module "+ModulePath+"\n\ngo 1.27\n"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(repo, "internal"), 0o750))

	return repo
}

func TestInside(t *testing.T) {
	t.Parallel()

	repo := fakeRepository(t)
	other := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(other, "go.mod"), []byte("module example.org/other\n"), 0o600))
	links := t.TempDir()
	symlink(t, filepath.Join(repo, "internal", "leak.md"), filepath.Join(links, "dangling.md"))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "existing.md"), nil, 0o600))
	symlink(t, filepath.Join(repo, "existing.md"), filepath.Join(links, "existing.md"))
	symlink(t, filepath.Join(repo, "internal"), filepath.Join(links, "dir"))
	symlink(t, "dangling.md", filepath.Join(links, "chain.md"))
	symlink(t, filepath.Join(other, "review.md"), filepath.Join(links, "outside.md"))

	for path, want := range map[string]bool{
		filepath.Join(repo, "review.md"):                   true,
		filepath.Join(repo, "deep", "down", "x.md"):        true,
		filepath.Join(other, "review.md"):                  false,
		filepath.Join(t.TempDir(), "review.md"):            false,
		filepath.Join(repo, "..", "outside-review.md"):     false,
		filepath.Join(links, "dangling.md"):                true,
		filepath.Join(links, "existing.md"):                true,
		filepath.Join(links, "dir", "review.md"):           true,
		filepath.Join(links, "dir", "new", "deep", "x.md"): true,
		filepath.Join(links, "chain.md"):                   true,
		filepath.Join(links, "outside.md"):                 false,
	} {
		got, err := Inside(path)
		require.NoError(t, err)
		assert.Equal(t, want, got, path)
	}
}

func TestInside_LinkLoopIsAnError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	symlink(t, "b", filepath.Join(dir, "a"))
	symlink(t, "a", filepath.Join(dir, "b"))
	_, err := Inside(filepath.Join(dir, "a"))
	require.ErrorIs(t, err, ErrTooManyLinks)
}

func TestWritePrivate_ModeIs0600OverAnExistingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "review.md")
	require.NoError(t, os.WriteFile(path, []byte("old content, longer than the new"), 0o600))
	before, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(path, before.Mode()|0o044), "readable by everyone")

	require.NoError(t, WritePrivate(path, []byte("new")))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	content, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	assert.Equal(t, "new", string(content))
}

func TestWritePrivate_RefusesTheRepositoryThroughSymlinks(t *testing.T) {
	t.Parallel()

	repo := fakeRepository(t)
	links := t.TempDir()
	symlink(t, filepath.Join(repo, "internal", "leak.md"), filepath.Join(links, "review.md"))
	symlink(t, filepath.Join(repo, "internal"), filepath.Join(links, "dir"))

	for _, path := range []string{
		filepath.Join(repo, "review.md"),
		filepath.Join(links, "review.md"),
		filepath.Join(links, "dir", "review.md"),
	} {
		require.ErrorIs(t, WritePrivate(path, []byte("private")), ErrInsideRepository, path)
	}
	entries, err := os.ReadDir(filepath.Join(repo, "internal"))
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is written into the repository")
}

func TestWritePrivate_FollowsALinkOutsideTheRepository(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "review.md")
	link := filepath.Join(t.TempDir(), "review.md")
	symlink(t, target, link)

	require.NoError(t, WritePrivate(link, []byte("private")))

	content, err := os.ReadFile(filepath.Clean(target))
	require.NoError(t, err)
	assert.Equal(t, "private", string(content))
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	require.NoError(t, os.Symlink(target, link))
}
