package app

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/tbrestore"
)

// covers: MA-1138, R41, S70
// A backup run leaves a restore's leftover out and says so in its run log (design §7.2
// RT-5 note): the run builds the skip set from restores.log (tbrestore.LogPath, in
// MailArchive's default settings folder whatever -config says — here a temp log through
// the seam), the leftover's mail is not in the archive, Thunderbird's own folder is,
// and the log line says "Left out 1 folder that MailArchive is restoring, or has
// restored, into Thunderbird" (not a fault). Positive twin: with no record of that
// restore, the same folder is backed up.
func TestBackupRunLeavesOutARestoreLeftover(t *testing.T) {
	root := t.TempDir()
	local := filepath.Join(root, "p", "Mail", "Local Folders")
	if err := os.MkdirAll(local, 0o700); err != nil {
		t.Fatal(err)
	}
	local, _ = filepath.EvalSymlinks(local)
	if err := os.WriteFile(filepath.Join(local, "Trash"), []byte("From - x\nSubject: kept\nMessage-ID: <k@x>\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), tbrestore.LogName)
	id, _ := tbrestore.DirIdentity(local)
	lf := tbrestore.LocalFolders{Dir: local, Identity: id}
	layout, err := tbrestore.PlanLayout("Work", []tbrestore.FolderIn{{Names: []string{"Inbox"}}}, local, "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := tbrestore.OpenLocalRoot(lf)
	if err != nil {
		t.Fatal(err)
	}
	rid, _ := tbrestore.NewID()
	res := tbrestore.Write(context.Background(), tbrestore.NewRootFS(r), tbrestore.WriteInput{ID: rid, ProfileDir: filepath.Join(root, "p"), Local: lf,
		Layout: layout, Top: "Restored — Work", Log: tbrestore.NewLog(logPath), Messages: func(int, func([]byte) error) error { return nil }})
	r.Close()
	if res.Outcome != tbrestore.OutDone {
		t.Fatal(res.Err)
	}
	_ = os.Remove(filepath.Join(local, "Restored — Work")) // the leftover: no parent file
	if err := os.WriteFile(filepath.Join(local, "Restored — Work.sbd", "Inbox"), []byte("From - x\nSubject: restored\nMessage-ID: <r@x>\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	run := func(log string) (string, int) {
		t.Helper()
		was := restoreSkipLogPath
		restoreSkipLogPath = func() (string, error) { return log, nil }
		defer func() { restoreSkipLogPath = was }()
		var buf bytes.Buffer
		res, err := Run(context.Background(), Options{Inputs: []string{local}, Out: filepath.Join(t.TempDir(), "archive")}, logNew(&buf), nil)
		if err != nil {
			t.Fatal(err)
		}
		return buf.String(), res.ManifestSize
	}
	out, n := run(logPath)
	if n != 1 || !strings.Contains(out, "Left out 1 folder that MailArchive is restoring, or has restored, into Thunderbird") {
		t.Fatalf("the leftover left out, said in the run log: %d records\n%s", n, out)
	}
	out, n = run(filepath.Join(t.TempDir(), tbrestore.LogName))
	if n != 2 || strings.Contains(out, "Left out") {
		t.Fatalf("with no record of the restore, the folder is backed up: %d records\n%s", n, out)
	}
}

func logNew(b *bytes.Buffer) *log.Logger { return log.New(b, "", 0) }

// covers: MA-1140, R40, R31, S71
// Every backup — scheduled (which never takes -config), Desktop's own runs (a Desktop
// started with -config included) and the command line — reads restores.log from
// MailArchive's default settings folder, job.Dir(), through tbrestore.LogPath: the run
// takes no settings path at all, so a restore a -config Desktop logs (also through
// LogPath) is the one a scheduled backup's skip sees (design §7.4 foldcheck note M4).
// Presync finds the restore lock the same way (internal/presync's MA-1140 test).
func TestEveryBackupReadsTheOneRestoreRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	got, err := restoreSkipLogPath()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := tbrestore.LogPath()
	dir, _ := tbrestore.SettingsDir()
	if got != want || filepath.Dir(got) != dir || !strings.HasPrefix(got, home) {
		t.Fatalf("the run's restore record: %s, want %s in %s", got, want, dir)
	}
	lr := &localRun{}
	if lr.restoreSkipSet() == nil || lr.restoreSkipSet() != lr.restoreSkipSet() {
		t.Fatal("the record is read once per run")
	}
}
