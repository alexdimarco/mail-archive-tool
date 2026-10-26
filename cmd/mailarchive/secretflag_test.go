package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// secretFlagsAllowed are the only flags whose names speak of a secret: each names a
// file or an environment variable holding it, never the secret itself
// (design-encrypted-archives-phase3 §3.4, A26).
var secretFlagsAllowed = map[string]bool{
	"password-file": true, "recovery-key-file": true, "recovery-key-out": true,
	"client-secret-file": true, "client-secret-env": true, "token-cache": true,
}

var secretWord = regexp.MustCompile(`(?i)pass|secret|recovery|token|key|credential|pin`)

// flagNames lists, for every production Go file under the tops of root that imports
// "flag", the names of the flags it defines (fs.String("name", …), fs.Var(&v,
// "name", …) and the like, on any flag set).
func flagNames(t *testing.T, root string, tops ...string) (names map[string][]string, files int) {
	t.Helper()
	defs := map[string]int{"String": 0, "Bool": 0, "Int": 0, "Int64": 0, "Uint": 0, "Uint64": 0, "Float64": 0, "Duration": 0,
		"Func": 0, "BoolFunc": 0, "Var": 1, "StringVar": 1, "BoolVar": 1, "IntVar": 1, "DurationVar": 1, "TextVar": 1}
	names = map[string][]string{}
	for _, top := range tops {
		filepath.WalkDir(filepath.Join(root, top), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
			if perr != nil {
				t.Fatalf("parse %s: %v", p, perr)
			}
			imports := false
			for _, im := range f.Imports {
				imports = imports || im.Path.Value == `"flag"`
			}
			if !imports {
				return nil
			}
			files++
			rel, _ := filepath.Rel(root, p)
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				i, ok := defs[sel.Sel.Name]
				if !ok || len(call.Args) <= i {
					return true
				}
				lit, ok := call.Args[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if name, err := strconv.Unquote(lit.Value); err == nil && regexp.MustCompile(`^[a-z][a-z0-9-]*$`).MatchString(name) {
					names[name] = append(names[name], filepath.ToSlash(rel))
				}
				return true
			})
			return nil
		})
	}
	return names, files
}

// covers: MA-789, R37
// No flag of any MailArchive command takes a password, a recovery key, a client
// secret or a token as its value — it would sit in shell history and in every
// process listing. A flag whose name speaks of a secret must name a file or an
// environment variable holding it, from a closed list.
func TestNoFlagTakesASecretAsItsValue(t *testing.T) {
	names, files := flagNames(t, "../..", "cmd", "internal")
	if files < 3 || len(names["out"]) == 0 || len(names["password-file"]) == 0 {
		t.Fatalf("the scan did not see the command line's flags (%d files, %d names)", files, len(names))
	}
	bad := secretFlagViolations(names)
	assure.Reached(t, len(names), "flag names scanned")
	if len(bad) > 0 {
		t.Fatalf("flags that may take a secret as their value: %s", strings.Join(bad, "; "))
	}
	for name := range secretFlagsAllowed {
		if len(names[name]) == 0 {
			t.Errorf("the allowed secret flag -%s is defined nowhere: drop it from the list", name)
		}
	}
	// The scan finds what it looks for.
	plant := t.TempDir()
	os.MkdirAll(filepath.Join(plant, "cmd"), 0o700)
	os.WriteFile(filepath.Join(plant, "cmd", "x.go"), []byte("package x\nimport \"flag\"\nvar fs = flag.NewFlagSet(\"x\", 0)\nvar p = fs.String(\"password\", \"\", \"\")\nvar q string\nfunc init() { fs.StringVar(&q, \"api-token\", \"\", \"\") }\n"), 0o600)
	planted, _ := flagNames(t, plant, "cmd")
	if v := secretFlagViolations(planted); len(v) != 2 || !strings.HasPrefix(v[0], "-api-token") || !strings.HasPrefix(v[1], "-password ") {
		t.Fatalf("the check did not refuse the planted -password and -api-token flags: %v (found %v)", v, planted)
	}
}

// secretFlagViolations lists the flags whose names speak of a secret and are not on
// the closed list, with where each is defined.
func secretFlagViolations(names map[string][]string) []string {
	var bad []string
	for name, where := range names {
		if secretWord.MatchString(name) && !secretFlagsAllowed[name] {
			bad = append(bad, "-"+name+" ("+strings.Join(where, ", ")+")")
		}
	}
	sort.Strings(bad)
	return bad
}
