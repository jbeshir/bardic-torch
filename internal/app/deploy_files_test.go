package app

import (
	"bufio"
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repositoryRoot is the repository's root directory, which holds the deployment files.
func repositoryRoot() fs.FS {
	return os.DirFS(filepath.Join("..", ".."))
}

func TestEnvDistListsEveryEnvVar(t *testing.T) {
	t.Parallel()

	root := repositoryRoot()
	read := envVarsReadByCode(t, root)
	listed := envDistVars(t, root)

	// The scan must see names given as literals (DATABASE_URL), through os.Getenv (cmd/migrate) and through
	// constants (AUTH_DRIVERS); otherwise it is not finding the reads at all.
	assert.Subset(t, slices.Collect(maps.Keys(read)), []string{"DATABASE_URL", "AUTH_DRIVERS"})

	for name := range read {
		assert.Truef(t, listed[name], "%s is read by the code but missing from .env.dist", name)
	}
	for name := range listed {
		assert.Truef(t, read[name], "%s is in .env.dist but no code reads it", name)
	}
}

// envVarsReadByCode returns the names of the environment variables that the non-test Go code under cmd/ and
// internal/ reads through os.Getenv, os.LookupEnv or the MustGetEnvAs* helpers. internal/testutil is skipped:
// its variables configure tests, not the application.
func envVarsReadByCode(t *testing.T, root fs.FS) map[string]bool {
	t.Helper()

	read := map[string]bool{}
	for dir, files := range goPackages(t, root, "cmd", "internal") {
		constants := stringConstants(files)
		for _, file := range files {
			for _, decl := range file.Decls {
				collectEnvReads(t, dir, decl, constants, read)
			}
		}
	}

	return read
}

// goPackages parses the non-test Go files in the given directories, grouped by directory.
func goPackages(t *testing.T, root fs.FS, dirs ...string) map[string][]*ast.File {
	t.Helper()

	fset := token.NewFileSet()
	packages := map[string][]*ast.File{}
	for _, dir := range dirs {
		err := fs.WalkDir(root, dir, func(name string, entry fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case entry.IsDir() && name == "internal/testutil":
				return fs.SkipDir
			case entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go"):
				return nil
			}

			src, err := fs.ReadFile(root, name)
			if err != nil {
				return err
			}
			file, err := parser.ParseFile(fset, name, src, 0)
			if err != nil {
				return err
			}
			packages[path.Dir(name)] = append(packages[path.Dir(name)], file)
			return nil
		})
		require.NoError(t, err)
	}

	return packages
}

// stringConstants returns the package-level string constants declared in files, by name.
func stringConstants(files []*ast.File) map[string]string {
	constants := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok || len(valueSpec.Names) != len(valueSpec.Values) {
					continue
				}
				for i, name := range valueSpec.Names {
					if value, ok := stringLiteral(valueSpec.Values[i]); ok {
						constants[name.Name] = value
					}
				}
			}
		}
	}

	return constants
}

// collectEnvReads adds to read the variable named by every environment read in decl. A name passed in as a
// parameter of the enclosing function is skipped: that function is a helper, and its callers' reads are
// collected instead. Any other name the scan cannot resolve fails the test, so no read goes unchecked.
func collectEnvReads(t *testing.T, dir string, decl ast.Decl, constants map[string]string, read map[string]bool) {
	t.Helper()

	params := map[string]bool{}
	if fn, ok := decl.(*ast.FuncDecl); ok {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				params[name.Name] = true
			}
		}
	}

	ast.Inspect(decl, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isEnvRead(call.Fun) {
			return true
		}

		arg := call.Args[len(call.Args)-1]
		literal, isLiteral := stringLiteral(arg)
		ident, isIdent := arg.(*ast.Ident)
		switch {
		case isLiteral:
			read[literal] = true
		case isIdent && constants[ident.Name] != "":
			read[constants[ident.Name]] = true
		case isIdent && params[ident.Name]:
			// A helper passing on its caller's name; the caller's read is collected.
		default:
			t.Errorf("%s: cannot tell which environment variable %s reads", dir, types.ExprString(call))
		}
		return true
	})
}

// isEnvRead reports whether fun is os.Getenv, os.LookupEnv or one of the MustGetEnvAs* helpers, all of which
// take the variable name as their last argument.
func isEnvRead(fun ast.Expr) bool {
	switch fun := fun.(type) {
	case *ast.Ident:
		return strings.HasPrefix(fun.Name, "MustGetEnvAs")
	case *ast.SelectorExpr:
		if pkg, ok := fun.X.(*ast.Ident); ok && pkg.Name == "os" {
			return fun.Sel.Name == "Getenv" || fun.Sel.Name == "LookupEnv"
		}
		return strings.HasPrefix(fun.Sel.Name, "MustGetEnvAs")
	default:
		return false
	}
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

// envDistVars returns the names of the variables that .env.dist sets.
func envDistVars(t *testing.T, root fs.FS) map[string]bool {
	t.Helper()

	listed := map[string]bool{}
	for _, line := range nonCommentLines(t, root, ".env.dist") {
		name, _, ok := strings.Cut(line, "=")
		require.Truef(t, ok, ".env.dist line [%s] is not NAME=value", line)
		listed[name] = true
	}

	return listed
}

// nonCommentLines returns the trimmed lines of the named file, without blank lines and # comments.
func nonCommentLines(t *testing.T, root fs.FS, name string) []string {
	t.Helper()

	src, err := fs.ReadFile(root, name)
	require.NoError(t, err)

	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(src))
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	require.NoError(t, scanner.Err())

	return lines
}
