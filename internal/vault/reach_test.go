package vault

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// keyProducers are the internal/vault functions that hand out an archive key.
// Until Phase 3 builds the encrypted-archive UX, no production code may call one:
// Phase 2a's encrypted write path protects nothing on its own (the manifest,
// index and history are still cleartext), so it must be unreachable
// (design-encrypted-archives-phase2a §0, P2a-C20).
var keyProducers = map[string]bool{
	"Create": true, "PrepareCreate": true, "Open": true,
	"PrepareNewRecovery": true, "RequireRetrievableKey": true,
}

// encryptionReachAllowed lists the production files allowed to reach the
// encrypted path. It starts EMPTY; Phases 3–4 add the UX and unattended entry
// points here, and each addition is an operator-visible gate change.
var encryptionReachAllowed = map[string]bool{}

const vaultImport = "mail-archive-tool/internal/vault"

// moduleRoot is the repository root: the nearest directory above this package
// holding go.mod.
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
			t.Fatal("no go.mod above the vault package")
		}
		dir = parent
	}
}

// covers: MA-726, R37
func TestNoProductionCodeCanReachTheEncryptedPathYet(t *testing.T) {
	root := moduleRoot(t)
	var reaches []string
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "testdata", "assure", "assureblock":
				return filepath.SkipDir
			}
			if filepath.ToSlash(rel) == "internal/vault" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		if encryptionReachAllowed[filepath.ToSlash(rel)] {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", rel, perr)
		}
		reaches = append(reaches, reachesIn(rel, f)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 50 {
		t.Fatalf("scanned only %d production files: the walk is not seeing the repository", scanned)
	}
	sort.Strings(reaches)
	if len(reaches) > 0 {
		t.Errorf("production code reaches the encrypted-archive path before Phase 3 (an operator decision moves a file onto encryptionReachAllowed): %s",
			strings.Join(reaches, "; "))
	}
}

// reachesIn lists, for one parsed file, every call of a vault key producer and
// every Options literal that sets Key.
func reachesIn(rel string, f *ast.File) []string {
	alias := ""
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == vaultImport {
			alias = "vault"
			if imp.Name != nil {
				alias = imp.Name.Name
			}
		}
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok && alias != "" && id.Name == alias && keyProducers[x.Sel.Name] {
				out = append(out, rel+": calls vault."+x.Sel.Name)
			}
		case *ast.CompositeLit:
			if isOptionsType(x.Type) {
				for _, el := range x.Elts {
					if kv, ok := el.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Key" {
							out = append(out, rel+": sets Options.Key")
						}
					}
				}
			}
		}
		return true
	})
	return out
}

func isOptionsType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name == "Options"
	case *ast.SelectorExpr:
		return t.Sel.Name == "Options"
	}
	return false
}

// covers: MA-726, R37
func TestTheReachScanFindsWhatItLooksFor(t *testing.T) {
	// One string, so no line of it starts with "func " (the vacuous-test scan reads
	// that as the next test).
	src := "package x\n" +
		"import v \"mail-archive-tool/internal/vault\"\n" +
		"import \"mail-archive-tool/internal/app\"\n" +
		"var _ = func() {\n" +
		"\tp, _ := v.PrepareCreate(\"out\", \"pw\")\n" +
		"\t_ = p\n" +
		"\t_ = app.Options{Out: \"o\", Key: nil}\n" +
		"\t_ = v.KeySecret(nil)\n" +
		"}\n"
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := reachesIn("x.go", f)
	if len(got) != 2 || !strings.Contains(got[0]+got[1], "vault.PrepareCreate") || !strings.Contains(got[0]+got[1], "Options.Key") {
		t.Fatalf("the scan must find an aliased producer call and an Options.Key literal (and not KeySecret): %v", got)
	}
}
