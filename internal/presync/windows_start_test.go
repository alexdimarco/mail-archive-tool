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
	"strings"
	"testing"
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

func (m *minimizingEnv) minimizeMain(ctx, stop context.Context, pid int, attended bool) bool {
	m.attended = append(m.attended, attended)
	return m.firstOK
}

func (m *minimizingEnv) minimizeAgain(pid int, attended bool) bool {
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
	if !strings.Contains(line, "Mail Archive Desktop was started in a way that cannot start Thunderbird; start Mail Archive Desktop from its shortcut, then click Run now again.") {
		t.Errorf("the Desktop line lacks §2.7's words: %q", line)
	}
	if cli := OutcomeLine(OutcomeStartBlocked, SurfaceCLI, "windows", LeftNone, 0, false); strings.Contains(cli, "Run now") || strings.Contains(cli, "shortcut") {
		t.Errorf("the command line's line names a Desktop button: %q", cli)
	}
	if msg, ok := WarnText(OutcomeStartBlocked); !ok || !strings.Contains(msg, "archive ran") {
		t.Errorf("start-blocked's warning %q (%v) must say the archive ran", msg, ok)
	}
}

// covers: MA-928
// A Desktop started "as administrator" gets refused-elevated with the Desktop's own
// line (C-65): start Mail Archive Desktop normally, then click Run now again — not
// the scheduled task's Repair remedy, and the command line's words name no button.
func TestElevatedDesktopLine(t *testing.T) {
	d := OutcomeLine(OutcomeRefusedElevated, SurfaceDesktop, "windows", LeftNone, 0, false)
	if !strings.Contains(d, "Start Mail Archive Desktop normally, not as administrator, then click Run now again.") {
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

// windowsPinned are the functions the pin covers, by name ("Recv.Name" for a
// method, the type without its pointer star): the start, the minimize and the close.
var windowsPinned = []string{
	"buildLaunchCmd", "winStart", "winEnv.startHidden",
	"winEnv.minimizeMain", "winEnv.minimizeAgain", "winEnv.minimizeWindow", "minimizeMode",
	"realWin32.showWindow", "realWin32.isIconic", "realWin32.foregroundWindow",
	"winEnv.quit", "realWin32.postClose",
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

// readWindowsPin reads the pin file's hash and record lines.
func readWindowsPin(path string) (hash, record string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "hash:"):
			hash = strings.TrimSpace(strings.TrimPrefix(line, "hash:"))
		case strings.HasPrefix(line, "record:"):
			record = strings.TrimSpace(strings.TrimPrefix(line, "record:"))
		}
	}
	if hash == "" || record == "" {
		return "", "", errors.New("the pin file needs a hash: line and a record: line")
	}
	return hash, record, nil
}

// windowsPinProblems judges a pin against the code's hash and the Windows start
// entry (§2.7, C-66, OQ-7): the hash must match; with "no lab record" the Windows
// start entry must be false; otherwise the record must be a file under docs/lab/
// that exists, names windows and carries the same hash, so the entry can be true
// only when the measured code is the code that ships.
func windowsPinProblems(root, pinHash, record, codeHash string, startAllowed bool) []string {
	var probs []string
	if pinHash != codeHash {
		probs = append(probs, "the Windows start, minimize or close code changed since its pin: re-run lab MA-918 and add its record with the new hash in the same commit")
	}
	if record == "no lab record" {
		if startAllowed {
			probs = append(probs, "the Windows start entry is true but the pin names no lab record")
		}
		return probs
	}
	if !strings.HasPrefix(record, "docs/lab/") {
		return append(probs, "the pin's record is not under docs/lab/: "+record)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(record)))
	if err != nil {
		return append(probs, "the pin's lab record is missing: "+record)
	}
	if !strings.Contains(string(b), "windows") || !strings.Contains(string(b), pinHash) {
		probs = append(probs, "the pin's lab record does not name windows and the pinned hash: "+record)
	}
	return probs
}

// covers: MA-927
// The Windows re-run trigger (§2.7, C-66, OQ-7): a hash of the Windows start,
// minimize and close functions matches the one pinned in docs/lab, and before any
// MA-918 record exists the pin is marked "no lab record" with the Windows start
// entry false. Positive twin first: the real pin passes. Then a changed function, a
// true entry with no record, and a record that does not carry the hash each fail.
func TestWindowsCodePin(t *testing.T) {
	root := repoRoot(t)
	files := []string{filepath.Join(root, "internal", "presync", "quit_windows.go"), filepath.Join(root, "internal", "presync", "minimize.go")}
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
	pin, record, err := readWindowsPin(filepath.Join(root, filepath.FromSlash(windowsPinFile)))
	if err != nil {
		t.Fatalf("%s: %v (code hash now %s)", windowsPinFile, err, code)
	}
	if p := windowsPinProblems(root, pin, record, code, attendedStartTable["windows"].allowed); len(p) != 0 {
		t.Fatalf("the real pin: %v (code hash now %s)", p, code)
	}

	// A changed function moves the hash.
	dir := t.TempDir()
	src, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(src), "return errQuitNotPosted // no single main window", "return nil // no single main window", 1)
	if changed == string(src) {
		t.Fatal("the planted change found nothing to change")
	}
	mutated := filepath.Join(dir, "quit_windows.go")
	if err := os.WriteFile(mutated, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if h, _ := windowsCodeHash(t, mutated, files[1]); h == code {
		t.Error("a changed quit did not move the hash")
	}
	// A comment does not.
	commented := strings.Replace(string(src), "func buildLaunchCmd(", "// a new comment\nfunc buildLaunchCmd(", 1)
	if err := os.WriteFile(mutated, []byte(commented), 0o600); err != nil {
		t.Fatal(err)
	}
	if h, _ := windowsCodeHash(t, mutated, files[1]); h != code {
		t.Error("a comment moved the hash")
	}

	if p := windowsPinProblems(root, pin, "no lab record", code, true); len(p) == 0 {
		t.Error("planted: the Windows start entry true with no lab record was not caught")
	}
	if p := windowsPinProblems(root, pin, "docs/lab/none-such.md", code, true); len(p) == 0 {
		t.Error("planted: a record that does not exist was not caught")
	}
	rec := filepath.Join(dir, "docs", "lab", "r.md")
	if err := os.MkdirAll(filepath.Dir(rec), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rec, []byte("MA-918 windows, hash 0000"), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := windowsPinProblems(dir, pin, "docs/lab/r.md", code, true); len(p) == 0 {
		t.Error("planted: a record without the pinned hash was not caught")
	}
	if err := os.WriteFile(rec, []byte("MA-918 windows, hash "+pin), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := windowsPinProblems(dir, pin, "docs/lab/r.md", code, true); len(p) != 0 {
		t.Errorf("twin: a record naming windows and the hash was refused: %v", p)
	}
}
