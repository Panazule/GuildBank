package api

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoRepositoryImports enforces the layering rule (Design.md L109-L120):
// the HTTP layer must reach data only through services.
func TestNoRepositoryImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(path, "github.com/Panazule/guildbank/internal/repository") {
				t.Errorf("%s imports %s; api must not import repositories", name, path)
			}
		}
	}
	if _, err := os.Stat("router.go"); err != nil {
		t.Fatalf("import guard ran in the wrong directory: %v", err)
	}
}
