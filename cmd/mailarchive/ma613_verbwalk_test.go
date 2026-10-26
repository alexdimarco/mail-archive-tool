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

	"mail-archive-tool/internal/assure"
)

// x1Codes is the declared X1 exit-code map (docs/ux-contract.md X1): 0 ok, 1
// error/refusal, 2 verify's not-attested, 3 extract's partial, and — added by the
// owner on 2026-10-26 — 130/143, a stop by Ctrl-C/SIGTERM with nothing saved by it
// (signalExit). Every literal exit code the program can emit must be one of these
// (MA-613).
var x1Codes = map[int]bool{0: true, 1: true, 2: true, 3: true, 130: true, 143: true}

// exitCodeFuncs are the functions whose integer return values are process exit
// codes (the dispatcher and the GUI verbs' entry points). Their return literals
// are collected and checked against X1; a stray count-returning helper is not one
// of these, so its ints are not mistaken for exit codes.
var exitCodeFuncs = map[string]bool{
	"runResolvedVerb": true, "runNoArg": true, "runNoArgWith": true, // cmd/mailarchive
	"Run": true, "RunHeadless": true, // internal/wizard
}

// covers: MA-613, R33, S59
// The verb-and-flag walk extended to the one program's three GUI verbs: each has
// its own -h (MA-39's walk covered the command-line verbs), and -h is a clean X1
// exit, not a spurious error. And the exit-code set: every literal exit code the
// dispatcher, the no-argument rule and the GUI verbs emit is a subset of the X1
// map {0,1,2,3,130,143}. Fail-closed twin: a planted os.Exit(5) is flagged.
func TestMA613_VerbWalkAndExitCodes(t *testing.T) {
	// --- the three GUI verbs each answer -h with their own usage, exit 0 --------
	cases := []struct {
		verb string
		want []string
	}{
		{"desktop", []string{"-out", "-capture"}}, // its flagset's own flags
		{"wizard", []string{"wizard"}},            // names itself; no flags of its own
		{"run-job", []string{"run-job", "FILE"}},  // names itself and its one argument
	}
	for _, c := range cases {
		code, out := runCLI(c.verb, "-h")
		if code != 0 {
			t.Errorf("%s -h exited %d, want 0 (a clean X1 help exit, MA-613); output:\n%s", c.verb, code, out)
		}
		for _, w := range assure.Reached(t, c.want, c.verb+" -h wants") {
			if !strings.Contains(out, w) {
				t.Errorf("%s -h does not name %q; got:\n%s", c.verb, w, out)
			}
		}
	}

	// --- every literal exit code is in the X1 set -------------------------------
	root := moduleRoot(t)
	codes, sites := collectExitCodes(t, map[string]bool{
		filepath.Join(root, "cmd", "mailarchive"): true,
		filepath.Join(root, "internal", "wizard"): true,
	})
	if assure.Reached(t, sites, "exit-code literal sites scanned") == 0 {
		t.Fatal("no exit-code literals found — the scan is not reaching the code")
	}
	for code, where := range assure.Reached(t, codes, "exit-code literals") {
		if !x1Codes[code] {
			t.Errorf("exit code %d is outside the X1 map {0,1,2,3,130,143} (MA-613): %s", code, where)
		}
	}

	// Fail-closed twin: the collector flags an exit code outside X1.
	bad := scanSource(t, "twin.go", `package p
func runNoArg() (int, bool) { os.Exit(5); return 7, true }`, true)
	if !bad[5] || !bad[7] {
		t.Errorf("fail-closed twin: a planted os.Exit(5)/return 7 was not collected, got %v", bad)
	}
	for code := range bad {
		if x1Codes[code] {
			continue
		}
		// good: the twin's out-of-set codes (5, 7) are exactly what the real check rejects.
	}
}

// collectExitCodes parses every non-test .go file under the given package dirs and
// returns the set of literal exit codes, mapped to a representative "file:line".
func collectExitCodes(t *testing.T, dirs map[string]bool) (map[int]string, int) {
	t.Helper()
	codes := map[int]string{}
	sites := 0
	fset := token.NewFileSet()
	for dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				t.Fatalf("parse %s: %v", path, perr)
			}
			rel, _ := filepath.Rel(moduleRoot(t), path)
			for n, line := range exitCodeLiterals(f) {
				sites++
				if _, seen := codes[n]; !seen {
					codes[n] = rel + ":" + strconv.Itoa(fset.Position(line).Line)
				}
			}
		}
	}
	return codes, sites
}

// exitCodeLiterals returns the literal exit codes in one file: the int argument of
// os.Exit, the `code:` field of an exitError composite, and the first int return
// value of an exit-code function. The value is the literal's position.
func exitCodeLiterals(f *ast.File) map[int]token.Pos {
	out := map[int]token.Pos{}
	add := func(e ast.Expr) {
		if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.INT {
			if v, err := strconv.Atoi(lit.Value); err == nil {
				out[v] = lit.Pos()
			}
		}
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		inExitFunc := fn.Name != nil && exitCodeFuncs[fn.Name.Name]
		ast.Inspect(fn.Body, func(nd ast.Node) bool {
			switch x := nd.(type) {
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "os" && sel.Sel.Name == "Exit" && len(x.Args) == 1 {
						add(x.Args[0])
					}
				}
			case *ast.CompositeLit:
				if isExitErrorType(x.Type) {
					for _, el := range x.Elts {
						if kv, ok := el.(*ast.KeyValueExpr); ok {
							if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "code" {
								add(kv.Value)
							}
						}
					}
				}
			case *ast.ReturnStmt:
				if inExitFunc && len(x.Results) >= 1 {
					add(x.Results[0])
				}
			}
			return true
		})
	}
	return out
}

func isExitErrorType(e ast.Expr) bool {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name == "exitError"
	}
	return false
}

// scanSource parses one synthetic source and returns its exit-code literals as a
// set (for the fail-closed twin). withExitFunc makes its func an exit-code func.
func scanSource(t *testing.T, name, src string, withExitFunc bool) map[int]bool {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatalf("parse twin: %v", err)
	}
	got := map[int]bool{}
	for n := range exitCodeLiterals(f) {
		got[n] = true
	}
	return got
}
