package repopath

import (
	"os"
	"path/filepath"
	"syscall"
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

func TestWritePrivate_LeavesNoTemporaryFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, WritePrivate(filepath.Join(dir, "review.md"), []byte("private")))
	require.NoError(t, WritePrivate(filepath.Join(dir, "review.md"), []byte("again")))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "review.md", entries[0].Name())
}

// TestWritePrivate_RefusesAHardLinkIntoTheRepository: a file outside the repository that is a hard link to a file
// inside it is not written through.
func TestWritePrivate_RefusesAHardLinkIntoTheRepository(t *testing.T) {
	t.Parallel()

	repo := fakeRepository(t)
	tracked := filepath.Join(repo, "internal", "tracked.md")
	require.NoError(t, os.WriteFile(tracked, []byte("public"), 0o600))
	link := filepath.Join(t.TempDir(), "review.md")
	if err := os.Link(tracked, link); err != nil {
		t.Skipf("no hard links across these directories: %v", err)
	}

	require.ErrorIs(t, WritePrivate(link, []byte("private")), ErrNotPrivateFile)

	content, err := os.ReadFile(filepath.Clean(tracked))
	require.NoError(t, err)
	assert.Equal(t, "public", string(content))
}

func TestWritePrivate_RefusesAnythingButARegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(sub, 0o700))

	for _, path := range []string{fifo, sub} {
		require.ErrorIs(t, WritePrivate(path, []byte("private")), ErrNotPrivateFile, path)
	}
}

// TestCheckOpenedDirectory_DirectorySwapped: a directory opened for a write that is moved away and replaced, by
// another directory or by a symbolic link into the repository, is no longer the one the path resolved to.
func TestCheckOpenedDirectory_DirectorySwapped(t *testing.T) {
	t.Parallel()

	repo := fakeRepository(t)
	for name, swap := range map[string]func(path string){
		"directory": func(path string) { require.NoError(t, os.Mkdir(path, 0o700)) },
		"symlink":   func(path string) { symlink(t, filepath.Join(repo, "internal"), path) },
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "out")
			require.NoError(t, os.Mkdir(dir, 0o700))
			resolved, err := Resolve(filepath.Join(dir, "review.md"))
			require.NoError(t, err)
			root, err := os.OpenRoot(filepath.Dir(resolved))
			require.NoError(t, err)
			t.Cleanup(func() { _ = root.Close() })
			require.NoError(t, checkOpenedDirectory(root, resolved), "not swapped yet")

			require.NoError(t, os.Rename(dir, filepath.Join(parent, "moved")))
			swap(dir)

			require.ErrorIs(t, checkOpenedDirectory(root, resolved), ErrDirectoryChanged)
		})
	}
}

// TestCheckOpenedDirectory_SwappedBeforeOpen: a directory replaced by a symbolic link into the repository after
// the path was resolved, but before the directory was opened, opens the repository's directory; the path no
// longer resolves to itself.
func TestCheckOpenedDirectory_SwappedBeforeOpen(t *testing.T) {
	t.Parallel()

	repo := fakeRepository(t)
	parent := t.TempDir()
	dir := filepath.Join(parent, "out")
	require.NoError(t, os.Mkdir(dir, 0o700))
	resolved, err := Resolve(filepath.Join(dir, "review.md"))
	require.NoError(t, err)

	require.NoError(t, os.Rename(dir, filepath.Join(parent, "moved")))
	symlink(t, filepath.Join(repo, "internal"), dir)
	root, err := os.OpenRoot(filepath.Dir(resolved))
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	require.ErrorIs(t, checkOpenedDirectory(root, resolved), ErrDirectoryChanged)
}
