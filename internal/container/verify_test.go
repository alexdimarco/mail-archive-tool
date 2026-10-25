package container

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// unitBytes is record i's sealed unit as the keyless check sees it: some bytes on
// disk whose digest names the file (unitLister's shape).
func unitBytes(i int) []byte {
	return []byte(fmt.Sprintf("sealed unit %d, opaque to a reader without the key", i))
}

// recOnDisk is rec(i) with its unit file written under out.
func recOnDisk(t *testing.T, out string, i int) state.Record {
	t.Helper()
	b := unitBytes(i)
	h := sha256.Sum256(b)
	sum := hex.EncodeToString(h[:])
	r := rec(i)
	r.Sealed = &state.SealedSet{HTML: &state.SealedUnit{Rev: 1, SHA256: sum, Size: int64(len(b))}}
	p := filepath.Join(out, "units", sum[:2], sum[:32])
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return r
}

// saveOnDisk saves n records from `from` in run `run`, committing `commits` times,
// with every unit file on disk, after the run's start sweep.
func saveOnDisk(t *testing.T, out string, run, from, n, commits int) {
	t.Helper()
	m, s := open(t, out, opts(run))
	// The start sweep a keyed run makes before its first write (§3).
	if _, err := s.SweepContainers(runAt(run)); err != nil {
		t.Fatal(err)
	}
	for c := 0; c < commits; c++ {
		for i := from + c*n; i < from+(c+1)*n; i++ {
			m.Add(key(i), recOnDisk(t, out, i))
		}
		if err := m.Save(); err != nil {
			t.Fatal(err)
		}
	}
}

// wholeArchive is an archive saved over three runs, the last with two commits (so
// its file list holds retained entries), every file it lists on disk.
func wholeArchive(t *testing.T) string {
	t.Helper()
	out := fresh(t)
	saveOnDisk(t, out, 1, 0, 3, 1)
	saveOnDisk(t, out, 2, 10, 3, 1)
	saveOnDisk(t, out, 3, 20, 3, 2)
	return out
}

// listed is every file the archive's file list names, by state.
func listed(t *testing.T, out string) (live, retained []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(out, InventoryName))
	if err != nil {
		t.Fatal(err)
	}
	top, err := ParseTop(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range top.Shards {
		b, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(ref.Name)))
		sh, err := ParseShard(ref.Prefix, b)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range sh.Files {
			if f.State == Live {
				live = append(live, f.Path)
			} else {
				retained = append(retained, f.Path)
			}
		}
	}
	return live, retained
}

// keyless checks out without a key, asserting it wrote, healed and removed nothing.
func keyless(t *testing.T, out string, recordedGen uint64) Check {
	t.Helper()
	before := treeBytes(t, out)
	c := CheckKeyless(out, recordedGen)
	if !reflect.DeepEqual(before, treeBytes(t, out)) {
		t.Fatal("a keyless check changed the archive")
	}
	return c
}

func wantOutcome(t *testing.T, c Check, outcome string, reason ...string) {
	t.Helper()
	if c.Outcome != outcome {
		t.Fatalf("outcome %q (%s), want %q", c.Outcome, c.Reason, outcome)
	}
	for _, r := range reason {
		if !strings.Contains(c.Reason, r) {
			t.Fatalf("reason %q does not say %q", c.Reason, r)
		}
	}
}

// supersededShards counts the files under inventory/ the top file does not name.
func supersededShards(t *testing.T, out string) int {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(out, InventoryName))
	top, err := ParseTop(raw)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(out, InventoryDir))
	return len(entries) - len(top.Shards)
}

// unitPath is the archive-relative path of record i's unit file.
func unitPath(i int) string {
	h := sha256.Sum256(unitBytes(i))
	sum := hex.EncodeToString(h[:])
	return "units/" + sum[:2] + "/" + sum[:32]
}

// covers: MA-764, R37
// The keyless tier-1 check (phase2b §4): a whole archive is intact — every live file
// its file list names is present with its size and digest, both pointer copies match
// the top file — and a missing retained file is not damage (the next run's start
// sweep removes them by design). It reads only: nothing is written or removed.
func TestAKeylessCheckFindsAWholeArchiveIntact(t *testing.T) {
	out := wholeArchive(t)
	live, retained := listed(t, out)
	if len(retained) == 0 {
		t.Fatal("the fixture must hold retained entries (two commits in one run)")
	}
	c := keyless(t, out, 0)
	wantOutcome(t, c, state.OutcomeIntact)
	// The leftovers of a whole archive are the shard files the last run superseded,
	// which the next run's start sweep removes (§3).
	if c.Checked != len(live) || c.Leftovers != supersededShards(t, out) || len(c.Problems) != 0 {
		t.Fatalf("checked %d of %d live files, %d leftovers (want %d), problems %v", c.Checked, len(live), c.Leftovers, supersededShards(t, out), c.Problems)
	}
	for _, p := range retained {
		os.Remove(filepath.Join(out, filepath.FromSlash(p)))
	}
	wantOutcome(t, keyless(t, out, 0), state.OutcomeIntact)
}

// covers: MA-764, R37
// Damage (§4's damaged row): a live file missing, changed, truncated, grown or a
// link; a pointer copy that no longer matches the top file; both pointer copies gone;
// a top file that is present but unreadable (a writer renames it whole, so this is
// damage, never a crash: phase2b §13); sealed files with no record at all; a
// plaintext manifest at the record's name. Each names what it found.
func TestAKeylessCheckFindsDamage(t *testing.T) {
	flip := func(t *testing.T, p string) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)/2] ^= 1
		os.WriteFile(p, b, 0o600)
	}
	cases := []struct {
		name    string
		harm    func(t *testing.T, out string)
		reason  string
		problem string // a path the problems name, "" for none
		detail  string // what the problem says of it
	}{
		{"a unit missing", func(t *testing.T, out string) { os.Remove(filepath.Join(out, unitPath(1))) }, "missing or changed", unitPath(1), ""},
		{"a unit changed", func(t *testing.T, out string) { flip(t, filepath.Join(out, unitPath(11))) }, "missing or changed", unitPath(11), "sha256 mismatch"},
		{"a unit truncated", func(t *testing.T, out string) { os.Truncate(filepath.Join(out, unitPath(20)), 5) }, "missing or changed", unitPath(20), "size 5, listed"},
		{"a unit grown", func(t *testing.T, out string) {
			f, _ := os.OpenFile(filepath.Join(out, unitPath(2)), os.O_APPEND|os.O_WRONLY, 0)
			f.Write([]byte("more"))
			f.Close()
		}, "missing or changed", unitPath(2), "listed"},
		{"a link in place of a unit", func(t *testing.T, out string) {
			p := filepath.Join(out, unitPath(12))
			b, _ := os.ReadFile(p)
			elsewhere := filepath.Join(t.TempDir(), "unit")
			os.WriteFile(elsewhere, b, 0o600)
			os.Remove(p)
			if err := os.Symlink(elsewhere, p); err != nil {
				t.Skip("no symlinks here:", err)
			}
		}, "missing or changed", unitPath(12), "a link stands where a sealed file should be"},
		{"a chunk missing", func(t *testing.T, out string) {
			for f := range chunkFiles(t, out) {
				if l, _ := listed(t, out); contains(l, f) {
					os.Remove(filepath.Join(out, filepath.FromSlash(f)))
					return
				}
			}
		}, "missing or changed", "", ""},
		{"the primary pointer changed", func(t *testing.T, out string) { flip(t, filepath.Join(out, PointerName)) }, "record pointer", "", ""},
		{"the mirror changed", func(t *testing.T, out string) { flip(t, filepath.Join(out, MirrorName)) }, "record pointer", "", ""},
		{"the primary pointer gone", func(t *testing.T, out string) { os.Remove(filepath.Join(out, PointerName)) }, "record pointer", "", ""},
		{"both pointers gone", func(t *testing.T, out string) {
			os.Remove(filepath.Join(out, PointerName))
			os.Remove(filepath.Join(out, MirrorName))
		}, "record pointer", "", ""},
		{"the top file garbled", func(t *testing.T, out string) {
			p := filepath.Join(out, InventoryName)
			b, _ := os.ReadFile(p)
			os.WriteFile(p, []byte(strings.Replace(string(b), "fanout ", "fan-out ", 1)), 0o600)
		}, "file list is damaged", "", ""},
		{"the top file reads as newer beside a current pointer", func(t *testing.T, out string) {
			p := filepath.Join(out, InventoryName)
			b, _ := os.ReadFile(p)
			os.WriteFile(p, []byte(strings.Replace(string(b), "mailarchive-inventory 1", "mailarchive-inventory 2", 1)), 0o600)
		}, "file list is damaged", "", ""},
		{"a plaintext manifest at the record's name", func(t *testing.T, out string) {
			os.WriteFile(filepath.Join(out, EnvelopeName), []byte(`{"version":3,"entries":{}}`), 0o600)
		}, "not a sealed record", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := wholeArchive(t)
			tc.harm(t, out)
			c := keyless(t, out, 0)
			wantOutcome(t, c, state.OutcomeDamaged, tc.reason)
			if tc.problem != "" && !hasProblem(c, tc.problem, tc.detail) {
				t.Fatalf("the problems do not name %s (%q): %v", tc.problem, tc.detail, c.Problems)
			}
		})
	}
	t.Run("sealed files with no record", func(t *testing.T) {
		out := t.TempDir()
		recOnDisk(t, out, 1)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeDamaged, "record is missing")
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func hasProblem(c Check, path, detail string) bool {
	for _, p := range c.Problems {
		if p.Path == path && strings.Contains(p.Detail, detail) {
			return true
		}
	}
	return false
}

// covers: MA-764, R37
// The record and the file list out of step (§3, §4): a record one save older than
// its file list is "the last save did not finish, or an older copy was restored";
// more than one save older is restored_older; a file list older than the record (its
// write failed or crashed, already reported RED) is not_attested, the files it lists
// still checked — a crash between the shard writes and the top file leaves the G−1
// listing, which verifies not_attested and never damaged, the new shards reported
// as leftovers.
func TestAKeylessCheckComparesTheRecordWithItsFileList(t *testing.T) {
	t.Run("one save older", func(t *testing.T) {
		out := wholeArchive(t)
		snap := snapshotRecord(t, out)
		saveOnDisk(t, out, 4, 30, 2, 1)
		restore(t, out, snap)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeUnfinishedSave, "did not finish")
	})
	t.Run("two saves older", func(t *testing.T) {
		out := wholeArchive(t)
		snap := snapshotRecord(t, out)
		saveOnDisk(t, out, 4, 30, 2, 1)
		saveOnDisk(t, out, 5, 40, 2, 1)
		restore(t, out, snap)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeRestoredOlder, "older than its file list")
	})
	t.Run("a crash between the shards and the top file", func(t *testing.T) {
		out := wholeArchive(t)
		top, _ := os.ReadFile(filepath.Join(out, InventoryName))
		saveOnDisk(t, out, 4, 30, 2, 1)
		os.WriteFile(filepath.Join(out, InventoryName), top, 0o600)
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeNotAttested, "file list is older than its record")
		if c.Leftovers == 0 || c.Checked == 0 {
			t.Fatalf("the newer shards and units are leftovers and the G-1 listing is checked: %+v", c)
		}
	})
	t.Run("an older file list still finds damage", func(t *testing.T) {
		out := wholeArchive(t)
		top, _ := os.ReadFile(filepath.Join(out, InventoryName))
		saveOnDisk(t, out, 4, 30, 2, 1)
		os.WriteFile(filepath.Join(out, InventoryName), top, 0o600)
		os.Remove(filepath.Join(out, unitPath(1)))
		wantOutcome(t, keyless(t, out, 0), state.OutcomeDamaged, "missing or changed")
	})
}

// covers: MA-764, R37
// What cannot be attested without the key, and is not damage (§4's not_attested
// row): no file list (the first one never landed); a file list whose mirror write
// failed (`mirror none`); a damaged part of the file list under a current top file
// (the next backup rewrites it); a folder with no backup yet. Leftovers — files
// nothing lists, under units/, chunks/ or inventory/ — are reported and never
// change the outcome.
func TestAKeylessCheckSaysWhatItCannotAttest(t *testing.T) {
	t.Run("no file list", func(t *testing.T) {
		out := wholeArchive(t)
		os.Remove(filepath.Join(out, InventoryName))
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeNotAttested, "no file list")
		if c.Leftovers == 0 {
			t.Fatal("with no file list every sealed file is unlisted")
		}
	})
	t.Run("mirror none", func(t *testing.T) {
		out := wholeArchive(t)
		p := filepath.Join(out, InventoryName)
		raw, _ := os.ReadFile(p)
		top, err := ParseTop(raw)
		if err != nil {
			t.Fatal(err)
		}
		top.Mirror = nil
		b, err := top.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(p, b, 0o600)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeNotAttested, "second copy of its record pointer")
	})
	t.Run("a damaged part of the file list", func(t *testing.T) {
		out := wholeArchive(t)
		raw, _ := os.ReadFile(filepath.Join(out, InventoryName))
		top, _ := ParseTop(raw)
		os.Remove(filepath.Join(out, filepath.FromSlash(top.Shards[0].Name)))
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeNotAttested, "part of its file list")
		if c.PartsUnread != 1 {
			t.Fatalf("parts unread %d, want 1", c.PartsUnread)
		}
	})
	t.Run("a changed part of the file list", func(t *testing.T) {
		// A shard whose bytes still parse but no longer match the top file's digest
		// is unread, never trusted: its listed digests could be anything.
		out := wholeArchive(t)
		raw, _ := os.ReadFile(filepath.Join(out, InventoryName))
		top, _ := ParseTop(raw)
		p := filepath.Join(out, filepath.FromSlash(top.Shards[0].Name))
		b, _ := os.ReadFile(p)
		i := strings.IndexByte(string(b), ' ') + 1
		if b[i] == '0' {
			b[i] = '1'
		} else {
			b[i] = '0'
		}
		os.WriteFile(p, b, 0o600)
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeNotAttested, "part of its file list")
		if c.PartsUnread != 1 {
			t.Fatalf("parts unread %d, want 1", c.PartsUnread)
		}
	})
	t.Run("no backup yet", func(t *testing.T) {
		wantOutcome(t, keyless(t, t.TempDir(), 0), state.OutcomeNotAttested, "no backup has saved")
	})
	t.Run("leftovers", func(t *testing.T) {
		out := wholeArchive(t)
		before := keyless(t, out, 0).Leftovers
		for _, rel := range []string{"units/ee/ee" + strings.Repeat("0", 30), "chunks/ab/ab" + strings.Repeat("1", 30), InventoryDir + "/ff.1"} {
			p := filepath.Join(out, filepath.FromSlash(rel))
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte("x"), 0o600)
		}
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeIntact)
		if c.Leftovers != before+3 {
			t.Fatalf("leftovers %d, want %d", c.Leftovers, before+3)
		}
	})
}

// covers: MA-764, R37
// RecordedGen (A25), as the keyed load applies it: an archive older than this
// computer's last save with no file its file list does not name (a clean restore)
// is not_attested — the next backup accepts it; with such a file (a merge-restore)
// it is restored_older; one save behind is the unfinished save (CON-6).
func TestAKeylessCheckAppliesThisComputersLastSave(t *testing.T) {
	out := wholeArchive(t)
	raw, _ := os.ReadFile(filepath.Join(out, PointerName))
	_, gen, err := Peek(raw)
	if err != nil {
		t.Fatal(err)
	}
	wantOutcome(t, keyless(t, out, gen), state.OutcomeIntact)
	wantOutcome(t, keyless(t, out, gen+1), state.OutcomeUnfinishedSave)
	wantOutcome(t, keyless(t, out, gen+5), state.OutcomeNotAttested, "restored to an older copy")
	stray := filepath.Join(out, "units", "ee", "ee"+strings.Repeat("0", 30))
	os.MkdirAll(filepath.Dir(stray), 0o755)
	os.WriteFile(stray, []byte("a newer message's unit"), 0o600)
	wantOutcome(t, keyless(t, out, gen+5), state.OutcomeRestoredOlder, "older than the last backup this computer made")
}

// covers: MA-764, R37
// A newer MailArchive (P2b-C11, §11 CRIT-2): both pointer copies in a newer format,
// or a newer copy that outranks the current one, or a newer envelope with no
// pointer, ask for an upgrade; a newer copy outranked by a current one is damage.
func TestAKeylessCheckAsksForAnUpgrade(t *testing.T) {
	newer := func(t *testing.T, p string, bump uint64) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		binary.BigEndian.PutUint16(b[4:6], vault.FormatVersion+1)
		binary.BigEndian.PutUint64(b[6:14], binary.BigEndian.Uint64(b[6:14])+bump)
		os.WriteFile(p, b, 0o600)
	}
	t.Run("both copies", func(t *testing.T) {
		out := wholeArchive(t)
		newer(t, filepath.Join(out, PointerName), 0)
		newer(t, filepath.Join(out, MirrorName), 0)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeNewerFormat, "newer MailArchive")
	})
	t.Run("a newer copy that outranks", func(t *testing.T) {
		out := wholeArchive(t)
		newer(t, filepath.Join(out, PointerName), 1)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeNewerFormat, "newer MailArchive")
	})
	t.Run("a newer copy outranked", func(t *testing.T) {
		out := wholeArchive(t)
		p := filepath.Join(out, MirrorName)
		b, _ := os.ReadFile(p)
		binary.BigEndian.PutUint16(b[4:6], vault.FormatVersion+1)
		binary.BigEndian.PutUint64(b[6:14], 1)
		os.WriteFile(p, b, 0o600)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeDamaged, "record pointer")
	})
	t.Run("a newer envelope and no pointer", func(t *testing.T) {
		out := t.TempDir()
		os.WriteFile(filepath.Join(out, EnvelopeName), []byte(fmt.Sprintf(`{"version":%d,"encrypted":true}`, vault.EncryptedManifestVersion+1)), 0o600)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeNewerFormat, "newer MailArchive")
	})
}
