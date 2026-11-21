package web

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoRepositoryImports keeps the UI layer behind the REST/service
// boundary (Design.md L113-L120).
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
				t.Errorf("%s imports %s; web must not import repositories", name, path)
			}
		}
	}
}

func TestEmbeddedAssetsExist(t *testing.T) {
	if _, err := Templates.ReadFile("templates/placeholder.html"); err != nil {
		t.Fatalf("template not embedded: %v", err)
	}
	if _, err := Static.ReadFile("static/placeholder.css"); err != nil {
		t.Fatalf("static asset not embedded: %v", err)
	}
}
