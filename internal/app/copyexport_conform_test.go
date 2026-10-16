package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-412, R29, S52
// The preview carries the filesystem-type warning: a FAT or exFAT destination says a message
// over 4 GiB can't be written and permissions aren't kept; a local filesystem and a share (whose
// line is the posture's) add none. The positive twin, a filesystem with no warning, first.
func TestPreviewWarnsAboutAFatDestination(t *testing.T) {
	for _, fsType := range []string{"apfs", "ext4", "ntfs", "smbfs", "cifs", ""} {
		if w := fsTypeWarnings(fsType); len(w) != 0 {
			t.Errorf("%q warns: %v", fsType, w)
		}
	}
	for _, fsType := range []string{"vfat", "exfat", "msdos"} {
		w := fsTypeWarnings(fsType)
		if len(w) != 1 || !strings.Contains(w[0], "FAT or exFAT") || !strings.Contains(w[0], "4 GiB") || !strings.Contains(w[0], "permissions aren't kept") {
			t.Errorf("%q: %v, want the FAT warning", fsType, w)
		}
	}
}

// covers: MA-437, R29, S52
// Finish as partial on a killed run lists a present recorded file whose bytes changed as a
// failure, "damaged when the computer stopped", and never puts it in SHA256SUMS.txt; the
// message is retryable and comes back from "Export failed messages again". The files that
// are intact are listed and verify. The positive twin, an undamaged killed run, first.
func TestFinishAsPartialListsADamagedFileAndNeverSumsIt(t *testing.T) {
	finish := func(damage bool) (*CopyExportEnv, *CopyResult, string) {
		e, src := newCEEnv(t), newFakeSrc()
		k := killedIn(t, e, src, "W6:2", ceMsgs(4))
		victim := emlOf(t, k.Root)
		if damage {
			p := filepath.Join(k.Root, victim)
			b, _ := os.ReadFile(p)
			os.WriteFile(p, append(b, []byte("torn by a power loss")...), 0o600)
		}
		fin, err := e.FinishAsPartial(k.RunID)
		if err != nil {
			t.Fatalf("Finish as partial: %v", err)
		}
		return e, fin, victim
	}
	failuresOf := func(root string) map[string]string {
		out := map[string]string{}
		for _, f := range manifestOf(t, root)["failures"].([]any) {
			m := f.(map[string]any)
			out[m["source_id"].(string)], _ = m["reason"].(string)
		}
		return out
	}

	_, clean, _ := finish(false)
	for id, why := range failuresOf(clean.Root) {
		if strings.Contains(why, "damaged") {
			t.Fatalf("the positive twin lists %s as damaged: %q", id, why)
		}
	}

	e, fin, victim := finish(true)
	damaged := 0
	for _, why := range failuresOf(fin.Root) {
		if why == "damaged when the computer stopped" {
			damaged++
		}
	}
	if damaged != 1 {
		t.Errorf("the manifest lists %d damaged files, want 1: %v", damaged, failuresOf(fin.Root))
	}
	sums, err := os.ReadFile(filepath.Join(fin.Root, copySumsName))
	if err != nil || strings.Contains(string(sums), victim) {
		t.Errorf("the damaged file %s is in SHA256SUMS.txt (%v)", victim, err)
	}
	if bad, missing, extra := sumsCheck(t, fin.Root); len(bad)+len(missing) != 0 || len(extra) != 1 || extra[0] != victim {
		t.Errorf("the finished part: changed %v, missing %v, unlisted %v; want only %s unlisted", bad, missing, extra, victim)
	}
	if failed, err := e.RetryInput(fin.RunID, "failed", ""); err != nil || len(failed.Messages) != 1 {
		t.Errorf("the damaged message is not retryable from the record: %v", err)
	}
}

// covers: MA-448, MA-447, R29, R12, S52
// A sealed run killed between W9a and W10 whose folder is absent at the next launch gets its
// ledger line (from the sealed line) but no journal end; "folder not found" is an observation,
// so the run is probed again at the launch after, when the folder is back, and is then offered
// Check and finish, which appends the end. The positive twin, the folder present at the first
// launch, is offered Check and finish at once.
func TestASealedRunWhoseFolderWasAbsentIsProbedAgainAtTheNextLaunch(t *testing.T) {
	launch := func(e *CopyExportEnv, run string) (*CopyExportEnv, CopyRunInfo) {
		n := relaunch(e)
		rec := n.NewReconciler()
		if err := rec.Launch(); err != nil {
			t.Fatal(err)
		}
		rec.Wait()
		hist, err := n.History(rec.Observations(), "")
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range hist {
			if c.Run == run {
				return n, c
			}
		}
		t.Fatalf("the run %s is not in the history", run)
		return nil, CopyRunInfo{}
	}

	// the positive twin
	e0, res0, _ := killedRun(t, "W9a", 3)
	if _, c := launch(e0, res0.RunID); !hasAction(c.Actions, ActCheckAndFinish) {
		t.Fatalf("the positive twin: label %q, actions %v, want Check and finish", c.Label, c.Actions)
	}

	e, res, _ := killedRun(t, "W9a", 3)
	away := res.Root + ".away"
	if err := os.Rename(res.Root, away); err != nil {
		t.Fatal(err)
	}
	n, c := launch(e, res.RunID)
	if ledger, _ := n.readLedger(); len(ledger) != 1 || ledger[0].Run != res.RunID || ledger[0].Fingerprint == "" {
		t.Errorf("the launch sweep left the ledger %+v, want the sealed run's line", ledger)
	}
	if n.journalHas(res.RunID, "end") || n.terminalReconciled(res.RunID) != nil {
		t.Error("an absent folder gave the run an end or a terminal outcome")
	}
	if !strings.Contains(c.Label, "folder not found") || hasAction(c.Actions, ActCheckAndFinish) {
		t.Errorf("at the first launch: label %q, actions %v", c.Label, c.Actions)
	}

	if err := os.Rename(away, res.Root); err != nil {
		t.Fatal(err)
	}
	n2, c := launch(n, res.RunID)
	if !hasAction(c.Actions, ActCheckAndFinish) || strings.Contains(c.Label, "folder not found") {
		t.Fatalf("at the launch after: label %q, actions %v, want Check and finish", c.Label, c.Actions)
	}
	if _, err := n2.CheckAndFinish(res.RunID); err != nil {
		t.Fatal(err)
	}
	if !n2.journalHas(res.RunID, "end") || exists(res.Root, copyMarkerName) {
		t.Errorf("Check and finish: end %v, marker %v", n2.journalHas(res.RunID, "end"), exists(res.Root, copyMarkerName))
	}
}

// covers: MA-448, R29, R12, S52
// A positive identity mismatch (another folder where the export was) is shown as "moved or
// replaced" on the run's card, with Locate… and This folder is gone offered, and it is an
// observation: not terminal, and the card keeps saying so after the history is read again. The
// positive twin, the folder where it was, is not shown as moved.
func TestAPositiveMismatchIsShownAsMovedOrReplaced(t *testing.T) {
	e0, src0 := newCEEnv(t), newFakeSrc() // the twin, in an environment of its own: its answer is terminal
	twin := killedIn(t, e0, src0, "W4", ceMsgs(2))
	if obs, _ := e0.ReconcileRun(twin.RunID); obs.Kind == obsMoved || strings.Contains(obs.Label, "moved or replaced") {
		t.Fatalf("the positive twin: the folder where it was reads as %+v", obs)
	}
	e, src := newCEEnv(t), newFakeSrc()
	res := killedIn(t, e, src, "W4", ceMsgs(2))
	moved := res.Root + ".away"
	if err := os.Rename(res.Root, moved); err != nil {
		t.Fatal(err)
	}
	os.Mkdir(res.Root, 0o700) // another folder at the path
	n := relaunch(e)
	rec := n.NewReconciler()
	rec.Launch()
	rec.Wait()
	hist, err := n.History(rec.Observations(), "")
	if err != nil {
		t.Fatal(err)
	}
	var card *CopyRunInfo
	for i := range hist {
		if hist[i].Run == res.RunID {
			card = &hist[i]
		}
	}
	if card == nil {
		t.Fatalf("the run is not in the history: %+v", hist)
	}
	if !strings.Contains(card.Label, "moved or replaced") || !hasAction(card.Actions, ActLocate) || !hasAction(card.Actions, ActGone) {
		t.Errorf("a different folder at the path: label %q, actions %v", card.Label, card.Actions)
	}
	if obs, _ := n.ReconcileRun(res.RunID); obs.Terminal {
		t.Errorf("a positive mismatch was recorded as terminal: %+v", obs)
	}
}

// covers: MA-438, R12, S52
// R-i, the one Start-side refusal the engine raises before it looks at the destination: a
// selection past 200,000 messages is refused in the design's words (what it holds, the cap,
// the remedy), with nothing written to the destination. The positive twin, a small plan, first.
func TestSelectionPastTheCapIsRIInTheDesignsWords(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	if p := cePlan(t, e, src, ceMsgs(1)); p == nil {
		t.Fatal("the positive twin made no plan")
	}
	in := CopyPlanInput{Source: CopySourceInfo{Kind: "m365", Mailbox: "alice@contoso.org"}, Dest: t.TempDir(), Messages: make([]CopyMessage, copyMaxMessages+1)}
	before := treeFiles(t, in.Dest)
	_, err := e.PlanCopyExport(in)
	ref, ok := err.(*CopyRefusal)
	if !ok || ref.ID != "R-i" {
		t.Fatalf("a selection of %d messages: %v, want R-i", len(in.Messages), err)
	}
	assure.Refused(t, 1, ref.Message, assure.Code(1),
		assure.Names("This selection has 200,001 messages", "one export copies up to 200,000", "Split it into several exports", "for example by folder"),
		assure.Forbid("panic", "goroutine", "http://", "https://", "access_token", "Bearer"),
		assure.NoSideEffect(func() bool { return len(treeFiles(t, in.Dest)) == len(before) }))
}

// covers: MA-430, R29, R12, S52
// Control characters in the journal are stripped on read (XC3): a run whose journal lines carry
// an ANSI escape, a bell, a NUL and a C1 control in its source, mailbox and folder path comes
// back from History with none of them anywhere on its card, root name included (the card is
// echoed into the page, a dialog and a pasteable remedy). The positive twin, a clean run, first.
func TestJournalTextHasNoControlCharactersOnRead(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	good := mustRun(t, e, cePlan(t, e, src, ceMsgs(1)), src)
	hist, err := e.History(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if h := assure.Reached(t, hist, "the history"); len(h) != 1 || h[0].Run != good.RunID {
		t.Fatalf("the positive twin: %+v", h)
	}

	const dirty = "\x1b[31m\x07\x00\u0085"
	planted := "planted-run"
	now := time.Now().UTC()
	for _, l := range []ceJournalLine{
		{Run: planted, Event: "start", At: now, SourceKind: "m365" + dirty, Mailbox: "mallory" + dirty + "@contoso.org",
			DestDir: "/exports/Mail" + dirty + "Export", Planned: 3},
		{Run: planted, Event: "end", At: now, Planned: 3, Exported: 3, Status: string(CopyStatusComplete), Fingerprint: strings.Repeat("ab", 32)},
	} {
		if err := e.journalAppend(l); err != nil {
			t.Fatal(err)
		}
	}
	hist, err = e.History(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var card *CopyRunInfo
	for i := range hist {
		if hist[i].Run == planted {
			card = &hist[i]
		}
	}
	if card == nil {
		t.Fatalf("the planted run is not in the history: %+v", hist)
	}
	for what, s := range map[string]string{"source": card.SourceKind, "mailbox": card.Mailbox, "folder": card.Dest, "root name": card.RootName, "label": card.Label} {
		if strings.ContainsAny(s, "\x1b\x07\x00\u0085") {
			t.Errorf("the card's %s still holds a control character: %q", what, s)
		}
	}
	if card.Mailbox != "mallory[31m@contoso.org" || card.RootName != "Mail[31mExport" {
		t.Errorf("the stripped words: mailbox %q, root name %q", card.Mailbox, card.RootName)
	}
}
