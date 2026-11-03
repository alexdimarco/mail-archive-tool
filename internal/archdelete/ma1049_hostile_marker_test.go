package archdelete

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/state"
)

// A hostile or damaged marker (docs/design-archive-delete.md §15.3; MA-1049): whatever is at the
// name, every backup is refused, in the right words, without following it, opening what is not a
// regular file, or hanging; nothing read from it is echoed but a run id and a valid began.

// ma1049Within fails the test when f does not return within a few seconds (a pipe opened, a
// huge file read whole).
func ma1049Within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("%s did not return: the marker was opened or read whole", what)
	}
}

// ma1049Regular are the damaged regular markers: each a marker whose details are not known.
var ma1049Regular = map[string]func(p string) error{
	"empty":        func(p string) error { return os.WriteFile(p, nil, 0o600) },
	"invalid JSON": func(p string) error { return os.WriteFile(p, []byte(`{"version":1,"run":`), 0o600) },
	"version 2": func(p string) error {
		return os.WriteFile(p, []byte(`{"what":"MailArchive delete marker","version":2,"run":"0123456789abcdef","began":"2026-11-02T15:04:05Z"}`), 0o600)
	},
	"a control character and a bidi override in began": func(p string) error {
		return os.WriteFile(p, []byte(`{"version":1,"run":"0123456789abcdef","began":"2026-11-02T15:04:05Z\u001b[31m‮"}`), 0o600)
	},
	"64 MiB": func(p string) error {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if err := f.Truncate(64 << 20); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	},
}

// covers: MA-1049, R36
// Each damaged regular marker (empty, not JSON, version 2, control characters and a bidi override
// in began, 64 MiB) refuses every backup in the marker-only words, nothing of it echoed, nothing
// written; its details are not known, so the row says when it began is not known; Delete's plan
// is made (the archive is planned, the marker in none of its lists). Positive twin first: a
// well-formed marker gives its run and began, and the archive without one is planned.
func TestMA1049_ADamagedRegularMarkerRefusesEveryBackup(t *testing.T) {
	half, _, mbox := ma1048Half(t)
	logPath := filepath.Join(t.TempDir(), LogName)

	// Positive twins.
	if d := delmark.Read(half); !d.Known || d.Run != "0123456789abcdef" {
		t.Fatalf("positive twin: a well-formed marker's details: %+v", d)
	}
	dir := smallArchive(t, t.TempDir())
	mustPlan(t, dir, testDeps())

	for name, mk := range ma1049Regular {
		t.Run(name, func(t *testing.T) {
			for _, d := range []string{half, dir} {
				os.Remove(filepath.Join(d, delmark.Name))
				if err := mk(filepath.Join(d, delmark.Name)); err != nil {
					t.Fatal(err)
				}
			}
			before := ma1048Names(half)
			var err error
			ma1049Within(t, "the backup", func() { err = ma1048Backup(half, logPath, mbox) })
			assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+half+")"),
				assure.Forbid("\u001b", "‮", "2026-11-02"),
				assure.NoSideEffect(func() bool { return slices.Equal(before, ma1048Names(half)) }))
			if k := app.FailureKind(err); k != state.FailDeleteUnfinished {
				t.Errorf("kind %q", k)
			}
			u, _ := UnfinishedAt(logPath, half, "")
			if u == nil || !u.MarkerOnly || u.MarkerDetails.Known {
				t.Fatalf("a damaged marker is a marker whose details are not known: %+v", u)
			}
			if txt := MarkerRowText(u); !strings.Contains(txt, "Delete not finished: a delete of this archive did not finish. ") || strings.Contains(txt, "archive begun") {
				t.Errorf("the row's words when it began is not known: %q", txt)
			}
			p := mustPlan(t, dir, testDeps())
			requireNotPlanned(t, p)
		})
	}
}

// covers: MA-1049, R36
// A name that is not a regular file — a link to a file, to a folder, to nothing, and a folder —
// is never followed and still refuses every backup, in its own words (it is a link, a folder or a
// device; remove it by hand), kind delete_unfinished, nothing written; Delete's plan refuses the
// link by the link row and the folder in MsgPlanNotRegular's words, nothing changed. Positive twin
// first: the same archive without it is planned and backed up.
func TestMA1049_ANameThatIsNotARegularFileRefusesEveryBackupAndThePlan(t *testing.T) {
	half, _, mbox := ma1048Half(t)
	logPath := filepath.Join(t.TempDir(), LogName)
	os.Remove(filepath.Join(half, delmark.Name))
	dir := smallArchive(t, t.TempDir())

	// Positive twins.
	mustPlan(t, dir, testDeps())
	if err := ma1048Backup(half, logPath, mbox); err != nil {
		t.Fatalf("positive twin: %v", err)
	}

	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte(`{"version":1,"run":"fedcba9876543210","began":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	nothing := filepath.Join(t.TempDir(), "nothing")
	// Each case makes its entry with the subtest's own t, so a link this account may not make
	// skips that case alone (a Skip on the parent from a subtest is a failure).
	cases := map[string]func(t *testing.T, p string){
		"a link to a file":   func(t *testing.T, p string) { symlinkOrSkip(t, target, p) },
		"a link to a folder": func(t *testing.T, p string) { symlinkOrSkip(t, folder, p) },
		"a link to nothing":  func(t *testing.T, p string) { symlinkOrSkip(t, nothing, p) },
		"a folder": func(t *testing.T, p string) {
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			for _, d := range []string{half, dir} {
				os.Remove(filepath.Join(d, delmark.Name))
				mk(t, filepath.Join(d, delmark.Name))
			}
			before := ma1048Names(half)
			err := ma1048Backup(half, logPath, mbox)
			assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.MsgNotRegular+" (Folder: "+half+")"),
				assure.Forbid("fedcba9876543210", "2026-01-01"),
				assure.NoSideEffect(func() bool { return slices.Equal(before, ma1048Names(half)) }))
			if k := app.FailureKind(err); k != state.FailDeleteUnfinished {
				t.Errorf("kind %q", k)
			}
			if strings.HasPrefix(name, "a link") {
				rc, msg, opts := refusedCase(t, dir, testDeps(), CaseLink, "This archive's folder is, or holds where its files are, a link:", delmark.Name, "Delete it by hand.")
				assure.Refused(t, rc, msg, opts...)
			} else {
				rc, msg, opts := refusedCase(t, dir, testDeps(), CaseMarkerNotRegular, delmark.MsgPlanNotRegular)
				assure.Refused(t, rc, msg, opts...)
			}
		})
	}
}

// covers: MA-1049, R36
// Every root lister refuses a folder at the marker's name in MsgPlanNotRegular's words (§15.3): the
// encrypted lister (which refused it as "more than one kind") and §5.4's no-anchor re-run, plain and
// encrypted (which kept it), nothing changed. Positive twin first: each plan is made without it.
func TestMA1049_EveryListerRefusesAFolderAtTheMarkersName(t *testing.T) {
	enc := encryptedArchive(t)
	mustPlan(t, enc, testDeps())
	if err := os.Mkdir(filepath.Join(enc, delmark.Name), 0o755); err != nil {
		t.Fatal(err)
	}
	rc, msg, opts := refusedCase(t, enc, testDeps(), CaseMarkerNotRegular, delmark.MsgPlanNotRegular)
	assure.Refused(t, rc, msg, opts...)

	for _, kind := range []string{"plain", "encrypted"} {
		t.Run("no anchor "+kind, func(t *testing.T) {
			dir := smallArchive(t, t.TempDir())
			r := newRig(t, dir)
			line := map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "folder": dir, "row": dir, "kind": kind, "identity": idOf(t, dir)}
			if kind == "encrypted" {
				line["record_id"], line["record_spelling"] = testArchiveID, dir
			}
			appendRawLog(t, r.x.Log, line)
			if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
				t.Fatal(err)
			}
			if p := r.plan(t); p.Rerun != RerunNoAnchor {
				t.Fatalf("positive twin: rerun %v", p.Rerun)
			}
			if err := os.Mkdir(filepath.Join(dir, delmark.Name), 0o755); err != nil {
				t.Fatal(err)
			}
			deps := r.x.Deps
			rc, msg, opts := refusedCase(t, dir, deps, CaseMarkerNotRegular, delmark.MsgPlanNotRegular)
			assure.Refused(t, rc, msg, opts...)
		})
	}
}

// covers: MA-1049, R36
// The rows see the marker (DM-15, DM-21): RowsCheck.State, outside its deletion log's guard (no
// log at all), finds a delete only the marker knows, with §15.4's row words; a dangling link at
// the name — which the page's stat memo (os.Stat, following links) would read as "not there" — is
// seen too, and Delete is not offered for it (the plan's link row). Positive twin first: the same
// folder without the marker has no unfinished delete and Delete is offered.
func TestMA1049_TheRowsSeeTheMarker(t *testing.T) {
	dir := smallArchive(t, t.TempDir())

	// Positive twin.
	if u, err := NewRowsCheck(testDeps()).State(dir, true); u != nil || err != nil {
		t.Fatalf("positive twin: %+v %v", u, err)
	}

	plantMarker(t, dir)
	u, err := NewRowsCheck(testDeps()).State(dir, true)
	if err != nil || u == nil || !u.MarkerOnly {
		t.Fatalf("a regular marker with no log: %+v %v", u, err)
	}
	began := localStamp(time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC))
	want := "Delete not finished: a delete of this archive begun " + began + " did not finish. This computer's deletion log has no record of it (it was begun on another computer or user account). Click Delete… to finish it."
	if got := MarkerRowText(u); got != want {
		t.Fatalf("the row's words:\n got  %q\n want %q", got, want)
	}

	os.Remove(filepath.Join(dir, delmark.Name))
	symlinkOrSkip(t, filepath.Join(t.TempDir(), "nothing"), filepath.Join(dir, delmark.Name))
	u, err = NewRowsCheck(testDeps()).State(dir, true)
	if u == nil || u.Marker != delmark.NotRegular {
		t.Fatalf("a dangling link at the name is seen: %+v", u)
	}
	if r := AsRefusal(err); r == nil || r.Case != CaseLink {
		t.Fatalf("Delete is not offered for a link at the name: %v", err)
	}
}
