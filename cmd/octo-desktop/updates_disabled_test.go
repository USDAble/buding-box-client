package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// The portable directory is this product's shipped form (需求 §5.1.1), so
// 需求 §5.1.2 第 13 条 closes the automatic update check and the tray's update
// entry, and PQ12 fixes the scope at "P0 does no updates at all" — whose reason
// is the write-back, not the download: the program directory may be a read-only
// U disk or the running image, and a write that cannot be rolled back destroys
// the user's portable directory.
//
// Four claims, each one a way the requirement could quietly come undone:
//
//	1. the decision itself — productUpdatesEnabled is false;
//	2. no update surface sits outside a productUpdatesEnabled guard, so adding
//	   the call back (or un-guarding it) cannot pass review by accident;
//	3. the outbound lookup is not re-enabled through a second literal —
//	   server.Config.UpdateCheck reads the same flag;
//	4. upstream's machinery is still in the tree. 硬规则 3 prefers unreachable
//	   over deleted, so "someone deleted it" must fail here rather than look
//	   like a clean fix — a deletion is a delete-vs-modify conflict on the next
//	   upstream merge.
//
// A behavioural test cannot see any of this: every surface lives inside main()
// or buildTrayMenu, both of which need a running Wails app. So this reads the
// source, the way internal/productruntime's TestOnlyOnePlaceFetchesTheCatalog
// reads it to catch a third call site — the failure mode is the same one, a
// path that appears later.

// updateSurfaces are the upstream entry points that must not be reachable
// without the guard. Each is named where it is called in main.go, never where
// it is declared: a declaration outside a guard is exactly right.
var updateSurfaces = []string{
	"autoUpdateLoop",     // the delayed-first + daily check goroutine
	"initInplaceUpdater", // configuring app.Updater = the write-back path
	"canInplaceUpdate",   // its precondition
	"trayUpdateAvailFmt", // tray "Update to vX…"
	"trayCheckUpdates",   // tray "Check for updates…"
}

// upstreamUpdateDecls are the upstream functions claim 4 requires to still
// exist. Deleting them is the one "fix" 硬规则 3 rules out.
var upstreamUpdateDecls = map[string][]string{
	"update.go": {"canInplaceUpdate", "initInplaceUpdater", "startUpdateFlow"},
	"main.go":   {"autoUpdateLoop"},
}

func TestThePortableFormOffersNoUpdates(t *testing.T) {
	// 1. The decision.
	if productUpdatesEnabled {
		t.Errorf("productUpdatesEnabled = true: this product form is the portable " +
			"directory (需求 §5.1.1) and 需求 §5.1.2 第 13 条 closes its updates; " +
			"PQ12 fixed the scope at \"P0 does no updates at all\" (see 需求基线 V-86)")
	}

	fset := token.NewFileSet()
	mainSrc, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	guards := guardSpans(fset, mainSrc)
	// A vacuous pass is the failure this whole file exists to avoid: with no
	// guard found, claim 2 would hold because nothing was ever checked.
	if len(guards) == 0 && anySurfaceReferenced(t, fset, mainSrc) {
		t.Fatalf("main.go calls an update surface but carries no `if productUpdatesEnabled` guard — " +
			"the reachability check below would pass vacuously")
	}

	// 2. Every occurrence of a surface is inside a guard. A declaration is not a
	//    call site: a function defined outside a guard and never called is
	//    exactly the unreachable shape 硬规则 3 asks for.
	declNames := declaredNamePositions(mainSrc)
	claimed := map[string]bool{}
	ast.Inspect(mainSrc, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if declNames[id.Pos()] {
			return true
		}
		for _, name := range updateSurfaces {
			if id.Name != name {
				continue
			}
			claimed[name] = true
			line := fset.Position(id.Pos()).Line
			if !within(guards, line) {
				t.Errorf("main.go:%d: %s is reachable without the productUpdatesEnabled guard — "+
					"this product form does not offer updates (需求 §5.1.2 第 13 条; 需求基线 V-86)",
					line, name)
			}
		}
		return true
	})

	// 3. One owner for the outbound lookup. A second literal here would let
	//    someone re-enable the network check without touching the flag.
	if got := configValue(t, fset, mainSrc, "UpdateCheck"); got != "productUpdatesEnabled" {
		t.Errorf("server.Config.UpdateCheck = %s, want productUpdatesEnabled: it is the field that "+
			"turns GET /api/version into an outbound latest-release lookup, and a second literal "+
			"next to the flag is how that check comes back (需求 §5.1.2 第 13 条)", got)
	}

	// 4. Upstream's code is still here.
	for file, names := range upstreamUpdateDecls {
		declared := declaredFuncs(t, fset, file)
		for _, name := range names {
			if !declared[name] {
				t.Errorf("%s no longer declares %s: upstream's update code must stay in the tree and "+
					"stay unreachable, because a deletion conflicts on every upstream merge (硬规则 3)",
					file, name)
			}
		}
	}
}

// declaredNamePositions returns the positions of package-level function and
// method names, so the reachability scan can tell a declaration from a call.
func declaredNamePositions(file *ast.File) map[token.Pos]bool {
	out := map[token.Pos]bool{}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name != nil {
			out[fn.Name.Pos()] = true
		}
	}
	return out
}

// guardSpans returns the inclusive line range of every `if productUpdatesEnabled
// { ... }` body in the file.
func guardSpans(fset *token.FileSet, file *ast.File) [][2]int {
	var spans [][2]int
	ast.Inspect(file, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		cond, ok := ifStmt.Cond.(*ast.Ident)
		if !ok || cond.Name != "productUpdatesEnabled" {
			return true
		}
		spans = append(spans, [2]int{
			fset.Position(ifStmt.Body.Lbrace).Line,
			fset.Position(ifStmt.Body.Rbrace).Line,
		})
		return true
	})
	return spans
}

func within(spans [][2]int, line int) bool {
	for _, s := range spans {
		if line >= s[0] && line <= s[1] {
			return true
		}
	}
	return false
}

func anySurfaceReferenced(t *testing.T, fset *token.FileSet, file *ast.File) bool {
	t.Helper()
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			for _, name := range updateSurfaces {
				if id.Name == name {
					found = true
				}
			}
		}
		return true
	})
	return found
}

// configValue returns the rendered value expression assigned to key in the
// file's server.Config composite literal, or "" — including a composite literal
// is not rendered, because only a plain identifier is accepted here.
func configValue(t *testing.T, fset *token.FileSet, file *ast.File, key string) string {
	t.Helper()
	var got string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Config" {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == key {
				if val, ok := kv.Value.(*ast.Ident); ok {
					got = val.Name
				} else {
					got = "<not a plain identifier>"
				}
			}
		}
		return true
	})
	if got == "" {
		t.Fatalf("no %s key found in a server.Config literal — the check would pass vacuously", key)
	}
	return got
}

func declaredFuncs(t *testing.T, fset *token.FileSet, file string) map[string]bool {
	t.Helper()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	out := map[string]bool{}
	for _, decl := range parsed.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
			out[fn.Name.Name] = true
		}
	}
	return out
}

// TestTheUpdateSurfacesStillExist is the other half of claim 4: it fails when a
// surface is renamed out of existence, so the reachability check above cannot be
// satisfied by deleting the thing it watches. Kept separate from the main test
// so a rename reads as its own failure rather than as a guard that vanished.
func TestTheUpdateSurfacesStillExist(t *testing.T) {
	fset := token.NewFileSet()
	all := map[string]bool{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		for fn := range declaredFuncs(t, fset, name) {
			all[fn] = true
		}
	}
	for _, name := range []string{"autoUpdateLoop", "initInplaceUpdater", "startUpdateFlow", "canInplaceUpdate", "runUpdateCheck", "checkForUpdates"} {
		if !all[name] {
			t.Errorf("the package no longer declares %s: upstream's update code is kept and made "+
				"unreachable, not deleted (硬规则 3) — updateSurfaces watches call sites, so deleting "+
				"the callee would hide the surface from it", name)
		}
	}
}
