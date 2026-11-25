package domain

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// allowedImports is the dependency rule of the hexagon: the domain may only
// use these standard-library packages (no I/O, no network, no frameworks,
// no other layer of this module).
var allowedImports = map[string]bool{
	"errors": true, "fmt": true, "math": true, "math/bits": true,
	"slices": true, "strings": true, "time": true,
}

func TestDomainDependencyRule(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f) //nolint:gosec // reading this package's own sources
		if err != nil {
			t.Fatal(err)
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if !allowedImports[p] {
				t.Errorf("%s imports %q: forbidden in the domain", f, p)
			}
		}
		checked++
	}
	if checked < 5 {
		t.Fatalf("only %d files checked; glob broken?", checked)
	}
}
