package container

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	if c.Checked != len(live) || c.Leftovers != supersededShards(t, out) || c.Unnamed != 0 || c.LeftoversStay || len(c.Problems) != 0 {
		t.Fatalf("checked %d of %d live files, %d leftovers (want %d), %d unnamed, problems %v", c.Checked, len(live), c.Leftovers, supersededShards(t, out), c.Unnamed, c.Problems)
	}
	for _, p := range retained {
		os.Remove(filepath.Join(out, filepath.FromSlash(p)))
	}
	wantOutcome(t, keyless(t, out, 0), state.OutcomeIntact)
}

// flip changes one bit in the middle of the file at p.
func flip(t *testing.T, p string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)/2] ^= 1
	os.WriteFile(p, b, 0o600)
}

// listedChunk is a chunk the archive's file list names as live.
func listedChunk(t *testing.T, out string) string {
	t.Helper()
	live, _ := listed(t, out)
	for f := range chunkFiles(t, out) {
		if contains(live, f) {
			return f
		}
	}
	t.Fatal("the fixture lists no chunk")
	return ""
}

// linkOrSkip puts a link to target at p, skipping the test where links cannot be made.
func linkOrSkip(t *testing.T, target, p string) {
	t.Helper()
	if err := os.Symlink(target, p); err != nil {
		t.Skip("no symlinks here:", err)
	}
}

// moveAndLink moves the folder p out of the archive and puts a link to it at p.
func moveAndLink(t *testing.T, p string) {
	t.Helper()
	elsewhere := filepath.Join(t.TempDir(), filepath.Base(p))
	if err := os.Rename(p, elsewhere); err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, elsewhere, p)
}

// noAccess makes p unreadable for the rest of the test, restoring it at cleanup.
func noAccess(t *testing.T, p string) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions do not refuse a read here")
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	os.Chmod(p, 0)
	t.Cleanup(func() { os.Chmod(p, fi.Mode().Perm()) })
}

// covers: MA-764, MA-766, R37
// Damage (§4's damaged row): a live file under units/ missing, changed, truncated,
// grown, a link or unreadable (the file, or its folder); a pointer copy that no
// longer matches the top file; both pointer copies gone or at the last generation; a
// top file present but unreadable (a writer renames it whole, so this is damage,
// never a crash: phase2b §13) or at the last generation; a plaintext manifest at the
// record's name, small or too large for an envelope; a folder or a link at that name
// (the keyed load refuses it: the adversarial pass, DEC-01). Each says its own rule
// and names what it found.
func TestAKeylessCheckFindsDamage(t *testing.T) {
	cases := []struct {
		name    string
		harm    func(t *testing.T, out string)
		reason  string
		problem string // a path the problems name, "" for none
		detail  string // what the problem says of it
	}{
		{"a unit missing", func(t *testing.T, out string) { os.Remove(filepath.Join(out, unitPath(1))) }, reasonFilesBad, unitPath(1), ""},
		{"a unit changed", func(t *testing.T, out string) { flip(t, filepath.Join(out, unitPath(11))) }, reasonFilesBad, unitPath(11), "sha256 mismatch"},
		{"a unit truncated", func(t *testing.T, out string) { os.Truncate(filepath.Join(out, unitPath(20)), 5) }, reasonFilesBad, unitPath(20), "size 5, listed"},
		{"a unit grown", func(t *testing.T, out string) {
			f, _ := os.OpenFile(filepath.Join(out, unitPath(2)), os.O_APPEND|os.O_WRONLY, 0)
			f.Write([]byte("more"))
			f.Close()
		}, reasonFilesBad, unitPath(2), "listed"},
		{"a link in place of a unit", func(t *testing.T, out string) {
			p := filepath.Join(out, unitPath(12))
			b, _ := os.ReadFile(p)
			elsewhere := filepath.Join(t.TempDir(), "unit")
			os.WriteFile(elsewhere, b, 0o600)
			os.Remove(p)
			if err := os.Symlink(elsewhere, p); err != nil {
				t.Skip("no symlinks here:", err)
			}
		}, reasonFilesBad, unitPath(12), "a link stands where a sealed file should be"},
		{"a unit that cannot be read", func(t *testing.T, out string) { noAccess(t, filepath.Join(out, unitPath(10))) }, reasonFilesBad, unitPath(10), "could not be read"},
		{"a unit's folder that cannot be searched", func(t *testing.T, out string) {
			noAccess(t, filepath.Join(out, filepath.Dir(filepath.FromSlash(unitPath(21)))))
		}, reasonFilesBad, unitPath(21), "could not be read"},
		{"a unit and a chunk missing", func(t *testing.T, out string) {
			os.Remove(filepath.Join(out, unitPath(1)))
			os.Remove(filepath.Join(out, filepath.FromSlash(listedChunk(t, out))))
		}, reasonFilesBad, unitPath(1), ""},
		{"the primary pointer changed", func(t *testing.T, out string) { flip(t, filepath.Join(out, PointerName)) }, reasonPointerBad, "", ""},
		{"the mirror changed", func(t *testing.T, out string) { flip(t, filepath.Join(out, MirrorName)) }, reasonPointerBad, "", ""},
		{"the primary pointer gone", func(t *testing.T, out string) { os.Remove(filepath.Join(out, PointerName)) }, reasonPointerBad, "", ""},
		{"both pointers gone", func(t *testing.T, out string) {
			os.Remove(filepath.Join(out, PointerName))
			os.Remove(filepath.Join(out, MirrorName))
		}, reasonPointersGone, "", ""},
		{"both pointers at the last generation", func(t *testing.T, out string) {
			// No writer reaches it, and P + 1 must not wrap to this computer's
			// RecordedGen of 0 (the adversarial pass, OUT-03).
			for _, name := range []string{PointerName, MirrorName} {
				p := filepath.Join(out, name)
				b, _ := os.ReadFile(p)
				binary.BigEndian.PutUint64(b[6:14], math.MaxUint64)
				os.WriteFile(p, b, 0o600)
			}
		}, reasonPointersGone, "", ""},
		{"the top file garbled", func(t *testing.T, out string) {
			p := filepath.Join(out, InventoryName)
			b, _ := os.ReadFile(p)
			os.WriteFile(p, []byte(strings.Replace(string(b), "fanout ", "fan-out ", 1)), 0o600)
		}, reasonListDamaged, "", ""},
		{"the top file reads as newer beside a current pointer", func(t *testing.T, out string) {
			p := filepath.Join(out, InventoryName)
			b, _ := os.ReadFile(p)
			os.WriteFile(p, []byte(strings.Replace(string(b), "mailarchive-inventory 1", "mailarchive-inventory 2", 1)), 0o600)
		}, reasonListDamaged, "", ""},
		{"the top file at the last generation", func(t *testing.T, out string) {
			p := filepath.Join(out, InventoryName)
			raw, _ := os.ReadFile(p)
			top, err := ParseTop(raw)
			if err != nil {
				t.Fatal(err)
			}
			top.Gen = math.MaxUint64
			b, err := top.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(p, b, 0o600)
		}, reasonListDamaged, "", ""},
		{"a plaintext manifest at the record's name", func(t *testing.T, out string) {
			os.WriteFile(filepath.Join(out, EnvelopeName), []byte(`{"version":3,"entries":{}}`), 0o600)
		}, reasonNotSealed, "", ""},
		{"a plaintext manifest too large for an envelope", func(t *testing.T, out string) {
			os.WriteFile(filepath.Join(out, EnvelopeName), []byte(strings.Repeat(" ", maxEnvelopeSize+10)), 0o600)
		}, reasonNotSealed, "", ""},
		{"a folder at the record file's name", func(t *testing.T, out string) {
			p := filepath.Join(out, EnvelopeName)
			os.Remove(p)
			os.Mkdir(p, 0o755)
		}, reasonEnvelopeBad, "", ""},
		{"a link at the record file's name", func(t *testing.T, out string) {
			p := filepath.Join(out, EnvelopeName)
			elsewhere := filepath.Join(t.TempDir(), "envelope")
			b, _ := os.ReadFile(p)
			os.WriteFile(elsewhere, b, 0o600)
			os.Remove(p)
			linkOrSkip(t, elsewhere, p)
		}, reasonEnvelopeBad, "", ""},
		{"a plaintext manifest of a newer version beside a current pointer", func(t *testing.T, out string) {
			os.WriteFile(filepath.Join(out, EnvelopeName), []byte(fmt.Sprintf(`{"version":%d,"entries":{}}`, vault.EncryptedManifestVersion+1)), 0o600)
		}, reasonNotSealed, "", ""},
		// Every record file gone, which the check can tell without pairing copies (the
		// fix re-verification, FV-C-01).
		{"chunks gone", func(t *testing.T, out string) { os.RemoveAll(filepath.Join(out, ChunksDir)) }, reasonRecordGone, "", ""},
		// A link where a folder of the archive belongs: the backup's start sweep, or its
		// file-list write, refuses it (FV-C-02, FV-C-03).
		{"chunks a link", func(t *testing.T, out string) { moveAndLink(t, filepath.Join(out, ChunksDir)) }, reasonNotAFolder, "", ""},
		{"inventory a link", func(t *testing.T, out string) { moveAndLink(t, filepath.Join(out, InventoryDir)) }, reasonNotAFolder, "", ""},
		{"a link where a unit's folder belongs", func(t *testing.T, out string) {
			linkOrSkip(t, t.TempDir(), filepath.Join(out, "units", "zz"))
		}, reasonNotAFolder, "", ""},
		{"a link where a chunk's folder belongs", func(t *testing.T, out string) {
			linkOrSkip(t, t.TempDir(), filepath.Join(out, ChunksDir, "zz"))
		}, reasonNotAFolder, "", ""},
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
	t.Run("the keyed run refuses the folder rows", func(t *testing.T) {
		// The rows added for the keyed run's own refusals: the load, or the start
		// sweep a backup makes once the load accepts the archive.
		for _, harm := range []func(out string){
			func(out string) { os.RemoveAll(filepath.Join(out, ChunksDir)) },
			func(out string) { moveAndLink(t, filepath.Join(out, ChunksDir)) },
			func(out string) { linkOrSkip(t, t.TempDir(), filepath.Join(out, "units", "zz")) },
		} {
			out := wholeArchive(t)
			harm(out)
			_, s, err := LoadManifest(out, opts(9))
			if err == nil {
				_, err = s.SweepContainers(runAt(9))
			}
			if err == nil {
				t.Fatal("the keyed run accepted a folder the check calls damaged")
			}
		}
	})
	t.Run("an envelope only gone is healed", func(t *testing.T) {
		// The positive twin of the non-file rows: a missing envelope is rewritten by
		// the next backup (§3), never damage.
		out := wholeArchive(t)
		os.Remove(filepath.Join(out, EnvelopeName))
		wantOutcome(t, keyless(t, out, 0), state.OutcomeIntact, reasonIntact)
	})
}

// covers: MA-764, MA-766, R37
// A listed file must be the very file judged: a unit's folder swapped, between the
// judgement and the open (a writer racing the check), for a link to a folder holding
// a good copy — outside the archive, or inside it — is never read as matching; the
// unit is named, changed while it was checked (the adversarial pass, OUT-02,
// CON-02; the fix re-verification, FV-T-01).
func TestAKeylessCheckNeverReadsThroughASwappedFolder(t *testing.T) {
	for _, where := range []string{"outside the archive", "inside the archive"} {
		t.Run(where, func(t *testing.T) {
			out := wholeArchive(t)
			rel := unitPath(11)
			dir := filepath.Join(out, filepath.Dir(filepath.FromSlash(rel)))
			stash := filepath.Join(t.TempDir(), "units")
			if where == "inside the archive" {
				stash = filepath.Join(out, "stash")
			}
			os.MkdirAll(stash, 0o755)
			good, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel)))
			os.WriteFile(filepath.Join(stash, filepath.Base(rel)), good, 0o600)
			flip(t, filepath.Join(out, filepath.FromSlash(rel)))
			swapped := false
			beforeOpen = func(r string) {
				if r != rel || swapped {
					return
				}
				swapped = true
				os.Rename(dir, dir+".real")
				linkOrSkip(t, stash, dir)
			}
			defer func() { beforeOpen = nil }()
			c := CheckKeyless(out, 0)
			if !swapped {
				t.Fatal("the swap never ran")
			}
			if !hasProblem(c, rel, "changed while it was checked") || c.Outcome != state.OutcomeDamaged {
				t.Fatalf("a unit read through a swapped folder: outcome %s, problems %v", c.Outcome, c.Problems)
			}
		})
	}
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
		wantOutcome(t, c, state.OutcomeNotAttested, reasonListBehind)
		// The newer run's files are not in the older list, and the next backup keeps
		// them — they are live under the newer pointer (the adversarial pass, DEC-03).
		if c.Unnamed == 0 || !c.LeftoversStay || c.Checked == 0 {
			t.Fatalf("the newer shards and units are unlisted and stay, and the G-1 listing is checked: %+v", c)
		}
	})
	t.Run("an older file list still finds damage", func(t *testing.T) {
		out := wholeArchive(t)
		top, _ := os.ReadFile(filepath.Join(out, InventoryName))
		saveOnDisk(t, out, 4, 30, 2, 1)
		os.WriteFile(filepath.Join(out, InventoryName), top, 0o600)
		os.Remove(filepath.Join(out, unitPath(1)))
		wantOutcome(t, keyless(t, out, 0), state.OutcomeDamaged, reasonFilesBad)
	})
	t.Run("an older file list with a damaged part", func(t *testing.T) {
		// The keyed load tolerates a damaged part only beside the record's own
		// generation; here the backup refuses the archive, so this is damage, not
		// "run a backup" (the adversarial pass, CON-01, DEC-02).
		out := wholeArchive(t)
		raw, _ := os.ReadFile(filepath.Join(out, InventoryName))
		saveOnDisk(t, out, 4, 30, 2, 1)
		os.WriteFile(filepath.Join(out, InventoryName), raw, 0o600)
		top, _ := ParseTop(raw)
		flip(t, filepath.Join(out, filepath.FromSlash(top.Shards[0].Name)))
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeDamaged, reasonListDamaged)
		m, _, err := LoadManifest(out, opts(5))
		if err == nil || m != nil {
			t.Fatal("the keyed load this outcome stands for accepted the archive")
		}
	})
}

// covers: MA-764, MA-766, R37
// What cannot be attested without the key, and is not damage (§4's not_attested
// row): no file list (the first one never landed); a file list whose mirror write
// failed (`mirror none`); a damaged part of the file list under a current top file
// (the next backup rewrites it) — a shard missing, changed or naming another count
// of files; a record chunk missing or changed while every unit is as listed (the
// keyed load reads the other copy and the next save rewrites it: the adversarial
// pass, CRIT-1); a folder with no backup yet. inventory/ a link is damage, and is
// never read or stat'd through. Files nothing lists — unnamed under units/ and
// chunks/, superseded leftovers under inventory/ — are reported and never change
// the outcome; where the file list is missing, behind or partly unread, they are
// said to stay (DEC-03).
func TestAKeylessCheckSaysWhatItCannotAttest(t *testing.T) {
	t.Run("no file list", func(t *testing.T) {
		out := wholeArchive(t)
		os.Remove(filepath.Join(out, InventoryName))
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeNotAttested, reasonNoFileList)
		if c.Unnamed == 0 || !c.LeftoversStay {
			t.Fatal("with no file list every sealed file is unnamed, and stays")
		}
		if c.Advice != "" {
			t.Fatalf("a rule with no next step of its own gave %q", c.Advice)
		}
	})
	for _, harm := range []string{"missing", "changed"} {
		t.Run("a record chunk "+harm, func(t *testing.T) {
			out := wholeArchive(t)
			p := filepath.Join(out, filepath.FromSlash(listedChunk(t, out)))
			if harm == "missing" {
				os.Remove(p)
			} else {
				flip(t, p)
			}
			c := keyless(t, out, 0)
			wantOutcome(t, c, state.OutcomeNotAttested, reasonRecordCopy)
			if c.Missing+c.Modified != 1 || len(c.Problems) != 1 {
				t.Fatalf("the chunk is still named: %+v", c)
			}
			if c.Advice != adviceRecordCopy {
				t.Fatalf("the next step is %q, want the backup first", c.Advice)
			}
		})
	}
	t.Run("a shard naming another count of files", func(t *testing.T) {
		out := wholeArchive(t)
		p := filepath.Join(out, InventoryName)
		raw, _ := os.ReadFile(p)
		top, _ := ParseTop(raw)
		top.Shards[0].Entries++
		b, err := top.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(p, b, 0o600)
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeNotAttested, reasonPartsUnread)
		if c.PartsUnread != 1 {
			t.Fatalf("parts unread %d, want 1", c.PartsUnread)
		}
	})
	t.Run("inventory a link is not read through", func(t *testing.T) {
		// Damage — no backup can write its file list through the link (the fix
		// re-verification, FV-C-02) — and no shard is read through it (OUT-04).
		out := wholeArchive(t)
		moveAndLink(t, filepath.Join(out, InventoryDir))
		reads := 0
		was := readShard
		readShard = func(root, rel string, max int64) ([]byte, os.FileInfo, error) { reads++; return was(root, rel, max) }
		defer func() { readShard = was }()
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeDamaged, reasonNotAFolder)
		if reads != 0 {
			t.Fatalf("%d shard reads through the link", reads)
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
		wantOutcome(t, c, state.OutcomeNotAttested, reasonPartsUnread)
		// Its files are unjudged, not leftovers a backup removes: a damaged file list
		// is swept by nothing (DEC-03).
		if c.PartsUnread != 1 || !c.LeftoversStay {
			t.Fatalf("parts unread %d (want 1), leftovers stay %v", c.PartsUnread, c.LeftoversStay)
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
		wantOutcome(t, keyless(t, t.TempDir(), 0), state.OutcomeNotAttested, reasonNoRecordYet)
	})
	t.Run("leftovers", func(t *testing.T) {
		out := wholeArchive(t)
		before := keyless(t, out, 0).Leftovers
		// Under units/ and chunks/ — a regular file where a shard folder belongs
		// included, which no sweep removes — they are unnamed, and nothing is said of
		// their removal; under inventory/ they are superseded parts the next backup
		// removes (the fix re-verification, FV-C-04).
		for _, rel := range []string{"units/ee/ee" + strings.Repeat("0", 30), "chunks/ab/ab" + strings.Repeat("1", 30), "units/zz", InventoryDir + "/ff.1"} {
			p := filepath.Join(out, filepath.FromSlash(rel))
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte("x"), 0o600)
		}
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeIntact, reasonIntact)
		if c.Unnamed != 3 || c.Leftovers != before+1 || c.LeftoversStay {
			t.Fatalf("unnamed %d (want 3), leftovers %d (want %d), stay %v (want false)", c.Unnamed, c.Leftovers, before+1, c.LeftoversStay)
		}
	})
}

// covers: MA-764, R37
// The problems the check names are bounded (MaxCheckProblems) while every file not
// as listed is still counted; a file where a listed unit's folder belongs makes that
// unit missing, its path not followed.
func TestAKeylessCheckCountsEveryProblemAndNamesABoundedFew(t *testing.T) {
	out := wholeArchive(t)
	os.Remove(filepath.Join(out, unitPath(1)))
	os.Remove(filepath.Join(out, unitPath(2)))
	dir := filepath.Join(out, filepath.Dir(filepath.FromSlash(unitPath(20))))
	os.RemoveAll(dir)
	os.WriteFile(dir, []byte("not a folder"), 0o600)
	was := MaxCheckProblems
	MaxCheckProblems = 1
	defer func() { MaxCheckProblems = was }()
	c := keyless(t, out, 0)
	wantOutcome(t, c, state.OutcomeDamaged, reasonFilesBad)
	if c.Missing < 3 || c.Modified != 0 || len(c.Problems) != 1 {
		t.Fatalf("missing %d (want at least 3), modified %d, %d problems named (want 1)", c.Missing, c.Modified, len(c.Problems))
	}
}

// covers: MA-764, MA-766, R37
// With no record pointer the folder is a new archive whose first commit is owed only
// as the keyed load's checkFresh says, each other state damage under its own reason
// (the adversarial pass: FID-03, FID-11, DEC-07, OUT-07): a pointer copy present but
// unreadable; this computer's recorded save; a search index or a go-back history;
// files under units/ or inventory/; a link or a file where a record folder belongs;
// under chunks/ anything but two-character folders of regular chunk-shaped files and
// temps — a link at a chunk's name, a temp beside the shard folders, a longer folder
// name, a file of another shape. The owed first commit's chunks and temps are
// leftovers it removes; a plaintext manifest there is never a newer format (DEC-06).
func TestAKeylessCheckJudgesAFolderWithNoRecord(t *testing.T) {
	chunk := "chunks/ab/ab" + strings.Repeat("0", 30)
	write := func(t *testing.T, out string, rels ...string) {
		for _, rel := range rels {
			p := filepath.Join(out, filepath.FromSlash(rel))
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte("x"), 0o600)
		}
	}
	link := func(t *testing.T, out, rel string) {
		p := filepath.Join(out, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		target := filepath.Join(t.TempDir(), "x")
		os.WriteFile(target, []byte("x"), 0o600)
		if err := os.Symlink(target, p); err != nil {
			t.Skip("no symlinks here:", err)
		}
	}
	t.Run("an interrupted first commit", func(t *testing.T) {
		out := t.TempDir()
		write(t, out, chunk, "chunks/ab/.mailarchive-12.tmp")
		c := keyless(t, out, 0)
		wantOutcome(t, c, state.OutcomeNotAttested, reasonNoRecordYet)
		if c.Leftovers != 2 || c.LeftoversStay {
			t.Fatalf("leftovers %d (want 2, removed by the first commit), stay %v", c.Leftovers, c.LeftoversStay)
		}
	})
	cases := []struct {
		name        string
		make        func(t *testing.T, out string)
		recordedGen uint64
		reason      string
	}{
		{"a pointer copy present but unreadable", func(t *testing.T, out string) { write(t, out, PointerName) }, 0, reasonPointersGone},
		{"this computer saved it", func(t *testing.T, out string) {}, 3, reasonSavedNoRecord},
		{"a search index", func(t *testing.T, out string) { write(t, out, "search.db") }, 0, reasonUsedNoRecord},
		{"a go-back history", func(t *testing.T, out string) { write(t, out, ".mailarchive-history.jsonl") }, 0, reasonUsedNoRecord},
		{"units", func(t *testing.T, out string) { recOnDisk(t, out, 1) }, 0, reasonNoRecord},
		{"inventory", func(t *testing.T, out string) { write(t, out, InventoryDir+"/ab.1") }, 0, reasonListNoRecord},
		{"chunks a file", func(t *testing.T, out string) { write(t, out, ChunksDir) }, 0, reasonNotAFolder},
		{"units a file", func(t *testing.T, out string) { write(t, out, "units") }, 0, reasonNotAFolder},
		{"a file of another shape in chunks", func(t *testing.T, out string) { write(t, out, "chunks/ab/notachunk") }, 0, reasonStrayNoRecord},
		{"a temp beside the chunk folders", func(t *testing.T, out string) { write(t, out, "chunks/.mailarchive-1.tmp") }, 0, reasonStrayNoRecord},
		{"a longer chunk folder", func(t *testing.T, out string) { os.MkdirAll(filepath.Join(out, ChunksDir, "abc"), 0o755) }, 0, reasonStrayNoRecord},
		{"a link at a chunk's name", func(t *testing.T, out string) { link(t, out, chunk) }, 0, reasonStrayNoRecord},
		{"a plaintext manifest of a newer version", func(t *testing.T, out string) {
			os.WriteFile(filepath.Join(out, EnvelopeName), []byte(fmt.Sprintf(`{"version":%d,"entries":{}}`, vault.EncryptedManifestVersion+1)), 0o600)
		}, 0, reasonNotSealed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := t.TempDir()
			tc.make(t, out)
			wantOutcome(t, keyless(t, out, tc.recordedGen), state.OutcomeDamaged, tc.reason)
			// The keyed load refuses the same folder.
			if _, _, err := LoadManifest(out, LoadOptions{Key: keys[0], RecordedGen: tc.recordedGen}); err == nil {
				t.Fatal("the keyed load accepted a folder the check calls damaged")
			}
		})
	}
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
	wantOutcome(t, keyless(t, out, gen+5), state.OutcomeNotAttested, reasonCleanRestore)
	// A crash-left temp is not a merge's file: the keyed load counts only unit- and
	// chunk-shaped ones (the adversarial pass, DEC-05).
	temp := filepath.Join(out, ChunksDir, "ab", ".mailarchive-123.tmp")
	os.MkdirAll(filepath.Dir(temp), 0o755)
	os.WriteFile(temp, []byte("half a chunk"), 0o600)
	wantOutcome(t, keyless(t, out, gen+5), state.OutcomeNotAttested, reasonCleanRestore)
	stray := filepath.Join(out, "units", "ee", "ee"+strings.Repeat("0", 30))
	os.MkdirAll(filepath.Dir(stray), 0o755)
	os.WriteFile(stray, []byte("a newer message's unit"), 0o600)
	wantOutcome(t, keyless(t, out, gen+5), state.OutcomeRestoredOlder, "older than the last backup this computer made")
}

// covers: MA-764, MA-766, R37
// A newer MailArchive (P2b-C11, §11 CRIT-2): both pointer copies in a newer format,
// or a newer copy that outranks the current one, or a newer envelope with no
// pointer, ask for an upgrade; a newer copy outranked by, or level with, a current
// one is damage; a newer envelope beside a current pointer is healed (the archive
// is intact), and beside two damaged pointer copies is their damage, as the keyed
// load says (the adversarial pass, OUT-09).
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
		wantOutcome(t, keyless(t, out, 0), state.OutcomeDamaged, reasonPointerBad)
	})
	t.Run("a newer copy level with the current one", func(t *testing.T) {
		out := wholeArchive(t)
		newer(t, filepath.Join(out, MirrorName), 0)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeDamaged, reasonPointerBad)
	})
	newerEnvelope := []byte(fmt.Sprintf(`{"version":%d,"encrypted":true}`, vault.EncryptedManifestVersion+1))
	t.Run("a newer envelope and no pointer", func(t *testing.T) {
		out := t.TempDir()
		os.WriteFile(filepath.Join(out, EnvelopeName), newerEnvelope, 0o600)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeNewerFormat, reasonNewer)
	})
	t.Run("a newer envelope beside a current pointer", func(t *testing.T) {
		out := wholeArchive(t)
		os.WriteFile(filepath.Join(out, EnvelopeName), newerEnvelope, 0o600)
		wantOutcome(t, keyless(t, out, 0), state.OutcomeIntact, reasonIntact)
	})
	t.Run("a newer envelope beside two damaged pointer copies", func(t *testing.T) {
		out := wholeArchive(t)
		os.WriteFile(filepath.Join(out, EnvelopeName), newerEnvelope, 0o600)
		for _, name := range []string{PointerName, MirrorName} {
			os.WriteFile(filepath.Join(out, name), []byte(strings.Repeat("?", 100)), 0o600)
		}
		wantOutcome(t, keyless(t, out, 0), state.OutcomeDamaged, reasonPointersGone)
	})
}
