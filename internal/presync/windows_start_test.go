package presync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The Windows start details of slice AS4 (design-presync-attended §2.7, A14, A10;
// conditions C-56, C-58, C-64, C-65, C-66; owner ruling OQ-7), tested on every
// platform through the untagged pure functions and the session's fake env. The
// Win32 layer itself is tested in start_windows_test.go.

// covers: MA-928
// minimizeMode picks how an attended run minimizes Thunderbird's window (A14, C-58):
// already minimized → no call; not the foreground window → SW_SHOWMINNOACTIVE
// (minimize, activate nothing, so the person's typing stays where it was); the
// foreground window → SW_MINIMIZE, which hands activation back to the next window.
func TestMinimizeMode(t *testing.T) {
	for _, c := range []struct {
		iconic, foreground bool
		want               int
	}{
		{true, false, 0}, {true, true, 0},
		{false, false, swShowMinNoActive},
		{false, true, swMinimize},
	} {
		if got := minimizeMode(c.iconic, c.foreground); got != c.want {
			t.Errorf("minimizeMode(iconic %v, foreground %v) = %d, want %d", c.iconic, c.foreground, got, c.want)
		}
	}
	if swMinimize != 6 || swShowMinNoActive != 7 {
		t.Errorf("show values %d/%d, want Win32's SW_MINIMIZE 6 and SW_SHOWMINNOACTIVE 7", swMinimize, swShowMinNoActive)
	}
}

// minimizingEnv is the session's fake env with Windows' optional minimize
// capability: its first attempt (S6) fails, as when no single main window has
// appeared in 15 s; each later single try fails until the try numbered okOn, then
// succeeds. It records every try.
type minimizingEnv struct {
	*fakeEnv
	okOn     int
	tries    int
	attended []bool
	firstOK  bool
}

func (m *minimizingEnv) minimizeMain(ctx, stop context.Context, child proc, attended bool) bool {
	m.attended = append(m.attended, attended)
	return m.firstOK
}

func (m *minimizingEnv) minimizeAgain(child proc, attended bool) bool {
	m.tries++
	m.attended = append(m.attended, attended)
	return m.okOn > 0 && m.tries >= m.okOn
}

// covers: MA-928
// The backstop minimize is retried on each S7 poll until it succeeds once, and
// never after (A14, C-56): a person who restores our window to work in it is not
// fought. Result.Minimized records whether it ever succeeded, and the mode the
// session was in reaches the adapter (attended picks its show mode, A14 (3)).
// Positive twin first: a first attempt that succeeds is never retried.
func TestMinimizeRetriedUntilItSucceedsOnce(t *testing.T) {
	m := &minimizingEnv{fakeEnv: syncingFake(), firstOK: true}
	res := runSession(context.Background(), syncingDeps(m, filepath.Join(t.TempDir(), "s.json")))
	if res.outcome != OutcomeOK || !res.minimized || m.tries != 0 {
		t.Fatalf("twin: outcome %s, minimized %v, retries %d; want ok, minimized, no retry", res.outcome.Name, res.minimized, m.tries)
	}

	m = &minimizingEnv{fakeEnv: syncingFake(), okOn: 2}
	res = runSession(context.Background(), attendedDeps(m, filepath.Join(t.TempDir(), "s.json")))
	if !res.minimized {
		t.Fatalf("the retry never succeeded: tries %d, outcome %s", m.tries, res.outcome.Name)
	}
	if m.tries != 2 {
		t.Errorf("tries %d, want exactly 2: retried until it succeeded once, never after (the restored window is left alone)", m.tries)
	}
	if m.fakeEnv.sleeps < 4 {
		t.Fatalf("only %d S7 polls: the check needs polls after the success", m.fakeEnv.sleeps)
	}
	for i, a := range m.attended {
		if !a {
			t.Errorf("minimize call %d was told the run is scheduled; it is attended", i)
		}
	}

	m = &minimizingEnv{fakeEnv: syncingFake()}
	res = runSession(context.Background(), syncingDeps(m, filepath.Join(t.TempDir(), "s.json")))
	if res.minimized || m.tries < 2 {
		t.Errorf("never succeeding: minimized %v after %d tries, want false after a try on each poll", res.minimized, m.tries)
	}
	for i, a := range m.attended {
		if a {
			t.Errorf("minimize call %d was told the run is attended; it is scheduled (§1.3 keeps SW_MINIMIZE)", i)
		}
	}
}

// covers: MA-928
// A launch Windows refuses with "access denied" — a Desktop inside a job that
// forbids breakaway (C-65) — ends start-blocked, a warning with its own Desktop
// line, after exactly one launch request: never a second launch without breakaway.
// The write-ahead record is removed (nothing to announce). Positive twin first:
// another launch error stays cannot-start.
func TestStartBlockedOutcome(t *testing.T) {
	f := syncingFake()
	f.startErr = errors.New("some other launch failure")
	path := filepath.Join(t.TempDir(), "s.json")
	res := runSession(context.Background(), syncingDeps(f, path))
	if res.outcome != OutcomeCannotStart || f.startCalls != 1 {
		t.Fatalf("twin: outcome %s after %d launches, want cannot-start after 1", res.outcome.Name, f.startCalls)
	}

	f = syncingFake()
	f.startErr = fmt.Errorf("%w: access is denied", errStartBlocked)
	res = runSession(context.Background(), attendedDeps(f, path))
	if res.outcome != OutcomeStartBlocked {
		t.Fatalf("outcome %s, want start-blocked", res.outcome.Name)
	}
	if f.startCalls != 1 {
		t.Errorf("%d launch requests, want exactly 1 (no retry without breakaway)", f.startCalls)
	}
	if !recordGone(t, path) || res.left != LeftNone {
		t.Errorf("record gone %v, left %s: a refused launch leaves nothing to announce", recordGone(t, path), res.left)
	}
	if OutcomeStartBlocked.Class != ClassWarn || OutcomeStartBlocked.Synced() {
		t.Errorf("start-blocked is %s, synced %v; want a warning, never synced", OutcomeStartBlocked.Class, OutcomeStartBlocked.Synced())
	}
	line := OutcomeLine(OutcomeStartBlocked, SurfaceDesktop, "windows", LeftNone, 0, false)
	if !strings.Contains(line, "MailArchive Desktop was started in a way that cannot start Thunderbird; start MailArchive Desktop from its shortcut, then click Run now again.") {
		t.Errorf("the Desktop line lacks §2.7's words: %q", line)
	}
	if cli := OutcomeLine(OutcomeStartBlocked, SurfaceCLI, "windows", LeftNone, 0, false); strings.Contains(cli, "Run now") || strings.Contains(cli, "shortcut") {
		t.Errorf("the command line's line names a Desktop button: %q", cli)
	}
	if msg, ok := WarnText(OutcomeStartBlocked); !ok || !strings.Contains(msg, "archive ran") {
		t.Errorf("start-blocked's warning %q (%v) must say the archive ran", msg, ok)
	}
	// The run warning is one text for every surface (WarnText takes none), and a
	// scheduled run reaches it too: a Task Scheduler launch refused "access denied".
	// So it gives §5.4's remedy for the scheduled task first — Repair on the
	// Scheduled backup card — and the shortcut line only for Run now; a scheduled
	// run is never told only to restart the Desktop (gate-2 pass 1, as4win-7).
	msg, _ := WarnText(OutcomeStartBlocked)
	if !strings.Contains(msg, "click Repair on the Scheduled backup card") {
		t.Errorf("start-blocked's warning gives a scheduled run no remedy of its own (Repair): %q", msg)
	}
	if !strings.Contains(msg, "if you use Run now, start MailArchive Desktop from its shortcut") {
		t.Errorf("start-blocked's warning lost the Run now remedy: %q", msg)
	}
}

// covers: MA-928
// A Desktop started "as administrator" gets refused-elevated with the Desktop's own
// line (C-65): start MailArchive Desktop normally, then click Run now again — not
// the scheduled task's Repair remedy, and the command line's words name no button.
func TestElevatedDesktopLine(t *testing.T) {
	d := OutcomeLine(OutcomeRefusedElevated, SurfaceDesktop, "windows", LeftNone, 0, false)
	if !strings.Contains(d, "Thunderbird was not synced: MailArchive Desktop was started as administrator. Start MailArchive Desktop normally, not as administrator, then click Run now again.") {
		t.Errorf("the Desktop's elevated line: %q", d)
	}
	if c := OutcomeLine(OutcomeRefusedElevated, SurfaceCLI, "windows", LeftNone, 0, false); strings.Contains(c, "Run now") || strings.Contains(c, "Desktop") {
		t.Errorf("the command line's elevated line names the Desktop: %q", c)
	}
}

// windowsPinFile holds MA-927's pin of the Windows start, minimize and close code
// (§2.7, C-66): the hash, and either "no lab record" or the MA-918 record it was
// measured with.
const windowsPinFile = "docs/lab/presync-windows-code-pin.txt"

// windowsPinSources are the files the pinned declarations live in, relative to
// internal/presync.
var windowsPinSources = []string{"quit_windows.go", "minimize.go", "winpath.go", "env_windows.go", "session.go"}

// windowsPinned are the declarations the pin covers, by name ("Recv.Name" for a
// method, the type without its pointer star): the start, the minimize and the
// close, and the code they depend on (gate-2 pass 1, as4win-4) — which window is
// picked to minimize and close, the handle and identity re-checks, the wait, the
// working folder and the session's retry rule.
var windowsPinned = []string{
	// The start.
	"buildLaunchCmd", "winStart", "winEnv.startHidden", "winInstallDir",
	// The minimize.
	"winEnv.minimizeMain", "winEnv.minimizeAgain", "winEnv.minimizeOurs", "winEnv.minimizeWindow",
	"winEnv.foregroundIsOurs", "minimizeMode", "minimizeWait", "minimizeRetry", "swMinimize", "swShowMinNoActive",
	"realWin32.showWindow", "realWin32.isIconic", "realWin32.foregroundWindow",
	// The close.
	"winEnv.quit", "realWin32.postClose", "wWmClose",
	// Which window is the main one.
	"selectMainWindow", "isMainWindow", "realWin32.mainWindow", "winIsVisible", "winLong", "winOwner",
	"wGwOwner", "wWsChild", "wWsCaption", "wWsExToolWindow", "wGwlStyle", "wGwlExStyle",
	// The handle and identity re-checks.
	"realWin32.holdHandle", "realWin32.handleAlive", "realWin32.releaseHandle", "realWin32.windowPID",
	"realWin32.creationNs", "creationUnixNs",
}

// windowsCodeHash hashes the pinned declarations of the given files, each printed
// from its syntax tree without comments, so a comment or formatting change does not
// move it and any change to the code does. It also reports which names it found.
func windowsCodeHash(t *testing.T, files ...string) (string, []string) {
	t.Helper()
	fset := token.NewFileSet()
	found := map[string]string{}
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			var key string
			var node ast.Node
			switch d := decl.(type) {
			case *ast.FuncDecl:
				key = d.Name.Name
				if d.Recv != nil && len(d.Recv.List) == 1 {
					rt := d.Recv.List[0].Type
					if s, ok := rt.(*ast.StarExpr); ok {
						rt = s.X
					}
					if id, ok := rt.(*ast.Ident); ok {
						key = id.Name + "." + key
					}
				}
				c := *d
				c.Doc = nil
				node = &c
			case *ast.GenDecl:
				for _, s := range d.Specs {
					if vs, ok := s.(*ast.ValueSpec); ok && len(vs.Names) == 1 {
						c := *vs
						c.Doc, c.Comment = nil, nil
						var buf bytes.Buffer
						if err := printer.Fprint(&buf, fset, &c); err != nil {
							t.Fatal(err)
						}
						found[vs.Names[0].Name] = buf.String()
					}
				}
				continue
			}
			var buf bytes.Buffer
			if err := printer.Fprint(&buf, fset, node); err != nil {
				t.Fatal(err)
			}
			found[key] = buf.String()
		}
	}
	h := sha256.New()
	var names []string
	for _, n := range windowsPinned {
		src, ok := found[n]
		if !ok {
			continue
		}
		names = append(names, n)
		fmt.Fprintf(h, "%s\n%s\n", n, src)
	}
	return hex.EncodeToString(h.Sum(nil)), names
}

// windowsPin is the pin file's content: the hash, the record (a path under
// docs/lab/, or "no lab record") and, once a record is named, the date of that
// MA-918 record (§2.7: the pin carries "the date of the latest MA-918 record").
type windowsPin struct{ hash, record, date string }

// readWindowsPin reads the pin file's hash, record and date lines.
func readWindowsPin(path string) (windowsPin, error) {
	var p windowsPin
	b, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "hash:"):
			p.hash = strings.TrimSpace(strings.TrimPrefix(line, "hash:"))
		case strings.HasPrefix(line, "record:"):
			p.record = strings.TrimSpace(strings.TrimPrefix(line, "record:"))
		case strings.HasPrefix(line, "date:"):
			p.date = strings.TrimSpace(strings.TrimPrefix(line, "date:"))
		}
	}
	if p.hash == "" || p.record == "" {
		return windowsPin{}, errors.New("the pin file needs a hash: line and a record: line")
	}
	return p, nil
}

// windowsPinProblems judges a pin against the code's hash and the Windows start
// entry (§2.7, C-66, OQ-7): the hash must match; with "no lab record" the Windows
// start entry must be false; otherwise the record must be the Windows start
// entry's own record (tableRecord) — never the pin file itself — a file under
// docs/lab/ that exists and names MA-918, windows, the pinned hash and the pin's
// date (a YYYY-MM-DD date line in the pin), so the entry can be true only when the
// measured code is the code that ships (gate-2 pass 1, as4win-1).
func windowsPinProblems(root string, pin windowsPin, codeHash string, startAllowed bool, tableRecord string) []string {
	var probs []string
	if pin.hash != codeHash {
		probs = append(probs, "the Windows start, minimize or close code changed since its pin: re-run lab MA-918 and add its record with the new hash in the same commit")
	}
	if pin.record == "no lab record" {
		if startAllowed {
			probs = append(probs, "the Windows start entry is true but the pin names no lab record")
		}
		return probs
	}
	if pin.record == windowsPinFile {
		return append(probs, "the pin names itself as its lab record; name the MA-918 record")
	}
	if !strings.HasPrefix(pin.record, "docs/lab/") {
		return append(probs, "the pin's record is not under docs/lab/: "+pin.record)
	}
	if pin.record != tableRecord {
		probs = append(probs, "the pin's record "+pin.record+" is not the Windows start entry's record "+tableRecord)
	}
	if _, err := time.Parse("2006-01-02", pin.date); err != nil {
		probs = append(probs, "the pin needs a date: line (YYYY-MM-DD) giving the MA-918 record's date, got "+strconv.Quote(pin.date))
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pin.record)))
	if err != nil {
		return append(probs, "the pin's lab record is missing: "+pin.record)
	}
	text := string(b)
	if !strings.Contains(text, "windows") || !strings.Contains(text, pin.hash) {
		probs = append(probs, "the pin's lab record does not name windows and the pinned hash: "+pin.record)
	}
	if !strings.Contains(text, "MA-918") {
		probs = append(probs, "the pin's lab record does not name MA-918: "+pin.record)
	}
	if pin.date != "" && !strings.Contains(text, pin.date) {
		probs = append(probs, "the pin's lab record does not carry the pin's date "+pin.date+": "+pin.record)
	}
	return probs
}

// pinMoves reports whether replacing old with new in the source file named base
// (one of windowsPinSources) moves the pinned hash, by hashing a copy in a
// temporary folder in place of the real file.
func pinMoves(t *testing.T, files []string, base, old, repl string) bool {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "presync", base))
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(src), old, repl, 1)
	if changed == string(src) {
		t.Errorf("the planted change to %s found nothing to change: %q", base, old)
		return false
	}
	mutated := append([]string(nil), files...)
	idx := -1
	for i, f := range files {
		if filepath.Base(f) == base {
			idx = i
		}
	}
	if idx < 0 {
		return false // the file is not hashed at all
	}
	p := filepath.Join(t.TempDir(), base)
	if err := os.WriteFile(p, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	mutated[idx] = p
	code, _ := windowsCodeHash(t, files...)
	h, _ := windowsCodeHash(t, mutated...)
	return h != code
}

// covers: MA-927
// The Windows re-run trigger (§2.7, C-66, OQ-7): a hash of the Windows start,
// minimize and close functions matches the one pinned in docs/lab, and before any
// MA-918 record exists the pin is marked "no lab record" with the Windows start
// entry false. Positive twin first: the real pin passes. Then a changed function, a
// true entry with no record, and a record that does not carry the hash each fail.
func TestWindowsCodePin(t *testing.T) {
	root := repoRoot(t)
	var files []string
	for _, f := range windowsPinSources {
		files = append(files, filepath.Join(root, "internal", "presync", f))
	}
	code, names := windowsCodeHash(t, files...)
	missing := map[string]bool{}
	for _, n := range windowsPinned {
		missing[n] = true
	}
	for _, n := range names {
		delete(missing, n)
	}
	if len(missing) != 0 {
		var m []string
		for n := range missing {
			m = append(m, n)
		}
		sort.Strings(m)
		t.Fatalf("the pinned functions were not all found (renamed or moved?): %v", m)
	}
	cur, err := readWindowsPin(filepath.Join(root, filepath.FromSlash(windowsPinFile)))
	if err != nil {
		t.Fatalf("%s: %v (code hash now %s)", windowsPinFile, err, code)
	}
	tableRecord := attendedStartTable["windows"].record
	if p := windowsPinProblems(root, cur, code, attendedStartTable["windows"].allowed, tableRecord); len(p) != 0 {
		t.Fatalf("the real pin: %v (code hash now %s)", p, code)
	}
	pin := cur.hash

	// A changed function moves the hash; a comment does not.
	if !pinMoves(t, files, "quit_windows.go", "return errQuitNotPosted // no single main window", "return nil // no single main window") {
		t.Error("a changed quit did not move the hash")
	}
	if pinMoves(t, files, "quit_windows.go", "func buildLaunchCmd(", "// a new comment\nfunc buildLaunchCmd(") {
		t.Error("a comment moved the hash")
	}
	// The code the pinned functions depend on is pinned too (gate-2 pass 1,
	// as4win-4): which window is minimized and closed, the handle and identity
	// re-checks, the 15 s wait, the working folder, and the session's retry rule.
	// Each planted change must move the hash; none of these tests runs on the Linux
	// gate otherwise (their own tests are Windows-only).
	for _, c := range []struct{ file, old, repl, what string }{
		{"quit_windows.go", "case w.owner != 0:\n\t\treturn false", "case w.owner != 0:\n\t\treturn true", "isMainWindow accepting an owned dialog"},
		{"quit_windows.go", "if n == 1 {", "if n >= 1 {", "selectMainWindow picking among several windows"},
		{"quit_windows.go", "if wpid == pid {", "if wpid != 0 {", "mainWindow listing other programs' windows"},
		{"quit_windows.go", "return uint32(r) == wWaitTimeout // still running = the wait times out\n}\n\nfunc (realWin32) releaseHandle", "return true\n}\n\nfunc (realWin32) releaseHandle", "handleAlive answering alive always"},
		{"quit_windows.go", "var minimizeWait = 15 * time.Second", "var minimizeWait = 15 * time.Minute", "the 15 s minimize wait"},
		{"quit_windows.go", "wWsExToolWindow = 0x00000080", "wWsExToolWindow = 0x00000000", "the tool-window style bit"},
		{"env_windows.go", "return c.Nanoseconds()", "return c.Nanoseconds() / 1e9", "the creation time the identity re-check compares"},
		{"winpath.go", "return exe[:i+1] // a root: keep its separator", "return exe[:i] // a root: keep its separator", "winInstallDir, the launch's working folder"},
		{"session.go", "if m == nil || minimized {", "if m == nil {", "the session's retry-until-it-succeeds-once rule"},
	} {
		if !pinMoves(t, files, c.file, c.old, c.repl) {
			t.Errorf("planted: %s did not move the hash", c.what)
		}
	}

	// The record half (as4win-1): a named record must be the Windows start entry's
	// own record, never the pin file itself, and must name MA-918, windows, the
	// pinned hash and the pin's date, a real YYYY-MM-DD date.
	const date = "2026-11-02"
	good := "Lab record — MA-918, windows, " + date + ", code hash " + pin
	dir := t.TempDir()
	plant := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	named := windowsPin{hash: pin, record: tableRecord, date: date}
	plant(tableRecord, good)
	if p := windowsPinProblems(dir, named, code, true, tableRecord); len(p) != 0 {
		t.Fatalf("twin: the table's own record naming MA-918, windows, the hash and the date was refused: %v", p)
	}
	for _, c := range []struct {
		what        string
		pin         windowsPin
		body        string // the record's text, planted at pin.record
		tableRecord string
	}{
		{"the Windows start entry true with no lab record", windowsPin{hash: pin, record: "no lab record"}, "", tableRecord},
		{"a record that does not exist", windowsPin{hash: pin, record: "docs/lab/none-such.md", date: date}, "", "docs/lab/none-such.md"},
		{"a record outside docs/lab/", windowsPin{hash: pin, record: "docs/r.md", date: date}, good, "docs/r.md"},
		{"a record without the pinned hash", named, "Lab record — MA-918, windows, " + date + ", code hash 0000", tableRecord},
		{"a record not naming windows", named, "Lab record — MA-918, " + date + ", code hash " + pin, tableRecord},
		{"the pin file as its own record", windowsPin{hash: pin, record: windowsPinFile, date: date}, good, windowsPinFile},
		{"a record other than the Windows start entry's", windowsPin{hash: pin, record: "docs/lab/other-windows.md", date: date}, good, tableRecord},
		{"a record not naming MA-918", named, "Lab record — windows, " + date + ", code hash " + pin, tableRecord},
		{"a pin with no date line", windowsPin{hash: pin, record: tableRecord}, good, tableRecord},
		{"a pin whose date is not a date", windowsPin{hash: pin, record: tableRecord, date: "soon"}, good + " soon", tableRecord},
		{"a record not carrying the pin's date", named, "Lab record — MA-918, windows, 2026-11-01, code hash " + pin, tableRecord},
	} {
		dir = t.TempDir()
		if c.body != "" {
			plant(c.pin.record, c.body)
		}
		if p := windowsPinProblems(dir, c.pin, code, true, c.tableRecord); len(p) == 0 {
			t.Errorf("planted: %s was not caught", c.what)
		}
	}
}
