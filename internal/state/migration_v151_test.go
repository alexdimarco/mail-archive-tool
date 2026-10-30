package state

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The v1.5.1 record types, copied word for word from internal/state at ea6515a
// (the build the v1.5.1 fixtures were written by; its record files are byte for byte
// v1.5.1's), with only the type names changed so they sit beside today's (design-
// presync-attended §10, C-39).
type v151LastRun struct {
	Version  int       `json:"version"`
	Status   string    `json:"status"` // running | ok | failed | cancelled
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
	PID      int       `json:"pid"`
	Exe      string    `json:"exe,omitempty"`
	Mode     string    `json:"mode,omitempty"`
	// Job is the run's canonical export flags (e.g. -out … -auto, or -out …
	// -input …), recorded so `status` can shape a "keep it current" remedy from
	// the job that actually made this archive instead of hard-coding -auto.
	// Absent on older records and on runs with no determinable local job (a
	// Graph run); status then falls back to a generic phrase.
	Job         []string `json:"job,omitempty"`
	Exported    int      `json:"exported"`
	Filled      int      `json:"filled"`
	Fillable    int      `json:"fillable"`
	Terminal    int      `json:"terminal"`
	Unknown     int      `json:"unknown"`
	IndexErrors int      `json:"index_errors"`
	Error       string   `json:"error,omitempty"`
	// Warnings are problems the run found with its own setup that did not stop
	// it (additive and optional: absent on older records and on a clean run).
	Warnings []v151RunWarning `json:"warnings,omitempty"`
	// ErrorKind names the refusal Error carries when it is one the app owner's
	// surfaces say in their own words (an ErrKind* constant), so the desktop's and
	// mailarchive-gui's health views need not print the CLI command and SQLite's
	// text recorded in Error. Absent on older records and on any other failure.
	ErrorKind string `json:"error_kind,omitempty"`
	// NoIndex records that the run was told not to build the search index
	// (-index=false), so `status` does not warn that an archive deliberately
	// kept without search.db has lost it. Absent (false) on older records.
	NoIndex bool `json:"no_index,omitempty"`

	// The capture gaps this run observed (fault-isolation §4.2, additive, omitted
	// when zero): how many fillable and terminal ledger gaps it observed, how many
	// of them were new this run (what a monitor keys on, FC23(2)), how many earlier
	// gaps it recovered, how many fillable ones are stuck, and how many partly-read
	// records it wrote or re-examined.
	GapsObserved  int `json:"gaps_observed,omitempty"`
	GapsNew       int `json:"gaps_new,omitempty"`
	GapsRecovered int `json:"gaps_recovered,omitempty"`
	GapsFillable  int `json:"gaps_fillable,omitempty"`
	GapsTerminal  int `json:"gaps_terminal,omitempty"`
	GapsStuck     int `json:"gaps_stuck,omitempty"`
	Unreadable    int `json:"unreadable,omitempty"`

	// Undecodable and FidelityFallback (design-mime-fidelity §4.9, MC19; additive,
	// omitted when zero) are how many records, at the end of this run, carry a part this
	// version cannot decode (kept as received) and how many were written from the earlier
	// parser's content. Informational: neither makes the run partial.
	Undecodable      int `json:"undecodable,omitempty"`
	FidelityFallback int `json:"fidelity_fallback,omitempty"`

	// Presync is the Thunderbird pre-sync's trace in this run (design §4.9.2):
	// the in-flight "syncing" state while the session runs, then the final
	// outcome code and the streak counters. Additive: nil when the feature is off
	// or on an older record, so `status` reads a record without it as "not
	// configured" and an older binary ignores it.
	Presync *v151PresyncRecord `json:"presync,omitempty"`

	// Program is an encrypted archive's program step outcome, one of the Program*
	// codes (program design §4, §8). Absent when the step did not run.
	Program string `json:"program,omitempty"`

	// Encrypted marks the record of an encrypted archive: WriteLastRun then writes
	// only the allow-listed fields (encryptedLastRun) and the attention notice says
	// the failure kind in fixed words (P2a-C16). The caller sets it — state does not
	// recognise an encrypted archive itself. Absent (false) on a plaintext record,
	// whose bytes it leaves unchanged.
	Encrypted bool `json:"encrypted,omitempty"`
}

type v151PresyncRecord struct {
	State         string    `json:"state,omitempty"`          // "syncing" in flight; "" at the final outcome
	Code          string    `json:"code,omitempty"`           // the outcome code (presync_*); "" for a clean ok
	Since         time.Time `json:"since,omitempty"`          // when the session started
	ExpectedUntil time.Time `json:"expected_until,omitempty"` // when it should be done
	WarnStreak    int       `json:"warn_streak,omitempty"`
	NoteStreak    int       `json:"note_streak,omitempty"`
}

type v151RunWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type v151RunHistoryEntry struct {
	When       time.Time `json:"when"`
	Status     string    `json:"status"` // RunOK | RunFailed | RunCancelled
	Exported   int       `json:"exported,omitempty"`
	Filled     int       `json:"filled,omitempty"`
	DurationMs int64     `json:"durationMs,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Error      string    `json:"error,omitempty"`
	// ErrorKind is an encrypted archive's failure kind (FailureText says it), in
	// place of Error; absent on a plaintext entry.
	ErrorKind string `json:"errorKind,omitempty"`
	// Encrypted marks an encrypted archive's entry; absent on a plaintext one.
	Encrypted bool `json:"encrypted,omitempty"`
}

// fixture is a frozen testdata file, as bytes.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// placed puts data at name in a fresh archive folder and returns the folder.
func placed(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// as7Record is the record the AS7 fixture holds: a scheduled run whose sync left a
// Thunderbird open with no window to close.
func as7Record() LastRun {
	at := time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)
	return LastRun{
		Status: RunOK, Started: at, Finished: at.Add(14 * time.Minute), PID: 4242,
		Exe: "/Applications/MailArchive Desktop.app/Contents/MacOS/mailarchive", Mode: "incremental",
		Job:      []string{"-out", "/Volumes/Backup/Mail", "-mode", "incremental", "-auto"},
		Exported: 12, Filled: 1,
		Warnings: []RunWarning{{Code: "presync_quit_no_window", Message: "Thunderbird synced, and was left open."}},
		Presync:  &PresyncRecord{Code: "presync_quit_no_window", Name: "quit-no-window", Result: PresyncResultSynced, Since: at.Add(30 * time.Second)},
	}
}

// covers: MA-916
// Records cross versions both ways (design-presync-attended §10, slice AS7, C-39):
// v1.5.1's own last-run and history bytes (frozen from ea6515a) read in today's code
// with every field kept and no sync recorded; today's records (frozen as the AS7
// fixtures, written byte for byte by today's writers) read back in today's code with
// the sync's name and result, and read in the copied v1.5.1 types without error and
// with every field v1.5.1 knows kept; today's writer extends a v1.5.1 history,
// keeping its entries. The stated residual is pinned: a v1.5.1 run rewriting the
// history decodes it into its older type and drops every saved presync name.
func TestRecordsCrossVersions(t *testing.T) {
	// v1.5.1 → today.
	old := placed(t, LastRunName, fixture(t, "v151/lastrun.json"))
	lr, st, err := ReadLastRun(old)
	if st != LastRunPresent || err != nil {
		t.Fatalf("today's reader refuses a v1.5.1 record: %v %v", st, err)
	}
	if lr.Status != RunOK || lr.Exported != 12 || lr.Filled != 1 || len(lr.Job) != 5 || len(lr.Warnings) != 1 ||
		lr.Warnings[0].Code != "presync_no_activity" || lr.Presync != nil || !lr.Finished.Equal(time.Date(2026, 10, 27, 2, 14, 0, 0, time.UTC)) {
		t.Errorf("a v1.5.1 record read as %+v", lr)
	}
	if err := os.WriteFile(filepath.Join(old, RunHistoryName), fixture(t, "v151/runhistory.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := ReadRunHistory(old)
	if err != nil || len(h) != 3 {
		t.Fatalf("today's reader on a v1.5.1 history: %d entries, %v", len(h), err)
	}
	if h[1].Status != RunFailed || h[1].Error != "the backup drive was not mounted" || h[2].Exported != 6 || h[0].DurationMs != 61000 {
		t.Errorf("a v1.5.1 history read as %+v", h)
	}
	for _, e := range h {
		if e.Presync != "" {
			t.Errorf("a v1.5.1 entry read with a presync name %q", e.Presync)
		}
	}
	// Today's writer extends it: the v1.5.1 entries stay, the new one carries its name.
	if err := AppendRunHistory(old, RunHistoryEntryFrom(as7Record())); err != nil {
		t.Fatal(err)
	}
	if h, _ = ReadRunHistory(old); len(h) != 4 || h[1].Error != "the backup drive was not mounted" || h[3].Presync != "quit-no-window" {
		t.Errorf("today's append to a v1.5.1 history: %+v", h)
	}

	// Today's bytes are the AS7 fixtures, exactly: a renamed key fails here.
	now := t.TempDir()
	if err := WriteLastRun(now, as7Record()); err != nil {
		t.Fatal(err)
	}
	if err := AppendRunHistory(now, RunHistoryEntryFrom(as7Record())); err != nil {
		t.Fatal(err)
	}
	for name, file := range map[string]string{"as7/lastrun-presync.json": LastRunName, "as7/runhistory-presync.json": RunHistoryName} {
		got, _ := os.ReadFile(filepath.Join(now, file))
		if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(fixture(t, name))) {
			t.Errorf("today's %s is not the frozen %s:\n%s", file, name, got)
		}
	}
	// Today's reader on the frozen AS7 fixtures.
	lr, st, err = ReadLastRun(placed(t, LastRunName, fixture(t, "as7/lastrun-presync.json")))
	if st != LastRunPresent || err != nil || lr.Presync == nil || lr.Presync.Name != "quit-no-window" ||
		lr.Presync.Result != PresyncResultSynced || lr.Presync.Code != "presync_quit_no_window" {
		t.Errorf("today's reader on today's record: %+v (%v)", lr.Presync, err)
	}
	h, err = ReadRunHistory(placed(t, RunHistoryName, fixture(t, "as7/runhistory-presync.json")))
	if err != nil || len(h) != 1 || h[0].Presync != "quit-no-window" {
		t.Errorf("today's reader on today's history: %+v (%v)", h, err)
	}

	// Today → v1.5.1 (the copied types).
	var olr v151LastRun
	if err := json.Unmarshal(fixture(t, "as7/lastrun-presync.json"), &olr); err != nil {
		t.Fatalf("v1.5.1 cannot read today's record: %v", err)
	}
	if olr.Status != RunOK || olr.Exported != 12 || olr.Presync == nil || olr.Presync.Code != "presync_quit_no_window" || len(olr.Warnings) != 1 {
		t.Errorf("v1.5.1 read today's record as %+v", olr)
	}
	var oh []v151RunHistoryEntry
	if err := json.Unmarshal(fixture(t, "as7/runhistory-presync.json"), &oh); err != nil {
		t.Fatalf("v1.5.1 cannot read today's history: %v", err)
	}
	if len(oh) != 1 || oh[0].Status != RunOK || oh[0].Exported != 12 || oh[0].DurationMs != 840000 {
		t.Errorf("v1.5.1 read today's history as %+v", oh)
	}
	// The stated residual (§10 (2)): v1.5.1's AppendRunHistory decodes into its own
	// type and writes it back, so one v1.5.1 run drops every saved presync name.
	oh = append(oh, v151RunHistoryEntry{When: time.Now().UTC(), Status: RunOK})
	rewritten, _ := json.MarshalIndent(oh, "", "  ")
	if strings.Contains(string(rewritten), "presync") {
		t.Errorf("the pinned residual changed: a v1.5.1 rewrite kept presync:\n%s", rewritten)
	}
}

// covers: MA-905, MA-903
// The readers strip control characters from the sync's strings (§4.2, §4.3, C-37):
// ReadLastRun from every presync field, as it does for the job, error and warnings,
// and ReadRunHistory from every entry's strings, the presync name included. The
// history entry copies the record's name for a plaintext run and never for an
// encrypted one. Positive twin first: a clean record reads back unchanged.
func TestTheSyncsStringsAreStrippedOnRead(t *testing.T) {
	clean := t.TempDir()
	rec := as7Record()
	if err := WriteLastRun(clean, rec); err != nil {
		t.Fatal(err)
	}
	if lr, _, _ := ReadLastRun(clean); lr.Presync == nil || lr.Presync.Name != rec.Presync.Name || lr.Presync.Code != rec.Presync.Code ||
		lr.Presync.Result != rec.Presync.Result || !lr.Presync.Since.Equal(rec.Presync.Since) {
		t.Fatalf("positive twin: a clean presync read back as %+v", lr.Presync)
	}

	hostile := t.TempDir()
	rec.Presync = &PresyncRecord{State: "sync\x1b[2Jing", Code: "presync_\x07ok", Name: "ok\n\rforged", Result: "syn\x1bced"}
	if err := WriteLastRun(hostile, rec); err != nil {
		t.Fatal(err)
	}
	lr, st, _ := ReadLastRun(hostile)
	if st != LastRunPresent || lr.Presync == nil {
		t.Fatal("the hostile record did not read")
	}
	for _, s := range []string{lr.Presync.State, lr.Presync.Code, lr.Presync.Name, lr.Presync.Result} {
		if strings.ContainsAny(s, "\x1b\x07\n\r") {
			t.Errorf("a presync string kept a control character: %q", s)
		}
	}
	if lr.Presync.Name != "okforged" {
		t.Errorf("the stripped name is %q, want okforged", lr.Presync.Name)
	}

	e := RunHistoryEntryFrom(rec)
	if e.Presync != rec.Presync.Name {
		t.Errorf("a plaintext entry's presync is %q, want the record's name", e.Presync)
	}
	if err := AppendRunHistory(hostile, e); err != nil {
		t.Fatal(err)
	}
	if err := AppendRunHistory(hostile, RunHistoryEntry{When: time.Now(), Status: "o\x1bk", Error: "bad\x1b[31m", Mode: "in\x07c", ErrorKind: "x\ny"}); err != nil {
		t.Fatal(err)
	}
	h, err := ReadRunHistory(hostile)
	if err != nil || len(h) != 2 {
		t.Fatalf("history: %+v %v", h, err)
	}
	for _, e := range h {
		for _, s := range []string{e.Presync, e.Status, e.Error, e.Mode, e.ErrorKind} {
			if strings.ContainsAny(s, "\x1b\x07\n\r") {
				t.Errorf("a history string kept a control character: %q", s)
			}
		}
	}
	if h[0].Presync != "okforged" {
		t.Errorf("the history's stripped presync is %q", h[0].Presync)
	}

	rec.Encrypted = true
	if e := RunHistoryEntryFrom(rec); e.Presync != "" {
		t.Errorf("an encrypted entry carries presync %q", e.Presync)
	}
}
