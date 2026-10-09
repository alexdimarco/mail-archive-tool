package localweb

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// moduleRoot walks up from the package directory to the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = parent
	}
}

// sourceFile is one parsed production (non-test) Go file.
type sourceFile struct {
	rel  string // slash-separated, relative to the module root
	dir  string // its package directory, relative
	fset *token.FileSet
	file *ast.File
}

// productionFiles parses every non-test .go file under the module root's subdirs.
func productionFiles(t *testing.T, root string, subdirs ...string) []sourceFile {
	t.Helper()
	var out []sourceFile
	for _, sub := range subdirs {
		err := filepath.WalkDir(filepath.Join(root, sub), func(p string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			out = append(out, sourceFile{rel: rel, dir: filepath.ToSlash(filepath.Dir(rel)), fset: fset, file: f})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// importName is the name f gives the import path ("" when f does not import it).
func importName(f *ast.File, path string) string {
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return path[strings.LastIndex(path, "/")+1:]
	}
	return ""
}

// isSel reports whether e is the selector pkg.name.
func isSel(e ast.Expr, pkg, name string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok || pkg == "" {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == pkg && s.Sel.Name == name
}

// covers: MA-378, R23, R19
// One route model [CC15]: no production code outside internal/localweb builds an
// http.ServeMux or registers on the DefaultServeMux, so every route a surface
// serves is registered through localweb.Mux and recorded — none can escape the
// probe of every registered pattern.
func TestNoRouteOutsideTheRegistry(t *testing.T) {
	root := moduleRoot(t)
	files := assure.Reached(t, productionFiles(t, root, "cmd", "internal"), "production Go files under cmd/ and internal/")
	var bad []string
	for _, sf := range files {
		if sf.dir == "internal/localweb" || strings.HasPrefix(sf.dir, "internal/localweb/") {
			continue
		}
		httpName := importName(sf.file, "net/http")
		if httpName == "" {
			continue
		}
		ast.Inspect(sf.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				for _, name := range []string{"NewServeMux", "Handle", "HandleFunc", "DefaultServeMux"} {
					if isSel(x, httpName, name) {
						bad = append(bad, sf.rel+":"+strconv.Itoa(sf.fset.Position(x.Pos()).Line)+": http."+name)
					}
				}
			case *ast.CompositeLit:
				if isSel(x.Type, httpName, "ServeMux") {
					bad = append(bad, sf.rel+":"+strconv.Itoa(sf.fset.Position(x.Pos()).Line)+": http.ServeMux{}")
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 && isSel(x.Args[0], httpName, "ServeMux") {
					bad = append(bad, sf.rel+":"+strconv.Itoa(sf.fset.Position(x.Pos()).Line)+": new(http.ServeMux)")
				}
			}
			return true
		})
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("routes registered outside localweb.Mux (register them on a localweb.NewMux() so the route probe sees them):\n  %s", strings.Join(bad, "\n  "))
	}
}
