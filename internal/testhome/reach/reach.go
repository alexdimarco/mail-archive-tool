// Package reach holds the syntax checks assureblock's meta-test uses to find the test packages
// that must run in a throw-away home checked by internal/testhome (the B3 adversarial pass, round
// 3's H4, MA-1333; round 4's J5, MA-1340). They read Go source; they look up no folder and read no
// environment variable themselves. A package of its own, apart from internal/testhome, so the
// meta-test that uses them does not itself depend on the settings folder's lookup.
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

// windowsPath is the import path of the Windows system calls whose known-folder and profile
// lookups name a per-user folder whatever the environment says.
const windowsPath = "golang.org/x/sys/windows"

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

// userLookupFunc says the os/user function name looks up an account, whose HomeDir is its home
// folder.
func userLookupFunc(name string) bool {
	return name == "Current" || name == "Lookup" || name == "LookupId"
}

// windowsFolderFunc says a function or method of the Windows system calls names a per-user folder:
// a known folder (the roaming and local application data folders among them) or a profile.
func windowsFolderFunc(name string) bool {
	return name == "KnownFolderPath" || name == "SHGetKnownFolderPath" || name == "GetUserProfileDirectory"
}

// expandsUserFolderVar says s, as os.ExpandEnv reads it, names a per-user folder variable as
// $NAME or ${NAME}.
func expandsUserFolderVar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '$' || i+1 >= len(s) {
			continue
		}
		rest := s[i+1:]
		var name string
		if rest[0] == '{' {
			end := strings.IndexByte(rest, '}')
			if end < 0 {
				continue
			}
			name = rest[1:end]
		} else {
			end := 0
			for end < len(rest) && (rest[end] == '_' || rest[end] >= '0' && rest[end] <= '9' || rest[end] >= 'A' && rest[end] <= 'Z' || rest[end] >= 'a' && rest[end] <= 'z') {
				end++
			}
			name = rest[:end]
		}
		if userFolderVar(name) {
			return true
		}
	}
	return false
}

// stringLit is the value of e when it is a string literal.
func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}

// namesUserFolderVar says some string literal of the file names a per-user folder variable, as a
// name or as $NAME: the file's constants and lists of variable names among them. A lookup whose
// argument is not a literal is read against it, so a constant or a list that names one counts.
func namesUserFolderVar(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		if e, ok := n.(ast.Expr); ok {
			if v, ok := stringLit(e); ok && (userFolderVar(v) || expandsUserFolderVar(v)) {
				found = true
			}
		}
		return !found
	})
	return found
}

// ComputesUserFolder reports whether the Go source src computes a per-user folder: it calls one of
// the os package's per-user folder functions; reads a per-user folder variable through os.Getenv,
// os.LookupEnv or syscall.Getenv, or expands one through os.ExpandEnv or os.Expand (named by a
// literal, or by a constant or a list the file names it in); looks up an account through os/user
// (its HomeDir); or asks Windows for a known folder or a profile. Each under the name the file
// imports its package as. A comment or a string that names one does not count. Source that does
// not parse does not count either (the go command would refuse it).
func ComputesUserFolder(src []byte) bool {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	osName, userName := importName(f, "os"), importName(f, "os/user")
	sysName, winName := importName(f, "syscall"), importName(f, windowsPath)
	if osName == "" && userName == "" && sysName == "" && winName == "" {
		return false
	}
	named := namesUserFolderVar(f)
	// arg says the call's first argument names a per-user folder variable: as a literal (expanded
	// when expand), or, when it is not a literal, through the file's own literals.
	arg := func(call *ast.CallExpr, expand bool) bool {
		if len(call.Args) == 0 {
			return false
		}
		v, ok := stringLit(call.Args[0])
		switch {
		case !ok:
			return named
		case expand:
			return expandsUserFolderVar(v)
		}
		return userFolderVar(v)
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
		name := sel.Sel.Name
		if winName != "" && windowsFolderFunc(name) {
			found = true // the package's function, or a method of its Token
			return false
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch x.Name {
		case osName:
			switch {
			case userFolderFunc(name):
				found = true
			case name == "Getenv" || name == "LookupEnv":
				found = arg(call, false)
			case name == "ExpandEnv" || name == "Expand":
				found = arg(call, true)
			}
		case userName:
			found = userLookupFunc(name)
		case sysName:
			found = name == "Getenv" && arg(call, false)
		}
		return !found
	})
	return found
}

// CallsGuard reports whether the Go test source src has a TestMain that calls internal/testhome's
// Run, under the name the file imports it as, before its tests run (inTesthome reads an
// unqualified Run too, for that package's own tests). The call counts only as a statement of
// TestMain's own body (the call, an assignment or declaration of its result, or an argument of the
// statement's call, as os.Exit(testhome.Run(root, m))), reached before any statement of the body
// that runs the tests (m.Run) or ends TestMain (a return, os.Exit or panic): never one in dead
// code, a branch, a loop or a closure (the B3 adversarial pass, round 4's J5). Check alone does not count: it
// reports only after the tests ran. A comment, a string, a call outside TestMain, or a call on
// something else of that name does not count either.
func CallsGuard(src []byte, inTesthome bool) bool {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	name, osName := importName(f, TesthomePath), importName(f, "os")
	isGuard := func(call *ast.CallExpr) bool {
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			x, ok := fun.X.(*ast.Ident)
			return ok && name != "" && x.Name == name && fun.Sel.Name == "Run"
		case *ast.Ident:
			return inTesthome && fun.Name == "Run"
		}
		return false
	}
	var direct func(e ast.Expr) bool
	direct = func(e ast.Expr) bool {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		if isGuard(call) {
			return true
		}
		for _, a := range call.Args {
			if direct(a) {
				return true
			}
		}
		return false
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "TestMain" || fn.Body == nil {
			continue
		}
		m := ""
		if ps := fn.Type.Params.List; len(ps) == 1 && len(ps[0].Names) == 1 {
			m = ps[0].Names[0].Name
		}
		for _, st := range fn.Body.List {
			for _, e := range statementExprs(st) {
				if direct(e) {
					return true
				}
			}
			if endsOrRunsTests(st, m, osName) {
				break
			}
		}
	}
	return false
}

// statementExprs are the expressions a statement of TestMain's body evaluates as itself: an
// expression statement's, an assignment's right-hand side, a var declaration's values.
func statementExprs(st ast.Stmt) []ast.Expr {
	switch s := st.(type) {
	case *ast.ExprStmt:
		return []ast.Expr{s.X}
	case *ast.AssignStmt:
		return s.Rhs
	case *ast.DeclStmt:
		var out []ast.Expr
		if g, ok := s.Decl.(*ast.GenDecl); ok && g.Tok == token.VAR {
			for _, spec := range g.Specs {
				if v, ok := spec.(*ast.ValueSpec); ok {
					out = append(out, v.Values...)
				}
			}
		}
		return out
	}
	return nil
}

// endsOrRunsTests says the statement of TestMain's body runs the tests whatever happens (m.Run is
// the statement's own call, or an argument of it, as c := m.Run() or os.Exit(m.Run())) or ends
// TestMain (a return, or an os.Exit or panic as the statement itself), so a guard after it comes
// too late or never. A branch that does either (a child helper's own path) is not the run's.
func endsOrRunsTests(st ast.Stmt, m, osName string) bool {
	if _, ok := st.(*ast.ReturnStmt); ok {
		return true
	}
	var runs func(e ast.Expr) bool
	runs = func(e ast.Expr) bool {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Run" && len(call.Args) == 0 {
			if x, ok := sel.X.(*ast.Ident); ok && m != "" && m != "_" && x.Name == m {
				return true
			}
		}
		for _, a := range call.Args {
			if runs(a) {
				return true
			}
		}
		return false
	}
	for _, e := range statementExprs(st) {
		if runs(e) {
			return true
		}
	}
	if es, ok := st.(*ast.ExprStmt); ok {
		if call, ok := es.X.(*ast.CallExpr); ok {
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				return fun.Name == "panic"
			case *ast.SelectorExpr:
				x, ok := fun.X.(*ast.Ident)
				return ok && osName != "" && x.Name == osName && fun.Sel.Name == "Exit"
			}
		}
	}
	return false
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
