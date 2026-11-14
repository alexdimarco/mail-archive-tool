// Package reach holds the syntax checks assureblock's meta-test uses to find the test packages
// that must run in a throw-away home checked by internal/testhome (the B3 adversarial pass, round
// 3's H4, MA-1333). They read Go source; they look up no folder and read no environment variable
// themselves. A package of its own, apart from internal/testhome, so the meta-test that uses them
// does not itself depend on the settings folder's lookup.
package reach

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// TesthomePath is internal/testhome's import path, as a test file imports it.
const TesthomePath = "mail-archive-tool/internal/testhome"

// userFolderFunc says the os function name computes a per-user folder: the os package's User…Dir
// functions (the home, cache and config folders).
func userFolderFunc(name string) bool {
	return strings.HasPrefix(name, "User") && strings.HasSuffix(name, "Dir")
}

// userFolderVar says the environment variable names a per-user folder: the home folder's
// (Unix's, Windows' profile), Windows' roaming and local application data folders, and every
// XDG folder variable.
func userFolderVar(name string) bool {
	return name == "HOME" || name == "USERPROFILE" || strings.HasSuffix(name, "APPDATA") || strings.HasPrefix(name, "XDG_")
}

// ComputesUserFolder reports whether the Go source src calls one of the os package's per-user
// folder functions, or reads a per-user folder variable through os.Getenv or os.LookupEnv, under
// the name it imports "os" as. A comment or a string that names one does not count. Source that
// does not parse does not count either (the go command would refuse it).
func ComputesUserFolder(src []byte) bool {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	osName := importName(f, "os")
	if osName == "" {
		return false
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); !ok || x.Name != osName {
			return true
		}
		switch {
		case userFolderFunc(sel.Sel.Name):
			found = true
		case (sel.Sel.Name == "Getenv" || sel.Sel.Name == "LookupEnv") && len(call.Args) == 1:
			if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if v, err := strconv.Unquote(lit.Value); err == nil && userFolderVar(v) {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// CallsGuard reports whether the Go test source src has a TestMain whose body calls
// internal/testhome's Run or Check, under the name the file imports it as; inTesthome reads an
// unqualified call too, for that package's own tests. A comment, a string, a call outside
// TestMain, or a call on something else of that name does not count.
func CallsGuard(src []byte, inTesthome bool) bool {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	name := importName(f, TesthomePath)
	isGuard := func(sel string) bool { return sel == "Run" || sel == "Check" }
	found := false
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "TestMain" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if found {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				if x, ok := fun.X.(*ast.Ident); ok && name != "" && x.Name == name && isGuard(fun.Sel.Name) {
					found = true
				}
			case *ast.Ident:
				if inTesthome && isGuard(fun.Name) {
					found = true
				}
			}
			return !found
		})
	}
	return found
}

// importName is the name the file f imports path as ("" when it does not, or imports it as _ or
// with a dot).
func importName(f *ast.File, path string) string {
	for _, im := range f.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil || p != path {
			continue
		}
		if im.Name == nil {
			return path[strings.LastIndex(path, "/")+1:]
		}
		if im.Name.Name == "_" || im.Name.Name == "." {
			return ""
		}
		return im.Name.Name
	}
	return ""
}
