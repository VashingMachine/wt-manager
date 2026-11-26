package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePrefix = "github.com/VashingMachine/wt-manager/internal/"

// allowedImports lists, for every package under internal/, the other internal
// packages its non-test code may import. "integrations/*" stands for any one
// integration. A package that is not listed fails the test: decide where a new
// package sits before using it.
//
//	tui, cli            the UIs; they see the domain and its ports only
//	core                the domain; depends on nothing
//	services            the application; reaches the outside through its ports
//	integrations/*      one outside system each; never each other
//	app                 the composition root; the only one that sees it all
var allowedImports = map[string][]string{
	"core":           {},
	"fspath":         {},
	"shell":          {},
	"cache":          {},
	"arch":           {},
	"testkit":        {"core"},
	"services":       {"core", "cache", "fspath", "shell"},
	"integrations/*": {"core", "fspath", "shell"},
	"tui":            {"core", "fspath"},
	"cli":            {"core", "fspath"},
	"app":            {"core", "cache", "services", "integrations/*"},
}

// The domain holds rules, not side effects: it may not run programs, touch
// files or talk to a network or database.
var forbiddenInCore = []string{"os", "os/exec", "io/ioutil", "net", "net/http", "database/sql"}

// internalPackages maps every package under internal/ to the imports of its
// non-test files.
func internalPackages(t *testing.T) map[string][]string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	packages := map[string][]string{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if _, ok := packages[name]; !ok {
			packages[name] = nil
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if !slices.Contains(packages[name], imported) {
				packages[name] = append(packages[name], imported)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return packages
}

func ruleFor(name string) ([]string, bool) {
	if allowed, ok := allowedImports[name]; ok {
		return allowed, true
	}
	if strings.HasPrefix(name, "integrations/") {
		return allowedImports["integrations/*"], true
	}
	return nil, false
}

func permitted(allowed []string, imported string) bool {
	for _, rule := range allowed {
		if rule == imported || (rule == "integrations/*" && strings.HasPrefix(imported, "integrations/")) {
			return true
		}
	}
	return false
}

// Assures that dependencies point inwards: the UIs and the application never
// import an integration, integrations never import each other, the domain
// imports nothing, and only the composition root knows every concrete package.
func TestPackagesOnlyImportWhatTheirLayerAllows(t *testing.T) {
	packages := internalPackages(t)
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		allowed, ok := ruleFor(name)
		if !ok {
			t.Errorf("internal/%s has no dependency rule; add it to allowedImports", name)
			continue
		}
		for _, imported := range packages[name] {
			if !strings.HasPrefix(imported, modulePrefix) {
				continue
			}
			target := strings.TrimPrefix(imported, modulePrefix)
			if !permitted(allowed, target) {
				t.Errorf("internal/%s imports internal/%s; it may only import %v", name, target, allowed)
			}
		}
	}
}

// Assures that the domain stays free of side effects, so its rules can be
// tested without files, processes or a network.
func TestCoreHasNoSideEffectImports(t *testing.T) {
	for _, imported := range internalPackages(t)["core"] {
		if slices.Contains(forbiddenInCore, imported) {
			t.Errorf("internal/core imports %q; code that reaches outside the process belongs in an integration", imported)
		}
	}
}

// Assures that every integration is wired into the application: one that
// exists but is not imported by the composition root is dead code.
func TestEveryIntegrationIsWiredInApp(t *testing.T) {
	packages := internalPackages(t)
	for name := range packages {
		if !strings.HasPrefix(name, "integrations/") {
			continue
		}
		if !slices.Contains(packages["app"], modulePrefix+name) {
			t.Errorf("internal/%s is not wired in internal/app", name)
		}
	}
}

// Assures that test fixtures never end up in the binary.
func TestOnlyTestsImportTestkit(t *testing.T) {
	for name, imports := range internalPackages(t) {
		if slices.Contains(imports, modulePrefix+"testkit") {
			t.Errorf("internal/%s imports internal/testkit from non-test code", name)
		}
	}
}
