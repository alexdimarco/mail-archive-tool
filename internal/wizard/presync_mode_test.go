package wizard

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// fakeSession is the scripted pre-sync the presyncjob seam takes
// (design-presync-attended §2.2, C-41, C-79): Thunderbird is the external binary,
// so it is the mock CLAUDE.md allows. It records the whole Config of each call,
// writes the given narration to the Config's Log, and returns ok. It starts
// nothing.
type fakeSession struct {
	mu    sync.Mutex
	cfgs  []presync.Config
	lines string
}

func (f *fakeSession) run(_ context.Context, cfg presync.Config) presync.Result {
	f.mu.Lock()
	f.cfgs = append(f.cfgs, cfg)
	f.mu.Unlock()
	if cfg.Log != nil && f.lines != "" {
		_, _ = io.WriteString(cfg.Log, f.lines)
	}
	return presync.Result{Outcome: presync.OutcomeOK, Left: presync.LeftClosed}
}

func (f *fakeSession) calls() []presync.Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]presync.Config(nil), f.cfgs...)
}

// headlessFixture makes an archive folder and one mbox source, and quiets the
// scheduled run's notices; it returns the archive folder and the source.
func headlessFixture(t *testing.T) (out, mbox string) {
	t.Helper()
	old := notify
	notify = func(string, ...zenity.Option) error { return nil }
	t.Cleanup(func() { notify = old })
	fdir := t.TempDir()
	oldDir := failureDir
	failureDir = func() (string, error) { return fdir, nil }
	t.Cleanup(func() { failureDir = oldDir })

	dir := t.TempDir()
	out = filepath.Join(dir, "archive")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	mbox = filepath.Join(dir, "Inbox")
	const msg = "From - Mon Jan 01 00:00:00 2024\r\nFrom: Bob <bob@example.com>\r\nTo: Me <me@example.com>\r\n" +
		"Subject: Hello\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\nMessage-ID: <abc@example.com>\r\n\r\nplain body\r\n"
	if err := os.WriteFile(mbox, []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	return out, mbox
}

// covers: MA-898
// A scheduled run is never attended (R38; design-presync-attended §2.2, slice AS3
// — the negative half; C-41, C-44, OQ-1, OQ-6). Through the presyncjob seam, a
// real RunHeadless of a job with the sync calls the session exactly once with
// Attended false, NeverQuit false and EndWait nil — whether this platform's attended
// tables say yes or no, so neither "it was attended" nor "the attended hook was not
// installed" can hide. A job file carrying "attended": true in any spelling, and a
// schedule descriptor beside the archive carrying it, change nothing. A job with
// the sync off installs no hook in either mode (R30). optsForJob sets PresyncWanted
// from the job and PresyncAttended from the run mode only.
func TestScheduledRunIsNeverAttended(t *testing.T) {
	out, mbox := headlessFixture(t)
	fake := &fakeSession{}
	restore := presyncjob.OverrideSessionForTest(fake.run, t.TempDir())
	defer restore()

	jobPath := filepath.Join(t.TempDir(), "mailarchive-nightly.json")
	if err := job.Write(jobPath, job.Job{Name: "mailarchive-nightly", Out: out, Mode: "incremental",
		Inputs: []string{mbox}, ThunderbirdSync: &job.ThunderbirdSync{}}); err != nil {
		t.Fatal(err)
	}
	check := func(label string, n int) {
		t.Helper()
		calls := fake.calls()
		if len(calls) != n {
			t.Fatalf("%s: the session was called %d times in all, want %d (a sync job must sync, unattended)", label, len(calls), n)
		}
		c := calls[n-1]
		if c.Attended || c.NeverQuit || c.EndWait != nil {
			t.Errorf("%s: a scheduled run handed the session Attended %v, NeverQuit %v, EndWait set %v", label, c.Attended, c.NeverQuit, c.EndWait != nil)
		}
		if len(c.Inputs) == 0 {
			t.Errorf("%s: the session was handed no inputs", label)
		}
	}

	// Both values of this platform's tables: start and quit allowed, then neither.
	n := 0
	for _, allowed := range []bool{true, false} {
		r := presync.OverrideTablesForTest(map[string]bool{runtime.GOOS: allowed}, map[string]bool{runtime.GOOS: allowed})
		code := RunHeadless(jobPath)
		r()
		if code != 0 {
			t.Fatalf("tables %v: the scheduled run exit = %d, want 0", allowed, code)
		}
		n++
		check(fmt.Sprintf("tables %v", allowed), n)
	}

	// A hand-made job file and a planted descriptor that say "attended".
	raw := map[string]any{"version": job.Version, "name": "mailarchive-nightly", "out": out, "mode": "incremental",
		"inputs": []string{mbox}, "attended": true, "Attended": true, "ATTENDED": true, "presync_attended": true,
		"PresyncAttended": true, "thunderbird_sync": map[string]any{"attended": true, "Attended": true}}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	desc := `{"version":1,"name":"mailarchive-nightly","interval":"daily","at":"02:00","exe":"/x","job":["-out","` +
		filepath.ToSlash(out) + `"],"installed_at":"2026-10-30T00:00:00Z","host":"h","attended":true,` +
		`"presync":{"kind":"thunderbird","attended":true}}`
	if err := os.WriteFile(filepath.Join(out, schedule.DescriptorName), []byte(desc), 0o600); err != nil {
		t.Fatal(err)
	}
	r := presync.OverrideTablesForTest(map[string]bool{runtime.GOOS: true}, map[string]bool{runtime.GOOS: true})
	code := RunHeadless(jobPath)
	r()
	if code != 0 {
		t.Fatalf("the planted job's scheduled run exit = %d, want 0", code)
	}
	check("a job and a descriptor saying attended", n+1)

	// The sync off: no hook in either mode, whatever the tables say (R30).
	r = presync.OverrideTablesForTest(map[string]bool{runtime.GOOS: true}, map[string]bool{runtime.GOOS: true})
	defer r()
	logger := log.New(io.Discard, "", 0)
	off := job.Job{Name: "x", Out: out, Mode: "incremental"}
	for _, mode := range []runMode{runScheduled, runAttended} {
		if o := optsForJob(off, nil, logger, mode); o.BeforeRead != nil || o.PresyncWanted {
			t.Errorf("mode %v, sync off: hook installed %v, PresyncWanted %v", mode, o.BeforeRead != nil, o.PresyncWanted)
		}
	}
	on := job.Job{Name: "x", Out: out, Mode: "incremental", ThunderbirdSync: &job.ThunderbirdSync{}}
	if o := optsForJob(on, nil, logger, runScheduled); !o.PresyncWanted || o.PresyncAttended || o.BeforeRead == nil {
		t.Errorf("scheduled sync job: PresyncWanted %v, PresyncAttended %v, hook %v", o.PresyncWanted, o.PresyncAttended, o.BeforeRead != nil)
	}
	if o := optsForJob(on, nil, logger, runAttended); !o.PresyncWanted || !o.PresyncAttended {
		t.Errorf("attended sync job: PresyncWanted %v, PresyncAttended %v", o.PresyncWanted, o.PresyncAttended)
	}
}

// covers: MA-898
// Nothing that is written down can carry "attended" (R38, C-36): no field of the
// job file, the schedule descriptor, the last-run record, the run history, the
// run's options, the in-memory outcome, the session's Config or the hook's options
// is named for attendance with a JSON tag, and no JSON tag names it. A job file
// and a descriptor carrying the key lose it on a round trip.
func TestNoAttendedFieldIsPersisted(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(job.Job{}), reflect.TypeOf(schedule.Descriptor{}), reflect.TypeOf(state.LastRun{}),
		reflect.TypeOf(state.RunHistoryEntry{}), reflect.TypeOf(app.Options{}), reflect.TypeOf(app.PresyncOutcome{}),
		reflect.TypeOf(presync.Config{}), reflect.TypeOf(presyncjob.HookOptions{}),
	}
	if p := attendedTagProblems(types); len(p) != 0 {
		t.Errorf("fields that could persist attendance: %v", p)
	}
	type planted struct {
		Attended bool `json:"x"`
	}
	type planted2 struct {
		Y bool `json:"presync_attended,omitempty"`
	}
	if len(attendedTagProblems([]reflect.Type{reflect.TypeOf(planted{})})) == 0 ||
		len(attendedTagProblems([]reflect.Type{reflect.TypeOf(planted2{})})) == 0 {
		t.Fatal("twin: a planted tagged field was not caught")
	}

	for _, v := range []any{&job.Job{}, &schedule.Descriptor{}} {
		if err := json.Unmarshal([]byte(`{"attended":true,"Attended":true,"presync":{"attended":true},"thunderbird_sync":{"attended":true}}`), v); err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(b)), "attended") {
			t.Errorf("%T kept the attended key on a round trip: %s", v, b)
		}
	}
}

// attendedTagProblems walks the types (and the structs they hold) for a field
// named for attendance that has a JSON tag, or a JSON tag that names it.
func attendedTagProblems(types []reflect.Type) []string {
	var probs []string
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map || t.Kind() == reflect.Array {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || seen[t] {
			return
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag, tagged := f.Tag.Lookup("json")
			if strings.Contains(strings.ToLower(f.Name), "attended") && tagged && tag != "-" {
				probs = append(probs, t.String()+"."+f.Name+" has a JSON tag")
			}
			if strings.Contains(strings.ToLower(tag), "attended") {
				probs = append(probs, t.String()+"."+f.Name+" is tagged "+tag)
			}
			walk(f.Type)
		}
	}
	for _, t := range types {
		walk(t)
	}
	return probs
}

// covers: MA-898
// The attended mode is set only through the run mode (R38; design-presync-attended
// §2.2, C-41/T10 — matched on the mode constant, never a literal): every call of
// optsForJob passes one of the two mode constants, and RunHeadless passes the
// scheduled one; in the wizard only optsForJob sets PresyncAttended or Attended,
// and only from the mode; presyncjob hands the session the caller's Attended and
// nothing else; the command line sets neither (until slice AS10); and no non-test
// file calls the session seam's test override. Each check catches a planted
// breach first.
func TestAttendedSetOnlyThroughTheMode(t *testing.T) {
	planted := map[string]struct{ kind, src string }{
		"RunHeadless attended": {"wizard", "package wizard\nfunc RunHeadless(p string) int { optsForJob(j, nil, l, runAttended); return 0 }\n"},
		"a literal mode":       {"wizard", "package wizard\nfunc RunHeadless(p string) int { optsForJob(j, nil, l, 1); return 0 }\n"},
		"a literal attended":   {"wizard", "package wizard\nfunc optsForJob() { opts.PresyncAttended = true }\n"},
		"set elsewhere":        {"wizard", "package wizard\nfunc RunJobInProcess() { opts.PresyncAttended = mode == runAttended }\n"},
		"hook literal":         {"presyncjob", "package presyncjob\nfunc SetHook() { _ = presync.Config{Attended: true} }\n"},
		"cli sets it":          {"cli", "package main\nfunc f() { _ = presyncjob.HookOptions{Attended: x} }\n"},
		"cli assigns it":       {"cli", "package main\nfunc f() { opts.PresyncAttended = false }\n"},
		"override called":      {"any", "package x\nfunc f() { presyncjob.OverrideSessionForTest(nil, \"\") }\n"},
	}
	for name, p := range planted {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "planted.go", p.src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		if probs := modeProblems(fset, p.kind, []*ast.File{f}); len(probs) == 0 {
			t.Errorf("twin: planted %q was not caught", name)
		}
	}

	root := wizardRepoRoot(t)
	scanned := 0
	for _, pkg := range []struct{ kind, dir string }{
		{"wizard", "internal/wizard"}, {"presyncjob", "internal/presyncjob"}, {"cli", "cmd/mailarchive"},
	} {
		fset := token.NewFileSet()
		files := parseNonTest(t, fset, filepath.Join(root, pkg.dir))
		scanned += len(files)
		for _, p := range modeProblems(fset, pkg.kind, files) {
			t.Errorf("%s: %s", pkg.dir, p)
		}
	}
	if scanned < 20 {
		t.Fatalf("only %d files scanned: the check is not reading the packages", scanned)
	}

	// The seam's override is test-only, repository-wide.
	var offenders []string
	walked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		walked++
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return perr
		}
		if len(modeProblems(fset, "any", []*ast.File{f})) != 0 {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 50 || len(offenders) != 0 {
		t.Errorf("walked %d files; non-test files calling OverrideSessionForTest: %v", walked, offenders)
	}
}

// modeProblems applies the R38 structural rules for one package kind ("wizard",
// "presyncjob", "cli"; every kind also gets the repository-wide "any" rule) to its
// non-test files. In the wizard, RunHeadless must also call optsForJob at all.
func modeProblems(fset *token.FileSet, kind string, files []*ast.File) []string {
	var probs []string
	at := func(n ast.Node, what string) { probs = append(probs, fset.Position(n.Pos()).String()+": "+what) }
	isModeConst := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		return ok && (id.Name == "runScheduled" || id.Name == "runAttended")
	}
	fromMode := func(e ast.Expr) bool { // mode == runAttended, or the options field set from it
		if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.EQL {
			x, xok := b.X.(*ast.Ident)
			y, yok := b.Y.(*ast.Ident)
			return xok && yok && x.Name == "mode" && y.Name == "runAttended"
		}
		s, ok := e.(*ast.SelectorExpr)
		return ok && s.Sel.Name == "PresyncAttended"
	}
	headlessCalls := 0
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					callee := ""
					switch c := x.Fun.(type) {
					case *ast.Ident:
						callee = c.Name
					case *ast.SelectorExpr:
						callee = c.Sel.Name
					}
					if callee == "OverrideSessionForTest" {
						at(x, "calls the session seam's test-only override")
					}
					if kind == "wizard" && callee == "optsForJob" && len(x.Args) > 0 {
						last := x.Args[len(x.Args)-1]
						if !isModeConst(last) {
							at(x, "optsForJob's mode is not one of the mode constants")
						}
						if name == "RunHeadless" {
							headlessCalls++
							if id, ok := last.(*ast.Ident); !ok || id.Name != "runScheduled" {
								at(x, "RunHeadless does not pass runScheduled")
							}
						}
					}
				case *ast.KeyValueExpr:
					if k, ok := x.Key.(*ast.Ident); ok && (k.Name == "Attended" || k.Name == "PresyncAttended") {
						switch kind {
						case "wizard":
							if name != "optsForJob" || !fromMode(x.Value) {
								at(x, name+" sets "+k.Name+" other than from the mode")
							}
						case "presyncjob":
							if s, ok := x.Value.(*ast.SelectorExpr); !ok || s.Sel.Name != "Attended" {
								at(x, name+" hands the session an Attended not from its caller")
							}
						case "cli":
							at(x, "the command line sets "+k.Name)
						}
					}
				case *ast.AssignStmt:
					for i, lhs := range x.Lhs {
						s, ok := lhs.(*ast.SelectorExpr)
						if !ok || (s.Sel.Name != "Attended" && s.Sel.Name != "PresyncAttended") {
							continue
						}
						var rhs ast.Expr
						if i < len(x.Rhs) {
							rhs = x.Rhs[i]
						}
						switch kind {
						case "wizard":
							if name != "optsForJob" || rhs == nil || !fromMode(rhs) {
								at(x, name+" assigns "+s.Sel.Name+" other than from the mode")
							}
						case "presyncjob", "cli":
							at(x, name+" assigns "+s.Sel.Name)
						}
					}
				}
				return true
			})
		}
	}
	if kind == "wizard" && headlessCalls == 0 && hasFunc(files, "RunHeadless") {
		probs = append(probs, "RunHeadless calls optsForJob nowhere")
	}
	return probs
}

func hasFunc(files []*ast.File, name string) bool {
	for _, f := range files {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name && fn.Recv == nil {
				return true
			}
		}
	}
	return false
}

// parseNonTest parses every non-test Go file of one package folder, whatever its
// build tags (so the darwin and windows files are checked on every platform).
func parseNonTest(t *testing.T, fset *token.FileSet, dir string) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return files
}

// wizardRepoRoot walks up from the package folder to the one holding go.mod.
func wizardRepoRoot(t *testing.T) string {
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
			t.Fatal("go.mod not found above the package")
		}
		dir = parent
	}
}

// covers: MA-907
// Every line a scheduled run writes to mailarchive.log starts with a date and time
// (design-presync-attended §4.8, T8, slice AS3 — the Run now log and rotation are
// AS-L's): the Thunderbird sync's narration, several lines in one write, reaches
// the log one dated line at a time, and so does the warning for an unreadable
// "since". Positive twin first: the run logs the sync's lines and the warning at
// all.
func TestScheduledLogLinesAreDated(t *testing.T) {
	out, mbox := headlessFixture(t)
	fake := &fakeSession{lines: "Thunderbird sync: first line\nThunderbird sync: second line\n\nThunderbird sync: third, unterminated"}
	restore := presyncjob.OverrideSessionForTest(fake.run, t.TempDir())
	defer restore()
	jobPath := filepath.Join(t.TempDir(), "mailarchive-nightly.json")
	if err := job.Write(jobPath, job.Job{Name: "mailarchive-nightly", Out: out, Mode: "incremental", Since: "not a date",
		Inputs: []string{mbox}, ThunderbirdSync: &job.ThunderbirdSync{}}); err != nil {
		t.Fatal(err)
	}
	if code := RunHeadless(jobPath); code != 0 {
		t.Fatalf("the scheduled run exit = %d, want 0", code)
	}
	b, err := os.ReadFile(filepath.Join(out, "mailarchive.log"))
	if err != nil {
		t.Fatal(err)
	}
	logText := string(b)
	for _, want := range []string{"Thunderbird sync: first line", "Thunderbird sync: second line", "third, unterminated", `ignoring invalid since "not a date"`} {
		if !strings.Contains(logText, want) {
			t.Fatalf("twin: the log lacks %q:\n%s", want, logText)
		}
	}
	// runlog.Open's own header names the run's start date and time after its
	// "====" marker; every other line starts with the logger's date and time.
	dated := regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)
	header := regexp.MustCompile(`^==== run started \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\S* \(pid \d+\)$`)
	for i, line := range strings.Split(strings.TrimRight(logText, "\n"), "\n") {
		if !dated.MatchString(line) && !(i == 0 && header.MatchString(line)) {
			t.Errorf("log line %d has no date and time: %q", i+1, line)
		}
	}
}
