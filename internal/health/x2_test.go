package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

func joinReasons(rep Report) string { return strings.Join(rep.Reasons, "\n") }

// covers: MA-155, R18, S31
// status reflects verify's separate last-verify record: a "Last verify" line in
// the summary (attested / NOT attested with counts), a RED posture when the
// last verify found modified/missing files (with the restore/re-export remedy),
// and a WARN when it left files unrecorded. Positive twin: an attested verify
// shows the attested line and does not drive RED.
func TestStatusLastVerify(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	when := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)

	// Attested: summary shows the attested line; posture is not RED for it.
	att := healthyInput(now)
	att.LastVerifyState = state.LastVerifyPresent
	att.LastVerify = state.LastVerify{Status: state.VerifyDone, Started: when, Finished: &when, Attested: true, Records: 100, WithFixity: 100, Checked: 100, OK: 100}
	lines := strings.Join(Summary(att, Assess(att, now)), "\n")
	if !strings.Contains(lines, "Last verify: 2026-09-01 02:00 → attested") {
		t.Errorf("attested last-verify line missing/UTC:\n%s", lines)
	}
	if Assess(att, now).Posture == "RED" {
		t.Errorf("an attested verify must not drive RED: %v", Assess(att, now).Reasons)
	}

	// modified+missing → RED with the restore/re-export/re-verify remedy.
	bad := healthyInput(now)
	bad.LastVerifyState = state.LastVerifyPresent
	bad.LastVerify = state.LastVerify{Status: state.VerifyDone, Started: when, Finished: &when, Attested: false, Modified: 2, Missing: 1, ExitCode: 2}
	rep := Assess(bad, now)
	if rep.Posture != "RED" {
		t.Errorf("modified/missing verify should be RED, got %s", rep.Posture)
	}
	r := joinReasons(rep)
	if !strings.Contains(r, "NOT intact") || !strings.Contains(r, "restore") || !strings.Contains(r, "verify -out") {
		t.Errorf("not-attested RED missing the remedy:\n%s", r)
	}
	sl := strings.Join(Summary(bad, rep), "\n")
	if !strings.Contains(sl, "Last verify: 2026-09-01 02:00 → NOT attested (modified 2, missing 1, unrecorded 0)") {
		t.Errorf("NOT-attested summary line wrong:\n%s", sl)
	}

	// unrecorded-only → WARN with the baseline remedy (not RED).
	un := healthyInput(now)
	un.LastVerifyState = state.LastVerifyPresent
	un.LastVerify = state.LastVerify{Status: state.VerifyDone, Started: when, Finished: &when, Attested: false, Unrecorded: 3, ExitCode: 2}
	urep := Assess(un, now)
	if urep.Posture != "WARN" {
		t.Errorf("unrecorded-only verify should be WARN, got %s (%v)", urep.Posture, urep.Reasons)
	}
	if !strings.Contains(joinReasons(urep), "verify -record") {
		t.Errorf("unrecorded-only WARN missing the baseline remedy:\n%s", joinReasons(urep))
	}

	// A verify older than twice its own verify schedule cadence is stale (WARN).
	stale := healthyInput(now)
	stale.Desc.Job = []string{"verify", "-out", "/a"}
	stale.Desc.Interval = "daily"
	old := now.Add(-72 * time.Hour)
	stale.LastVerifyState = state.LastVerifyPresent
	stale.LastVerify = state.LastVerify{Status: state.VerifyDone, Started: old, Finished: &old, Attested: true}
	if !strings.Contains(joinReasons(Assess(stale, now)), "more than twice the daily verify schedule period") {
		t.Errorf("stale-verify WARN missing:\n%s", joinReasons(Assess(stale, now)))
	}
}

// covers: MA-155, R18, S29
// status -json is version 2: it adds a fixity coverage object, a last_verify
// object (both present, null when absent), a reason_codes array parallel to
// reasons, and last_run.finished is null while a run is in progress (UJ-A5).
func TestStatusJSONV2(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	when := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)

	in := healthyInput(now)
	in.WithFixity = 60
	in.LastVerifyState = state.LastVerifyPresent
	in.LastVerify = state.LastVerify{Status: state.VerifyDone, Started: when, Finished: &when, Attested: true, Records: 100, WithFixity: 60}
	doc := JSON(in, Assess(in, now))
	if doc.Version != 2 {
		t.Errorf("status -json version = %d, want 2", doc.Version)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"fixity", "last_verify", "reason_codes"} {
		if _, ok := m[k]; !ok {
			t.Errorf("status -json v2 missing key %q:\n%s", k, data)
		}
	}
	fx, ok := m["fixity"].(map[string]any)
	if !ok {
		t.Fatalf("fixity is not an object:\n%s", data)
	}
	for _, k := range []string{"records", "with_fixity"} {
		if _, ok := fx[k]; !ok {
			t.Errorf("fixity missing key %q", k)
		}
	}
	if _, ok := m["last_verify"].(map[string]any); !ok {
		t.Fatalf("last_verify is not an object when a verify is recorded:\n%s", data)
	}

	// last_verify is null (present) when no verify is recorded.
	noV := healthyInput(now)
	dj, _ := json.Marshal(JSON(noV, Assess(noV, now)))
	var mm map[string]any
	json.Unmarshal(dj, &mm)
	if v, ok := mm["last_verify"]; !ok || v != nil {
		t.Errorf("last_verify should be present and null when no verify recorded: %v\n%s", v, dj)
	}

	// last_run.finished is null while a run is in progress.
	run := healthyInput(now)
	run.LastRun = state.LastRun{Status: state.RunRunning, Started: now.Add(-5 * time.Minute), PID: 42}
	run.LockHeld = true
	rj, _ := json.Marshal(JSON(run, Assess(run, now)))
	var rm map[string]any
	json.Unmarshal(rj, &rm)
	lr, ok := rm["last_run"].(map[string]any)
	if !ok {
		t.Fatalf("last_run missing:\n%s", rj)
	}
	if f, present := lr["finished"]; !present || f != nil {
		t.Errorf("last_run.finished should be present and null while running, got %v\n%s", f, rj)
	}

	// reason_codes is parallel to reasons and carries the documented codes.
	warnHeavy := healthyInput(now)
	warnHeavy.HasDescriptor = false
	warnHeavy.DescErr = os.ErrNotExist
	warnHeavy.OutUnderSync = "Dropbox"
	warnHeavy.Fillable = 1
	wdoc := JSON(warnHeavy, Assess(warnHeavy, now))
	if len(wdoc.Reasons) != len(wdoc.ReasonCodes) {
		t.Errorf("reason_codes not parallel to reasons: %d vs %d", len(wdoc.ReasonCodes), len(wdoc.Reasons))
	}
	codes := strings.Join(wdoc.ReasonCodes, ",")
	for _, want := range []string{"no_schedule", "cloud_sync", "incomplete_content"} {
		if !strings.Contains(codes, want) {
			t.Errorf("reason_codes missing %q: %v", want, wdoc.ReasonCodes)
		}
	}
}

// covers: MA-157, R18, S29
// The last-run record is untrusted: control characters in its Job are stripped
// at the read choke point (ReadLastRun), and status's pasteable remedy
// shell-quotes each job token, so a token carrying a newline/ESC and one
// carrying spaces and $() render as one inert, correctly quoted line.
func TestStatusRemedyIsInertAndQuoted(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	out := t.TempDir()
	job := []string{"-out", out, "-input", "/mail box/$(id).pst\x1b[2J\n-fake"}
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now, Finished: now, Job: job}); err != nil {
		t.Fatal(err)
	}
	lr, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent {
		t.Fatalf("read last-run: state=%v err=%v", st, err)
	}
	// Stripped at the read boundary: no control byte survives in the Job.
	if strings.ContainsAny(strings.Join(lr.Job, ""), "\x1b\r\n") {
		t.Fatalf("control characters survived in Job: %q", lr.Job)
	}

	in := healthyInput(now)
	in.Out = out
	in.HasDescriptor = false
	in.DescErr = os.ErrNotExist
	in.LastRun = lr
	in.LastRunState = st
	in.GOOS = "linux"

	var remedy string
	for _, reason := range Assess(in, now).Reasons {
		if strings.Contains(reason, "schedule") && strings.Contains(reason, "-install") {
			remedy = reason
		}
	}
	if remedy == "" {
		t.Fatalf("no schedule remedy in reasons: %v", Assess(in, now).Reasons)
	}
	if strings.ContainsAny(remedy, "\x1b\r\n") {
		t.Errorf("remedy carries control characters (not a single inert line): %q", remedy)
	}
	// The spaces/$() token is single-quoted (POSIX), so pasting it is inert.
	if !strings.Contains(remedy, "'/mail box/$(id).pst[2J-fake'") {
		t.Errorf("remedy did not shell-quote the metacharacter token:\n%s", remedy)
	}
}

// covers: MA-158, R18, S29
// The moved-archive WARN compares the two -out spellings by os.SameFile when
// both stat (so a symlink spelling of the same directory does not warn), else
// case-insensitively only on darwin/windows and byte-exact on linux.
func TestMovedArchivePathComparison(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	warned := func(in Input) bool { return strings.Contains(joinReasons(Assess(in, now)), "not this archive") }

	// SameFile: a symlink spelling of the same real directory must not warn.
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err == nil {
		in := healthyInput(now)
		in.Out = real
		in.Desc.Job = []string{"-out", link, "-auto"}
		in.GOOS = "linux"
		if warned(in) {
			t.Errorf("a symlink spelling of the same directory wrongly warned")
		}
	} else {
		t.Logf("skipping the symlink sub-case: %v", err)
	}

	// darwin/windows: two case-only spellings of a (non-existent) path fold equal.
	fold := healthyInput(now)
	fold.Out = "/nope/Archive"
	fold.Desc.Job = []string{"-out", "/nope/archive", "-auto"}
	fold.GOOS = "darwin"
	if warned(fold) {
		t.Errorf("darwin case-only difference wrongly warned as a moved archive")
	}

	// linux: the same case-only difference is a genuine different directory.
	exact := healthyInput(now)
	exact.Out = "/nope/Archive"
	exact.Desc.Job = []string{"-out", "/nope/archive", "-auto"}
	exact.GOOS = "linux"
	if !warned(exact) {
		t.Errorf("linux case-only difference should warn (case-sensitive volume)")
	}
}

// covers: MA-159, R18, S29
// A manifest file that exists but does not load (a newer format, or corrupt) is
// a RED naming the file, the loader's own wording (format version / corrupt)
// and the remedy — status no longer reports it as "no archive here". Positive
// twin: the corrupt sibling case also REDs.
func TestUnreadableManifestIsRed(t *testing.T) {
	// Newer-format manifest.
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":99,"entries":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	in := Gather(out, "")
	if !in.HasManifestFile || in.HasManifest {
		t.Fatalf("expected a present-but-unreadable manifest: file=%v loaded=%v err=%v", in.HasManifestFile, in.HasManifest, in.ManifestLoadErr)
	}
	rep := Assess(in, time.Now())
	r := joinReasons(rep)
	if rep.Posture != "RED" {
		t.Errorf("a newer-format manifest should be RED, got %s", rep.Posture)
	}
	if !strings.Contains(r, "manifest") || !strings.Contains(r, "format version") {
		t.Errorf("RED did not name the file/format-version wording:\n%s", r)
	}
	if strings.Contains(r, "no archive") {
		t.Errorf("must not report a present manifest as 'no archive':\n%s", r)
	}
	if !containsCode(rep, "manifest_unreadable") {
		t.Errorf("missing the manifest_unreadable reason code: %v", rep.Codes)
	}

	// Corrupt manifest.
	out2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(out2, ".mailarchive-manifest.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep2 := Assess(Gather(out2, ""), time.Now())
	if rep2.Posture != "RED" || !strings.Contains(joinReasons(rep2), "corrupt") {
		t.Errorf("a corrupt manifest should be RED naming 'corrupt':\n%s", joinReasons(rep2))
	}
}

// covers: MA-160, R18, S29
// The no-schedule remedy targets the archive's actual -out when it differs from
// the recorded job's, the moved-archive remedy substitutes the recorded job
// (no "..." placeholder), and the schedule line labels the installed timestamp
// UTC (friction #13/#14).
func TestStatusRemedyUsesActualOutAndUTC(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	// no-schedule remedy uses in.Out, not the (stale) recorded job -out.
	ns := healthyInput(now)
	ns.HasDescriptor = false
	ns.DescErr = os.ErrNotExist
	ns.Out = "/new/place"
	ns.GOOS = "linux"
	ns.LastRun.Job = []string{"-out", "/old/place", "-auto"}
	nr := joinReasons(Assess(ns, now))
	if !strings.Contains(nr, "/new/place") {
		t.Errorf("no-schedule remedy did not target the actual -out:\n%s", nr)
	}

	// moved-archive remedy substitutes the job (no "..."), targeting in.Out.
	mv := healthyInput(now)
	mv.Out = "/archives/current"
	mv.Desc.Name = "mailarchive-x"
	mv.Desc.Job = []string{"-out", "/archives/old", "-auto"}
	mv.GOOS = "linux"
	mr := joinReasons(Assess(mv, now))
	if !strings.Contains(mr, "not this archive") {
		t.Fatalf("expected the moved-archive WARN:\n%s", mr)
	}
	if strings.Contains(mr, "...") {
		t.Errorf("moved-archive remedy still carries a `...` placeholder:\n%s", mr)
	}
	if !strings.Contains(mr, "/archives/current -auto -install") {
		t.Errorf("moved-archive re-install remedy did not substitute this archive's job:\n%s", mr)
	}

	// The installed timestamp is rendered and labelled UTC.
	utc := healthyInput(now)
	utc.Desc.InstalledAt = time.Date(2026, 8, 31, 23, 30, 0, 0, time.UTC)
	if l := strings.Join(Summary(utc, Assess(utc, now)), "\n"); !strings.Contains(l, "installed 2026-08-31 (UTC)") {
		t.Errorf("installed timestamp not labelled UTC:\n%s", l)
	}
}

func containsCode(rep Report, code string) bool {
	for _, c := range rep.Codes {
		if c == code {
			return true
		}
	}
	return false
}
