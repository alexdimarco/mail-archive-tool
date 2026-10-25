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
// Parse is one: it yields the *Vault that Unlock and UnlockRecovery turn into a
// key. PendingCreate.Commit is reached only through PrepareCreate.
var keyProducers = map[string]bool{
	"Create": true, "PrepareCreate": true, "Open": true, "Parse": true,
	"PrepareNewRecovery": true, "RequireRetrievableKey": true,
}

// keyMethods are the *Vault methods that return a usable *Key. Without type
// information the scan matches them by name in any file importing the vault.
var keyMethods = map[string]bool{"Unlock": true, "UnlockRecovery": true}

// encryptionReachAllowed lists the production files allowed to reach the
// encrypted path. It starts EMPTY; Phases 3–4 add the UX and unattended entry
// points here, and each addition is an operator-visible gate change.
var encryptionReachAllowed = map[string]bool{}

const (
	vaultImport = "mail-archive-tool/internal/vault"
	appImport   = "mail-archive-tool/internal/app"
)

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

// covers: MA-726, MA-727, R37
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

// reachesIn lists, for one parsed file, every call of a vault key producer or
// key method, every Options literal that sets Key, and — in package app or a
// file importing it — every assignment to a .Key field (o.Key = k).
func reachesIn(rel string, f *ast.File) []string {
	alias := ""
	seesApp := f.Name.Name == "app"
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		switch p {
		case vaultImport:
			alias = "vault"
			if imp.Name != nil {
				alias = imp.Name.Name
			}
		case appImport:
			seesApp = true
		}
	}
	// A zero-argument Unlock is a mutex, not Vault.Unlock(password).
	mutexUnlock := map[*ast.SelectorExpr]bool{}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Unlock" && len(x.Args) == 0 {
				mutexUnlock[sel] = true
			}
		case *ast.AssignStmt:
			if !seesApp {
				break
			}
			for _, lhs := range x.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Key" {
					out = append(out, rel+": assigns .Key (sets Options.Key)")
				}
			}
		case *ast.SelectorExpr:
			if alias != "" && keyMethods[x.Sel.Name] && !mutexUnlock[x] {
				out = append(out, rel+": calls Vault."+x.Sel.Name)
			}
			if id, ok := x.X.(*ast.Ident); ok && alias != "" && id.Name == alias {
				if keyProducers[x.Sel.Name] {
					out = append(out, rel+": calls vault."+x.Sel.Name)
				}
				// Detect sees only a readable vault: an archive is recognised as
				// encrypted by EncryptedMarker alone (phase2a §5, P2a-C8).
				if x.Sel.Name == "Detect" {
					out = append(out, rel+": calls vault.Detect (recognise an encrypted archive with vault.EncryptedMarker)")
				}
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

// covers: MA-726, MA-727, R37
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
		"\t_, _ = v.EncryptedMarker(\"out\")\n" +
		"\t_, _, _ = v.Detect(\"out\")\n" +
		"\tvv, _ := v.Parse(nil)\n" +
		"\tk, _ := vv.Unlock(\"pw\")\n" +
		"\t_, _ = vv.UnlockRecovery(\"code\")\n" +
		"\tvar mu sync.Mutex\n" +
		"\tmu.Unlock()\n" +
		"\tvar o app.Options\n" +
		"\to.Out, o.Key = \"o\", k\n" +
		"}\n"
	// Package app sets Key without importing itself; an html attribute's Key in a
	// file that cannot see app.Options is not a reach.
	inApp := "package app\nvar _ = func(o Options) { o.Key = nil }\n"
	attr := "package y\ntype a struct{ Key string }\nvar _ = func(x a) { x.Key = \"href\" }\n"
	var got []string
	for name, s := range map[string]string{"x.go": src, "app.go": inApp, "attr.go": attr} {
		f, err := parser.ParseFile(token.NewFileSet(), name, s, 0)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, reachesIn(name, f)...)
	}
	sort.Strings(got)
	want := []string{
		"app.go: assigns .Key (sets Options.Key)",
		"x.go: assigns .Key (sets Options.Key)",
		"x.go: calls Vault.Unlock",
		"x.go: calls Vault.UnlockRecovery",
		"x.go: calls vault.Detect (recognise an encrypted archive with vault.EncryptedMarker)",
		"x.go: calls vault.Parse",
		"x.go: calls vault.PrepareCreate",
		"x.go: sets Options.Key",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the scan must find exactly the aliased producer calls (PrepareCreate, Parse), the key methods (Unlock, UnlockRecovery), Detect, the Options.Key literal and both .Key assignments — and not KeySecret, EncryptedMarker, a mutex Unlock or attr.Key outside app:\n got: %q\nwant: %q", got, want)
	}
}
