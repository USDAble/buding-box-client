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

// errorCodeRegistry is the single owner of the wire error codes this client
// speaks (开发规范 §3.8). Every Code* constant this package sends or matches must
// appear there.
//
// It used to be `dev-docs-usdable/需求/20260911/中台交付包.md` §3.2, and this test
// read that document. That document was deleted on 2026-09-15 (人工拍板): a
// design-first contract was generating requirements the code had outgrown, so the
// interface contract is now regenerated from the code rather than ahead of it.
// The registry itself could not go with it — it is a superset of what this
// package implements, and that superset exists nowhere in Go. See the file's own
// header for the full reasoning.
const errorCodeRegistry = "testdata/wire-error-codes.txt"

// TestEveryCodeIsRegistered keeps the error-code registry and this package's
// constant table from drifting apart. `testdata/wire-error-codes.txt` is the
// single owner of the codes (开发规范 §3.8); this test is the guard that the
// contract document claimed to already have (需求基线 V-19).
//
// It discovers the constants by parsing the package source rather than listing
// them here: a hand-maintained list would be a second copy of the same fact,
// and a newly added constant would then escape the guard unnoticed - which is
// exactly the drift this test exists to catch.
//
// Direction is forward only: a code this package knows must be registered.
// The reverse does not hold yet, by design - the registry is the whole
// platform surface and the client implements a subset of it (e.g.
// `safety_blocked`, `account_restricted` arrive with later work), so
// "registered but not implemented" is an expected state rather than drift.
func TestEveryCodeIsRegistered(t *testing.T) {
	registry, err := registeredCodes(errorCodeRegistry)
	if err != nil {
		t.Fatalf("read the error-code registry: %v", err)
	}

	codes, err := packageCodeConstants()
	if err != nil {
		t.Fatalf("collect Code constants: %v", err)
	}
	// Without this the guard would pass vacuously the moment the constants
	// move files or stop being plain string literals.
	if len(codes) == 0 {
		t.Fatal("found no Code constants to check - the guard is vacuous")
	}
	// Likewise, an unparsable or emptied registry would make every lookup miss
	// and every constant fail; a missing file would be caught above, but a file
	// that lost its body would not.
	if len(registry) == 0 {
		t.Fatal("the registry listed no codes - the guard is vacuous")
	}

	for _, constant := range codes {
		if !registry[constant.value] {
			t.Errorf("%s = %q is not registered in %s; "+
				"that file is the single owner of the wire codes "+
				"(开发规范 §3.8, V-19)",
				constant.name, constant.value, errorCodeRegistry)
		}
	}
}

// registeredCodes parses the registry into a set of code values. Comments and
// blank lines are ignored, and each remaining line contributes its last
// whitespace-separated token: the format is `<http-status|local> <code>`, so the
// code is what follows the tag. Matching on a parsed set rather than on
// substrings keeps a code from being "registered" by a mention in prose - the
// substring form would let `code` match anywhere the word appears.
func registeredCodes(path string) (map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		out[fields[len(fields)-1]] = true
	}
	return out, nil
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
