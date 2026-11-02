// Package repopath tells whether a path is inside this repository, which is public, and writes private files
// (lists naming real people) only outside it.
package repopath

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ModulePath is this repository's module; a directory whose go.mod declares it is inside the repository.
const ModulePath = "github.com/jbeshir/bardic-torch"

// maxLinks is how many symbolic links in a row Resolve follows, as the kernel's ELOOP limit.
const maxLinks = 40

var (
	// ErrInsideRepository is returned by WritePrivate for a path inside this repository, which is public.
	ErrInsideRepository = errors.New("a private file must be written outside the repository")
	// ErrTooManyLinks is returned for a path through more than maxLinks symbolic links in a row.
	ErrTooManyLinks = errors.New("too many symbolic links")
	// ErrNotPrivateFile is returned by WritePrivate for an existing file it must not replace: one that is not a
	// regular file, or has other hard links (one could be inside this repository).
	ErrNotPrivateFile = errors.New("a private file must replace only a regular file with no other hard links")
	// ErrDirectoryChanged is returned by WritePrivate when the directory it opened is no longer the one its path
	// resolved to (a component was swapped meanwhile).
	ErrDirectoryChanged = errors.New("the directory changed while the private file was written")
)

// Inside reports whether path, with its symbolic links resolved (Resolve), is under a directory whose go.mod
// declares ModulePath.
func Inside(path string) (bool, error) {
	resolved, err := Resolve(path)
	if err != nil {
		return false, err
	}

	return insideResolved(resolved), nil
}

func insideResolved(resolved string) bool {
	for dir := filepath.Dir(resolved); ; dir = filepath.Dir(dir) {
		if declaresModule(filepath.Join(dir, "go.mod")) {
			return true
		}
		if filepath.Dir(dir) == dir {
			return false
		}
	}
}

// Resolve is the absolute path that writing to path would write to: a symbolic link as the last element is
// followed, also when its target does not exist yet, and the symbolic links of the deepest existing directory are
// resolved.
func Resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for hops := 0; ; hops++ {
		info, err := os.Lstat(abs)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			break
		}
		if hops == maxLinks {
			return "", fmt.Errorf("resolving %s: %w", path, ErrTooManyLinks)
		}
		target, err := os.Readlink(abs)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(abs), target)
		}
		abs = filepath.Clean(target)
	}
	rest := ""
	for dir := abs; ; dir = filepath.Dir(dir) {
		if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolvedDir, rest), nil
		}
		if filepath.Dir(dir) == dir {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// WritePrivate writes content to path with mode 0600, also over an existing file with another mode. It refuses a
// path inside this repository (ErrInsideRepository) after resolving its symbolic links, and an existing file that
// is not regular or has other hard links (ErrNotPrivateFile). It opens the resolved directory once and checks that
// it is still the resolved, outside directory (ErrDirectoryChanged); every later step is relative to that open
// directory, so a component swapped since cannot redirect the write. The content goes to a new temporary file in
// the directory, which then replaces path: an existing file is never written through.
func WritePrivate(path string, content []byte) error {
	resolved, err := Resolve(path)
	if err != nil {
		return err
	}
	if insideResolved(resolved) {
		return ErrInsideRepository
	}
	dir, name := filepath.Split(resolved)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := checkOpenedDirectory(root, resolved); err != nil {
		return err
	}
	if err := checkReplaceable(root, name); err != nil {
		return err
	}
	temp := "." + name + "." + rand.Text() + ".tmp"
	if err := writeNew(root, temp, content); err != nil {
		_ = root.Remove(temp)

		return err
	}
	if err := root.Rename(temp, name); err != nil {
		_ = root.Remove(temp)

		return err
	}

	return nil
}

// checkOpenedDirectory checks that the opened root is the directory of resolved (a path Resolve returned), that
// the directory still resolves to itself and that resolved is still outside this repository.
func checkOpenedDirectory(root *os.Root, resolved string) error {
	dir := filepath.Dir(resolved)
	opened, err := root.Stat(".")
	if err != nil {
		return err
	}
	again, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	if again != dir || insideResolved(resolved) {
		return ErrDirectoryChanged
	}
	current, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !os.SameFile(opened, current) {
		return ErrDirectoryChanged
	}

	return nil
}

// checkReplaceable checks that name in root, if it exists, is a regular file with no other hard link. It is
// opened without following a symbolic link and without blocking on a FIFO.
func checkReplaceable(root *os.Root, name string) error {
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if errors.Is(err, syscall.ELOOP) {
		return ErrNotPrivateFile
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrNotPrivateFile
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink > 1 {
		return ErrNotPrivateFile
	}

	return nil
}

// writeNew writes content to a file name in root that must not exist yet, with mode 0600.
func writeNew(root *os.Root, name string, content []byte) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()

		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()

		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()

		return err
	}

	return f.Close()
}

func declaresModule(goMod string) bool {
	content, err := os.ReadFile(filepath.Clean(goMod))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(content), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "module" {
			return fields[1] == ModulePath
		}
	}

	return false
}
