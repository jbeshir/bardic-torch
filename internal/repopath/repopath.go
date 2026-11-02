// Package repopath tells whether a path is inside this repository, which is public, and writes private files
// (lists naming real people) only outside it.
package repopath

import (
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
// path inside this repository (ErrInsideRepository) after resolving its symbolic links, and writes to the resolved
// path without following a symbolic link put there since.
func WritePrivate(path string, content []byte) error {
	resolved, err := Resolve(path)
	if err != nil {
		return err
	}
	if insideResolved(resolved) {
		return ErrInsideRepository
	}
	f, err := os.OpenFile(filepath.Clean(resolved), os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
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
