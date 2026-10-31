package nowindow

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// thisPkg is this package's import path, which the walk looks for in each file.
const thisPkg = "mail-archive-tool/internal/nowindow"

// startsNotThroughHide are the process starts that do not go through Hide, each
// with its reason: the file, the function the start is written in, and the call.
// A start here that the walk no longer finds is reported too, so the list never
// outlives the code.
var startsNotThroughHide = []struct{ file, fn, call, reason string }{
	{
		file: "internal/presync/quit_windows.go", fn: "realCreateProcess", call: "windows.CreateProcess",
		reason: "Thunderbird's own start (MA-923): one CreateProcess call that asks for Thunderbird's " +
			"minimized-not-active window itself; Thunderbird is not a console program, so " +
			"CREATE_NO_WINDOW would change nothing, and the code is held by MA-927's pin",
	},
}

// otherStarts are the process starts that do not take an *exec.Cmd, so Hide
// cannot wrap them; each one in a file built for Windows must be listed in
// startsNotThroughHide.
var otherStarts = map[string]bool{
	"os.StartProcess": true, "syscall.StartProcess": true, "syscall.ForkExec": true, "syscall.Exec": true,
	"windows.CreateProcess": true, "syscall.CreateProcess": true,
	"windows.CreateProcessAsUser": true, "syscall.CreateProcessAsUser": true,
	"windows.ShellExecute": true,
}

// walkResult is what the walk found in the files it read.
type walkResult struct {
	hidden   int            // exec.Command / exec.CommandContext calls wrapped by Hide
	hiddenIn map[string]int // the same, per file
	findings []string       // starts that are neither wrapped nor listed
	listed   map[string]bool
	skipped  map[string]bool // files not built for Windows
}

// importNames maps each import path of f to the name the file uses for it.
func importNames(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := p[strings.LastIndex(p, "/")+1:]
		if im.Name != nil {
			name = im.Name.Name
		}
		m[p] = name
	}
	return m
}

// walkFile checks one parsed file: every reference to exec.Command or
// exec.CommandContext must be the callee of a call that is itself the one
// argument of a call to Hide; every other start (otherStarts, or an exec.Cmd
// written out as a literal) must be listed in startsNotThroughHide.
func walkFile(fset *token.FileSet, f *ast.File, rel string, r *walkResult) {
	names := importNames(f)
	execName, hasExec := names["os/exec"]
	hideName, hasHide := names[thisPkg]
	pkgOf := map[string]string{"os": "os", "syscall": "syscall", "golang.org/x/sys/windows": "windows"}
	local := map[string]string{} // the file's name for os, syscall and windows → the canonical one
	for path, canon := range pkgOf {
		if n, ok := names[path]; ok {
			local[n] = canon
		}
	}

	isHide := func(call *ast.CallExpr) bool {
		fn, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !hasHide {
			return false
		}
		x, ok := fn.X.(*ast.Ident)
		return ok && x.Name == hideName && fn.Sel.Name == "Hide"
	}

	var stack []ast.Node // the nodes enclosing n, outermost first
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		defer func() { stack = append(stack, n) }()
		fnName := "" // the function declaration n is written in ("" at package level)
		for _, p := range stack {
			if fd, ok := p.(*ast.FuncDecl); ok {
				fnName = fd.Name.Name
			}
		}
		at := rel + ":" + strconv.Itoa(fset.Position(n.Pos()).Line)
		switch x := n.(type) {
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			if !ok {
				break
			}
			if hasExec && id.Name == execName && (x.Sel.Name == "Command" || x.Sel.Name == "CommandContext") {
				// stack[-1] is the call whose callee this is; stack[-2] must be Hide's call.
				var call, outer *ast.CallExpr
				if len(stack) >= 1 {
					call, _ = stack[len(stack)-1].(*ast.CallExpr)
				}
				if len(stack) >= 2 {
					outer, _ = stack[len(stack)-2].(*ast.CallExpr)
				}
				if call != nil && call.Fun == ast.Expr(x) && outer != nil && isHide(outer) &&
					len(outer.Args) == 1 && outer.Args[0] == ast.Expr(call) {
					r.hidden++
					r.hiddenIn[rel]++
				} else {
					r.findings = append(r.findings, at+": exec."+x.Sel.Name+" not wrapped by nowindow.Hide where it is written")
				}
				break
			}
			if canon, ok := local[id.Name]; ok && otherStarts[canon+"."+x.Sel.Name] {
				name := canon + "." + x.Sel.Name
				for _, s := range startsNotThroughHide {
					if s.file == rel && s.fn == fnName && s.call == name {
						r.listed[s.file+" "+s.fn+" "+s.call] = true
						return true
					}
				}
				r.findings = append(r.findings, at+": "+name+" in "+fnName+" starts a process that nowindow.Hide cannot wrap and is not listed with a reason")
			}
		case *ast.CompositeLit:
			if sel, ok := x.Type.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && hasExec && id.Name == execName && sel.Sel.Name == "Cmd" {
					r.findings = append(r.findings, at+": an exec.Cmd written out as a literal, not made by exec.Command inside nowindow.Hide")
				}
			}
		}
		return true
	})
}

// moduleRoot finds the folder holding go.mod above this package.
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
			t.Fatal("no go.mod above the test's folder")
		}
		dir = parent
	}
}

// walkTree checks every non-test Go file under cmd/ and internal/ that is built
// for Windows (by its name and its //go:build line).
func walkTree(t *testing.T, root string) *walkResult {
	t.Helper()
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = "windows", "amd64", false
	r := &walkResult{hiddenIn: map[string]int{}, listed: map[string]bool{}, skipped: map[string]bool{}}
	fset := token.NewFileSet()
	for _, base := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, base), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			ok, err := ctx.MatchFile(filepath.Dir(path), filepath.Base(path))
			if err != nil {
				return err
			}
			if !ok {
				r.skipped[rel] = true
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			walkFile(fset, f, rel, r)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", base, err)
		}
	}
	return r
}

// walkSource runs the walk over one planted file.
func walkSource(t *testing.T, src string) *walkResult {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "planted.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse the planted file: %v", err)
	}
	r := &walkResult{hiddenIn: map[string]int{}, listed: map[string]bool{}, skipped: map[string]bool{}}
	walkFile(fset, f, "planted.go", r)
	return r
}

// covers: MA-951, S59
// No console window from the program's own child processes on Windows: every
// exec.Command and exec.CommandContext in a file built for Windows, under cmd/
// and internal/, is wrapped by nowindow.Hide where it is written, and every other
// process start is listed with its reason (only Thunderbird's own start, MA-923).
// Positive twin first (wrapped starts, under the usual and an aliased import, pass;
// the real tree passes and counts the Task Scheduler, clipboard, browser and
// folder openers' starts; files not built for Windows are left out); then each
// planted unwrapped start is caught.
func TestEveryChildProcessStartsWithNoWindow(t *testing.T) {
	// Positive twins: a wrapped start passes, also with renamed imports.
	for name, src := range map[string]string{
		"wrapped Command": `package p
import ("os/exec"; "mail-archive-tool/internal/nowindow")
	func f() { _ = nowindow.Hide(exec.Command("schtasks", "/Query")).Run() }`,
		"wrapped CommandContext": `package p
import ("context"; "os/exec"; "mail-archive-tool/internal/nowindow")
	func f(ctx context.Context) { _ = nowindow.Hide(exec.CommandContext(ctx, "clip")).Run() }`,
		"renamed imports": `package p
import (osexec "os/exec"; nw "mail-archive-tool/internal/nowindow")
var run = func() error { return nw.Hide(osexec.Command("schtasks")).Run() }`,
	} {
		r := walkSource(t, src)
		if len(r.findings) != 0 || r.hidden != 1 {
			t.Errorf("positive twin %q: findings %v, wrapped starts %d (want none and 1)", name, r.findings, r.hidden)
		}
	}

	// The real tree.
	r := walkTree(t, moduleRoot(t))
	for _, f := range r.findings {
		t.Errorf("a child process that may open a console window on Windows: %s", f)
	}
	for _, s := range startsNotThroughHide {
		if !r.listed[s.file+" "+s.fn+" "+s.call] {
			t.Errorf("listed start %s in %s (%s) was not found: remove it from the list, or the walk lost it", s.call, s.fn, s.file)
		}
	}
	// The starts the Desktop and the scheduled run make on Windows are counted:
	// Task Scheduler (schtasks), the clipboard (clip), the browser and folder openers.
	for _, file := range []string{
		"internal/schedule/schedule.go", "internal/desktop/dialogs.go", "internal/desktop/exports.go",
		"internal/browser/browser.go", "internal/wizard/wizard.go",
	} {
		if r.hiddenIn[file] == 0 {
			t.Errorf("the walk counted no wrapped start in %s", file)
		}
	}
	// A file built only for macOS never runs on Windows and is left out.
	if !r.skipped["internal/presync/env_darwin.go"] {
		t.Error("the walk read internal/presync/env_darwin.go, a macOS-only file")
	}
	assure.Reached(t, r.hidden, "exec.Command starts wrapped by nowindow.Hide")

	// Fail-closed twins: each planted start is caught.
	for name, src := range map[string]string{
		"bare Command": `package p
import "os/exec"
	func f() { _ = exec.Command("schtasks", "/Query").Run() }`,
		"bare CommandContext": `package p
import ("context"; "os/exec")
	func f(ctx context.Context) { _ = exec.CommandContext(ctx, "clip").Run() }`,
		"wrapped after the fact": `package p
import ("os/exec"; "mail-archive-tool/internal/nowindow")
	func f() { c := exec.Command("schtasks"); nowindow.Hide(c); _ = c.Run() }`,
		"Command taken as a value": `package p
import "os/exec"
var start = exec.Command`,
		"a renamed exec import": `package p
import osexec "os/exec"
	func f() { _ = osexec.Command("clip").Run() }`,
		"an exec.Cmd literal": `package p
import "os/exec"
	func f() { c := &exec.Cmd{Path: "C:\\Windows\\System32\\schtasks.exe"}; _ = c.Run() }`,
		"os.StartProcess": `package p
import "os"
	func f() { _, _ = os.StartProcess("schtasks.exe", nil, &os.ProcAttr{}) }`,
		"CreateProcess outside the list": `package p
import "golang.org/x/sys/windows"
	func g(app *uint16) { _ = windows.CreateProcess(app, nil, nil, nil, false, 0, nil, nil, nil, nil) }`,
		"ShellExecute": `package p
import "golang.org/x/sys/windows"
	func g(file *uint16) { _ = windows.ShellExecute(0, nil, file, nil, nil, 1) }`,
	} {
		if r := walkSource(t, src); len(r.findings) == 0 {
			t.Errorf("fail-closed twin %q was not caught", name)
		}
	}
}
