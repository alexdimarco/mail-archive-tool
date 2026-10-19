package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-623, R18, S59
// Rollback reads every file (SC32, SC33, SC36): an older-shaped reader decodes the
// files SE2 writes, because they add no required field and only optional ones
// (non-strict json.Unmarshal ignores what it does not know); and the composed
// identity predicate judges a pre-SE2 descriptor naming mailarchivew.exe, with
// self outside the recorded file's directory, by rules 1, 6 and 7 — this program
// and silent while the file is present and unchanged, RED exe_missing once it is
// removed, WARN exe_moved only if it was rewritten in place. It proves the files,
// not the installed product's rollback (L-W10).
func TestMA623_RollbackReadsEveryFile(t *testing.T) {
	// 1. Forward-compatible decode: a document carrying fields (incl. a future one)
	// an older reader never knew still decodes, and the known fields land.
	var d schedule.Descriptor
	if err := json.Unmarshal([]byte(`{"name":"mailarchive-backup","exe":"/opt/MailArchive/mailarchivew.exe","a_future_field":true}`), &d); err != nil {
		t.Fatalf("descriptor decode: %v", err)
	}
	if d.Name != "mailarchive-backup" || d.Exe == "" {
		t.Errorf("descriptor fields did not land: %+v", d)
	}

	var in instance.Info
	if err := json.Unmarshal([]byte(`{"v":1,"pid":7,"dashboard":"127.0.0.1:8097","key":"k","version":"1.2.0","former":"ended","ended":{"pid":9,"program":"mailarchivew.exe desktop"},"a_future_field":1}`), &in); err != nil {
		t.Fatalf("instance decode: %v", err)
	}
	if in.V != 1 || in.Version != "1.2.0" || in.Former != "ended" || in.Ended == nil || in.Ended.PID != 9 {
		t.Errorf("instance optional fields did not land: %+v", in)
	}

	var j job.Job
	if err := json.Unmarshal([]byte(`{"version":1,"out":"/data/archive","a_future_field":"x"}`), &j); err != nil {
		t.Fatalf("job decode: %v", err)
	}
	if j.Version != 1 {
		t.Errorf("job version did not land: %+v", j)
	}

	// 2. The composed predicate for a mailarchivew.exe descriptor, self elsewhere.
	orig := schedule.SelfResolver
	t.Cleanup(func() { schedule.SelfResolver = orig })
	schedule.SelfResolver = func() (string, error) { return filepath.Join(t.TempDir(), "mailarchive"), nil } // a different directory

	dir := t.TempDir()
	rec := filepath.Join(dir, "mailarchivew.exe")
	if err := os.WriteFile(rec, []byte("the recorded program"), 0o755); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(rec)
	if err != nil {
		t.Fatal(err)
	}

	// present and unchanged → this program, silent (rule 6).
	id := exeIdentity(schedule.Descriptor{Exe: rec, ExeSize: fi.Size(), ExeMTime: fi.ModTime().UTC()})
	if !id.exists || !id.isThis || id.basis != "unchanged_since_install" {
		t.Errorf("present+unchanged: got %+v, want this program (unchanged_since_install)", id)
	}

	// rewritten in place (mtime no longer matches, size unchanged) → WARN exe_moved (rule 7).
	id = exeIdentity(schedule.Descriptor{Exe: rec, ExeSize: fi.Size(), ExeMTime: fi.ModTime().UTC().Add(-time.Hour)})
	if !id.exists || id.isThis {
		t.Errorf("rewritten in place: got %+v, want exists && not this program (exe_moved)", id)
	}

	// removed → RED exe_missing (rule 1).
	if err := os.Remove(rec); err != nil {
		t.Fatal(err)
	}
	id = exeIdentity(schedule.Descriptor{Exe: rec, ExeSize: fi.Size(), ExeMTime: fi.ModTime().UTC()})
	if id.exists || id.isThis {
		t.Errorf("removed: got %+v, want neither exists nor this program (exe_missing)", id)
	}
}
