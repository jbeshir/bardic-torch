// Package repopath tells whether a path is inside this repository, which is public: commands writing files that
// name real people refuse such paths.
package repopath

import (
	"os"
	"path/filepath"
	"strings"
)

// ModulePath is this repository's module; a directory whose go.mod declares it is inside the repository.
const ModulePath = "github.com/jbeshir/bardic-torch"

// Inside reports whether path is under a directory whose go.mod declares ModulePath.
func Inside(path string) (bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	for dir := filepath.Dir(abs); ; dir = filepath.Dir(dir) {
		if declaresModule(filepath.Join(dir, "go.mod")) {
			return true, nil
		}
		if filepath.Dir(dir) == dir {
			return false, nil
		}
	}
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
