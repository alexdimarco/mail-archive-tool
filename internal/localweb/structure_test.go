package localweb

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
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

// pkgFunc is a package-level function and the name its file gives localweb.
type pkgFunc struct {
	decl     *ast.FuncDecl
	localweb string
}

// guardedExpr reports whether e builds its handler with localweb.Wrap: e is a
// localweb.Wrap(...) call, or a call to a package-level function of the same
// package every one of whose returns is (followed a few levels deep).
func guardedExpr(e ast.Expr, lw string, funcs map[string]pkgFunc, depth int) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || depth > 4 {
		return false
	}
	if isSel(call.Fun, lw, "Wrap") {
		return true
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	fn, ok := funcs[id.Name]
	if !ok || fn.decl.Body == nil {
		return false
	}
	returns := 0
	all := true
	ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
		switch r := n.(type) {
		case *ast.FuncLit:
			return false // a closure's returns are not the function's
		case *ast.ReturnStmt:
			returns++
			if len(r.Results) == 0 || !guardedExpr(r.Results[0], fn.localweb, funcs, depth+1) {
				all = false
			}
		}
		return true
	})
	return returns > 0 && all
}

// servedViolations checks every http.Server and Serve call in files for a
// localweb.Wrap handler, and returns what it found wrong and how many servers it saw.
func servedViolations(files []sourceFile) ([]string, int) {
	funcs := map[string]map[string]pkgFunc{} // package dir -> function name -> declaration
	for _, sf := range files {
		if funcs[sf.dir] == nil {
			funcs[sf.dir] = map[string]pkgFunc{}
		}
		for _, d := range sf.file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[sf.dir][fd.Name.Name] = pkgFunc{decl: fd, localweb: importName(sf.file, "mail-archive-tool/internal/localweb")}
			}
		}
	}
	serveArg := map[string]int{"Serve": 1, "ServeTLS": 1, "ListenAndServe": 1, "ListenAndServeTLS": 3}
	servers := 0
	var bad []string
	for _, sf := range files {
		httpName := importName(sf.file, "net/http")
		if httpName == "" {
			continue
		}
		at := func(n ast.Node) string { return sf.rel + ":" + strconv.Itoa(sf.fset.Position(n.Pos()).Line) }
		if httpName == "." {
			// No selector would ever name http.Server or http.Serve: refuse the
			// import rather than let the check pass over what it cannot see.
			bad = append(bad, sf.rel+": a dot-import of net/http hides every http.Server and http.Serve from this check")
			continue
		}
		lw := importName(sf.file, "mail-archive-tool/internal/localweb")
		check := func(n ast.Node, e ast.Expr, what string) {
			if !guardedExpr(e, lw, funcs[sf.dir], 0) {
				bad = append(bad, at(n)+": "+what+" is given "+types.ExprString(e)+", not a localweb.Wrap handler")
			}
		}
		ast.Inspect(sf.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.TypeSpec:
				if isSel(x.Type, httpName, "Server") {
					bad = append(bad, at(x)+": a type declared from http.Server hides its literals from this check")
				}
			case *ast.CompositeLit:
				if !isSel(x.Type, httpName, "Server") {
					return true
				}
				servers++
				var handler ast.Expr
				noOptions := false
				for _, el := range x.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Handler" {
							handler = kv.Value
						}
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "DisableGeneralOptionsHandler" {
							if v, ok := kv.Value.(*ast.Ident); ok && v.Name == "true" {
								noOptions = true
							}
						}
					}
				}
				if !noOptions {
					// net/http answers `OPTIONS *` itself, before Handler runs, so the
					// guard would see neither the Host nor the headers (AC8).
					bad = append(bad, at(x)+": an http.Server without DisableGeneralOptionsHandler: true answers OPTIONS * past the guard")
				}
				if handler == nil {
					bad = append(bad, at(x)+": an http.Server with no Handler serves the DefaultServeMux")
				} else {
					check(x, handler, "http.Server{Handler: ...}")
				}
			case *ast.AssignStmt:
				for i, lhs := range x.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Handler" && i < len(x.Rhs) {
						check(x, x.Rhs[i], types.ExprString(lhs))
					}
				}
			case *ast.CallExpr:
				for name, arg := range serveArg {
					if isSel(x.Fun, httpName, name) {
						servers++
						if len(x.Args) <= arg {
							bad = append(bad, at(x)+": http."+name+" without a handler serves the DefaultServeMux")
						} else {
							check(x, x.Args[arg], "http."+name)
						}
					}
				}
			}
			return true
		})
	}
	sort.Strings(bad)
	return bad, servers
}

// covers: MA-378, R23, R19
// Every handler a production binary serves is built by localweb.Wrap [CC15]:
// each http.Server literal and Handler assignment in cmd/, and each
// http.Serve/ServeTLS/ListenAndServe/ListenAndServeTLS call there, is given
// localweb.Wrap(...) or a call to a package function whose every return is one.
// A server with no Handler would serve the DefaultServeMux, so it fails too.
func TestEveryServedHandlerIsGuarded(t *testing.T) {
	root := moduleRoot(t)
	files := assure.Reached(t, productionFiles(t, root, "cmd"), "production Go files under cmd/")
	bad, servers := servedViolations(files)
	assure.Reached(t, servers, "HTTP servers the binaries start")
	if len(bad) > 0 {
		t.Errorf("binaries serve handlers the local-surface guard does not wrap:\n  %s", strings.Join(bad, "\n  "))
	}
}

// parseSource parses one synthetic production file, for the checks' own tests.
func parseSource(t *testing.T, rel, src string) sourceFile {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return sourceFile{rel: rel, dir: filepath.ToSlash(filepath.Dir(rel)), fset: fset, file: f}
}

// covers: MA-378, R23, R19
// The structural checks fail closed on what they cannot see [CC15]: a dot-import
// of net/http (no selector would ever match), and a type declared from
// http.Server or http.ServeMux (an alias hides its literals). A healthy file is
// the positive twin. (Adversarial pass AI1, AO4.)
func TestStructuralChecksFailClosedOnHiddenHTTP(t *testing.T) {
	healthy := parseSource(t, "cmd/x/ok.go", `package x
import (
	"net/http"
	"mail-archive-tool/internal/localweb"
)
	func serve() { _ = &http.Server{Handler: localweb.Wrap(localweb.Surface{}, nil), DisableGeneralOptionsHandler: true} }
`)
	if bad, n := servedViolations([]sourceFile{healthy}); len(bad) != 0 || n != 1 {
		t.Fatalf("positive twin: violations %v, servers %d; want none and 1", bad, n)
	}
	if bad := routeViolations([]sourceFile{healthy}); len(bad) != 0 {
		t.Fatalf("positive twin: route violations %v", bad)
	}
	dot := parseSource(t, "cmd/x/dot.go", `package x
import . "net/http"
	func serve() { m := NewServeMux(); m.HandleFunc("/leak", nil); s := &Server{Handler: m}; _ = s }
`)
	alias := parseSource(t, "cmd/x/alias.go", `package x
import "net/http"
type S = http.Server
type M = http.ServeMux
`)
	refused := 0
	for name, sf := range map[string]sourceFile{"dot-import": dot, "alias": alias} {
		sv, _ := servedViolations([]sourceFile{sf})
		rv := routeViolations([]sourceFile{sf})
		refused += len(sv) + len(rv)
		if len(sv) == 0 {
			t.Errorf("%s: the served-handler check passed over a file it cannot read", name)
		}
		if len(rv) == 0 {
			t.Errorf("%s: the route-model check passed over a file it cannot read", name)
		}
	}
	assure.Reached(t, refused, "violations reported over files the checks cannot read")
}

// routeViolations lists every route-registering use of net/http in files outside
// internal/localweb.
func routeViolations(files []sourceFile) []string {
	var bad []string
	for _, sf := range files {
		if sf.dir == "internal/localweb" || strings.HasPrefix(sf.dir, "internal/localweb/") {
			continue
		}
		httpName := importName(sf.file, "net/http")
		if httpName == "" {
			continue
		}
		if httpName == "." {
			bad = append(bad, sf.rel+": a dot-import of net/http hides every http.ServeMux and http.Handle from this check")
			continue
		}
		named := map[token.Pos]bool{} // http.ServeMux types a literal or new() already reported
		ast.Inspect(sf.file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.TypeSpec:
				if isSel(x.Type, httpName, "ServeMux") {
					named[x.Type.Pos()] = true
					bad = append(bad, sf.rel+":"+strconv.Itoa(sf.fset.Position(x.Pos()).Line)+": a type declared from http.ServeMux")
				}
			case *ast.SelectorExpr:
				for _, name := range []string{"NewServeMux", "Handle", "HandleFunc", "DefaultServeMux"} {
					if isSel(x, httpName, name) {
						bad = append(bad, sf.rel+":"+strconv.Itoa(sf.fset.Position(x.Pos()).Line)+": http."+name)
					}
				}
				// Every other use of the type: a var, a field, a parameter, a
				// conversion. (A literal or new() is visited first, and named.)
				if isSel(x, httpName, "ServeMux") && !named[x.Pos()] {
					bad = append(bad, sf.rel+":"+strconv.Itoa(sf.fset.Position(x.Pos()).Line)+": http.ServeMux")
				}
			case *ast.CompositeLit:
				if isSel(x.Type, httpName, "ServeMux") {
					named[x.Type.Pos()] = true
					bad = append(bad, sf.rel+":"+strconv.Itoa(sf.fset.Position(x.Pos()).Line)+": http.ServeMux{}")
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 && isSel(x.Args[0], httpName, "ServeMux") {
					named[x.Args[0].Pos()] = true
					bad = append(bad, sf.rel+":"+strconv.Itoa(sf.fset.Position(x.Pos()).Line)+": new(http.ServeMux)")
				}
			}
			return true
		})
	}
	sort.Strings(bad)
	return bad
}

// covers: MA-378, R23, R19
// One route model [CC15]: no production code outside internal/localweb builds an
// http.ServeMux or registers on the DefaultServeMux, so every route a surface
// serves is registered through localweb.Mux and recorded — none can escape the
// probe of every registered pattern. Any use of the http.ServeMux type counts,
// not only NewServeMux, a literal or new(): a `var m http.ServeMux` (its zero
// value is a working mux), a field or a parameter of that type is a mux too.
func TestNoRouteOutsideTheRegistry(t *testing.T) {
	root := moduleRoot(t)
	files := assure.Reached(t, productionFiles(t, root, "cmd", "internal"), "production Go files under cmd/ and internal/")
	if bad := routeViolations(files); len(bad) > 0 {
		t.Errorf("routes registered outside localweb.Mux (register them on a localweb.NewMux() so the route probe sees them):\n  %s", strings.Join(bad, "\n  "))
	}
}
