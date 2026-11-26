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
	for _, name := range []string{"templates/setup.html", "templates/login.html", "templates/settings.html", "templates/inventory_list.html", "templates/inventory_detail.html", "templates/error.html"} {
		if _, err := Templates.ReadFile(name); err != nil {
			t.Fatalf("template %s not embedded: %v", name, err)
		}
	}
	for _, name := range []string{"static/app.css", "static/setup.js", "static/admin_inventory.js"} {
		if _, err := Static.ReadFile(name); err != nil {
			t.Fatalf("asset %s not embedded: %v", name, err)
		}
	}
}
