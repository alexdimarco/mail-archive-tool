package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// oldNameHits returns each place in s where the program is called "Mail Archive"
// (two words) other than as the Mac app bundle's own name, "Mail Archive.app",
// which is what the bundle is built and installed as and is kept as it is (the
// owner's ruling B, 2026-10-31: titles and words only, never the bundle).
func oldNameHits(s string) []string {
	var hits []string
	for i := 0; ; {
		j := strings.Index(s[i:], "Mail Archive")
		if j < 0 {
			return hits
		}
		at := i + j
		end := at + len("Mail Archive")
		if !strings.HasPrefix(s[end:], ".app") {
			lo, hi := max(0, at-30), min(len(s), end+30)
			hits = append(hits, s[lo:hi])
		}
		i = end
	}
}

// covers: MA-952
// The program's own words name it "MailArchive", never "Mail Archive" (two words):
// the wizard's window titles, the schedule refusals, the Thunderbird sync's warnings
// and every other string literal in the program's code (internal/ and cmd/, test
// files aside, comments aside). The one exception is the Mac app bundle's own name,
// "Mail Archive.app", which the owner's ruling B keeps as it is built. Positive twin
// first: the sweep reads the program's code and finds the bundle name, which it lets
// pass, and a planted old title is caught.
func TestTheProgramNamesItselfMailArchive(t *testing.T) {
	// Fail-closed twins on the matcher itself.
	if got := oldNameHits(`const appTitle = "Mail Archive Export"`); len(got) != 1 {
		t.Errorf("planted: the old wizard title was not caught: %v", got)
	}
	if got := oldNameHits("Do this: drag Mail Archive.app to your Applications folder."); len(got) != 0 {
		t.Errorf("the bundle's own name must pass: %v", got)
	}
	if got := oldNameHits("Mail Archive is running; drag Mail Archive.app"); len(got) != 1 {
		t.Errorf("a line naming the bundle does not excuse the old name beside it: %v", got)
	}

	root := moduleRoot(t)
	files, bundle := 0, 0
	var bad []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if perr != nil {
				return perr
			}
			files++
			rel, _ := filepath.Rel(root, path)
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, uerr := strconv.Unquote(lit.Value)
				if uerr != nil {
					s = lit.Value
				}
				bundle += strings.Count(s, "Mail Archive.app")
				for _, h := range oldNameHits(s) {
					bad = append(bad, filepath.ToSlash(rel)+": …"+h+"…")
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	assure.Reached(t, files, "Go source files swept")
	// Positive twin: the bundle's name is in the program's words (the uninstall note,
	// the guide, the translocation refusal) and passes.
	assure.Reached(t, bundle, `"Mail Archive.app" in the program's words`)
	for _, b := range bad {
		t.Errorf("the program calls itself \"Mail Archive\", not \"MailArchive\": %s", b)
	}
}
