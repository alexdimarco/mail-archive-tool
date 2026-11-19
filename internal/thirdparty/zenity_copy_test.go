package thirdparty

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/util"
)

// The copy of github.com/ncruces/zenity in third_party/zenity, and how MA-1468 checks it
// (docs/design-windows-dialog-fit.md §4.1, §5).
const (
	zenityModule  = "github.com/ncruces/zenity"
	zenityVersion = "v0.10.12"
	// zenityH1 is the published module's hash: go.sum's line for it before the copy
	// (go.sum at 17d730b), which `go mod verify` checked against the module cache on
	// 2026-11-18.
	zenityH1 = "h1:o4SErDa0kQijlqG6W4OYYzO6kA0fGu34uegvJGcMLBI="
	copyRel  = "third_party/zenity"
	pinsRel  = "testdata/zenity.pins" // relative to this package
	// replaceLine is go.mod's one replace for zenity, without its "replace " keyword.
	replaceLine = "github.com/ncruces/zenity v0.10.12 => ./third_party/zenity"
	// textLine is .gitattributes' rule that keeps a checkout from rewriting the copy.
	textLine = "third_party/zenity/** -text"
	// generatedFile is the one patch file allowed new package-level variables set by a
	// call: the generator's NewProc lines, which load nothing until first used (§4.2).
	generatedFile = "internal/win/zsyscall_windows.go"
)

var (
	zenityRecord = flag.Bool("zenity-record", false,
		"write testdata/zenity.pins from the Go module cache (an operator-accepted act, MA-1468)")
	zenityLab = flag.String("zenity-lab", "",
		`with -zenity-record: the lab run each patch file last passed, for example "MA-1470 2026-11-20 Windows 11 26100 150% pass"`)
)

// additions are the files in the copy that are not upstream's (design §2 P4). UPSTREAM.md
// must be there; fit.go and its tests come with the layout arithmetic.
var additions = map[string]bool{"UPSTREAM.md": true, "fit.go": true, "fit_test.go": true}

// otherOSes are the systems no patch file may build for (design §2 P3).
var otherOSes = []string{"darwin", "linux", "freebsd", "netbsd", "openbsd", "dragonfly"}

// labRe is a patch file's lab record: the row that proves it, then the run, ending in its
// result, which must read pass.
var labRe = regexp.MustCompile(`^MA-\d+ .+ pass$`)

type pinFile struct{ sha, state, path string } // state: copied, patched or left-out

type patchRecord struct {
	path, ours, lab                                string
	upstreamImports, addsImports, upstreamCallVars []string
}

type pins struct {
	header  map[string][]string
	files   []pinFile
	patches map[string]patchRecord
}

// leftOutByRule is the design's rule for the upstream files the copy leaves out:
// every _test.go, cmd/, .github/ and testdata/ (§4.1).
func leftOutByRule(rel string) bool {
	return strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "cmd/") ||
		strings.HasPrefix(rel, ".github/") || strings.HasPrefix(rel, "testdata/")
}

// neverSource reports the names MA-1468's walk ignores, so a developer's machine stays
// green: OS litter and Go's own leftovers, none of which is ever compiled (§5).
func neverSource(name string) bool {
	if util.IsLitter(name) {
		return true
	}
	for _, suffix := range []string{".test", ".test.exe", ".out", ".prof"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// moduleHash is the go command's h1: hash of a module (golang.org/x/mod/sumdb/dirhash's
// Hash1): one "sha256  module@version/path" line per file, in file-path order, hashed with
// SHA-256 and written in base64.
func moduleHash(files []pinFile) string {
	names := make([]string, 0, len(files))
	sums := map[string]string{}
	for _, f := range files {
		name := zenityModule + "@" + zenityVersion + "/" + f.path
		names = append(names, name)
		sums[name] = f.sha
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		fmt.Fprintf(h, "%s  %s\n", sums[name], name)
	}
	return "h1:" + base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// listFiles reads every file under dir, keyed by its slash path relative to dir, skipping
// the names skip reports. Anything that is not a regular file is an error.
func listFiles(dir string, skip func(string) bool) (map[string][]byte, error) {
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if skip != nil && skip(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", filepath.ToSlash(rel))
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = data
		return nil
	})
	return out, err
}

func splitList(v string) []string {
	if v == "" || v == "-" {
		return nil
	}
	return strings.Split(v, ",")
}

func joinList(v []string) string {
	if len(v) == 0 {
		return "-"
	}
	s := slices.Clone(v)
	sort.Strings(s)
	return strings.Join(s, ",")
}

func parsePins(data []byte) (pins, error) {
	p := pins{header: map[string][]string{}, patches: map[string]patchRecord{}}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rest, ok := strings.Cut(line, ": ")
		if !ok {
			return p, fmt.Errorf("pins line %d has no key: %q", i+1, line)
		}
		switch key {
		case "file":
			f := strings.SplitN(rest, " ", 3)
			if len(f) != 3 || len(f[0]) != 64 {
				return p, fmt.Errorf("pins line %d is not \"file: <sha256> <state> <path>\": %q", i+1, line)
			}
			switch f[1] {
			case "copied", "patched", "left-out":
			default:
				return p, fmt.Errorf("pins line %d has state %q, not copied, patched or left-out", i+1, f[1])
			}
			p.files = append(p.files, pinFile{sha: f[0], state: f[1], path: f[2]})
		case "patch":
			rec, err := parsePatch(rest)
			if err != nil {
				return p, fmt.Errorf("pins line %d: %v", i+1, err)
			}
			p.patches[rec.path] = rec
		default:
			p.header[key] = append(p.header[key], rest)
		}
	}
	return p, nil
}

// parsePatch reads "<path> ours=<sha256> upstream-imports=<a,b> adds-imports=<c>
// upstream-call-vars=<x,y> lab=<the lab run, to the end of the line>"; "-" is an empty list.
func parsePatch(rest string) (patchRecord, error) {
	var r patchRecord
	before, lab, ok := strings.Cut(rest, " lab=")
	if !ok {
		return r, fmt.Errorf("patch record has no lab=: %q", rest)
	}
	r.lab = lab
	f := strings.Fields(before)
	if len(f) == 0 {
		return r, fmt.Errorf("patch record has no path: %q", rest)
	}
	r.path = f[0]
	for _, kv := range f[1:] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return r, fmt.Errorf("patch record field %q is not key=value", kv)
		}
		switch k {
		case "ours":
			r.ours = v
		case "upstream-imports":
			r.upstreamImports = splitList(v)
		case "adds-imports":
			r.addsImports = splitList(v)
		case "upstream-call-vars":
			r.upstreamCallVars = splitList(v)
		default:
			return r, fmt.Errorf("patch record has an unknown field %q", k)
		}
	}
	if len(r.ours) != 64 {
		return r, fmt.Errorf("patch record for %s has no ours=<sha256>", r.path)
	}
	return r, nil
}

func formatPatch(r patchRecord) string {
	return fmt.Sprintf("patch: %s ours=%s upstream-imports=%s adds-imports=%s upstream-call-vars=%s lab=%s",
		r.path, r.ours, joinList(r.upstreamImports), joinList(r.addsImports), joinList(r.upstreamCallVars), r.lab)
}

// goFacts are what MA-1468 pins about a Go file of the copy: its imports, its
// package-level variables set by a call, and whether it has an init function.
type goFacts struct {
	imports, callVars []string
	hasInit           bool
}

// factsOf reads a Go file's facts. In the generated syscall file, the generator's
// NewLazySystemDLL and NewProc lines are not counted (§4.2).
func factsOf(name string, src []byte) (goFacts, error) {
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
	if err != nil {
		return goFacts{}, err
	}
	var g goFacts
	for _, im := range f.Imports {
		p, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			return goFacts{}, err
		}
		g.imports = append(g.imports, p)
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "init" {
				g.hasInit = true
			}
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, s := range d.Specs {
				vs := s.(*ast.ValueSpec)
				for i, v := range vs.Values {
					if !containsCall(v) || (name == generatedFile && generatorLine(v)) {
						continue
					}
					if len(vs.Values) == 1 && len(vs.Names) > 1 { // var a, b = f()
						for _, n := range vs.Names {
							g.callVars = append(g.callVars, n.Name)
						}
						continue
					}
					g.callVars = append(g.callVars, vs.Names[i].Name)
				}
			}
		}
	}
	sort.Strings(g.imports)
	sort.Strings(g.callVars)
	return g, nil
}

// containsCall reports a call in an initializer, outside any function literal (a
// function literal's body runs only when it is called).
func containsCall(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			found = true
		}
		return !found
	})
	return found
}

// generatorLine reports `x.NewProc("…")` and `windows.NewLazySystemDLL("…")`.
func generatorLine(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && (sel.Sel.Name == "NewProc" || sel.Sel.Name == "NewLazySystemDLL")
}

// buildsFor reports whether the file builds for goos (on amd64, without cgo).
func buildsFor(path, goos string) bool {
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = goos, "amd64", false
	ok, err := ctx.MatchFile(filepath.Dir(path), filepath.Base(path))
	return err == nil && ok
}

// crNote says when data differs from the wanted hash only by carriage returns.
func crNote(data []byte, want string) string {
	if bytes.Contains(data, []byte("\r\n")) && sha(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))) == want {
		return " only by carriage returns: a checkout rewrote its line endings (see .gitattributes)"
	}
	return ""
}

func hasLine(data []byte, want string) bool {
	for _, l := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(strings.TrimRight(l, "\r")) == want {
			return true
		}
	}
	return false
}

var requireRe = regexp.MustCompile(`(?m)^\s*(?:require\s+)?github\.com/ncruces/zenity v0\.10\.12\s*(?://.*)?\r?$`)

// checkCopy is MA-1468: it returns every way the repository at root breaks the pins.
func checkCopy(root string, p pins) []string {
	var probs []string
	add := func(format string, a ...any) { probs = append(probs, fmt.Sprintf(format, a...)) }

	if gomod, err := os.ReadFile(filepath.Join(root, "go.mod")); err != nil {
		add("go.mod: %v", err)
	} else {
		if !requireRe.Match(gomod) {
			add("go.mod does not require %s %s: moving the version is the operator's move procedure (UPSTREAM.md)", zenityModule, zenityVersion)
		}
		var reps []string
		for _, l := range strings.Split(string(gomod), "\n") {
			l = strings.TrimSpace(strings.TrimRight(l, "\r"))
			if strings.Contains(l, zenityModule) && strings.Contains(l, "=>") {
				reps = append(reps, strings.TrimSpace(strings.TrimPrefix(l, "replace ")))
			}
		}
		if len(reps) != 1 || reps[0] != replaceLine {
			add("go.mod's replace lines for zenity are %q; want exactly one: replace %s", reps, replaceLine)
		}
	}
	if ga, err := os.ReadFile(filepath.Join(root, ".gitattributes")); err != nil || !hasLine(ga, textLine) {
		add(".gitattributes has no line %q, so a checkout could rewrite the copy's line endings", textLine)
	}

	if got := moduleHash(p.files); got != zenityH1 {
		add("the pins' upstream lines hash to %s, not the published module's %s", got, zenityH1)
	}
	if h := p.header["h1"]; len(h) != 1 || h[0] != zenityH1 {
		add("the pins' header says h1 %q; want %s", h, zenityH1)
	}
	if m := p.header["module"]; len(m) != 1 || m[0] != zenityModule+" "+zenityVersion {
		add("the pins' header names module %q; want %s %s", m, zenityModule, zenityVersion)
	}

	copyDir := filepath.Join(root, filepath.FromSlash(copyRel))
	have, err := listFiles(copyDir, neverSource)
	if err != nil {
		add("reading the copy: %v", err)
		return probs
	}
	pinned := map[string]pinFile{}
	for _, f := range p.files {
		pinned[f.path] = f
		data, present := have[f.path]
		switch f.state {
		case "left-out":
			if !leftOutByRule(f.path) {
				add("%s is pinned as left out, but the design leaves out only _test.go files, cmd/, .github/ and testdata/", f.path)
			}
			if present {
				add("%s is in the copy, but the design leaves it out", f.path)
			}
		case "copied":
			if leftOutByRule(f.path) {
				add("%s is pinned as copied, but the design leaves it out", f.path)
			}
			if !present {
				add("%s is missing from the copy", f.path)
			} else if sha(data) != f.sha {
				add("%s differs from the published module%s", f.path, crNote(data, f.sha))
			}
		case "patched":
			rec, ok := p.patches[f.path]
			switch {
			case !present:
				add("patch file %s is missing from the copy", f.path)
			case !ok:
				add("patch file %s has no record", f.path)
			default:
				probs = append(probs, checkPatchFile(copyDir, f.path, data, rec)...)
			}
		}
	}
	for path := range p.patches {
		if pinned[path].state != "patched" {
			add("the pins record a patch to %s but do not mark it patched", path)
		}
	}
	paths := make([]string, 0, len(have))
	for path := range have {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if _, ok := pinned[path]; ok {
			continue
		}
		if !additions[path] {
			add("%s is in the copy but is neither upstream's nor one of the design's additions (UPSTREAM.md, fit.go, fit_test.go)", path)
			continue
		}
		if path == "fit.go" {
			probs = append(probs, checkFitGo(filepath.Join(copyDir, "fit.go"), have[path])...)
		}
	}
	if _, ok := have["UPSTREAM.md"]; !ok {
		add("UPSTREAM.md is missing from the copy: it says where the copy came from (design §4.1)")
	}
	return probs
}

// checkPatchFile holds a patch file to its record and to the design's rules (§2 P3, §4.2).
func checkPatchFile(copyDir, rel string, data []byte, rec patchRecord) []string {
	var probs []string
	add := func(format string, a ...any) { probs = append(probs, fmt.Sprintf(format, a...)) }
	if s := sha(data); s != rec.ours {
		add("patch file %s changed since its record (sha256 %s, recorded %s): record it again, with the lab run it passed%s",
			rel, s, rec.ours, crNote(data, rec.ours))
	}
	path := filepath.Join(copyDir, filepath.FromSlash(rel))
	if !buildsFor(path, "windows") {
		add("patch file %s does not build for Windows", rel)
	}
	for _, goos := range otherOSes {
		if buildsFor(path, goos) {
			add("patch file %s builds for %s: patch files build for Windows only (P3)", rel, goos)
		}
	}
	facts, err := factsOf(rel, data)
	if err != nil {
		add("patch file %s does not parse: %v", rel, err)
		return probs
	}
	want := append(slices.Clone(rec.upstreamImports), rec.addsImports...)
	sort.Strings(want)
	want = slices.Compact(want)
	if !slices.Equal(facts.imports, want) {
		add("patch file %s imports %v; its record allows %v (upstream's, plus %v)", rel, facts.imports, want, rec.addsImports)
	}
	if facts.hasInit {
		add("patch file %s declares an init function (design §4.2)", rel)
	}
	for _, v := range facts.callVars {
		if !slices.Contains(rec.upstreamCallVars, v) {
			add("patch file %s adds package-level variable %s, set by a call (design §4.2)", rel, v)
		}
	}
	if !labRe.MatchString(rec.lab) {
		add("patch file %s's record names no passing lab run: %q", rel, rec.lab)
	}
	return probs
}

// checkFitGo holds fit.go to §4.2: pure arithmetic, built everywhere, no imports, no
// init, no package-level variable set by a call.
func checkFitGo(path string, data []byte) []string {
	var probs []string
	facts, err := factsOf("fit.go", data)
	if err != nil {
		return []string{fmt.Sprintf("fit.go does not parse: %v", err)}
	}
	if len(facts.imports) > 0 {
		probs = append(probs, fmt.Sprintf("fit.go imports %v: it is pure arithmetic with no imports (design §4.2)", facts.imports))
	}
	if facts.hasInit {
		probs = append(probs, "fit.go declares an init function (design §4.2)")
	}
	if len(facts.callVars) > 0 {
		probs = append(probs, fmt.Sprintf("fit.go has package-level variables set by a call: %v (design §4.2)", facts.callVars))
	}
	for _, goos := range []string{"windows", "darwin", "linux"} {
		if !buildsFor(path, goos) {
			probs = append(probs, fmt.Sprintf("fit.go does not build for %s; it is built everywhere (design §4.2)", goos))
		}
	}
	return probs
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at the repository root %s: %v", root, err)
	}
	return root
}

func readPins(t *testing.T) pins {
	t.Helper()
	data, err := os.ReadFile(pinsRel)
	if err != nil {
		t.Fatalf("the pins: %v (written by: go test ./internal/thirdparty -run TestRecordZenityPins -zenity-record)", err)
	}
	p, err := parsePins(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"recorded", "toolchain", "module", "h1"} {
		if v := p.header[key]; len(v) != 1 || strings.TrimSpace(v[0]) == "" {
			t.Errorf("the pins' provenance header has no %q line", key)
		}
	}
	if len(p.header["go.sum"]) != 2 {
		t.Errorf("the pins' provenance header holds %d go.sum lines; want zenity's two", len(p.header["go.sum"]))
	}
	return p
}

func goTool(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	p := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("no go command (PATH, or %s): %v", p, err)
	}
	return p
}

// covers: MA-1468, S86
// The copy of zenity is the published v0.10.12 module, file by file (design §2 P4): every
// copied file's sha256 is its pin and the pins reproduce the module's h1: hash; the files
// the design leaves out are absent and only its additions are added; go.mod replaces
// exactly v0.10.12 with the copy; .gitattributes keeps a checkout from rewriting it; and
// each patch file matches its record, builds for Windows only, imports what its record
// allows, and adds no init function and no variable set by a call.
func TestTheZenityCopyIsThePublishedModule(t *testing.T) {
	p := readPins(t)
	for _, prob := range checkCopy(repoRoot(t), p) {
		t.Error(prob)
	}
}

// refusal is one change MA-1468 must refuse: change applies it to a scratch repository
// and returns the pins to check it against; want is part of the refusal's message.
type refusal struct {
	name, want string
	change     func(t *testing.T, dir string) pins
}

// scratchRepo copies what MA-1468 reads (go.mod, .gitattributes and the copy) into a new
// directory, so a test can change it.
func scratchRepo(t *testing.T, root string) string {
	t.Helper()
	dst := t.TempDir()
	for _, name := range []string{"go.mod", ".gitattributes"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := listFiles(filepath.Join(root, filepath.FromSlash(copyRel)), neverSource)
	if err != nil {
		t.Fatal(err)
	}
	for rel, data := range files {
		out := filepath.Join(dst, filepath.FromSlash(copyRel), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

func editFile(t *testing.T, path string, edit func([]byte) []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, edit(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// asPatched returns pins that mark rel as a patch file whose record matches its current
// content in the scratch copy, as the recorder would write it after a lab run.
func asPatched(t *testing.T, p pins, scratch, rel, upstreamSrc string) pins {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(scratch, filepath.FromSlash(copyRel), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	up, err := factsOf(rel, []byte(upstreamSrc))
	if err != nil {
		t.Fatal(err)
	}
	q := pins{header: p.header, patches: map[string]patchRecord{}}
	for _, f := range p.files {
		if f.path == rel {
			f.state = "patched"
		}
		q.files = append(q.files, f)
	}
	for k, v := range p.patches {
		q.patches[k] = v
	}
	q.patches[rel] = patchRecord{path: rel, ours: sha(data), upstreamImports: up.imports,
		upstreamCallVars: up.callVars, lab: "MA-1470 2026-11-19 a scratch run pass"}
	return q
}

// covers: MA-1468
// MA-1468's check refuses what it must (positive twin first): a scratch repository made
// from the real copy passes, and each change below turns it red with a message that names
// what changed, whether a copied file, the line endings, an extra or a left-out file, the
// go.mod replace, the .gitattributes rule, a patch file and its record, its imports, its
// init function, its variables, its build target and its lab run, or fit.go's rules.
func TestTheZenityCopyCheckRefusesEachChange(t *testing.T) {
	root := repoRoot(t)
	p := readPins(t)
	if probs := checkCopy(scratchRepo(t, root), p); len(probs) != 0 {
		t.Fatalf("positive twin: a scratch copy of the repository should pass, got %q", probs)
	}
	copied := func(rel string) string { return filepath.Join(copyRel, filepath.FromSlash(rel)) }
	cases := []refusal{
		{"a byte of a copied file", "LICENSE differs from the published module", func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, copied("LICENSE")), func(b []byte) []byte { b[0] ^= 1; return b })
			return p
		}},
		{"line endings", "list.go differs from the published module only by carriage returns", func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, copied("list.go")), func(b []byte) []byte {
				return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
			})
			return p
		}},
		{"an extra file", "extra.go is in the copy but is neither upstream's", func(t *testing.T, dir string) pins {
			if err := os.WriteFile(filepath.Join(dir, copied("extra.go")), []byte("package zenity\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"a left-out file", "list_test.go is in the copy, but the design leaves it out", func(t *testing.T, dir string) pins {
			if err := os.WriteFile(filepath.Join(dir, copied("list_test.go")), []byte("package zenity\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"a missing file", "msg_windows.go is missing from the copy", func(t *testing.T, dir string) pins {
			if err := os.Remove(filepath.Join(dir, copied("msg_windows.go"))); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"no UPSTREAM.md", "UPSTREAM.md is missing from the copy", func(t *testing.T, dir string) pins {
			if err := os.Remove(filepath.Join(dir, copied("UPSTREAM.md"))); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{"the replace", "go.mod's replace lines for zenity", func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, "go.mod"), func(b []byte) []byte {
				return bytes.ReplaceAll(b, []byte("replace "+replaceLine), nil)
			})
			return p
		}},
		{"the version", "go.mod does not require github.com/ncruces/zenity v0.10.12", func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, "go.mod"), func(b []byte) []byte {
				return regexp.MustCompile(`(?m)^(\s*github\.com/ncruces/zenity) v0\.10\.12`).ReplaceAll(b, []byte("$1 v0.10.15"))
			})
			return p
		}},
		{"the -text rule", ".gitattributes has no line", func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, ".gitattributes"), func(b []byte) []byte {
				return bytes.ReplaceAll(b, []byte(textLine), nil)
			})
			return p
		}},
		{"a pin", "the pins' upstream lines hash to", func(t *testing.T, dir string) pins {
			q := pins{header: p.header, patches: p.patches, files: slices.Clone(p.files)}
			q.files[0].sha = strings.Repeat("0", 64)
			return q
		}},
	}

	// A patch file, simulated on a file the design never patches: date_windows.go given one
	// more line and recorded as the recorder would; the record must hold it, as the positive twin shows, and each change
	// after it must not pass.
	upstream, err := os.ReadFile(filepath.Join(root, copied("date_windows.go")))
	if err != nil {
		t.Fatal(err)
	}
	patched := func(t *testing.T, dir string, extra string) pins {
		editFile(t, filepath.Join(dir, copied("date_windows.go")), func(b []byte) []byte { return append(b, extra...) })
		return asPatched(t, p, dir, "date_windows.go", string(upstream))
	}
	dir := scratchRepo(t, root)
	q := patched(t, dir, "\n// a patch\n")
	if probs := checkCopy(dir, q); len(probs) != 0 {
		t.Fatalf("positive twin: a patch file with its record should pass, got %q", probs)
	}
	if probs := checkCopy(dir, p); len(probs) == 0 {
		t.Fatal("a changed date_windows.go still pinned as copied should not pass")
	}
	cases = append(cases,
		refusal{"a patch file after its record", "patch file date_windows.go changed since its record", func(t *testing.T, dir string) pins {
			q := patched(t, dir, "\n// a patch\n")
			editFile(t, filepath.Join(dir, copied("date_windows.go")), func(b []byte) []byte { return append(b, "// later\n"...) })
			return q
		}},
		refusal{"a patch file's init function", "patch file date_windows.go declares an init function", func(t *testing.T, dir string) pins {
			return patched(t, dir, "\nfunc init() {}\n")
		}},
		refusal{"a patch file's variable set by a call", "patch file date_windows.go adds package-level variable zzz, set by a call", func(t *testing.T, dir string) pins {
			return patched(t, dir, "\nvar zzz = len(\"x\")\n")
		}},
		refusal{"a patch file's lab run", "patch file date_windows.go's record names no passing lab run", func(t *testing.T, dir string) pins {
			q := patched(t, dir, "\n// a patch\n")
			rec := q.patches["date_windows.go"]
			rec.lab = "MA-1470 2026-11-19 a scratch run fail"
			q.patches["date_windows.go"] = rec
			return q
		}},
		refusal{"a patch file's imports", "patch file date_windows.go imports", func(t *testing.T, dir string) pins {
			q := patched(t, dir, "\n// a patch\n")
			rec := q.patches["date_windows.go"]
			rec.upstreamImports = rec.upstreamImports[1:]
			q.patches["date_windows.go"] = rec
			return q
		}},
		refusal{"a patch file built for Linux", "patch file list.go builds for linux", func(t *testing.T, dir string) pins {
			src, err := os.ReadFile(filepath.Join(dir, copied("list.go")))
			if err != nil {
				t.Fatal(err)
			}
			editFile(t, filepath.Join(dir, copied("list.go")), func(b []byte) []byte { return append(b, "\n// a patch\n"...) })
			return asPatched(t, p, dir, "list.go", string(src))
		}},
		refusal{"fit.go's imports", "fit.go imports [fmt]", func(t *testing.T, dir string) pins {
			if err := os.WriteFile(filepath.Join(dir, copied("fit.go")), []byte("package zenity\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		refusal{"fit.go built for one system", "fit.go does not build for darwin", func(t *testing.T, dir string) pins {
			if err := os.WriteFile(filepath.Join(dir, copied("fit.go")), []byte("//go:build windows\n\npackage zenity\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}},
	)

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := scratchRepo(t, root)
			probs := checkCopy(dir, c.change(t, dir))
			if !slices.ContainsFunc(probs, func(s string) bool { return strings.Contains(s, c.want) }) {
				t.Errorf("want a refusal containing %q, got %q", c.want, probs)
			}
		})
	}
}

// vetLineRe is one finding of go vet: "path.go:line:col: message".
var vetLineRe = regexp.MustCompile(`^(?:\./)?(\S+\.go):(\d+):\d+: (.+)$`)

// enclosingFunc names the function of the file at path whose body holds line.
func enclosingFunc(path string, line int) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return ""
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fset.Position(fd.Pos()).Line <= line && line <= fset.Position(fd.End()).Line {
			return fd.Name.Name
		}
	}
	return ""
}

func runGo(t *testing.T, dir string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(goTool(t), append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if _, exit := err.(*exec.ExitError); err != nil && !exit {
		t.Fatalf("go %v in %s did not run: %v", args, dir, err)
	}
	return out.String(), err
}

// covers: MA-1468, MA-1469
// The copy's own tests pass and its vet is clean, run from the main module: the
// repository's unfiltered `go test ./...` never enters a nested module, so this test runs
// `go test ./...` and `go vet ./...` in third_party/zenity, for GOOS windows, darwin and
// linux, with -mod=readonly so nothing writes into the copy (design §5). The one finding
// allowed is upstream's in SHBrowseForFolder (internal/win/zsyscall_windows.go), a
// generated function the patch does not change; on Windows it must still be there, so the
// exception cannot quietly cover anything else.
func TestTheZenityCopyTestsAndVetsClean(t *testing.T) {
	copyDir := filepath.Join(repoRoot(t), filepath.FromSlash(copyRel))
	base := []string{"GOFLAGS=-mod=readonly", "GOWORK=off"}

	if out, err := runGo(t, copyDir, base, "test", "./..."); err != nil {
		t.Errorf("go test ./... in the copy failed: %v\n%s", err, out)
	}

	for _, goos := range []string{"windows", "darwin", "linux"} {
		env := append(slices.Clone(base), "GOOS="+goos, "GOARCH=amd64", "CGO_ENABLED=0")
		out, _ := runGo(t, copyDir, env, "vet", "./...")
		allowed := 0
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "go: downloading ") {
				continue
			}
			if m := vetLineRe.FindStringSubmatch(line); m != nil && m[1] == generatedFile &&
				m[3] == "possible misuse of unsafe.Pointer" {
				n, _ := strconv.Atoi(m[2])
				if enclosingFunc(filepath.Join(copyDir, filepath.FromSlash(m[1])), n) == "SHBrowseForFolder" {
					allowed++
					continue
				}
			}
			t.Errorf("go vet (GOOS=%s) in the copy: %s", goos, line)
		}
		want := 0
		if goos == "windows" {
			want = 1
		}
		if allowed != want {
			t.Errorf("go vet (GOOS=%s) reported upstream's SHBrowseForFolder finding %d times; want %d: the exception covers exactly that one finding", goos, allowed, want)
		}
	}
}

// covers: MA-1468
// The recorder for testdata/zenity.pins. It writes only when -zenity-record is given:
// changing an upstream line is an operator decision (as for MA-331's pins), and a patch
// file's record is written in the same change as the patch file, with the lab run it
// passed (-zenity-lab). It reads v0.10.12 from the Go module cache and refuses to write
// unless its files reproduce the published module's h1: hash.
func TestRecordZenityPins(t *testing.T) {
	if !*zenityRecord {
		t.Skip("writes testdata/zenity.pins only when -zenity-record is given (an operator-accepted act, MA-1468)")
	}
	root := repoRoot(t)
	cmd := exec.Command(goTool(t), "mod", "download", "-json", zenityModule+"@"+zenityVersion)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go mod download: %v", err)
	}
	var mod struct{ Dir, Sum, GoModSum string }
	if err := json.Unmarshal(out, &mod); err != nil || mod.Dir == "" {
		t.Fatalf("go mod download gave no module folder: %v\n%s", err, out)
	}
	upstream, err := listFiles(mod.Dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	var files []pinFile
	for path, data := range upstream {
		files = append(files, pinFile{sha: sha(data), path: path})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	if got := moduleHash(files); got != zenityH1 || mod.Sum != zenityH1 {
		t.Fatalf("the module cache's v0.10.12 hashes to %s (go says %s); the published module is %s: not recording", got, mod.Sum, zenityH1)
	}

	have, err := listFiles(filepath.Join(root, filepath.FromSlash(copyRel)), neverSource)
	if err != nil {
		t.Fatal(err)
	}
	var patches []string
	for i, f := range files {
		data, present := have[f.path]
		switch {
		case leftOutByRule(f.path):
			if present {
				t.Fatalf("the copy holds %s, which the design leaves out", f.path)
			}
			files[i].state = "left-out"
		case !present:
			t.Fatalf("the copy lacks upstream's %s", f.path)
		case sha(data) == f.sha:
			files[i].state = "copied"
		default:
			if *zenityLab == "" || !labRe.MatchString(*zenityLab) {
				t.Fatalf("%s is patched: give -zenity-lab with the passing lab run it was checked by (got %q)", f.path, *zenityLab)
			}
			up, err := factsOf(f.path, upstream[f.path])
			if err != nil {
				t.Fatal(err)
			}
			ours, err := factsOf(f.path, data)
			if err != nil {
				t.Fatal(err)
			}
			var adds []string
			for _, im := range ours.imports {
				if !slices.Contains(up.imports, im) {
					adds = append(adds, im)
				}
			}
			files[i].state = "patched"
			patches = append(patches, formatPatch(patchRecord{path: f.path, ours: sha(data), lab: *zenityLab,
				upstreamImports: up.imports, addsImports: adds, upstreamCallVars: up.callVars}))
		}
	}
	for path := range have {
		if _, ok := upstream[path]; !ok && !additions[path] {
			t.Fatalf("the copy holds %s, which is neither upstream's nor one of the design's additions", path)
		}
	}

	var b strings.Builder
	b.WriteString("# zenity.pins: the published github.com/ncruces/zenity v0.10.12, file by file, and the\n")
	b.WriteString("# state of its copy in third_party/zenity (docs/design-windows-dialog-fit.md §4.1, MA-1468).\n")
	b.WriteString("# Written only by: go test ./internal/thirdparty -run TestRecordZenityPins -zenity-record\n")
	b.WriteString("# Changing an upstream line is an operator decision, as for MA-331's pins. A patch\n")
	b.WriteString("# file's record changes with the patch file, naming the lab run it passed.\n")
	fmt.Fprintf(&b, "recorded: %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(&b, "toolchain: %s\n", runtime.Version())
	fmt.Fprintf(&b, "module: %s %s\n", zenityModule, zenityVersion)
	fmt.Fprintf(&b, "h1: %s\n", mod.Sum)
	fmt.Fprintf(&b, "go.sum: %s %s %s\n", zenityModule, zenityVersion, mod.Sum)
	fmt.Fprintf(&b, "go.sum: %s %s/go.mod %s\n", zenityModule, zenityVersion, mod.GoModSum)
	for _, f := range files {
		fmt.Fprintf(&b, "file: %s %s %s\n", f.sha, f.state, f.path)
	}
	for _, line := range patches {
		b.WriteString(line + "\n")
	}
	if err := os.MkdirAll(filepath.Dir(pinsRel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pinsRel, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s: %d files, %d patched", pinsRel, len(files), len(patches))
}
