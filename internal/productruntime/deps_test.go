package productruntime

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file has no build tag on purpose: it must run in the production-tagged
// test pass too, because the dependency rules it asserts are exactly what a
// release build must satisfy.
//
// It lives in productruntime because productruntime is the composition root and
// therefore the owner of the dependency-direction contract: whatever it composes
// is what the rules are about (开发计划 §2).

// productPackages are the packages this PR introduces, relative to this
// package's directory.
var productPackages = []string{
	"../productclient",
	"../productclient/gateway",
	"../productpolicy",
	"../credentialstore",
	".",
}

// bannedImports must never be imported by a product package.
//
//   - internal/server is the upstream HTTP/WS runtime. P0-01A exists to move
//     product logic *out* of it; a new import edge would undo that work and
//     recreate the merge conflict the whole exercise is about.
//   - internal/app is the single place provider clients are constructed. The
//     gateway *uses* it at assembly time (P0-01 §gateway 的复用形态), but a
//     contract package must not depend on it, or the composition ordering
//     becomes circular.
//   - internal/provider is where the wire formats live; reusing them is
//     mandatory, *wrapping* them from the contract layer is not.
var bannedImports = []struct{ suffix, why string }{
	{"internal/server", "product logic must not be added to the upstream server (P0-01A)"},
	{"internal/app", "provider construction belongs to the assembly point, not to the contract"},
	{"internal/provider", "wire formats are reused at the assembly point, not imported by the contract"},
}

// TestProductPackagesDoNotImportTheUpstreamRuntime walks the actual source
// rather than a dependency dump, so the failure names the file and the import.
func TestProductPackagesDoNotImportTheUpstreamRuntime(t *testing.T) {
	for _, pkg := range productPackages {
		for _, file := range packageFiles(t, pkg) {
			for _, imp := range importsOf(t, file) {
				for _, banned := range bannedImports {
					if strings.HasSuffix(imp, banned.suffix) || strings.Contains(imp, banned.suffix+"/") {
						t.Errorf("%s imports %s: %s", shortPath(file), imp, banned.why)
					}
				}
			}
		}
	}
}

// TestOnlyTheTransportLayerSpeaksHTTP pins the single most valuable boundary
// here: the model stream must reuse internal/provider, so a `net/http` import in
// the gateway package means someone is writing a second SSE client
// (scripts/reuse-guard.mjs fails on the same thing at CI level).
//
// productclient itself *is* allowed net/http — those are ordinary JSON endpoints
// with no upstream equivalent to reuse (P0-01 §2 spells out that this is not a
// contradiction).
func TestOnlyTheTransportLayerSpeaksHTTP(t *testing.T) {
	for _, file := range packageFiles(t, "../productclient/gateway") {
		for _, imp := range importsOf(t, file) {
			if imp == "net/http" || strings.HasPrefix(imp, "net/http/") {
				t.Errorf("%s imports %s: the gateway assembles the model stream from "+
					"internal/app + internal/provider instead of implementing its own transport",
					shortPath(file), imp)
			}
		}
	}
}

// TestProductPackagesRespectTheIntendedGraph expresses the dependency graph as
// an explicit allow-list rather than a graph search, so the intended shape is
// readable at a glance and an unintended new edge fails loudly:
//
//	productclient              -> (nothing product-side)   leaf
//	productclient/gateway      -> productclient
//	productpolicy              -> productclient
//	credentialstore            -> (nothing)                leaf
//	productruntime             -> all of the above
func TestProductPackagesRespectTheIntendedGraph(t *testing.T) {
	allowed := map[string][]string{
		"../productclient":         {},
		"../productclient/gateway": {"internal/productclient"},
		"../productpolicy":         {"internal/productclient"},
		"../credentialstore":       {},
		".": {
			"internal/productclient",
			"internal/productpolicy",
			"internal/credentialstore",
		},
	}

	for pkg, permitted := range allowed {
		for _, file := range packageFiles(t, pkg) {
			for _, imp := range importsOf(t, file) {
				// Only product-side edges are governed here; upstream
				// core (agent) and stdlib are out of scope for this rule.
				if !strings.Contains(imp, "internal/product") && !strings.Contains(imp, "internal/credentialstore") {
					continue
				}
				// The gateway mock is a test-only subpackage of an allowed
				// package; the prefix match below accepts it.
				if !hasAnyPrefix(imp, permitted) {
					t.Errorf("%s imports %s, which is not in its permitted set %v",
						shortPath(file), imp, permitted)
				}
			}
		}
	}
}

func hasAnyPrefix(imp string, permitted []string) bool {
	for _, p := range permitted {
		if strings.HasSuffix(imp, p) || strings.Contains(imp, p+"/") {
			return true
		}
	}
	return false
}

// packageFiles returns the non-test .go files of a package directory. Test files
// are excluded from the graph checks because a test may legitimately import a
// mock from a sibling package.
func packageFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	return out
}

func importsOf(t *testing.T, file string) []string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	var out []string
	for _, spec := range parsed.Imports {
		out = append(out, strings.Trim(spec.Path.Value, `"`))
	}
	return out
}

func shortPath(p string) string {
	return filepath.ToSlash(filepath.Clean(p))
}
