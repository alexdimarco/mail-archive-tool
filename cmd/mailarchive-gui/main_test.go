package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// covers: MA-122, R18, S29
// openPathArgv builds the OS's default-handler command per GOOS (never
// executing): xdg-open on Linux, open on macOS, rundll32 url.dll on Windows,
// each carrying the path to open.
func TestOpenPathArgv(t *testing.T) {
	cases := []struct {
		goos, name string
		firstArg   string // the argument the path follows (or is)
	}{
		{"linux", "xdg-open", ""},
		{"darwin", "open", ""},
		{"windows", "rundll32", "url.dll,FileProtocolHandler"},
	}
	const p = "/archive/index.html"
	for _, c := range cases {
		name, args := openPathArgv(c.goos, p)
		if name != c.name {
			t.Errorf("%s: command = %q, want %q", c.goos, name, c.name)
		}
		args = assure.Reached(t, args, c.goos+" argv")
		if args[len(args)-1] != p {
			t.Errorf("%s: argv %v does not end with the path %q", c.goos, args, p)
		}
		if c.firstArg != "" && args[0] != c.firstArg {
			t.Errorf("%s: argv[0] = %q, want %q", c.goos, args[0], c.firstArg)
		}
	}
	// Windows must pass the handler verb, then the path — two arguments.
	if _, args := openPathArgv("windows", p); len(args) != 2 {
		t.Errorf("windows argv = %v, want [verb path]", args)
	}
}

// covers: MA-123, R18, S29
// repairable offers a re-install exactly for the schedule states a re-install
// from the current binary fixes — not present in the scheduler, its program
// moved, or its program gone — and never for a healthy schedule, a schedule on
// another host, or an archive with no descriptor.
func TestScheduleRepairable(t *testing.T) {
	base := func() health.Input {
		return health.Input{
			HasDescriptor: true,
			ThisHost:      "host-a",
			Desc:          schedule.Descriptor{Host: "host-a", Name: "mailarchive-1a2b3c4d", Interval: "daily", At: "02:00"},
			SchedState:    schedule.Installed,
			ExeExists:     true,
			ExeIsThis:     true,
		}
	}
	// Healthy: not offered.
	if repairable(base()) {
		t.Error("healthy schedule should not be repairable")
	}
	// No descriptor: nothing to repair.
	if in := base(); func() bool { in.HasDescriptor = false; return repairable(in) }() {
		t.Error("no descriptor should not be repairable")
	}
	// Other host: cannot repair from here.
	if in := base(); func() bool { in.Desc.Host = "host-b"; in.SchedState = schedule.NotInstalled; return repairable(in) }() {
		t.Error("a schedule on another host must not be repairable from here")
	}
	// The three repairable states.
	if in := base(); func() bool { in.SchedState = schedule.NotInstalled; return !repairable(in) }() {
		t.Error("not-installed should be repairable")
	}
	if in := base(); func() bool { in.ExeExists = false; return !repairable(in) }() {
		t.Error("a missing program should be repairable")
	}
	if in := base(); func() bool { in.ExeIsThis = false; return !repairable(in) }() {
		t.Error("a moved/replaced program should be repairable")
	}
}

// covers: MA-123, R18, S29
// repairSpec rebuilds the install Spec from the descriptor's cadence and job but
// the CURRENT executable and a wrapper path derived from the sanitized name (not
// taken from the descriptor), and the result validates.
func TestRepairSpec(t *testing.T) {
	d := schedule.Descriptor{
		Name: "mailarchive-1a2b3c4d", Interval: "weekly", At: "03:00",
		Exe: "/old/place/mailarchive-gui", Job: []string{"-job", "/cfg/mailarchive-1a2b3c4d.json"},
		Log: "/archive/mailarchive.log", Host: "host-a",
	}
	const exe = "/new/place/mailarchive-gui"
	spec, err := repairSpec("/archive", d, exe)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Exe != exe {
		t.Errorf("spec.Exe = %q, want the current executable %q (not the descriptor's %q)", spec.Exe, exe, d.Exe)
	}
	name, _ := schedule.SanitizeName(d.Name)
	if spec.Name != name {
		t.Errorf("spec.Name = %q, want sanitized %q", spec.Name, name)
	}
	if spec.WrapperPath != schedule.DefaultWrapperPath(name) {
		t.Errorf("spec.WrapperPath = %q, want the name-derived default", spec.WrapperPath)
	}
	if spec.Interval != schedule.Weekly || spec.At != "03:00" {
		t.Errorf("cadence = %s at %s, want weekly at 03:00 (from the descriptor)", spec.Interval, spec.At)
	}
	args := assure.Reached(t, spec.Args, "rebuilt job args")
	if strings.Join(args, " ") != strings.Join(d.Job, " ") {
		t.Errorf("spec.Args = %v, want the descriptor's job %v", args, d.Job)
	}
	if err := spec.Validate(); err != nil {
		t.Errorf("rebuilt spec must validate: %v", err)
	}

	// An unreadable recorded time ("??:??") is repaired to a valid default.
	d.At = "??:??"
	spec, err = repairSpec("/archive", d, exe)
	if err != nil {
		t.Fatalf("unreadable At should not block repair: %v", err)
	}
	if spec.At != "02:00" {
		t.Errorf("spec.At = %q, want the 02:00 fallback for an unreadable recorded time", spec.At)
	}
}

// covers: MA-124, R18, S29
// A failed headless (scheduled) run raises exactly one desktop notification
// naming the backup; a healthy run raises none (the positive twin) — so a
// backup that no one is watching does not fail silently, and success stays quiet.
func TestNotifyFailureHeadless(t *testing.T) {
	var fired []string
	old := notify
	notify = func(text string, _ ...zenity.Option) error { fired = append(fired, text); return nil }
	defer func() { notify = old }()

	// Failing run: the job's -out does not exist (the backup drive is unmounted).
	dir := t.TempDir()
	failJob := filepath.Join(dir, "mailarchive-fail.json")
	if err := job.Write(failJob, job.Job{Name: "mailarchive-fail", Out: filepath.Join(dir, "no-such-archive"), Mode: "incremental", Inputs: []string{"/does/not/matter"}}); err != nil {
		t.Fatal(err)
	}
	if code := runHeadless(failJob); code != 1 {
		t.Fatalf("failing run exit = %d, want 1", code)
	}
	fired = assure.Reached(t, fired, "failure notifications")
	if len(fired) != 1 {
		t.Fatalf("failure fired %d notifications, want exactly 1: %v", len(fired), fired)
	}
	if !strings.Contains(fired[0], "mailarchive-fail") || !strings.Contains(fired[0], "failed") {
		t.Errorf("notification %q must name the backup and say it failed", fired[0])
	}

	// Positive twin: a healthy run over a real one-message mbox notifies nothing.
	fired = nil
	out := filepath.Join(dir, "archive")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	mbox := filepath.Join(dir, "Inbox")
	const msg = "From - Mon Jan 01 00:00:00 2024\r\n" +
		"From: Bob <bob@example.com>\r\n" +
		"To: Me <me@example.com>\r\n" +
		"Subject: Hello\r\n" +
		"Date: Mon, 01 Jan 2024 10:00:00 +0000\r\n" +
		"Message-ID: <abc@example.com>\r\n" +
		"\r\n" +
		"plain body\r\n"
	if err := os.WriteFile(mbox, []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	okJob := filepath.Join(dir, "mailarchive-ok.json")
	if err := job.Write(okJob, job.Job{Name: "mailarchive-ok", Out: out, Mode: "full", Inputs: []string{mbox}}); err != nil {
		t.Fatal(err)
	}
	if code := runHeadless(okJob); code != 0 {
		t.Fatalf("healthy run exit = %d, want 0", code)
	}
	if len(fired) != 0 {
		t.Errorf("healthy run fired %d notifications, want 0: %v", len(fired), fired)
	}
}

// covers: MA-125, R18, S29
// The GUI success summary keeps the numeric counts status reports (X6) but in
// plain words: filled becomes "Newly downloaded this run"; the still-fillable and
// not-yet-re-examined counts merge into one "Not fully downloaded yet" line;
// source-empty becomes "Empty at the source" and is omitted at zero; keep-raw
// explains .html vs .eml.
func TestExportSummary(t *testing.T) {
	r := app.Result{
		Stats:       export.Stats{Exported: 12, Filled: 3, SkippedManifest: 4, SkippedDate: 1, Attachments: 5},
		Fillable:    2,
		Terminal:    7,
		Unknown:     1,
		ReportPath:  "/archive/attachments-report.tsv",
		IndexErrors: 0,
	}
	got := exportSummary("/archive", false, r)

	// Counts are present and correctly labelled.
	for _, want := range []string{
		"Newly downloaded this run: 3",
		"Exported:                  12",
		"Attachments archived:      5",
		"Not fully downloaded yet: 3", // 2 fillable + 1 unknown
		"Empty at the source (nothing to download): 7",
		"/archive/attachments-report.tsv",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
	// The engine's vocabulary is gone.
	for _, gone := range []string{"Filled (content arrived)", "Source-empty", "Not yet re-examined", "Still missing content"} {
		if strings.Contains(got, gone) {
			t.Errorf("summary still uses engine wording %q:\n%s", gone, got)
		}
	}
	// The re-examined remainder is noted as part of the merged line.
	if !strings.Contains(got, "older archive") {
		t.Errorf("summary should note the re-examined remainder:\n%s", got)
	}
	// Whole-archive search points at the CLI; the folder filter is named.
	if !strings.Contains(got, "mailarchive serve") || !strings.Contains(got, "filter within that folder") {
		t.Errorf("summary should reword search (CLI for whole-archive, folder filter):\n%s", got)
	}
	// Without keep-raw, nothing about .eml.
	if strings.Contains(got, ".eml") {
		t.Errorf("summary mentions .eml without keep-raw:\n%s", got)
	}

	// With keep-raw the .html/.eml distinction is explained.
	rawGot := exportSummary("/archive", true, r)
	if !strings.Contains(rawGot, ".eml") || !strings.Contains(rawGot, ".html") {
		t.Errorf("keep-raw summary should explain .html reading vs .eml restoring:\n%s", rawGot)
	}

	// Source-empty and the details line are omitted when nothing is incomplete.
	clean := exportSummary("/archive", false, app.Result{Stats: export.Stats{Exported: 1}, ReportPath: "/archive/attachments-report.tsv"})
	if strings.Contains(clean, "Empty at the source") || strings.Contains(clean, "Not fully downloaded") || strings.Contains(clean, "Details:") {
		t.Errorf("a clean run should omit the incomplete/details lines:\n%s", clean)
	}
}

// covers: MA-126, R18, S29
// The keep-raw question is asked only for sources that yield raw RFC-822 bytes
// (Thunderbird/mbox, Evolution, a single mbox, auto-detect), never for the
// Outlook .pst/.ost pick or the Outlook-app (COM) path; and the choice rides
// into the scheduled job file.
func TestKeepRawApplies(t *testing.T) {
	for _, src := range []string{srcThunderbird, srcEvolution, srcMbox, srcAuto} {
		if !keepRawApplies(src) {
			t.Errorf("keep-raw should be offered for %q", src)
		}
	}
	for _, src := range []string{srcOutlook, srcOutlookCOM} {
		if keepRawApplies(src) {
			t.Errorf("keep-raw must not be offered for %q (no raw bytes)", src)
		}
	}
	// The wizard's keep-raw answer is carried into the job file for a repeat.
	j := jobFor("mailarchive-1a2b3c4d", wizardChoice{out: "/archive", inputs: []string{"/x/Inbox"}, keepRaw: true})
	if !j.KeepRaw {
		t.Error("jobFor dropped keepRaw; the scheduled repeat would not keep originals")
	}
	if jobFor("mailarchive-1a2b3c4d", wizardChoice{out: "/archive", keepRaw: false}).KeepRaw {
		t.Error("jobFor invented keepRaw")
	}
}

// covers: MA-126, R16, S19
// On Windows with classic Outlook present, a discovered live .ost steers to the
// Outlook-app path; without Outlook, off Windows, or with no .ost among the
// finds, it does not.
func TestOutlookAppPreference(t *testing.T) {
	ost := []string{`C:\Users\me\AppData\Local\Microsoft\Outlook\me@corp.ost`}
	pst := []string{`C:\Users\me\Documents\archive.pst`}
	if !preferOutlookApp("windows", ost, true) {
		t.Error("Windows + classic Outlook + a live .ost should prefer the Outlook-app path")
	}
	if preferOutlookApp("windows", ost, false) {
		t.Error("without classic Outlook detected there is no Outlook-app path to prefer")
	}
	if preferOutlookApp("linux", ost, true) {
		t.Error("the Outlook-app path is Windows-only")
	}
	if preferOutlookApp("windows", pst, true) {
		t.Error("a .pst reads directly; no need to steer to the Outlook app")
	}
	if preferOutlookApp("windows", nil, true) {
		t.Error("nothing discovered should not prefer the Outlook-app path")
	}
}

// covers: MA-127, R18, S29
// The "nothing found" guidance is a pure function of GOOS: macOS names Apple
// Mail / New Outlook and the server-side (Graph) path; Windows and Linux name
// New Outlook's lack of .pst/.ost and the server-side path; all point back to
// choosing a type.
func TestNothingFoundMessage(t *testing.T) {
	mac := nothingFoundMessage("darwin")
	if !strings.Contains(mac, "Apple Mail") || !strings.Contains(mac, "graph-app-setup.md") {
		t.Errorf("macOS text should name Apple Mail and the Graph setup doc:\n%s", mac)
	}
	if !strings.Contains(mac, "Outlook for Mac") && !strings.Contains(mac, "New Outlook") {
		t.Errorf("macOS text should explain the Outlook-for-Mac / New Outlook case:\n%s", mac)
	}
	for _, goos := range []string{"windows", "linux"} {
		msg := nothingFoundMessage(goos)
		if !strings.Contains(msg, "New Outlook") || !strings.Contains(msg, "graph-app-setup.md") {
			t.Errorf("%s text should name New Outlook and the Graph setup doc:\n%s", goos, msg)
		}
		if strings.Contains(msg, "Apple Mail") {
			t.Errorf("%s text should not mention Apple Mail:\n%s", goos, msg)
		}
		if !strings.Contains(msg, "choose the type") {
			t.Errorf("%s text should point back at choosing a type:\n%s", goos, msg)
		}
	}
}

// covers: MA-127, R16, S19
// The Outlook-app scratch directory <out>/_outlook-pst is removed once the
// export that read it succeeded (reporting the bytes reclaimed) and kept when it
// failed, so a failed run can be retried or inspected.
func TestCleanupOutlookScratch(t *testing.T) {
	logger := discardLogger()
	makeScratch := func() string {
		out := t.TempDir()
		scratch := filepath.Join(out, "_outlook-pst")
		if err := os.MkdirAll(scratch, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scratch, "acct.pst"), make([]byte, 4096), 0o600); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Failure: keep it, reclaim nothing.
	out := makeScratch()
	if n := cleanupOutlookScratch(out, false, logger); n != 0 {
		t.Errorf("failed run reclaimed %d bytes, want 0 (kept)", n)
	}
	if _, err := os.Stat(filepath.Join(out, "_outlook-pst")); err != nil {
		t.Errorf("failed run must keep the scratch directory: %v", err)
	}

	// Success: remove it, report the bytes.
	out = makeScratch()
	n := cleanupOutlookScratch(out, true, logger)
	if n != 4096 {
		t.Errorf("reclaimed %d bytes, want 4096", n)
	}
	if _, err := os.Stat(filepath.Join(out, "_outlook-pst")); !os.IsNotExist(err) {
		t.Errorf("successful run must remove the scratch directory (stat err = %v)", err)
	}
}
