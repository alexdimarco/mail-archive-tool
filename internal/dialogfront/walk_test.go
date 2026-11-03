package dialogfront

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

const (
	module   = "mail-archive-tool"
	thisPkg  = module + "/internal/dialogfront"
	zenityPk = "github.com/ncruces/zenity"
)

// installingEntries are the entries that open dialogs, each of which calls Install
// as its first statement: MailArchive Desktop (which runs the wizard's Add another
// archive in-process) and the wizard on its own.
var installingEntries = []string{"internal/desktop.Launch", "internal/wizard.Run"}

// waysInNotThroughInstall are the ways to a dialog that do not pass an installing
// entry, each with its reason; the walk stops there. A listed way in the walk no
// longer reaches a dialog through is reported too, so the list never outlives the
// code.
var waysInNotThroughInstall = []struct{ decl, reason string }{
	{
		decl: "cmd/mailarchive.runResume",
		reason: "an open gap (S59, MA-1115): the command-line resume verb restores the schedule " +
			"through the wizard's repair, which asks \"Continue to the wizard?\" (and the " +
			"volatile-folder question); a command-line verb should open no dialog",
	},
}

// zenityWindows are the zenity functions that open a window. Notify is not one:
// it shows a notification, with no title bar, which the watch leaves alone.
var zenityWindows = map[string]bool{
	"Calendar": true, "Entry": true, "Password": true, "Question": true, "Info": true,
	"Warning": true, "Error": true, "List": true, "ListItems": true, "ListMultiple": true,
	"ListMultipleItems": true, "SelectFile": true, "SelectFileMultiple": true,
	"SelectFileSave": true, "SelectColor": true, "Progress": true,
}

// decl is one package-level declaration of the module: a function, a method or a
// variable.
type decl struct {
	key      string         // "<dir>.<name>", or "<dir>.<Type>.<name>" for a method
	at       string         // file:line
	refs     map[*decl]bool // the declarations it names
	dialogs  []string       // the zenity windows it opens or names (file:line zenity.X)
	installs bool           // a function whose first statement is dialogfront.Install()
}

// dialogWalk is what the walk found.
type dialogWalk struct {
	decls    map[string]*decl
	dialogs  int             // zenity window references followed
	findings []string        // dialogs reached by a way in that does not install first
	listed   map[string]bool // the listed ways in that a dialog was reached through
}

// importNames maps each import path of f to the name the file uses for it.
func importNames(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := path.Base(p)
		if im.Name != nil {
			name = im.Name.Name
		}
		m[p] = name
	}
	return m
}

// stubImporter gives the module's own packages, already checked, and an empty
// stand-in for every other package: the walk needs only the module's own names,
// and the checker still records them where an outside name is unknown.
type stubImporter map[string]*types.Package

func (s stubImporter) Import(p string) (*types.Package, error) {
	if pkg, ok := s[p]; ok {
		return pkg, nil
	}
	pkg := types.NewPackage(p, path.Base(p))
	pkg.MarkComplete()
	s[p] = pkg
	return pkg, nil
}

// walkDialogs type-checks the module's packages (files keyed by slash path from
// the module root), links every package-level declaration to the module
// declarations it names (a method through the checker's selection; a type named
// anywhere but in a method's own receiver links to all its methods, which covers a
// call through an interface), and follows each zenity window back through every
// declaration that names it. The walk stops at a function whose first statement
// is dialogfront.Install(); a declaration nothing names (main, init, an entry
// called only from outside) reached without passing one is a finding.
func walkDialogs(fset *token.FileSet, files map[string]*ast.File) *dialogWalk {
	w := &dialogWalk{decls: map[string]*decl{}, listed: map[string]bool{}}
	isListed := map[string]bool{}
	for _, l := range waysInNotThroughInstall {
		isListed[l.decl] = true
	}
	byDir := map[string][]string{}
	for rel := range files {
		byDir[path.Dir(rel)] = append(byDir[path.Dir(rel)], rel)
	}
	// Check each package after the module packages it imports.
	imports := map[string][]string{}
	for dir, rels := range byDir {
		for _, rel := range rels {
			for p := range importNames(files[rel]) {
				if d := strings.TrimPrefix(p, module+"/"); d != p && byDir[d] != nil {
					imports[dir] = append(imports[dir], d)
				}
			}
		}
	}
	var order []string
	done := map[string]bool{}
	var visit func(string)
	visit = func(dir string) {
		if done[dir] {
			return
		}
		done[dir] = true
		for _, d := range imports[dir] {
			visit(d)
		}
		order = append(order, dir)
	}
	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		visit(d)
	}

	imp := stubImporter{}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	type body struct {
		d     *decl
		node  ast.Node
		names map[string]string
		recv  ast.Node // a method's receiver, whose type is not a use of it
	}
	var bodies []body
	byObj := map[types.Object]*decl{}
	methodsOf := map[*types.TypeName][]*decl{}
	for _, dir := range order {
		sort.Strings(byDir[dir])
		var fs []*ast.File
		for _, rel := range byDir[dir] {
			fs = append(fs, files[rel])
		}
		conf := types.Config{Importer: imp, Error: func(error) {}, FakeImportC: true}
		pkg, _ := conf.Check(module+"/"+dir, fset, fs, info)
		imp[module+"/"+dir] = pkg
		for _, rel := range byDir[dir] {
			f := files[rel]
			names := importNames(f)
			at := func(n ast.Node) string { return rel + ":" + strconv.Itoa(fset.Position(n.Pos()).Line) }
			newDecl := func(key string, n ast.Node) *decl {
				for w.decls[key] != nil {
					key += "'"
				}
				d := &decl{key: key, at: at(n), refs: map[*decl]bool{}}
				w.decls[key] = d
				return d
			}
			for _, gd := range f.Decls {
				switch x := gd.(type) {
				case *ast.FuncDecl:
					key := dir + "." + x.Name.Name
					var recv ast.Node
					var recvType *types.TypeName
					obj, _ := info.Defs[x.Name].(*types.Func)
					if x.Recv != nil {
						recv = x.Recv
						if obj != nil {
							rt := obj.Type().(*types.Signature).Recv().Type()
							if p, ok := rt.(*types.Pointer); ok {
								rt = p.Elem()
							}
							if n, ok := types.Unalias(rt).(*types.Named); ok {
								recvType = n.Origin().Obj()
								key = dir + "." + recvType.Name() + "." + x.Name.Name
							}
						}
					} else if x.Name.Name == "init" {
						key += "@" + at(x) // each init is its own
					}
					d := newDecl(key, x)
					if obj != nil {
						byObj[obj] = d
					}
					if recvType != nil {
						methodsOf[recvType] = append(methodsOf[recvType], d)
					}
					if dn, ok := names[thisPkg]; ok && x.Body != nil && len(x.Body.List) > 0 {
						if es, ok := x.Body.List[0].(*ast.ExprStmt); ok {
							if c, ok := es.X.(*ast.CallExpr); ok && len(c.Args) == 0 {
								if s, ok := c.Fun.(*ast.SelectorExpr); ok {
									id, ok := s.X.(*ast.Ident)
									d.installs = ok && id.Name == dn && s.Sel.Name == "Install"
								}
							}
						}
					}
					bodies = append(bodies, body{d, x, names, recv})
				case *ast.GenDecl:
					for _, s := range x.Specs {
						vs, ok := s.(*ast.ValueSpec)
						if !ok {
							continue
						}
						for _, n := range vs.Names {
							d := newDecl(dir+"."+n.Name, n)
							if obj := info.Defs[n]; obj != nil {
								byObj[obj] = d
							}
							bodies = append(bodies, body{d, vs, names, nil})
						}
					}
				}
			}
		}
	}

	for _, b := range bodies {
		zen, hasZen := b.names[zenityPk]
		ast.Inspect(b.node, func(n ast.Node) bool {
			if n == b.recv {
				return false
			}
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && hasZen && id.Name == zen {
					if zenityWindows[x.Sel.Name] {
						b.d.dialogs = append(b.d.dialogs, "zenity."+x.Sel.Name+
							" (line "+strconv.Itoa(fset.Position(x.Pos()).Line)+")")
					}
					return false
				}
			case *ast.Ident:
				obj := info.Uses[x]
				if fn, ok := obj.(*types.Func); ok {
					obj = fn.Origin()
				}
				if d := byObj[obj]; d != nil {
					b.d.refs[d] = true
				}
				if tn, ok := obj.(*types.TypeName); ok {
					for _, m := range methodsOf[tn] {
						b.d.refs[m] = true
					}
				}
			}
			return true
		})
		delete(b.d.refs, b.d) // a function calling itself is not a way in
	}

	// Who names each declaration.
	namedBy := map[*decl][]*decl{}
	for _, d := range w.decls {
		for r := range d.refs {
			namedBy[r] = append(namedBy[r], d)
		}
	}
	for _, list := range namedBy {
		sort.Slice(list, func(i, j int) bool { return list[i].key < list[j].key })
	}
	keys := make([]string, 0, len(w.decls))
	for k := range w.decls {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d := w.decls[k]
		if len(d.dialogs) == 0 {
			continue
		}
		w.dialogs += len(d.dialogs)
		// Back from the dialog to every way in, stopping at an installing entry.
		from := map[*decl]*decl{d: nil}
		queue := []*decl{d}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if cur.installs {
				continue
			}
			if isListed[cur.key] {
				w.listed[cur.key] = true
				continue
			}
			if len(namedBy[cur]) == 0 {
				var chain []string
				for c := cur; c != nil; c = from[c] {
					chain = append(chain, c.key)
				}
				w.findings = append(w.findings, d.dialogs[0]+" in "+d.at+" is reached from "+cur.key+
					" ("+cur.at+"), which does not call dialogfront.Install first: "+strings.Join(chain, " → "))
				continue
			}
			for _, p := range namedBy[cur] {
				if _, seen := from[p]; !seen {
					from[p] = cur
					queue = append(queue, p)
				}
			}
		}
	}
	return w
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

// walkTree walks every non-test Go file under cmd/ and internal/ built for
// Windows (by its name and its //go:build line), where the dialogs went unseen.
func walkTree(t *testing.T, root string) *dialogWalk {
	t.Helper()
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = "windows", "amd64", false
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, base := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, base), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			if ok, err := ctx.MatchFile(filepath.Dir(p), filepath.Base(p)); err != nil || !ok {
				return err
			}
			f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			files[filepath.ToSlash(rel)] = f
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", base, err)
		}
	}
	return walkDialogs(fset, files)
}

// walkPlanted walks planted files, each given as "<slash path>": source.
func walkPlanted(t *testing.T, srcs map[string]string) *dialogWalk {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for rel, src := range srcs {
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse the planted %s: %v", rel, err)
		}
		files[rel] = f
	}
	return walkDialogs(fset, files)
}

// covers: MA-1111, R33
// Every dialog MailArchive opens on Windows is reached only through an entry that
// starts the bring-to-front watch first: a walk of every non-test Go file under
// cmd/ and internal/ built for Windows follows each zenity window (Question, Info,
// List, the file pickers, Progress, …, also taken as a value) back through every
// declaration that names it, and fails a way in that is not desktop.Launch or
// wizard.Run, which call dialogfront.Install as their first statement. Positive
// twin first (a planted dialog reached only through an installing entry passes;
// the real tree passes with Desktop's and the wizard's dialogs counted), then each
// planted way round it is caught.
func TestEveryDialogIsReachedOnlyThroughAnEntryThatBringsItToTheFront(t *testing.T) {
	const entry = `package desktop
import "mail-archive-tool/internal/dialogfront"
	func Launch() { dialogfront.Install(); serve() }
	func serve() { pick() }
`
	// Positive twin: a dialog behind an installing entry, through a function, a
	// variable holding a function, and a method of a type the entry's path makes.
	twin := walkPlanted(t, map[string]string{
		"internal/desktop/launch.go": entry,
		"internal/desktop/picker.go": `package desktop
import "github.com/ncruces/zenity"
var pick = func() { (&card{}).show() }
type card struct{}
	func (c *card) show() { _, _ = zenity.SelectFile(zenity.Directory()) }
`,
	})
	if len(twin.findings) != 0 || twin.dialogs != 1 {
		t.Errorf("positive twin: findings %v, dialogs %d (want none and 1)", twin.findings, twin.dialogs)
	}

	// The real tree.
	w := walkTree(t, moduleRoot(t))
	for _, f := range w.findings {
		t.Errorf("a dialog that may open behind the browser on Windows: %s", f)
	}
	for _, e := range installingEntries {
		if d := w.decls[e]; d == nil || !d.installs {
			t.Errorf("%s does not call dialogfront.Install as its first statement", e)
		}
	}
	for _, l := range waysInNotThroughInstall {
		if !w.listed[l.decl] {
			t.Errorf("listed way in %s reaches no dialog any more: remove it from the list (and close its gap's row)", l.decl)
		}
	}
	// Desktop's own dialogs and the wizard's are among those followed.
	perFile := map[string]int{}
	for _, d := range w.decls {
		perFile[d.at[:strings.Index(d.at, ":")]] += len(d.dialogs)
	}
	for _, file := range []string{"internal/desktop/dialogs.go", "internal/desktop/picker.go",
		"internal/desktop/encryption_restore.go", "internal/wizard/wizard.go"} {
		if perFile[file] == 0 {
			t.Errorf("the walk followed no dialog in %s", file)
		}
	}
	assure.Reached(t, w.dialogs, "zenity windows followed back to an installing entry")

	// Fail-closed twins: each way round the watch is caught.
	for name, srcs := range map[string]map[string]string{
		"an entry that does not install": {
			"internal/desktop/launch.go": `package desktop
import "github.com/ncruces/zenity"
	func Launch() { _ = zenity.Info("hello") }`,
		},
		"Install not the first statement": {
			"internal/desktop/launch.go": `package desktop
import ("github.com/ncruces/zenity"; "mail-archive-tool/internal/dialogfront")
	func Launch() { _ = zenity.Info("hello"); dialogfront.Install() }`,
		},
		"a second way in beside the entry": {
			"internal/desktop/launch.go": entry,
			"internal/desktop/picker.go": `package desktop
import "github.com/ncruces/zenity"
	func pick() { _ = zenity.Question("sure?") }
	func Elsewhere() { pick() }`,
		},
		"a dialog taken as a value": {
			"internal/wizard/ask.go": `package wizard
import zen "github.com/ncruces/zenity"
var ask = zen.Question
	func RunHeadless() { _ = ask("sure?") }`,
		},
		"a dialog in another package, called from a command-line verb": {
			"internal/other/other.go": `package other
import "github.com/ncruces/zenity"
	func Warn() { _ = zenity.Warning("careful") }`,
			"cmd/mailarchive/main.go": `package main
import "mail-archive-tool/internal/other"
	func main() { other.Warn() }`,
		},
		"a dialog in a method of a type made outside the entry": {
			"internal/desktop/launch.go": entry + "func pick() {}\n",
			"internal/desktop/card.go": `package desktop
import "github.com/ncruces/zenity"
type card struct{}
	func (card) show() { _ = zenity.Error("no") }
	func Make() any { return card{} }`,
		},
	} {
		if r := walkPlanted(t, srcs); len(r.findings) == 0 {
			t.Errorf("fail-closed twin %q was not caught", name)
		}
	}
}

// covers: MA-1115, S59
// OPEN GAP (found by MA-1111's walk, 3 November 2026; reported, not fixed here):
// `mailarchive resume -out DIR`, a command-line verb, restores the backup schedule
// through the wizard's repair (wizard.ResumeArchive → repairSchedule), which opens
// the "Repaired the scheduled backup … Continue to run an export now?" question,
// and the volatile-folder question first when the program runs from such a
// folder. S59 says no command-line verb shows a dialog (MA-624 does not probe
// resume). When the gap is closed, remove the skip and runResume's entry in
// waysInNotThroughInstall: the walk then finds no dialog through runResume.
func TestTheResumeVerbOpensNoDialog(t *testing.T) {
	t.Skip("open: the command-line resume verb reaches the wizard's repair questions (S59, MA-1115; reported with the dialog-front fix)")
	saved := waysInNotThroughInstall
	waysInNotThroughInstall = nil
	defer func() { waysInNotThroughInstall = saved }()
	w := walkTree(t, moduleRoot(t))
	for _, f := range w.findings {
		if strings.Contains(f, "cmd/mailarchive.runResume") {
			t.Errorf("the resume verb opens a dialog: %s", f)
		}
	}
	assure.Reached(t, w.dialogs, "zenity windows followed")
}
