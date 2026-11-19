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
	"maps"
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
	zenityLabReceipt = flag.String("zenity-lab-receipt", "",
		"with -zenity-record: the receipt MA-1470 wrote on the test PC after every lab test passed (ma1470-receipt.txt)")
	zenityLabHash = flag.Bool("zenity-lab-hash", false,
		"print the copy's lab-copy hash, which the MA-1470 lab build passes with -ldflags -X (UPSTREAM.md)")
	zenityLabBuild = flag.String("zenity-lab-build", "",
		"with -zenity-lab-hash: build the MA-1470 lab program for Windows at this path with the hash, refusing if the copy or the lab test changed meanwhile")
)

// additions are the files in the copy that are not upstream's (design §2 P4): UPSTREAM.md,
// fit.go (the layout arithmetic) and fit_test.go (its tests, MA-1469). All three must be
// there.
var additions = map[string]bool{"UPSTREAM.md": true, "fit.go": true, "fit_test.go": true}

// otherOSes are the systems no patch file may build for (design §2 P3).
var otherOSes = []string{"darwin", "linux", "freebsd", "netbsd", "openbsd", "dragonfly"}

// labRe is a patch file's lab record: the run line of the receipt MA-1470 writes, ending in
// its result, which must read pass.
var labRe = regexp.MustCompile(`^MA-1470 .+ pass$`)

// patchFacts are what MA-1468 knows, in its own code, about an upstream file the design
// patches: its v0.10.12 original's sha256, the packages that original imports, its
// package-level variables set by a call (the generator's lines in the syscall file not
// counted), and the imports a patch may add; "-" is an empty list.
type patchFacts struct{ upstreamSHA, imports, callVars, adds string }

// patchable are the upstream files the design patches (§2, §4.2), read from the published
// module on 2026-11-19. Each patch record must agree with this table, so a record cannot
// vouch for itself (adversarial finding INS-04): patching another file, or a patch file
// importing another package, is a change to this table, made in review.
var patchable = map[string]patchFacts{
	"entry_windows.go":                 {"717354c9e2e0507bd765760667d8af785af6285339822630b42b8c4a799b9ae8", "github.com/ncruces/zenity/internal/win,syscall,unsafe", "-", "-"},
	"list_windows.go":                  {"893189f95ee5ef030b3174c8b62fa25f77aabf647924372e3e75dfd868ae08e5", "github.com/ncruces/zenity/internal/win,syscall,unsafe", "-", "-"},
	"msg_windows.go":                   {"2294b9b95bade3b8be0252683ce5307373fd6b294fdb56075afbdddfda741aff", "context,github.com/ncruces/zenity/internal/win", "-", "-"},
	"progress_windows.go":              {"9af7223685510b917df8b1f6c81bb8fd9f0beaecfbe048a7cfd1dba5763d00e9", "context,github.com/ncruces/zenity/internal/win,sync,syscall,unsafe", "-", "-"},
	"util_windows.go":                  {"3f1047e751aad8b2bcfe51cfc8fda3bd2af9bce3c876efff42f3d7783632b70f", "bytes,context,github.com/ncruces/zenity/internal/win,io,os,path/filepath,runtime,strconv,sync,sync/atomic,syscall,unsafe", "-", "-"},
	"internal/win/gdi32.go":            {"4406fee46830dc39c91256f275d6d7d2abaefea7b151f74091cc4aaf2f7a54e4", "-", "-", "-"},
	"internal/win/user32.go":           {"2f28f84f08c1fe342741caf9d3b7666d3583ac84a53f75c04d66edba331f16ba", "golang.org/x/sys/windows,syscall,unsafe", "-", "-"},
	"internal/win/zsyscall_windows.go": {"0c31ca6212db768aa768d751849fc6483beb80e7487567907f9d9801a6c6fcf4", "golang.org/x/sys/windows,syscall,unsafe", "errERROR_IO_PENDING", "-"},
}

// fitTests are MA-1469: the tests in the copy's fit_test.go. The runner requires each to
// report a pass, so the file cannot quietly stop running (adversarial finding INS-05).
var fitTests = []string{
	"TestFitPositiveTwins", "TestFitListRows", "TestFitFloors", "TestFitPromptWraps",
	"TestFitOverTallPromptScrolls", "TestFitButtonsFitTheirLabels", "TestFitEntryStartingText",
	"TestFitDPIsRoundUp", "TestFitProgressGrowsAndNeverShrinks", "TestFitNothingPastTheCap",
	"TestFitIsTotal", "TestFitMessageRow", "TestFitPlacement",
}

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
			if _, dup := p.patches[rec.path]; dup {
				return p, fmt.Errorf("pins line %d records %s a second time: one record per patch file", i+1, rec.path)
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
// package-level variables set by a call, whether it has an init function, and its
// compiler directives other than build constraints.
type goFacts struct {
	imports, callVars, directives []string
	hasInit                       bool
}

// factsOf reads a Go file's facts. In the generated syscall file, the generator's
// NewLazySystemDLL and NewProc lines are not counted (§4.2).
func factsOf(name string, src []byte) (goFacts, error) {
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		return goFacts{}, err
	}
	var g goFacts
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if isDirective(c.Text) {
				g.directives = append(g.directives, c.Text)
			}
		}
	}
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

// generatorLine reports the generator's two shapes exactly, `windows.NewLazySystemDLL("…")`
// and `mod….NewProc("…")`, each with one string literal: anything else that calls, such
// as a function call in the argument, counts as a variable set by a call (adversarial
// finding INS-01). TestTheZenityGeneratedFileIsTheGenerators also holds the whole file to
// the generator's output.
func generatorLine(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return false
	}
	if lit, ok := call.Args[0].(*ast.BasicLit); !ok || lit.Kind != token.STRING {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	recv, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return (recv.Name == "windows" && sel.Sel.Name == "NewLazySystemDLL") ||
		(strings.HasPrefix(recv.Name, "mod") && sel.Sel.Name == "NewProc")
}

// isDirective reports a comment the compiler acts on, other than a build constraint:
// //go:linkname, //go:embed and the rest, //line and //export. A patch file or fit.go may
// carry none (adversarial finding INS-04: //go:linkname reaches code no import shows).
func isDirective(text string) bool {
	switch {
	case strings.HasPrefix(text, "//go:build "):
		return false
	case strings.HasPrefix(text, "//go:"), strings.HasPrefix(text, "//line "),
		strings.HasPrefix(text, "/*line "), strings.HasPrefix(text, "//export "):
		return true
	}
	return false
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

// labBuilt reports the copy's files a lab build compiles or reads: all but fit_test.go and
// UPSTREAM.md, whose changes need no new lab run.
func labBuilt(rel string) bool { return rel != "fit_test.go" && rel != "UPSTREAM.md" }

// labTestRel is MA-1470's lab test, which the lab-copy hash covers with the copy: a lab run
// stands for the copy it built and the test it ran (adversarial re-verify RV-03).
const labTestRel = "internal/thirdparty/ma1470_lab_windows_test.go"

// copyBuildHash is the "lab-copy" hash: SHA-256 over one "sha256  path" line per built file
// of the copy, in path order, then one for the lab test. MA-1470's receipt carries the hash
// its build was made from, and the pins keep it, so a patch file, fit.go or the lab test
// changed after that run turns MA-1468 red until the lab test passes again (adversarial
// finding INS-03, re-verify RV-03).
func copyBuildHash(files map[string][]byte, labTest []byte) string {
	paths := make([]string, 0, len(files))
	for rel := range files {
		if labBuilt(rel) {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		fmt.Fprintf(h, "%s  %s\n", sha(files[rel]), rel)
	}
	fmt.Fprintf(h, "%s  %s\n", sha(labTest), labTestRel)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// labHashOf is the lab-copy hash of the repository at root, whose copy holds files. The lab
// test is hashed with its line breaks as LF: a Windows checkout writes them as CR LF (the
// copy keeps its bytes through .gitattributes; the test file does not need to).
func labHashOf(root string, files map[string][]byte) (string, error) {
	lab, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(labTestRel)))
	if err != nil {
		return "", err
	}
	return copyBuildHash(files, bytes.ReplaceAll(lab, []byte("\r\n"), []byte("\n"))), nil
}

// checkCopy is MA-1468: it returns every way the repository at root breaks the pins.
// allowed is the table of patchable files (patchable; a test may add its own).
func checkCopy(root string, p pins, allowed map[string]patchFacts) []string {
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
	if _, err := os.Stat(filepath.Join(root, "vendor")); err == nil {
		add("vendor/ is in the repository: the go command would build a vendored zenity instead of the copy (adversarial finding INS-02)")
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
			facts, patchable := allowed[f.path]
			switch {
			case !patchable:
				add("%s is patched, but the design patches only the files MA-1468's table names (adversarial finding INS-04): patching another is a change to that table, in review", f.path)
			case facts.upstreamSHA != f.sha:
				add("%s's pin (sha256 %s) is not the original MA-1468's table names (%s)", f.path, f.sha, facts.upstreamSHA)
			case !present:
				add("patch file %s is missing from the copy", f.path)
			case !ok:
				add("patch file %s has no record", f.path)
			default:
				probs = append(probs, checkPatchFile(copyDir, f.path, data, rec, facts)...)
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
		switch path {
		case "fit.go":
			probs = append(probs, checkFitGo(filepath.Join(copyDir, "fit.go"), have[path])...)
		case "fit_test.go":
			for _, goos := range []string{"windows", "darwin", "linux"} {
				if !buildsFor(filepath.Join(copyDir, "fit_test.go"), goos) {
					add("fit_test.go does not build for %s: MA-1469's tests run everywhere (adversarial finding INS-05)", goos)
				}
			}
		}
	}
	for _, name := range []string{"UPSTREAM.md", "fit.go", "fit_test.go"} {
		if _, ok := have[name]; !ok {
			add("%s is missing from the copy: UPSTREAM.md says where the copy came from, fit.go is the layout and fit_test.go is MA-1469 (design §4.1)", name)
		}
	}
	if len(p.patches) > 0 {
		got, err := labHashOf(root, have)
		if err != nil {
			add("%s: %v (the lab-copy hash covers the lab test)", labTestRel, err)
		} else if lc := p.header["lab-copy"]; len(lc) != 1 || lc[0] != got {
			add("the copy's built files and the lab test hash to %s, but the last MA-1470 run that passed was built from %q: a patch file, fit.go or the lab test changed since; run MA-1470 on the test PC and record its receipt (UPSTREAM.md)", got, lc)
		}
	}
	return probs
}

// checkPatchFile holds a patch file to its record, to MA-1468's table and to the design's
// rules (§2 P3, §4.2).
func checkPatchFile(copyDir, rel string, data []byte, rec patchRecord, facts patchFacts) []string {
	var probs []string
	add := func(format string, a ...any) { probs = append(probs, fmt.Sprintf(format, a...)) }
	if s := sha(data); s != rec.ours {
		add("patch file %s changed since its record (sha256 %s, recorded %s): record it again, with the lab run it passed%s",
			rel, s, rec.ours, crNote(data, rec.ours))
	}
	if joinList(rec.upstreamImports) != facts.imports || joinList(rec.addsImports) != facts.adds ||
		joinList(rec.upstreamCallVars) != facts.callVars {
		add("patch file %s's record (upstream-imports=%s adds-imports=%s upstream-call-vars=%s) disagrees with MA-1468's table (%s, %s, %s): a record cannot vouch for itself",
			rel, joinList(rec.upstreamImports), joinList(rec.addsImports), joinList(rec.upstreamCallVars), facts.imports, facts.adds, facts.callVars)
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
	ours, err := factsOf(rel, data)
	if err != nil {
		add("patch file %s does not parse: %v", rel, err)
		return probs
	}
	want := append(splitList(facts.imports), splitList(facts.adds)...)
	sort.Strings(want)
	want = slices.Compact(want)
	if !slices.Equal(ours.imports, want) {
		add("patch file %s imports %v; MA-1468's table allows %v (upstream's, plus %s)", rel, ours.imports, want, facts.adds)
	}
	if ours.hasInit {
		add("patch file %s declares an init function (design §4.2)", rel)
	}
	for _, v := range ours.callVars {
		if !slices.Contains(splitList(facts.callVars), v) {
			add("patch file %s adds package-level variable %s, set by a call (design §4.2)", rel, v)
		}
	}
	for _, d := range ours.directives {
		add("patch file %s carries the compiler directive %q: a patch file carries only build constraints (adversarial finding INS-04)", rel, d)
	}
	if !labRe.MatchString(rec.lab) {
		add("patch file %s's record names no passing MA-1470 run: %q", rel, rec.lab)
	}
	return probs
}

// checkFitGo holds fit.go to §4.2: pure arithmetic, built everywhere, no imports, no
// init, no package-level variable set by a call, no compiler directive.
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
	for _, d := range facts.directives {
		probs = append(probs, fmt.Sprintf("fit.go carries the compiler directive %q (design §4.2; adversarial finding INS-04)", d))
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
// the design leaves out are absent, and its three additions are there; go.mod replaces
// exactly v0.10.12 with the copy and nothing is vendored; .gitattributes keeps a checkout
// from rewriting it; each patch file is one MA-1468's table names, matches its record,
// builds for Windows only, imports what the table allows, and adds no init function, no
// variable set by a call and no compiler directive; and the copy's built files are the
// ones the last passing MA-1470 run was built from.
func TestTheZenityCopyIsThePublishedModule(t *testing.T) {
	p := readPins(t)
	for _, prob := range checkCopy(repoRoot(t), p, patchable) {
		t.Error(prob)
	}
}

// refusal is one change MA-1468 must refuse: change applies it to a scratch repository
// and returns the pins to check it against; want is part of the refusal's message.
// allowed, when set, replaces the test's table of patchable files.
type refusal struct {
	name, want string
	change     func(t *testing.T, dir string) pins
	allowed    map[string]patchFacts
}

// scratchRepo copies what MA-1468 reads (go.mod, .gitattributes and the copy) into a new
// directory, so a test can change it.
func scratchRepo(t *testing.T, root string) string {
	t.Helper()
	dst := t.TempDir()
	for _, name := range []string{"go.mod", ".gitattributes", labTestRel} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dst, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
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

// recordAs returns pins whose record for rel matches its content in the scratch copy, and
// whose lab-copy is the scratch copy's, as the recorder writes them from a receipt for
// that copy. facts are the record's upstream facts.
func recordAs(t *testing.T, p pins, scratch, rel string, facts goFacts) pins {
	t.Helper()
	files, err := listFiles(filepath.Join(scratch, filepath.FromSlash(copyRel)), neverSource)
	if err != nil {
		t.Fatal(err)
	}
	h, err := labHashOf(scratch, files)
	if err != nil {
		t.Fatal(err)
	}
	q := pins{header: maps.Clone(p.header), patches: maps.Clone(p.patches)}
	q.header["lab-copy"] = []string{h}
	for _, f := range p.files {
		if f.path == rel {
			f.state = "patched"
		}
		q.files = append(q.files, f)
	}
	q.patches[rel] = patchRecord{path: rel, ours: sha(files[rel]), upstreamImports: facts.imports,
		upstreamCallVars: facts.callVars, lab: "MA-1470 2026-11-19 a scratch run pass"}
	return q
}

// asPatched returns pins that mark rel as a patch file whose record matches its current
// content in the scratch copy, its upstream facts read from upstreamSrc.
func asPatched(t *testing.T, p pins, scratch, rel, upstreamSrc string) pins {
	t.Helper()
	up, err := factsOf(rel, []byte(upstreamSrc))
	if err != nil {
		t.Fatal(err)
	}
	return recordAs(t, p, scratch, rel, up)
}

// rerecord returns pins whose record for the patch file rel, its upstream facts kept,
// matches its content in the scratch copy.
func rerecord(t *testing.T, p pins, scratch, rel string) pins {
	t.Helper()
	rec, ok := p.patches[rel]
	if !ok {
		t.Fatalf("%s has no record to keep", rel)
	}
	return recordAs(t, p, scratch, rel, goFacts{imports: rec.upstreamImports, callVars: rec.upstreamCallVars})
}

// withPatchable returns the table of patchable files with rel added as its upstream
// original reads (src, pinned at upstreamSHA): a test that simulates a patch to a file the
// design does not patch needs it.
func withPatchable(t *testing.T, base map[string]patchFacts, rel, upstreamSHA, src string) map[string]patchFacts {
	t.Helper()
	up, err := factsOf(rel, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out := maps.Clone(base)
	out[rel] = patchFacts{upstreamSHA: upstreamSHA, imports: joinList(up.imports), callVars: joinList(up.callVars), adds: "-"}
	return out
}

// covers: MA-1468
// MA-1468's check refuses what it must (positive twin first): a scratch repository made
// from the real copy passes, and each change below turns it red with a message that names
// what changed, whether a copied file, the line endings, an extra, a left-out or a missing
// file (fit_test.go included), the go.mod replace, a vendored module, the .gitattributes
// rule, a Windows file changed after the lab run, a patch file and its record, a file the
// table does not name, a record that vouches for itself, its imports, its init function,
// its variables (a generator line that calls included), its compiler directives, its
// build target and its lab run, or fit.go's rules; and a pins file with two records for
// one patch file does not parse.
func TestTheZenityCopyCheckRefusesEachChange(t *testing.T) {
	root := repoRoot(t)
	p := readPins(t)
	if probs := checkCopy(scratchRepo(t, root), p, patchable); len(probs) != 0 {
		t.Fatalf("positive twin: a scratch copy of the repository should pass, got %q", probs)
	}
	copied := func(rel string) string { return filepath.Join(copyRel, filepath.FromSlash(rel)) }
	pinOf := func(rel string) string {
		for _, f := range p.files {
			if f.path == rel {
				return f.sha
			}
		}
		t.Fatalf("no pin for %s", rel)
		return ""
	}
	cases := []refusal{
		{name: "a byte of a copied file", want: "LICENSE differs from the published module", change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, copied("LICENSE")), func(b []byte) []byte { b[0] ^= 1; return b })
			return p
		}},
		{name: "line endings", want: "list.go differs from the published module only by carriage returns", change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, copied("list.go")), func(b []byte) []byte {
				return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
			})
			return p
		}},
		{name: "an extra file", want: "extra.go is in the copy but is neither upstream's", change: func(t *testing.T, dir string) pins {
			if err := os.WriteFile(filepath.Join(dir, copied("extra.go")), []byte("package zenity\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "a left-out file", want: "list_test.go is in the copy, but the design leaves it out", change: func(t *testing.T, dir string) pins {
			if err := os.WriteFile(filepath.Join(dir, copied("list_test.go")), []byte("package zenity\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "a missing file", want: "msg_windows.go is missing from the copy", change: func(t *testing.T, dir string) pins {
			if err := os.Remove(filepath.Join(dir, copied("msg_windows.go"))); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "no UPSTREAM.md", want: "UPSTREAM.md is missing from the copy", change: func(t *testing.T, dir string) pins {
			if err := os.Remove(filepath.Join(dir, copied("UPSTREAM.md"))); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "no fit_test.go", want: "fit_test.go is missing from the copy", change: func(t *testing.T, dir string) pins {
			if err := os.Remove(filepath.Join(dir, copied("fit_test.go"))); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "fit_test.go behind a build tag", want: "fit_test.go does not build for darwin", change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, copied("fit_test.go")), func(b []byte) []byte {
				return append([]byte("//go:build race\n\n"), b...)
			})
			return p
		}},
		{name: "the replace", want: "go.mod's replace lines for zenity", change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, "go.mod"), func(b []byte) []byte {
				return bytes.ReplaceAll(b, []byte("replace "+replaceLine), nil)
			})
			return p
		}},
		{name: "the version", want: "go.mod does not require github.com/ncruces/zenity v0.10.12", change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, "go.mod"), func(b []byte) []byte {
				return regexp.MustCompile(`(?m)^(\s*github\.com/ncruces/zenity) v0\.10\.12`).ReplaceAll(b, []byte("$1 v0.10.15"))
			})
			return p
		}},
		{name: "a vendored zenity", want: "vendor/ is in the repository", change: func(t *testing.T, dir string) pins {
			if err := os.MkdirAll(filepath.Join(dir, "vendor", "github.com", "ncruces", "zenity"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "vendor", "modules.txt"), []byte("# github.com/ncruces/zenity v0.10.12\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return p
		}},
		{name: "the -text rule", want: ".gitattributes has no line", change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, ".gitattributes"), func(b []byte) []byte {
				return bytes.ReplaceAll(b, []byte(textLine), nil)
			})
			return p
		}},
		{name: "a pin", want: "the pins' upstream lines hash to", change: func(t *testing.T, dir string) pins {
			q := pins{header: p.header, patches: p.patches, files: slices.Clone(p.files)}
			q.files[0].sha = strings.Repeat("0", 64)
			return q
		}},
		{name: "a Windows file changed after the lab run", want: "the copy's built files and the lab test hash to", change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, copied("fit.go")), func(b []byte) []byte { return append(b, "\n// later\n"...) })
			return p
		}},
		{name: "the lab test changed after the lab run", want: "the copy's built files and the lab test hash to", change: func(t *testing.T, dir string) pins {
			// pins recorded from a receipt for this scratch copy, then the lab test changed
			q := rerecord(t, p, dir, "list_windows.go")
			editFile(t, filepath.Join(dir, filepath.FromSlash(labTestRel)), func(b []byte) []byte { return append(b, "\n// later\n"...) })
			return q
		}},
		{name: "a generator line that calls", want: "patch file internal/win/zsyscall_windows.go adds package-level variable zzz, set by a call", change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, copied(generatedFile)), func(b []byte) []byte {
				return append(b, "\nvar zzz = modkernel32.NewProc(zzzName())\n\nfunc zzzName() string { return \"GetTickCount\" }\n"...)
			})
			return rerecord(t, p, dir, generatedFile)
		}},
		{name: "a DLL loaded from the program's folder", want: "patch file internal/win/zsyscall_windows.go adds package-level variable zzz, set by a call", change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, copied(generatedFile)), func(b []byte) []byte {
				return append(b, "\nvar zzz = windows.NewLazyDLL(\"version.dll\").NewProc(\"GetFileVersionInfoW\")\n"...)
			})
			return rerecord(t, p, dir, generatedFile)
		}},
		{name: "fit.go's directives", want: "fit.go carries the compiler directive", change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, copied("fit.go")), func(b []byte) []byte {
				return append(b, "\n//go:noinline\nfunc fitZzz() {}\n"...)
			})
			return p
		}},
	}

	// A patch file, simulated on a file the design never patches: date_windows.go given one
	// more line and recorded as the recorder would, with a table that names it; the record
	// must hold it, as the positive twin shows, and each change after it must not pass.
	upstream, err := os.ReadFile(filepath.Join(root, copied("date_windows.go")))
	if err != nil {
		t.Fatal(err)
	}
	allowed := withPatchable(t, patchable, "date_windows.go", pinOf("date_windows.go"), string(upstream))
	patched := func(t *testing.T, dir string, edit func([]byte) []byte) pins {
		editFile(t, filepath.Join(dir, copied("date_windows.go")), edit)
		return asPatched(t, p, dir, "date_windows.go", string(upstream))
	}
	appended := func(extra string) func([]byte) []byte {
		return func(b []byte) []byte { return append(b, extra...) }
	}
	dir := scratchRepo(t, root)
	q := patched(t, dir, appended("\n// a patch\n"))
	if probs := checkCopy(dir, q, allowed); len(probs) != 0 {
		t.Fatalf("positive twin: a patch file with its record should pass, got %q", probs)
	}
	if probs := checkCopy(dir, p, allowed); len(probs) == 0 {
		t.Fatal("a changed date_windows.go still pinned as copied should not pass")
	}
	cases = append(cases,
		refusal{name: "a patch file after its record", want: "patch file date_windows.go changed since its record", allowed: allowed, change: func(t *testing.T, dir string) pins {
			q := patched(t, dir, appended("\n// a patch\n"))
			editFile(t, filepath.Join(dir, copied("date_windows.go")), appended("// later\n"))
			return q
		}},
		refusal{name: "a file the table does not name", want: "date_windows.go is patched, but the design patches only the files MA-1468's table names", allowed: patchable, change: func(t *testing.T, dir string) pins {
			return patched(t, dir, appended("\n// a patch\n"))
		}},
		refusal{name: "a record that vouches for itself", want: "patch file date_windows.go's record (upstream-imports=", allowed: allowed, change: func(t *testing.T, dir string) pins {
			q := patched(t, dir, appended("\nvar zzz = len(\"x\")\n"))
			rec := q.patches["date_windows.go"]
			rec.upstreamCallVars = append(slices.Clone(rec.upstreamCallVars), "zzz")
			q.patches["date_windows.go"] = rec
			return q
		}},
		refusal{name: "a patch file's init function", want: "patch file date_windows.go declares an init function", allowed: allowed, change: func(t *testing.T, dir string) pins {
			return patched(t, dir, appended("\nfunc init() {}\n"))
		}},
		refusal{name: "a patch file's variable set by a call", want: "patch file date_windows.go adds package-level variable zzz, set by a call", allowed: allowed, change: func(t *testing.T, dir string) pins {
			return patched(t, dir, appended("\nvar zzz = len(\"x\")\n"))
		}},
		refusal{name: "a patch file's directive", want: "patch file date_windows.go carries the compiler directive \"//go:linkname", allowed: allowed, change: func(t *testing.T, dir string) pins {
			return patched(t, dir, appended("\n//go:linkname zzz runtime.nanotime\nfunc zzz() int64\n"))
		}},
		refusal{name: "a patch file's lab run", want: "patch file date_windows.go's record names no passing MA-1470 run", allowed: allowed, change: func(t *testing.T, dir string) pins {
			q := patched(t, dir, appended("\n// a patch\n"))
			rec := q.patches["date_windows.go"]
			rec.lab = "MA-1470 2026-11-19 a scratch run fail"
			q.patches["date_windows.go"] = rec
			return q
		}},
		refusal{name: "another row's lab run", want: "patch file date_windows.go's record names no passing MA-1470 run", allowed: allowed, change: func(t *testing.T, dir string) pins {
			q := patched(t, dir, appended("\n// a patch\n"))
			rec := q.patches["date_windows.go"]
			rec.lab = "MA-1469 2026-11-19 a scratch run pass"
			q.patches["date_windows.go"] = rec
			return q
		}},
		refusal{name: "a patch file's imports", want: "patch file date_windows.go imports", allowed: allowed, change: func(t *testing.T, dir string) pins {
			// The recorder lists a new import under adds-imports; the table, not the record,
			// says what a patch may add.
			q := patched(t, dir, func(b []byte) []byte {
				return bytes.Replace(b, []byte("package zenity\n"), []byte("package zenity\n\nimport _ \"os/exec\"\n"), 1)
			})
			rec := q.patches["date_windows.go"]
			rec.addsImports = []string{"os/exec"}
			q.patches["date_windows.go"] = rec
			return q
		}},
	)
	src, err := os.ReadFile(filepath.Join(root, copied("list.go")))
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, refusal{name: "a patch file built for Linux", want: "patch file list.go builds for linux",
		allowed: withPatchable(t, patchable, "list.go", pinOf("list.go"), string(src)),
		change: func(t *testing.T, dir string) pins {
			editFile(t, filepath.Join(dir, copied("list.go")), appended("\n// a patch\n"))
			return asPatched(t, p, dir, "list.go", string(src))
		}})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := scratchRepo(t, root)
			table := c.allowed
			if table == nil {
				table = patchable
			}
			probs := checkCopy(dir, c.change(t, dir), table)
			if !slices.ContainsFunc(probs, func(s string) bool { return strings.Contains(s, c.want) }) {
				t.Errorf("want a refusal containing %q, got %q", c.want, probs)
			}
		})
	}

	data, err := os.ReadFile(pinsRel)
	if err != nil {
		t.Fatal(err)
	}
	var first string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(l, "patch: ") {
			first = l
			break
		}
	}
	if first == "" {
		t.Fatal("the pins hold no patch record to repeat")
	}
	if _, err := parsePins(append(data, (first + "\n")...)); err == nil || !strings.Contains(err.Error(), "a second time") {
		t.Errorf("a pins file with two records for one patch file parsed: %v", err)
	}
}

// vetLineRe is one finding of go vet: "path.go:line:col: message", the path relative and
// written with the host's separators.
var vetLineRe = regexp.MustCompile(`^(?:\.[/\\])?(\S+\.go):(\d+):\d+: (.+)$`)

// allowedVetFinding reports go vet's one allowed finding in the copy at copyDir: upstream's
// "possible misuse of unsafe.Pointer" inside the generated SHBrowseForFolder, its path
// written with either separator (Windows writes backslashes: adversarial re-verify RV-01).
func allowedVetFinding(copyDir, line string) bool {
	m := vetLineRe.FindStringSubmatch(line)
	if m == nil || strings.ReplaceAll(m[1], `\`, "/") != generatedFile || m[3] != "possible misuse of unsafe.Pointer" {
		return false
	}
	n, err := strconv.Atoi(m[2])
	return err == nil && enclosingFunc(filepath.Join(copyDir, filepath.FromSlash(generatedFile)), n) == "SHBrowseForFolder"
}

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

// testEvent is one line of `go test -json`.
type testEvent struct{ Action, Package, Test string }

// declaredTests are the Test functions a test file declares, read from its source, so a
// build constraint cannot hide one from the runner.
func declaredTests(path string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && strings.HasPrefix(fd.Name.Name, "Test") {
			names = append(names, fd.Name.Name)
		}
	}
	return names, nil
}

// passedTests reads `go test -json` output and returns the tests of pkg that passed.
func passedTests(out, pkg string) map[string]bool {
	passed := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		var e testEvent
		if json.Unmarshal([]byte(line), &e) == nil && e.Package == pkg && e.Test != "" && e.Action == "pass" {
			passed[e.Test] = true
		}
	}
	return passed
}

// covers: MA-1468, MA-1469
// The copy's own tests pass and its vet is clean, run from the main module: the
// repository's unfiltered `go test ./...` never enters a nested module, so this test runs
// `go test -json ./...` and `go vet ./...` in third_party/zenity, for GOOS windows, darwin
// and linux, with -mod=readonly so the go command does not rewrite the copy's go.mod or
// go.sum (design §5). Each of MA-1469's tests, those fitTests names and every Test
// function fit_test.go declares, must report a pass, so fit_test.go cannot quietly stop
// running (adversarial finding INS-05). The one vet finding allowed is upstream's in
// SHBrowseForFolder (internal/win/zsyscall_windows.go), a generated function the patch
// does not change; on Windows it must still be there, so the exception cannot quietly
// cover anything else.
func TestTheZenityCopyTestsAndVetsClean(t *testing.T) {
	copyDir := filepath.Join(repoRoot(t), filepath.FromSlash(copyRel))
	base := []string{"GOFLAGS=-mod=readonly", "GOWORK=off"}

	out, err := runGo(t, copyDir, base, "test", "-count=1", "-json", "./...")
	if err != nil {
		t.Errorf("go test ./... in the copy failed: %v\n%s", err, out)
	}
	passed := passedTests(out, zenityModule)
	declared, err := declaredTests(filepath.Join(copyDir, "fit_test.go"))
	if err != nil {
		t.Errorf("reading MA-1469's tests: %v", err)
	}
	for _, name := range append(slices.Clone(fitTests), declared...) {
		if !passed[name] {
			t.Errorf("MA-1469's %s did not report a pass in the copy's go test run", name)
		}
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
			if allowedVetFinding(copyDir, line) {
				allowed++
				continue
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

// readReceipt reads the receipt MA-1470 wrote on the test PC ("run: …" and "copy: …") and
// returns its run line. It refuses a receipt whose run did not pass, or that was built
// from other files than the copy now holds (adversarial finding INS-03).
func readReceipt(path, want string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var run, built string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if v, ok := strings.CutPrefix(line, "run: "); ok {
			run = v
		}
		if v, ok := strings.CutPrefix(line, "copy: "); ok {
			built = v
		}
	}
	if !labRe.MatchString(run) {
		return "", fmt.Errorf("the receipt's run line %q is not a passing MA-1470 run", run)
	}
	if built != want {
		return "", fmt.Errorf("the receipt's run was built from %q, but the copy's built files and the lab test now hash to %s: run MA-1470 again on them", built, want)
	}
	return run, nil
}

// funcBodyLine is the line after the opening brace of the function name in the file at path.
func funcBodyLine(t *testing.T, path, name string) int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name && fd.Body != nil {
			return fset.Position(fd.Body.Lbrace).Line + 1
		}
	}
	t.Fatalf("%s declares no %s", path, name)
	return 0
}

// covers: MA-1468
// go vet's one allowed finding is recognised whichever separators the host writes its path
// with: forward slashes (macOS, Linux) and backslashes (Windows; adversarial re-verify
// RV-01); the same message outside SHBrowseForFolder or in another file, or another message
// there, is not.
func TestTheVetExceptionReadsEveryHostsPaths(t *testing.T) {
	copyDir := filepath.Join(repoRoot(t), filepath.FromSlash(copyRel))
	n := funcBodyLine(t, filepath.Join(copyDir, filepath.FromSlash(generatedFile)), "SHBrowseForFolder")
	for _, p := range []string{"internal/win/zsyscall_windows.go", "./internal/win/zsyscall_windows.go",
		`internal\win\zsyscall_windows.go`, `.\internal\win\zsyscall_windows.go`} {
		if line := fmt.Sprintf("%s:%d:22: possible misuse of unsafe.Pointer", p, n); !allowedVetFinding(copyDir, line) {
			t.Errorf("the allowed finding written %q is not recognised", line)
		}
	}
	for _, line := range []string{
		fmt.Sprintf(`internal\win\zsyscall_windows.go:%d:22: printf format %%d reads arg #1`, n),
		`internal\win\zsyscall_windows.go:1:1: possible misuse of unsafe.Pointer`,
		fmt.Sprintf(`internal\win\user32.go:%d:22: possible misuse of unsafe.Pointer`, n),
	} {
		if allowedVetFinding(copyDir, line) {
			t.Errorf("%q is recognised as the allowed finding", line)
		}
	}
}

// covers: MA-1468
// The recorder for testdata/zenity.pins. It writes only when -zenity-record is given:
// changing an upstream line is an operator decision (as for MA-331's pins), and a patch
// file's record is written in the same change as the patch file, from the receipt MA-1470
// wrote after every lab test passed on the test PC (-zenity-lab-receipt), which must name
// the copy as it is now. It reads v0.10.12 from the Go module cache and refuses to write
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
	labCopy, err := labHashOf(root, have)
	if err != nil {
		t.Fatal(err)
	}
	labRun := ""
	if *zenityLabReceipt != "" {
		if labRun, err = readReceipt(*zenityLabReceipt, labCopy); err != nil {
			t.Fatal(err)
		}
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
			if labRun == "" {
				t.Fatalf("%s is patched: give -zenity-lab-receipt with the receipt MA-1470 wrote after it passed on this copy", f.path)
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
			patches = append(patches, formatPatch(patchRecord{path: f.path, ours: sha(data), lab: labRun,
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
	b.WriteString("# Written only by: go test ./internal/thirdparty -run TestRecordZenityPins -zenity-record -zenity-lab-receipt <receipt>\n")
	b.WriteString("# Changing an upstream line is an operator decision, as for MA-331's pins. A patch\n")
	b.WriteString("# file's record changes with the patch file, from the receipt of the MA-1470 run it passed;\n")
	b.WriteString("# lab-copy is the hash of the copy's built files that run was built from.\n")
	fmt.Fprintf(&b, "recorded: %s\n", time.Now().Format("2006-01-02"))
	fmt.Fprintf(&b, "toolchain: %s\n", runtime.Version())
	fmt.Fprintf(&b, "module: %s %s\n", zenityModule, zenityVersion)
	fmt.Fprintf(&b, "h1: %s\n", mod.Sum)
	fmt.Fprintf(&b, "go.sum: %s %s %s\n", zenityModule, zenityVersion, mod.Sum)
	fmt.Fprintf(&b, "go.sum: %s %s/go.mod %s\n", zenityModule, zenityVersion, mod.GoModSum)
	if len(patches) > 0 {
		fmt.Fprintf(&b, "lab-copy: %s\n", labCopy)
	}
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

// covers: MA-1468
// The MA-1470 lab build: with -zenity-lab-hash this prints the lab-copy hash of the copy
// and the lab test, which the lab build passes to the test program with -ldflags -X
// (UPSTREAM.md), so the receipt the program writes after it passes names what it was
// built from. With -zenity-lab-build it also builds that program for Windows, and refuses
// it if the copy or the lab test changed while it was built (adversarial re-verify RV-04).
func TestZenityLabBuildHash(t *testing.T) {
	if !*zenityLabHash {
		t.Skip("prints the lab-copy hash only when -zenity-lab-hash is given (the MA-1470 lab build, UPSTREAM.md)")
	}
	root := repoRoot(t)
	hash := func() string {
		have, err := listFiles(filepath.Join(root, filepath.FromSlash(copyRel)), neverSource)
		if err != nil {
			t.Fatal(err)
		}
		h, err := labHashOf(root, have)
		if err != nil || len(have) == 0 || !strings.HasPrefix(h, "sha256:") {
			t.Fatalf("no lab-copy hash for a copy of %d files: %q, %v", len(have), h, err)
		}
		return h
	}
	h := hash()
	fmt.Println("lab-copy: " + h)
	if *zenityLabBuild == "" {
		return
	}
	out, err := filepath.Abs(*zenityLabBuild)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goTool(t), "test", "-c", "-o", out, "-ldflags", "-X mail-archive-tool/internal/thirdparty.labCopyHash="+h, "./internal/thirdparty")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the lab program: %v\n%s", err, msg)
	}
	if again := hash(); again != h {
		os.Remove(out)
		t.Fatalf("the copy or the lab test changed while the lab program was built (%s, then %s): build it again", h, again)
	}
	fmt.Println("built: " + out)
}

// realPath resolves symbolic links, so two spellings of one folder compare equal.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// checkResolution asks the go command, in root and with env added to the environment the
// build has, which zenity it builds: v0.10.12 replaced by ./third_party/zenity, with the
// zenity packages a Windows build compiles in the copy's folders.
func checkResolution(t *testing.T, root string, env []string) []string {
	t.Helper()
	var probs []string
	if out, err := runGo(t, root, env, "env", "GOFLAGS"); err != nil || strings.Contains(out, "-overlay") || strings.Contains(out, "-toolexec") {
		probs = append(probs, fmt.Sprintf("GOFLAGS is %q (%v): an -overlay or -toolexec builds other files than the copy's (adversarial re-verify RV-05)", strings.TrimSpace(out), err))
	}
	out, err := runGo(t, root, env, "list", "-m", "-f", "{{.Path}} {{.Version}}{{with .Replace}} => {{.Path}}{{.Version}}{{end}}", zenityModule)
	if got := strings.TrimSpace(out); err != nil || got != replaceLine {
		probs = append(probs, fmt.Sprintf("go list -m says the build uses %q (%v); want %q", got, err, replaceLine))
	}
	winEnv := append(slices.Clone(env), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	out, err = runGo(t, root, winEnv, "list", "-f", "{{.ImportPath}}\t{{.Dir}}", zenityModule, zenityModule+"/internal/win")
	copyDir := realPath(filepath.Join(root, filepath.FromSlash(copyRel)))
	want := map[string]string{zenityModule: copyDir, zenityModule + "/internal/win": filepath.Join(copyDir, "internal", "win")}
	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if pkg, dir, ok := strings.Cut(line, "\t"); ok {
			got[pkg] = realPath(dir)
		}
	}
	if err != nil || !maps.Equal(got, want) {
		probs = append(probs, fmt.Sprintf("a Windows build compiles zenity from %q (%v); want %q", got, err, want))
	}
	return probs
}

// covers: MA-1468
// The build uses the copy as the go command resolves it, not only as go.mod reads
// (adversarial finding INS-02): from the repository root, with the build's environment,
// go list names v0.10.12 replaced by ./third_party/zenity and finds the zenity packages a
// Windows build compiles in the copy. Its refusal twin: a go.work that replaces zenity
// with another folder leaves go.mod's text as it was, and is refused.
func TestTheBuildUsesTheZenityCopy(t *testing.T) {
	root := repoRoot(t)
	for _, prob := range checkResolution(t, root, nil) {
		t.Error(prob)
	}

	dir := scratchRepo(t, root)
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
		t.Fatal(err)
	}
	if probs := checkResolution(t, dir, []string{"GOWORK=off"}); len(probs) != 0 {
		t.Fatalf("positive twin: a scratch repository should resolve zenity to its copy, got %q", probs)
	}
	goLine := regexp.MustCompile(`(?m)^go \S+`).Find(gomod)
	if goLine == nil {
		t.Fatal("go.mod has no go line")
	}
	files, err := listFiles(filepath.Join(dir, filepath.FromSlash(copyRel)), nil)
	if err != nil {
		t.Fatal(err)
	}
	for rel, data := range files {
		out := filepath.Join(dir, "elsewhere", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	work := fmt.Sprintf("%s\n\nuse .\n\nreplace %s %s => ./elsewhere\n", goLine, zenityModule, zenityVersion)
	if err := os.WriteFile(filepath.Join(dir, "go.work"), []byte(work), 0o644); err != nil {
		t.Fatal(err)
	}
	probs := checkResolution(t, dir, []string{"GOWORK=" + filepath.Join(dir, "go.work")})
	if !slices.ContainsFunc(probs, func(s string) bool { return strings.Contains(s, "a Windows build compiles zenity from") }) {
		t.Errorf("a go.work that builds zenity from another folder should be refused, got %q", probs)
	}
	overlay := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlay, []byte(`{"Replace":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	probs = checkResolution(t, dir, []string{"GOWORK=off", "GOFLAGS=-overlay=" + overlay})
	if !slices.ContainsFunc(probs, func(s string) bool { return strings.Contains(s, "-overlay or -toolexec") }) {
		t.Errorf("an -overlay in GOFLAGS should be refused, got %q", probs)
	}
}

// generateCommand is upstream's generate.go line that names the generator.
const generateCommand = "//go:generate -command mkwinsyscall go run golang.org/x/sys/windows/mkwinsyscall -output zsyscall_windows.go"

// firstDiff names the first line where got and want differ.
func firstDiff(got, want []byte) string {
	g, w := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("line %d reads %q; the generator writes %q", i+1, gl, wl)
		}
	}
	return "they differ"
}

// checkGenerated runs upstream's generator, as generate.go names it, in the copy at
// copyDir, and compares its output with internal/win/zsyscall_windows.go byte for byte.
func checkGenerated(t *testing.T, copyDir string) []string {
	t.Helper()
	winDir := filepath.Join(copyDir, "internal", "win")
	gen, err := os.ReadFile(filepath.Join(winDir, "generate.go"))
	if err != nil {
		return []string{fmt.Sprintf("internal/win/generate.go: %v", err)}
	}
	var files []string
	for _, l := range strings.Split(string(gen), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), "//go:generate mkwinsyscall "); ok {
			files = strings.Fields(v)
		}
	}
	if !hasLine(gen, generateCommand) || len(files) == 0 {
		return []string{"internal/win/generate.go does not name upstream's generator command and its files"}
	}
	out := filepath.Join(t.TempDir(), "zsyscall_windows.go")
	args := append([]string{"run", "golang.org/x/sys/windows/mkwinsyscall", "-output", out}, files...)
	if msg, err := runGo(t, winDir, []string{"GOFLAGS=-mod=readonly", "GOWORK=off"}, args...); err != nil {
		return []string{fmt.Sprintf("upstream's generator did not run in the copy: %v\n%s", err, msg)}
	}
	want, err := os.ReadFile(out)
	if err != nil {
		return []string{fmt.Sprintf("the generator wrote nothing: %v", err)}
	}
	got, err := os.ReadFile(filepath.Join(winDir, "zsyscall_windows.go"))
	if err != nil {
		return []string{fmt.Sprintf("internal/win/zsyscall_windows.go: %v", err)}
	}
	if !bytes.Equal(got, want) {
		return []string{"internal/win/zsyscall_windows.go is not what upstream's generator makes from the copy's //sys lines: " + firstDiff(got, want)}
	}
	return nil
}

// covers: MA-1468
// The generated syscall file is exactly what upstream's generator makes from the copy's
// //sys lines (adversarial finding INS-01): generate.go's command, run in the copy with the
// x/sys version the copy's go.mod pins (v0.17.0), writes internal/win/zsyscall_windows.go
// byte for byte, so no line of it, generated-looking or not, is written by hand. Its
// refusal twin: one hand-written line in a scratch copy is refused.
func TestTheZenityGeneratedFileIsTheGenerators(t *testing.T) {
	root := repoRoot(t)
	for _, prob := range checkGenerated(t, filepath.Join(root, filepath.FromSlash(copyRel))) {
		t.Error(prob)
	}
	dir := scratchRepo(t, root)
	editFile(t, filepath.Join(dir, filepath.FromSlash(copyRel), filepath.FromSlash(generatedFile)), func(b []byte) []byte {
		out := bytes.Replace(b, []byte(`moduser32.NewProc("DrawTextW")`), []byte(`moduser32.NewProc(drawTextName())`), 1)
		if bytes.Equal(out, b) {
			t.Fatal(`the generated file has no moduser32.NewProc("DrawTextW") to change`)
		}
		return out
	})
	probs := checkGenerated(t, filepath.Join(dir, filepath.FromSlash(copyRel)))
	if !slices.ContainsFunc(probs, func(s string) bool { return strings.Contains(s, "is not what upstream's generator makes") }) {
		t.Errorf("a hand-written line in the generated file should be refused, got %q", probs)
	}
}

// covers: MA-1468
// govulncheck cannot match the copy to zenity's advisories, so CI and the release scan
// again as if the published v0.10.12 were used (design §7): both workflows run
// packaging/vulncheck-published-zenity.sh on the whole module and on the Windows program
// MailArchive ships, ./cmd/mailarchive, and the script checks which zenity it scanned
// (adversarial finding INS-06).
func TestThePublishedZenityIsScanned(t *testing.T) {
	root := repoRoot(t)
	whole := regexp.MustCompile(`(?m)^\s*packaging/vulncheck-published-zenity\.sh "\$\(go env GOPATH\)/bin/govulncheck" \./\.\.\.\s*$`)
	shipped := regexp.MustCompile(`(?m)^\s*GOOS=windows packaging/vulncheck-published-zenity\.sh "\$\(go env GOPATH\)/bin/govulncheck" .*\./cmd/mailarchive(\s|$)`)
	for _, wf := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(wf)))
		if err != nil {
			t.Fatal(err)
		}
		if !whole.Match(data) {
			t.Errorf("%s does not scan the whole module as if the published zenity were used", wf)
		}
		if !shipped.Match(data) {
			t.Errorf("%s does not scan the Windows program MailArchive ships (./cmd/mailarchive) as if the published zenity were used", wf)
		}
	}
	script, err := os.ReadFile(filepath.Join(root, "packaging", "vulncheck-published-zenity.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(script, []byte(`go list -m -f '{{.Version}}{{with .Replace}} replaced{{end}}' github.com/ncruces/zenity`)) {
		t.Error("packaging/vulncheck-published-zenity.sh does not check that the go.mod it scans uses the published v0.10.12")
	}
	for _, prob := range checkGuardSteps(t, root) {
		t.Error(prob)
	}
}

// workflowSteps splits a workflow into its steps, each from its "- name:" line to the next.
func workflowSteps(data []byte) []string {
	var steps []string
	cur := -1
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- name:") || strings.HasPrefix(strings.TrimSpace(line), "- uses:") {
			steps = append(steps, "")
			cur = len(steps) - 1
		}
		if cur >= 0 {
			steps[cur] += line + "\n"
		}
	}
	return steps
}

// releaseRecheckRe is the release's re-check of the copy, the tests it runs named.
var releaseRecheckRe = regexp.MustCompile(`go test -count=1 -run '\^\(([A-Za-z0-9|]+)\)\$' \./internal/thirdparty`)

// checkGuardSteps holds the scan and re-check steps to doing what they say (adversarial
// re-verify RV-06, RV-07): no such step may continue on error, no such command may be
// followed by "||", and every test the release's re-check names must exist.
func checkGuardSteps(t *testing.T, root string) []string {
	t.Helper()
	var probs []string
	guard := []string{"vulncheck-published-zenity.sh", "govulncheck", "go mod verify", "./internal/thirdparty"}
	for _, wf := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(wf)))
		if err != nil {
			return append(probs, err.Error())
		}
		for _, step := range workflowSteps(data) {
			if !slices.ContainsFunc(guard, func(g string) bool { return strings.Contains(step, g) }) {
				continue
			}
			if strings.Contains(step, "continue-on-error") {
				probs = append(probs, fmt.Sprintf("%s: a scan or check step may continue on error: %q", wf, strings.SplitN(step, "\n", 2)[0]))
			}
			for _, line := range strings.Split(step, "\n") {
				if strings.Contains(line, "||") && slices.ContainsFunc(guard, func(g string) bool { return strings.Contains(line, g) }) {
					probs = append(probs, fmt.Sprintf("%s: a scan or check command is followed by ||: %q", wf, strings.TrimSpace(line)))
				}
			}
		}
	}
	release, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		return append(probs, err.Error())
	}
	m := releaseRecheckRe.FindSubmatch(release)
	if m == nil {
		return append(probs, "release.yml does not re-check the copy just before it builds")
	}
	declared, err := declaredTests(filepath.Join(root, "internal", "thirdparty", "zenity_copy_test.go"))
	if err != nil {
		return append(probs, err.Error())
	}
	for _, name := range strings.Split(string(m[1]), "|") {
		if !slices.Contains(declared, name) {
			probs = append(probs, fmt.Sprintf("release.yml's re-check runs %s, which zenity_copy_test.go does not declare: it would run nothing", name))
		}
	}
	return probs
}

// covers: MA-1468
// The scan and re-check steps cannot pass quietly (adversarial re-verify RV-06, RV-07),
// positive twin first: copies of the real workflows pass; a scan step that may continue on
// error, a scan followed by "|| true", and a release re-check naming a test that does not
// exist are each refused.
func TestTheGuardStepsCannotPassQuietly(t *testing.T) {
	root := repoRoot(t)
	scratch := func(t *testing.T, edit func(wf string, b []byte) []byte) string {
		t.Helper()
		dir := t.TempDir()
		for _, rel := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml", "internal/thirdparty/zenity_copy_test.go"} {
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")) // a Windows checkout's line breaks
			out := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(out, edit(rel, data), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	if probs := checkGuardSteps(t, scratch(t, func(_ string, b []byte) []byte { return b })); len(probs) != 0 {
		t.Fatalf("positive twin: copies of the real workflows should pass, got %q", probs)
	}
	once := func(b []byte, old, new string) []byte {
		if !bytes.Contains(b, []byte(old)) {
			t.Fatalf("no %q to change", old)
		}
		return bytes.Replace(b, []byte(old), []byte(new), 1)
	}
	for _, c := range []struct {
		name, want string
		edit       func(wf string, b []byte) []byte
	}{
		{"a scan step that may fail", "may continue on error", func(wf string, b []byte) []byte {
			if wf != ".github/workflows/ci.yml" {
				return b
			}
			return once(b, "      - name: Vulnerability scan (reachable code)\n", "      - name: Vulnerability scan (reachable code)\n        continue-on-error: true\n")
		}},
		{"a scan followed by || true", "followed by ||", func(wf string, b []byte) []byte {
			if wf != ".github/workflows/release.yml" {
				return b
			}
			return once(b, `packaging/vulncheck-published-zenity.sh "$(go env GOPATH)/bin/govulncheck" ./...`, `packaging/vulncheck-published-zenity.sh "$(go env GOPATH)/bin/govulncheck" ./... || true`)
		}},
		{"a re-check of a test that does not exist", "which zenity_copy_test.go does not declare", func(wf string, b []byte) []byte {
			if wf != ".github/workflows/release.yml" {
				return b
			}
			return once(b, "TestTheBuildUsesTheZenityCopy|", "TestTheBuildUsesTheZenityCopyRenamed|")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			probs := checkGuardSteps(t, scratch(t, c.edit))
			if !slices.ContainsFunc(probs, func(s string) bool { return strings.Contains(s, c.want) }) {
				t.Errorf("want a refusal containing %q, got %q", c.want, probs)
			}
		})
	}
}
