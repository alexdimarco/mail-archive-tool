package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/assure/leak"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/state"
)

// scriptedSync is a pre-sync hook that ends as outcome says, with a warning, and calls
// during from inside the session (a Cancel or a Lock arriving while it waits).
func scriptedSync(opts Options, outcome PresyncOutcome, warn string, during func()) Options {
	opts.BeforeRead = func(ctx context.Context, files []string) BeforeReadResult {
		if during != nil {
			during()
		}
		o := outcome
		var ws []state.RunWarning
		if warn != "" {
			ws = []state.RunWarning{{Code: o.Code, Message: warn}}
		}
		return BeforeReadResult{Warnings: ws, Outcome: &o}
	}
	return opts
}

// rawRecord is the last-run file on disk as JSON keys and values.
func rawRecord(t *testing.T, out string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, state.LastRunName))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("the last-run file is not JSON: %v\n%s", err, b)
	}
	return m
}

// rawHistory is the run-history file on disk, one key map per entry.
func rawHistory(t *testing.T, out string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, state.RunHistoryName))
	if err != nil {
		t.Fatal(err)
	}
	var h []map[string]any
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatalf("the history file is not JSON: %v\n%s", err, b)
	}
	return h
}

func jsonKeys(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// covers: MA-903, R32
// The sync's outcome travels through a real app.Run to the plaintext last-run FILE and
// the run history (design-presync-attended §4.1, §4.2, §4.3, slice AS7; C-12, C-36,
// C-53, C-80): the hook block converts the in-memory outcome into the record's own
// type, holding only code, name, result and since, on a full success and on a failure
// after the hook (an index refusal), and on a cancel and a recovered panic; the
// history's entry carries the outcome's name as "presync". A run with no sync hook
// records no presync at all (positive twin of the absence).
func TestThePresyncOutcomeIsRecorded(t *testing.T) {
	started := time.Date(2026, 10, 30, 9, 15, 0, 0, time.UTC)
	wantPresync := func(t *testing.T, out, name, code, result string) {
		t.Helper()
		lr := rawRecord(t, out)
		p, ok := lr["presync"].(map[string]any)
		if !ok {
			t.Fatalf("the last-run file has no presync object: %v", jsonKeys(lr))
		}
		for _, k := range jsonKeys(p) {
			switch k {
			case "code", "name", "result", "since":
			default:
				t.Errorf("the record's presync carries %q, outside code, name, result and since", k)
			}
		}
		if p["name"] != name || p["result"] != result {
			t.Errorf("presync = %v, want name %q and result %q", p, name, result)
		}
		if code != "" && p["code"] != code {
			t.Errorf("presync code = %v, want %q", p["code"], code)
		}
		h := rawHistory(t, out)
		if last := h[len(h)-1]; last["presync"] != name {
			t.Errorf("the history's last entry has presync %v, want %q", last["presync"], name)
		}
	}

	t.Run("positive twin: no sync hook records no presync", func(t *testing.T) {
		out := tmpDir(t)
		if _, err := Run(context.Background(), Options{Inputs: []string{completeMaildir(t)}, Out: out}, discard(), nil); err != nil {
			t.Fatal(err)
		}
		if _, ok := rawRecord(t, out)["presync"]; ok {
			t.Error("a run with no sync wrote a presync record")
		}
		h := rawHistory(t, out)
		assure.Reached(t, len(h), "history entries")
		if _, ok := h[len(h)-1]["presync"]; ok {
			t.Error("a run with no sync wrote a presync history name")
		}
	})

	t.Run("a full success", func(t *testing.T) {
		out := tmpDir(t)
		o := PresyncOutcome{Code: "presync_quit_no_window", Name: "quit-no-window", Class: "warn", Synced: true,
			Left: "left-open", Line: "Thunderbird synced, and was left open.", Started: started, Waited: 6 * time.Minute}
		if _, err := Run(context.Background(), scriptedSync(Options{Inputs: []string{completeMaildir(t)}, Out: out}, o, "", nil), discard(), nil); err != nil {
			t.Fatal(err)
		}
		wantPresync(t, out, "quit-no-window", "presync_quit_no_window", "synced")
		lr := recordOnDisk(t, out)
		got := assure.Reached(t, lr.Presync, "the record's presync")
		if !got.Since.Equal(started) || got.State != "" || got.WarnStreak != 0 || got.NoteStreak != 0 || !got.ExpectedUntil.IsZero() {
			t.Errorf("presync read back as %+v; want since the start and none of the older fields", got)
		}
	})

	t.Run("a failure after the hook", func(t *testing.T) {
		out := tmpDir(t)
		if err := os.MkdirAll(filepath.Join(out, "search.db", "x"), 0o755); err != nil {
			t.Fatal(err)
		}
		o := PresyncOutcome{Code: "presync_wait_ended", Name: "wait-ended", Class: "note", Left: "left-open-by-rule", Started: started}
		_, err := Run(context.Background(), scriptedSync(Options{Inputs: []string{completeMaildir(t)}, Out: out, Index: true}, o, "", nil), discard(), nil)
		if err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want the index open's failure", err)
		}
		if lr := recordOnDisk(t, out); lr.Status != state.RunFailed {
			t.Fatalf("the record reads %q, want failed", lr.Status)
		}
		wantPresync(t, out, "wait-ended", "presync_wait_ended", "wait_ended")
	})

	t.Run("a cancel during the sync, Thunderbird left open", func(t *testing.T) {
		out := tmpDir(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		o := PresyncOutcome{Name: "cancelled", Class: "none", Left: "left-open-by-rule", Started: started}
		_, err := Run(ctx, scriptedSync(Options{Inputs: []string{completeMaildir(t)}, Out: out}, o, "", cancel), discard(), nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		wantPresync(t, out, "cancelled", "", "cancelled")
	})

	t.Run("a skip", func(t *testing.T) {
		out := tmpDir(t)
		o := PresyncOutcome{Code: "presync_owner_active_skipped", Name: "owner-active-skipped", Class: "note", Left: "none"}
		if _, err := Run(context.Background(), scriptedSync(Options{Inputs: []string{completeMaildir(t)}, Out: out}, o, "", nil), discard(), nil); err != nil {
			t.Fatal(err)
		}
		wantPresync(t, out, "owner-active-skipped", "presync_owner_active_skipped", "not_synced")
		// Thunderbird never started: no start time is written.
		if _, ok := rawRecord(t, out)["presync"].(map[string]any)["since"]; ok {
			t.Error("a session that started nothing wrote a since")
		}
	})

	t.Run("a recovered panic", func(t *testing.T) {
		out := tmpDir(t)
		var p cancelProbe
		opts := probedOptions(Options{Inputs: []string{completeMaildir(t)}, Out: out}, &p, nil, func() { panic("pre-sync boom") })
		if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
			t.Fatal(err)
		}
		wantPresync(t, out, "internal-error", "", "not_synced")
	})
}

// covers: MA-903, R32
// The record's result reads the outcome alone (§4.2, §5.2, C-72, OQ-6): for every
// outcome the session can end in, and every way it can leave Thunderbird, synced
// outcomes record "synced", a cancelled run "cancelled" whatever it left open, a wait
// the person stopped "wait_ended" (never a failure), and every other "not_synced".
// Synced() itself is checked against §5.2's table written out here. Neither the
// record's type nor the in-memory outcome has anything named for attendance (C-80).
func TestThePresyncResultReadsTheOutcomeAlone(t *testing.T) {
	synced := map[string]bool{ // §5.2's synced rows, written out
		"ok": true, "watch-truncated": true, "not-quiet": true, "quit-not-permitted": true, "quit-timeout": true,
		"quit-skipped-others": true, "quit-no-window": true, "owner-active": true, "identity-unavailable": true,
		"ok-noactivity": true, "watch-truncated-noactivity": true, "already-running": true,
	}
	outcomes := presync.Outcomes()
	assure.Reached(t, len(outcomes), "the session's outcomes")
	lefts := []string{"none", "exited", "closed", "left-open", "left-open-by-choice", "left-open-by-rule", "may-be-starting"}
	for _, o := range outcomes {
		if o.Synced() != synced[o.Name] {
			t.Errorf("%s.Synced() = %v, want %v (§5.2)", o.Name, o.Synced(), synced[o.Name])
		}
		want := "not_synced"
		switch {
		case o.Name == presync.OutcomeCancelled.Name:
			want = "cancelled"
		case o.Name == presync.OutcomeWaitEnded.Name:
			want = "wait_ended"
		case synced[o.Name]:
			want = "synced"
		}
		for _, left := range lefts {
			rec := assure.Reached(t, presyncRecordFor(&PresyncOutcome{Code: o.Code, Name: o.Name, Class: o.Class.String(), Synced: o.Synced(), Left: left}), "the converted record")
			if rec.Result != want || rec.Name != o.Name || rec.Code != o.Code {
				t.Errorf("%s left %s: record %+v, want result %q", o.Name, left, rec, want)
			}
		}
	}
	if presyncRecordFor(nil) != nil {
		t.Error("no outcome must convert to no record")
	}
	for _, ty := range []reflect.Type{reflect.TypeOf(state.PresyncRecord{}), reflect.TypeOf(PresyncOutcome{})} {
		for i := 0; i < ty.NumField(); i++ {
			f := ty.Field(i)
			if strings.Contains(strings.ToLower(f.Name+f.Tag.Get("json")), "attended") {
				t.Errorf("%s.%s is named for attendance (C-36, C-80)", ty.Name(), f.Name)
			}
		}
	}
}

// covers: MA-904, R37
// An encrypted archive keeps nothing about the sync beside it (§4.4, slice AS7's
// record parts; C-16, A6, A18): after a synced, a skipped and a Lock-cancelled run of
// a keyed archive whose hook planted a profile path in its line and warning, the
// last-run file's keys and every history entry's keys are within the A18 allow-list
// (and hold its required fields), the record recordRun hands the writer has no presync
// at all (opts.Key set), the tree grammar holds, and a scan of the whole folder finds
// no outcome code or name, no "presync", "thunderbird" and no planted profile path;
// no mailarchive.log and no sync-streak file. Positive twin first: the same scripted
// run of a plaintext archive writes presync to the record and the history.
func TestAnEncryptedArchiveKeepsNothingOfTheSync(t *testing.T) {
	marker := "qzprofilemarkerxv"
	profileLine := "Thunderbird synced: /Users/someone/Library/Thunderbird/Profiles/" + marker + ".default"
	started := time.Date(2026, 10, 30, 9, 15, 0, 0, time.UTC)
	var handed []*state.PresyncRecord
	orig := writeLastRun
	writeLastRun = func(out string, r state.LastRun) error {
		handed = append(handed, r.Presync)
		return orig(out, r)
	}
	t.Cleanup(func() { writeLastRun = orig })

	syncedOutcome := PresyncOutcome{Code: "", Name: "ok", Class: "none", Synced: true, Left: "left-open-by-rule", Line: profileLine, Started: started}

	t.Run("positive twin: a plaintext archive records the sync", func(t *testing.T) {
		handed = nil
		out := tmpDir(t)
		if _, err := Run(context.Background(), scriptedSync(Options{Inputs: []string{completeMaildir(t)}, Out: out}, syncedOutcome, profileLine, nil), discard(), nil); err != nil {
			t.Fatal(err)
		}
		if len(handed) == 0 || handed[len(handed)-1] == nil {
			t.Fatalf("the plaintext run handed the writer no presync: %v", handed)
		}
		if p, ok := rawRecord(t, out)["presync"].(map[string]any); !ok || p["name"] != "ok" {
			t.Errorf("the plaintext record has no presync named ok: %v", rawRecord(t, out))
		}
		if h := rawHistory(t, out); h[len(h)-1]["presync"] != "ok" {
			t.Errorf("the plaintext history has no presync: %v", h)
		}
	})

	allowRecord := map[string]bool{"version": true, "status": true, "started": true, "finished": true, "error_kind": true, "program": true, "encrypted": true}
	allowEntry := map[string]bool{"when": true, "status": true, "errorKind": true, "encrypted": true}
	var needles []string
	for _, o := range presync.Outcomes() {
		if o.Code != "" {
			needles = append(needles, o.Code)
		}
		// Short or status-word names ("ok", "cancelled") are the record's own status
		// words: the key-set check covers them.
		if len(o.Name) >= 6 && o.Name != presync.OutcomeCancelled.Name {
			needles = append(needles, o.Name)
		}
	}
	needles = append(needles, "presync", "thunderbird", "Thunderbird", marker, "synced", "wait_ended")

	cases := []struct {
		name    string
		outcome PresyncOutcome
		lock    bool
	}{
		{"synced", syncedOutcome, false},
		{"skipped", PresyncOutcome{Code: "presync_owner_active_skipped", Name: "owner-active-skipped", Class: "note", Left: "none", Line: profileLine}, false},
		{"Lock-cancelled", PresyncOutcome{Name: "cancelled", Class: "none", Left: "left-open-by-rule", Line: profileLine, Started: started}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			handed = nil
			out, key := encryptedOut(t)
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			var during func()
			if c.lock {
				during = func() { cancel(errors.New("the archive was locked during the sync")) }
			}
			_, err := Run(ctx, scriptedSync(Options{Inputs: []string{completeMaildir(t)}, Out: out, Key: key}, c.outcome, profileLine, during), discard(), nil)
			if c.lock != errors.Is(err, context.Canceled) || (!c.lock && err != nil) {
				t.Fatalf("err = %v (lock %v)", err, c.lock)
			}
			assure.Reached(t, len(handed), "records handed to the writer")
			for i, p := range handed {
				if p != nil {
					t.Errorf("record %d handed to the writer carries presync %+v for a keyed run", i, p)
				}
			}
			lr := rawRecord(t, out)
			for _, k := range jsonKeys(lr) {
				if !allowRecord[k] {
					t.Errorf("the encrypted last-run carries %q, outside the A18 allow-list", k)
				}
			}
			for _, k := range []string{"version", "status", "started", "finished", "encrypted"} {
				if _, ok := lr[k]; !ok {
					t.Errorf("the encrypted last-run lacks %q: %v", k, jsonKeys(lr))
				}
			}
			h := rawHistory(t, out)
			assure.Reached(t, len(h), "history entries")
			for i, e := range h {
				for _, k := range jsonKeys(e) {
					if !allowEntry[k] {
						t.Errorf("history entry %d carries %q, outside the A18 allow-list", i, k)
					}
				}
				for _, k := range []string{"when", "status", "encrypted"} {
					if _, ok := e[k]; !ok {
						t.Errorf("history entry %d lacks %q: %v", i, k, jsonKeys(e))
					}
				}
			}
			requireTree(t, c.name, out, leak.AtRest)
			for _, n := range needles {
				hits, err := leak.ScanTree(out, leak.Markers{leak.SourcePath: n})
				if err != nil {
					t.Fatal(err)
				}
				for _, hit := range hits {
					t.Errorf("the encrypted archive names %q: %s", n, hit)
				}
			}
			for _, f := range []string{"mailarchive.log", ".mailarchive-syncstreak.json"} {
				if _, err := os.Stat(filepath.Join(out, f)); !os.IsNotExist(err) {
					t.Errorf("an encrypted archive holds %s (%v)", f, err)
				}
			}
		})
	}
}
