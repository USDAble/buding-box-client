package productclient_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// errorCodeRegistryDoc is the single owner of the central platform's wire
// error codes (需求基线 §3.8). Every Code* constant this package sends or
// matches must appear there.
const errorCodeRegistryDoc = "../../dev-docs-usdable/需求/20260911/中台交付包.md"

// TestEveryCodeIsRegisteredInTheDoc keeps the error-code registry and this
// package's constant table from drifting apart. 中台交付包.md §3.2 is the
// single owner of the codes (需求基线 §3.8); this test is the guard that
// document claimed to already have (需求基线 V-19).
//
// It discovers the constants by parsing the package source rather than listing
// them here: a hand-maintained list would be a second copy of the same fact,
// and a newly added constant would then escape the guard unnoticed - which is
// exactly the drift this test exists to catch.
//
// Direction is forward only: a code this package knows must be registered.
// The reverse does not hold yet, by design - the registry is the whole
// contract and the client implements a subset of it (e.g. `safety_blocked`,
// `account_restricted` arrive with later PRs), so "documented but not
// implemented" is an expected state rather than drift.
func TestEveryCodeIsRegisteredInTheDoc(t *testing.T) {
	raw, err := os.ReadFile(errorCodeRegistryDoc)
	if err != nil {
		t.Fatalf("read the error-code registry: %v", err)
	}
	registry := string(raw)

	codes, err := packageCodeConstants()
	if err != nil {
		t.Fatalf("collect Code constants: %v", err)
	}
	// Without this the guard would pass vacuously the moment the constants
	// move files or stop being plain string literals.
	if len(codes) == 0 {
		t.Fatal("found no Code constants to check - the guard is vacuous")
	}

	for _, constant := range codes {
		if !strings.Contains(registry, constant.value) {
			t.Errorf("%s = %q is not registered in %s (§3.2); "+
				"that document is the single owner of the wire codes "+
				"(需求基线 §3.8, V-19)",
				constant.name, constant.value, errorCodeRegistryDoc)
		}
	}
}

type codeConstant struct {
	name  string
	value string
}

// packageCodeConstants parses this package's non-test sources and returns every
// `const Code<Something> = "<value>"` declaration it finds.
func packageCodeConstants() ([]codeConstant, error) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	var found []codeConstant
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				values, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range values.Names {
					if !strings.HasPrefix(name.Name, "Code") || i >= len(values.Values) {
						continue
					}
					lit, ok := values.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						return nil, err
					}
					found = append(found, codeConstant{name: name.Name, value: value})
				}
			}
		}
	}
	return found, nil
}
