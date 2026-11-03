package archdelete

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
)

// plantMarker writes a regular delete marker into dir, as a delete stopped part way
// leaves it (§15.1).
func plantMarker(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, delmark.Name), `{"what":"MailArchive delete marker","version":1,"run":"0123456789abcdef","began":"2026-11-02T15:04:05Z","note":"x"}`)
}

// requireNotPlanned fails when the plan names the marker anywhere: its file list,
// its kept list, its litter.
func requireNotPlanned(t *testing.T, p *DeletePlan) {
	t.Helper()
	if contains(removeRels(p), delmark.Name) || contains(p.Kept, delmark.Name) || contains(p.Litter, delmark.Name) || contains(unrecordedRels(p), delmark.Name) {
		t.Fatalf("the plan names the delete marker: remove %q kept %q litter %q", removeRels(p), p.Kept, p.Litter)
	}
}

// covers: MA-1053, R36
// The plain lister skips the delete marker (§15.2): it is the delete's own note,
// written and removed by its own two steps, never in the plan's file list, counts or
// Kept list, so §3.1's re-check (samePlan) is unchanged by it. Positive twin first:
// the same archive without it. A link at the name is still refused by the link row.
func TestThePlainPlanNeverListsTheDeleteMarker(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	without := mustPlan(t, dir, testDeps())
	plantMarker(t, dir)
	before := tree(t, dir)
	with := mustPlan(t, dir, testDeps())
	requireNotPlanned(t, with)
	if with.Files != without.Files || with.Bytes != without.Bytes || !samePlan(without, with) {
		t.Fatalf("the marker changed the plan: %d files %d bytes, was %d files %d bytes", with.Files, with.Bytes, without.Files, without.Bytes)
	}
	if !sameTree(before, tree(t, dir)) {
		t.Fatal("planning changed the folder")
	}

	os.Remove(filepath.Join(dir, delmark.Name))
	symlinkOrSkip(t, filepath.Join(t.TempDir(), "elsewhere"), filepath.Join(dir, delmark.Name))
	rc, msg, opts := refusedCase(t, dir, testDeps(), CaseLink, "This archive's folder is, or holds where its files are, a link:", delmark.Name, "Delete it by hand.")
	assure.Refused(t, rc, msg, opts...)
}

// covers: MA-1053, R36
// The encrypted lister leaves the delete marker out, beside the lock and the vault
// (§15.2): a folder holding it is planned — never "more than one kind" — and the marker
// is in none of the plan's lists. Positive twin first: the archive without it.
func TestTheEncryptedPlanNeverListsTheDeleteMarker(t *testing.T) {
	dir := encryptedArchive(t)
	without := mustPlan(t, dir, testDeps())
	plantMarker(t, dir)
	with := mustPlan(t, dir, testDeps())
	if with.Kind != Encrypted {
		t.Fatalf("kind %v", with.Kind)
	}
	requireNotPlanned(t, with)
	if encRootFiles[delmark.Name] {
		t.Fatal("the delete marker is one of the encrypted root files the delete lists")
	}
	if with.Files != without.Files || !samePlan(without, with) {
		t.Fatalf("the marker changed the plan: %q, was %q", removeRels(with), removeRels(without))
	}
}

// covers: MA-1053, R36
// planNoAnchor skips the delete marker (§15.2): §5.4's re-run of a folder that holds
// no manifest or vault lists neither it nor keeps it, for a plain and an encrypted
// archive. Positive twin first: the same folder without it.
func TestTheNoAnchorReRunNeverListsTheDeleteMarker(t *testing.T) {
	for _, kind := range []string{"plain", "encrypted"} {
		t.Run(kind, func(t *testing.T) {
			dir := smallArchive(t, t.TempDir())
			r := newRig(t, dir)
			line := map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "folder": dir, "row": dir, "kind": kind, "identity": idOf(t, dir)}
			if kind == "encrypted" {
				line["record_id"], line["record_spelling"] = testArchiveID, dir
			}
			appendRawLog(t, r.x.Log, line)
			// The crash left state files beside a removed manifest.
			if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(dir, ".mailarchive-lastrun.json"), "{}")
			without := r.plan(t)
			if without.Rerun != RerunNoAnchor || !contains(removeRels(without), ".mailarchive-lastrun.json") {
				t.Fatalf("the positive twin: rerun %v remove %q", without.Rerun, removeRels(without))
			}
			plantMarker(t, dir)
			with := r.plan(t)
			if with.Rerun != RerunNoAnchor {
				t.Fatalf("rerun %v", with.Rerun)
			}
			requireNotPlanned(t, with)
			if with.Files != without.Files || setDiff("kept", with.Kept, without.Kept) != "" {
				t.Fatalf("the marker changed the plan: remove %q kept %q, was %q %q", removeRels(with), with.Kept, removeRels(without), without.Kept)
			}
		})
	}
}

// covers: MA-1053, R36
// §4.1's "another archive's marker in a store tree" gains the delete marker
// (§15.7, MA-1053): a store tree holding another archive's delete marker refuses the
// whole delete as mixed, the folder unchanged. Positive twin first: the same tree
// without it is planned.
func TestAStoreTreeHoldingAnotherArchivesDeleteMarkerIsMixed(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	writeFile(t, filepath.Join(dir, "Mailbox", "Inbox", "Old", "notes.txt"), "the person's")
	mustPlan(t, dir, testDeps())
	plantMarker(t, filepath.Join(dir, "Mailbox", "Inbox", "Old"))
	rc, msg, opts := refusedCase(t, dir, testDeps(), CaseMixed, "This folder holds files of more than one kind. Delete it by hand.")
	assure.Refused(t, rc, msg, opts...)
	if !isMarker("linux", delmark.Name) {
		t.Fatal("the delete marker is not one of another archive's markers")
	}
}
