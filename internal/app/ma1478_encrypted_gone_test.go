package app

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// encGoneRun runs the fixture's three listed mailboxes into the encrypted archive out with key.
func encGoneRun(t *testing.T, ctx context.Context, out string, key *vault.Key, inputs []string, opts Options) (Result, error) {
	t.Helper()
	opts.Inputs, opts.Auto, opts.Out, opts.Key, opts.Mode = inputs, true, out, key, export.Incremental
	return Run(ctx, opts, discard(), nil)
}

// plantEncryptedGone writes an encrypted record that says a mailbox is gone since since, count
// of them (0: no count written).
func plantEncryptedGone(t *testing.T, out string, since time.Time, count int) {
	t.Helper()
	at := time.Now().UTC().Add(-time.Hour)
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: at, Finished: at, Encrypted: true,
		MailboxGone: true, MailboxGoneSince: since, MailboxGoneCount: count}); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1478, MA-734, R37
// An encrypted combined archive skips a gone mailbox too (owner, Q3), and its record carries only
// mailbox_gone, mailbox_gone_since and mailbox_gone_count beside its allow-list: no file beside
// the archive names the mailbox, its account or its folder; the name and path are in the run's
// result in memory only. mailbox_gone_since is resolved by rules 1 and 3 (owner, Q5) with the
// count's restart (owner, Q6): the same count, or a lower one, carries the earlier time; a higher
// count restarts it at this run's start; a previous record with no count counts as 1, and one
// past 1,000,000 as 1 too; a time ahead of this run's start or before MinPlausibleTime becomes
// this run's start. Positive twin first: a run that skips nothing records none of it. (Amended
// 2026-11-20, CON-02, the owner's Q8: a time ahead of this run's start is kept as recorded.)
func TestAnEncryptedArchiveKeepsOnlyThatAMailboxIsGoneItsTimeAndCount(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example", "imap.qzc.example")
	a, b, c := f.stores["imap.qza.example"], f.stores["imap.qzb.example"], f.stores["imap.qzc.example"]
	inputs := []string{a, b, c}
	out, key := encryptedOut(t)
	ctx := context.Background()
	if _, err := encGoneRun(t, ctx, out, key, inputs, Options{}); err != nil {
		t.Fatal(err)
	}
	if lr, _, _ := state.ReadLastRun(out); lr.MailboxGone || lr.MailboxGoneCount != 0 || !lr.MailboxGoneSince.IsZero() {
		t.Fatalf("positive twin: nothing skipped, nothing recorded: %+v", lr)
	}

	os.RemoveAll(b)
	from := time.Now().UTC()
	res, err := encGoneRun(t, ctx, out, key, inputs, Options{Unattended: true})
	to := time.Now().UTC()
	if err != nil {
		t.Fatalf("an encrypted archive skips a gone mailbox too: %v", err)
	}
	lr, _, _ := state.ReadLastRun(out)
	if !lr.Encrypted || !lr.MailboxGone || lr.MailboxGoneCount != 1 || !between(lr.MailboxGoneSince, from, to) || len(lr.Warnings) != 0 {
		t.Fatalf("the record: %+v", lr)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Path != b || res.Skipped[0].Name != goneAddress("imap.qzb.example") {
		t.Fatalf("the result in memory names the mailbox: %+v", res.Skipped)
	}
	raw, _ := os.ReadFile(filepath.Join(out, state.LastRunName))
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for k := range keys {
		switch k {
		case "version", "status", "started", "finished", "error_kind", "encrypted", "program", "mailbox_gone", "mailbox_gone_since", "mailbox_gone_count":
		default:
			t.Fatalf("the encrypted record carries %q:\n%s", k, raw)
		}
	}
	n := 0
	err = filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, s := range []string{"qz-imap-qzb", "imap.qzb", ".thunderbird", "ImapMail", "Not found"} {
			if strings.Contains(string(data), s) || strings.Contains(p, s) {
				t.Errorf("%s holds %q", p, s)
			}
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("the walk of the archive: %d files, %v", n, err)
	}
	first := lr.MailboxGoneSince

	since := func(t *testing.T) (time.Time, time.Time, time.Time) {
		t.Helper()
		from := time.Now().UTC()
		if _, err := encGoneRun(t, ctx, out, key, inputs, Options{Unattended: true}); err != nil {
			t.Fatal(err)
		}
		lr, _, _ := state.ReadLastRun(out)
		return lr.MailboxGoneSince, from, time.Now().UTC()
	}
	// Rule 1: the same count carries.
	if got, _, _ := since(t); !got.Equal(first) {
		t.Fatalf("the same count must carry %s, got %s", first, got)
	}
	// Q6: a second mailbox gone raises the count, so the time restarts.
	os.RemoveAll(c)
	got, from, to := since(t)
	if lr, _, _ := state.ReadLastRun(out); lr.MailboxGoneCount != 2 || !between(got, from, to) {
		t.Fatalf("a higher count restarts the time at this run's start (%s to %s): %s, %+v", from, to, got, lr)
	}
	restarted := got
	// A lower count carries it.
	tbStoreAt(t, c, tbFolder{path: "INBOX", listed: 2, msgs: tbMsgs("imap.qzc.example", 2)})
	if got, _, _ := since(t); !got.Equal(restarted) {
		t.Fatalf("a lower count must carry %s, got %s", restarted, got)
	}
	// A previous record with no count counts as 1: one gone carries its time.
	old := time.Now().UTC().Add(-20 * 24 * time.Hour).Truncate(time.Second)
	plantEncryptedGone(t, out, old, 0)
	if got, _, _ := since(t); !got.Equal(old) {
		t.Fatalf("a record with no count counts as 1, so one gone carries %s: %s", old, got)
	}
	// One past 1,000,000 counts as 1 too: two gone restart.
	os.RemoveAll(c)
	plantEncryptedGone(t, out, old, 5_000_000)
	if got, from, to := since(t); !between(got, from, to) {
		t.Fatalf("a count past 1,000,000 counts as 1, so two gone restart (%s to %s): %s", from, to, got)
	}
	// Clamps: a time ahead of this run, and one before MinPlausibleTime.
	// Amended 2026-11-20 by the 1.7.5 adversarial pass (CON-02; the owner, 2026-11-20, Q8: "Yes,
	// change it (Recommended)"): a time ahead of this run is now kept as recorded (the shared
	// clampGoneSince), so the 48-hours-ahead case asserts that at the same strength; the 2020 case
	// stays unchanged.
	ahead := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	plantEncryptedGone(t, out, ahead, 2)
	if got, _, _ := since(t); !got.Equal(ahead) {
		t.Fatalf("a planted %s ahead of this run must be kept as recorded (Q8): %s", ahead, got)
	}
	for _, planted := range []time.Time{time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)} {
		plantEncryptedGone(t, out, planted, 2)
		if got, from, to := since(t); !between(got, from, to) {
			t.Fatalf("a planted %s must become this run's start (%s to %s): %s", planted, from, to, got)
		}
	}
}

// covers: MA-1478, R37
// The encrypted record's gone mailbox is kept by every record the run writes after discovery: a
// refusal after discovery (here sealed units with no record beside them), a cancelled
// run's, and the "running" record a killed run leaves. Positive twin first: the finished ok record
// (TestAnEncryptedArchiveKeeps…).
func TestEveryEncryptedRecordTheRunWritesKeepsTheGoneMailbox(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	inputs := []string{a, b}
	out, key := encryptedOut(t)
	ctx := context.Background()
	if _, err := encGoneRun(t, ctx, out, key, inputs, Options{}); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(b)
	if _, err := encGoneRun(t, ctx, out, key, inputs, Options{Unattended: true}); err != nil {
		t.Fatal(err)
	}
	lr, _, _ := state.ReadLastRun(out)
	first := lr.MailboxGoneSince
	kept := func(t *testing.T, what string, lr state.LastRun, status string) {
		t.Helper()
		if lr.Status != status || !lr.MailboxGone || lr.MailboxGoneCount != 1 || !lr.MailboxGoneSince.Equal(first) {
			t.Fatalf("%s must keep the gone mailbox and its time %s: %+v", what, first, lr)
		}
	}

	var running state.LastRun
	if _, err := encGoneRun(t, ctx, out, key, inputs, Options{BeforeRead: func(context.Context, []string) BeforeReadResult {
		running, _, _ = state.ReadLastRun(out)
		return BeforeReadResult{}
	}}); err != nil {
		t.Fatal(err)
	}
	kept(t, "the running record", running, state.RunRunning)

	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := encGoneRun(t, cctx, out, key, inputs, Options{}); err == nil {
		t.Fatal("a cancelled run must end cancelled")
	}
	lr, _, _ = state.ReadLastRun(out)
	kept(t, "the cancelled record", lr, state.RunCancelled)

	// Refused under the lock, after discovery: the archive's sealed units with no record beside
	// them (MA-725's case), which a scheduled run records as a failed run.
	for _, name := range []string{container.PointerName, container.MirrorName} {
		if err := os.Remove(filepath.Join(out, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := encGoneRun(t, ctx, out, key, inputs, Options{Unattended: true}); err == nil {
		t.Fatal("a run into sealed units with no record must be refused")
	}
	lr, _, _ = state.ReadLastRun(out)
	kept(t, "the refused record", lr, state.RunFailed)
}
