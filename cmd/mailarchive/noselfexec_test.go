package main

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

// osTools is the closed set of OS programs the one program may start (§5.4
// item 1 / §12, MA-612): the schedulers, the OS openers, the notifier/clipboard
// tools and the dialog program. Adding to it is a design change, reviewed with
// this walk's pin (SC52).
var osTools = map[string]bool{
	"crontab": true, "schtasks": true, "launchctl": true,
	"open": true, "xdg-open": true, "rundll32": true, "explorer.exe": true,
	"osascript": true, "zenity": true, "qarma": true, "matedialog": true,
	"pbcopy": true, "xclip": true, "xsel": true, "wl-copy": true, "clip": true,

	// The Thunderbird pre-sync macOS adapter (design §4.7.1, pinned by MA-557)
	// starts these system tools by ABSOLUTE path — §5.4 rule 7 forbids a PATH
	// lookup, so each literal is the full path, which keeps this set STRICTER
	// than a bare name (a planted `open` earlier on PATH would not match). The
	// quit request is osascript; the rest only observe (pgrep/ps/sysctl/ioreg/
	// plutil) or hold a wake lock (caffeinate). None signals or kills anything.
	"/usr/bin/open": true, "/usr/bin/osascript": true,
	"/usr/bin/pgrep": true, "/bin/ps": true,
	"/usr/sbin/sysctl": true, "/usr/sbin/ioreg": true,
	"/usr/bin/plutil": true, "/usr/bin/caffeinate": true,
}

// argvBuilders are the pinned functions that return an (OS-tool name, args)
// pair; a process-start site may take its program from one of these, whose
// returns the walk checks are osTools literals (MA-612). On fix/review's tree
// browser.Argv holds the browser opener's literals (openBrowserArgv delegates to
// it), so Argv is pinned here too.
var argvBuilders = map[string]bool{"openPathArgv": true, "openBrowserArgv": true, "Argv": true}

// calleeName renders a call's callee as "pkg.Func" or "Func".
func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	case *ast.Ident:
		return fn.Name
	}
	return ""
}

// collectAssigns maps each locally-assigned identifier in fn to the expression
// it was assigned from (last write wins; a multi-value := from one call maps
// every name to that call), so a program variable can be traced one hop.
func collectAssigns(fn *ast.FuncDecl) map[string]ast.Expr {
	m := map[string]ast.Expr{}
	if fn.Body == nil {
		return m
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			switch {
			case len(as.Rhs) == len(as.Lhs):
				m[id.Name] = as.Rhs[i]
			case len(as.Rhs) == 1:
				m[id.Name] = as.Rhs[0]
			}
		}
		return true
	})
	return m
}

// classifyProgram returns a non-empty reason when the program expression of a
// process-start site is (or one hop resolves to) a forbidden self/sibling
// source, or a non-OS-tool literal; "" when it is an OS-tool literal or an
// allowlisted argv-builder result. A program reached only through an untraced
// variable, field or unknown call is the stated SC52 open condition (not flagged).
func classifyProgram(prog ast.Expr, assigns map[string]ast.Expr) string {
	switch p := prog.(type) {
	case *ast.BasicLit:
		if p.Kind == token.STRING {
			v, _ := strconv.Unquote(p.Value)
			if !osTools[v] {
				return "non-OS-tool program literal " + strconv.Quote(v)
			}
		}
		return ""
	case *ast.CallExpr:
		return classifyCall(calleeName(p))
	case *ast.IndexExpr:
		if isOSArgs(p.X) {
			return "program is os.Args — a self reference"
		}
		return ""
	case *ast.Ident:
		if rhs, ok := assigns[p.Name]; ok {
			if call, ok := rhs.(*ast.CallExpr); ok {
				if r := classifyCall(calleeName(call)); r != "" {
					return r + " (via " + p.Name + ")"
				}
			}
			if ix, ok := rhs.(*ast.IndexExpr); ok && isOSArgs(ix.X) {
				return "program is os.Args — a self reference (via " + p.Name + ")"
			}
		}
		return ""
	}
	return ""
}

// classifyCall flags the forbidden program-producing calls; an allowlisted
// argv-builder and any other call are allowed (the latter is SC52 open).
func classifyCall(callee string) string {
	switch callee {
	case "os.Executable":
		return "program is os.Executable() — starts a copy of this program"
	case "filepath.Join":
		return "program is a filepath.Join — a sibling of this program"
	case "schedule.ProgramForSchedule", "ProgramForSchedule":
		return "program is schedule.ProgramForSchedule() — the program's own path"
	}
	return ""
}

func isOSArgs(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "os" && sel.Sel.Name == "Args"
}

// programExpr returns the program expression of a process-start call, or nil.
func programExpr(call *ast.CallExpr) ast.Expr {
	switch calleeName(call) {
	case "exec.Command", "os.StartProcess":
		if len(call.Args) >= 1 {
			return call.Args[0]
		}
	case "exec.CommandContext":
		if len(call.Args) >= 2 {
			return call.Args[1]
		}
	case "syscall.Exec", "syscall.StartProcess", "syscall.ForkExec":
		if len(call.Args) >= 1 {
			return call.Args[0]
		}
	}
	return nil
}

// walkSelfExec scans every non-test .go file under cmd/ and internal/ for
// process-start sites and classifies each one. It returns the findings and the
// number of sites examined.
func walkSelfExec(t *testing.T, root string) (findings []string, sites int) {
	t.Helper()
	fset := token.NewFileSet()
	for _, base := range []string{"cmd", "internal"} {
		_ = filepath.WalkDir(filepath.Join(root, base), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				t.Fatalf("parse %s: %v", path, perr)
			}
			rel, _ := filepath.Rel(root, path)
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				assigns := collectAssigns(fn)
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						if prog := programExpr(call); prog != nil {
							sites++
							if r := classifyProgram(prog, assigns); r != "" {
								findings = append(findings, posf(fset, call.Pos(), rel, r))
							}
						}
					}
					if lit, ok := n.(*ast.CompositeLit); ok && selName(lit.Type) == "exec.Cmd" {
						for _, el := range lit.Elts {
							kv, ok := el.(*ast.KeyValueExpr)
							if !ok {
								continue
							}
							if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Path" {
								sites++
								if r := classifyProgram(kv.Value, assigns); r != "" {
									findings = append(findings, posf(fset, kv.Pos(), rel, r))
								}
							}
						}
					}
					return true
				})
			}
			return nil
		})
	}
	return findings, sites
}

func selName(e ast.Expr) string {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if x, ok := sel.X.(*ast.Ident); ok {
			return x.Name + "." + sel.Sel.Name
		}
	}
	return ""
}

func posf(fset *token.FileSet, pos token.Pos, rel, reason string) string {
	return rel + ":" + strconv.Itoa(fset.Position(pos).Line) + ": " + reason
}

// covers: MA-612, R33, S59
// No self re-exec: every process-start site under cmd/ and internal/ names an
// OS tool (a literal from the closed set, or an allowlisted argv-builder's
// result), never os.Executable(), os.Args, a sibling filepath.Join or
// schedule.ProgramForSchedule — so the one program never starts a copy of
// itself. Positive twin: the healthy tree passes with a non-zero site count.
// Fail-closed twin: each planted self/sibling start is flagged.
func TestMA612_NoSelfReExec(t *testing.T) {
	root := moduleRoot(t)
	findings, sites := walkSelfExec(t, root)
	if sites == 0 {
		t.Fatal("the walk found no process-start sites — it is not scanning the tree")
	}
	for _, f := range findings {
		t.Errorf("self-exec risk: %s", f)
	}

	// The allowlisted argv-builders return only OS-tool literals.
	for _, bad := range builderReturnsNonTool(t, root) {
		t.Errorf("argv-builder returns a non-OS-tool program: %s", bad)
	}

	// Fail-closed twins: each planted pattern is flagged by classifyProgram.
	twins := map[string]string{
		"exec.Command(os.Executable())": `package p
func f() { exe, _ := os.Executable(); _ = exec.Command(exe, "wizard") }`,
		"sibling filepath.Join": `package p
func f(dir string) { _ = exec.Command(filepath.Join(dir, "mailarchive"), "desktop") }`,
		"exec.Cmd{Path: exe}": `package p
func f() { exe, _ := os.Executable(); _ = exec.Cmd{Path: exe} }`,
		"os.Args[0]": `package p
func f() { _ = exec.Command(os.Args[0], "desktop") }`,
	}
	for name, src := range twins {
		if !flaggedSource(t, src) {
			t.Errorf("fail-closed twin %q was NOT flagged by the walk", name)
		}
	}
}

// flaggedSource parses a single synthetic function and reports whether the walk
// flags any process-start site in it.
func flaggedSource(t *testing.T, src string) bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "twin.go", src, 0)
	if err != nil {
		t.Fatalf("parse twin: %v", err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		assigns := collectAssigns(fn)
		flagged := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if prog := programExpr(call); prog != nil && classifyProgram(prog, assigns) != "" {
					flagged = true
				}
			}
			if lit, ok := n.(*ast.CompositeLit); ok && selName(lit.Type) == "exec.Cmd" {
				for _, el := range lit.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Path" && classifyProgram(kv.Value, assigns) != "" {
							flagged = true
						}
					}
				}
			}
			return true
		})
		if flagged {
			return true
		}
	}
	return false
}

// builderReturnsNonTool checks each allowlisted argv-builder returns an OS-tool
// literal as its program name.
func builderReturnsNonTool(t *testing.T, root string) (bad []string) {
	t.Helper()
	fset := token.NewFileSet()
	for _, base := range []string{"cmd", "internal"} {
		_ = filepath.WalkDir(filepath.Join(root, base), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			for _, decl := range f.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name == nil || !argvBuilders[fn.Name.Name] {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					ret, ok := n.(*ast.ReturnStmt)
					if !ok || len(ret.Results) == 0 {
						return true
					}
					if lit, ok := ret.Results[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						v, _ := strconv.Unquote(lit.Value)
						if !osTools[v] {
							bad = append(bad, posf(fset, lit.Pos(), rel, fn.Name.Name+" returns "+strconv.Quote(v)))
						}
					}
					return true
				})
			}
			return nil
		})
	}
	return bad
}
