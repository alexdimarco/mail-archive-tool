package vault

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// The P3-1b adversarial pass's fixes (review-encrypted-archives-phase3-p31b-
// adversarial.md, MA-793).

// rewriteCopy writes name as the primary's vault changed by f, authenticated under k.
func rewriteCopy(t *testing.T, out, name string, k *Key, f func(v *Vault)) {
	t.Helper()
	v, err := Parse(readFile(t, out, FileName))
	if err != nil {
		t.Fatal(err)
	}
	f(v)
	v.mac = v.computeMAC(k)
	b, err := v.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, out, name, b)
}

// flipMAC damages name's MAC so it still parses but no longer authenticates.
func flipMAC(t *testing.T, out, name string) {
	t.Helper()
	editVault(t, out, name, func(m map[string]any) {
		mac := m["mac"].(string)
		c := byte('0')
		if mac[0] == '0' {
			c = '1'
		}
		m["mac"] = string(c) + mac[1:]
	})
}

// covers: MA-793, MA-777, R37
// OpenWithin judges every copy's cost before any derivation — the re-derivation on
// a newer copy too — and names the most each setting asks, so one question covers
// both copies; a wrong password beside a costlier copy derives nothing (the P3-1b
// pass, CRY-01, REG-02, REG-03).
func TestOpenWithinJudgesEveryCopyBeforeAnyDerivation(t *testing.T) {
	lowCost(t)
	out, k, _ := newArchive(t)
	v1, err := Parse(readFile(t, out, FileName))
	if err != nil {
		t.Fatal(err)
	}
	slower := Params{MemoryKiB: floor.MemoryKiB, Time: floor.Time + 1, Threads: floor.Threads}
	rewriteCopy(t, out, BackupName, k, func(v *Vault) {
		v.Generation++
		v.Password = wrapAt(t, k, entryPassword, passwordBytes(testPassword), slower)
	})
	runs := countDerivations(t)
	_, err = OpenWithin(out, ID{}, PasswordSecret(testPassword), floor)
	var ce *CostError
	if !errors.As(err, &ce) || ce.Params != slower {
		t.Fatalf("a newer copy above the limit: want a CostError naming %s, got %v", slower, err)
	}
	if len(*runs) != 0 {
		t.Fatalf("derived %v before refusing", *runs)
	}
	o, err := OpenWithin(out, ID{}, PasswordSecret(testPassword), slower)
	if err != nil {
		t.Fatalf("positive twin, the limit raised: %v", err)
	}
	assure.Reached(t, o.Vault.Generation == v1.Generation+1, "the newer copy opened once its cost is allowed")
	o.Key.Destroy()

	// Two copies costlier in different settings: one refusal names the most of each.
	wider := Params{MemoryKiB: floor.MemoryKiB, Time: floor.Time, Threads: floor.Threads + 1}
	rewriteCopy(t, out, FileName, k, func(v *Vault) {
		v.Password = wrapAt(t, k, entryPassword, passwordBytes(testPassword), wider)
	})
	_, err = OpenWithin(out, ID{}, PasswordSecret(testPassword), floor)
	both := Params{MemoryKiB: floor.MemoryKiB, Time: floor.Time + 1, Threads: floor.Threads + 1}
	if !errors.As(err, &ce) || ce.Params != both {
		t.Fatalf("want a CostError naming %s, got %v", both, err)
	}

	// A wrong password beside a costlier copy derives nothing.
	*runs = nil
	_, err = OpenWithin(out, ID{}, PasswordSecret("not the passphrase at all"), floor)
	if !errors.As(err, &ce) || len(*runs) != 0 {
		t.Fatalf("a wrong password: want a CostError with nothing derived, got %v after %v", err, *runs)
	}
}

// covers: MA-793, MA-791, R37
// At a recorded folder with no sealed record, the only evidence is the key file
// itself: a copy naming the archive that the recovery file did not make — a forged
// recovery file's other master key — is refused, writing nothing; a copy merely
// damaged, whose recovery entry is the file's own, is put back (the P3-1b pass,
// CRY-02, CRIT-03).
func TestARestoreWithNoRecordNeedsKeyFilesItsSourceVouchesFor(t *testing.T) {
	lowCost(t)
	out, k, rk, file := sealedArchive(t)
	removeFile(t, out, pointerNames[0])
	forged := testKey(t, k.ArchiveID(), 0x5a)
	frk, err := newRecoveryKey()
	if err != nil {
		t.Fatal(err)
	}
	fv := &Vault{ArchiveID: k.ArchiveID(), Generation: 1}
	fv.Recovery = wrapAt(t, forged, entryRecovery, frk[:], floor)
	ffile, err := fv.RecoveryFile()
	if err != nil {
		t.Fatal(err)
	}
	snap := snapshot(out)
	_, err = PrepareRestore(out, k.ArchiveID(), fromFile(t, ffile, frk), restoredPassword)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(FileName, "move"), assure.NoSideEffect(func() bool {
		now := snapshot(out)
		return string(now[0]) == string(snap[0]) && string(now[1]) == string(snap[1])
	}))
	if !errors.Is(err, ErrNotProven) {
		t.Fatalf("want ErrNotProven, got %v", err)
	}

	// Positive twin: the archive's own recovery file over its damaged copies.
	flipMAC(t, out, FileName)
	flipMAC(t, out, BackupName)
	p, err := PrepareRestore(out, k.ArchiveID(), fromFile(t, file, rk), restoredPassword)
	if err != nil {
		t.Fatalf("the archive's own recovery file over damaged copies: %v", err)
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	o, err := Open(out, ID{}, PasswordSecret(restoredPassword))
	assure.Reached(t, o, "opened after the restore")
	if err != nil {
		t.Fatal(err)
	}
	o.Key.Destroy()
}

// covers: MA-793, MA-791, R37
// A copy claiming a generation within the cap's headroom — far past any real
// clock — is refused by its name, writing nothing, never wrapped round or used up
// silently; a claim far ahead of the clock but nowhere near the cap is the floor
// the restore outranks (the P3-1b pass, CRY-03, OUT-03, FID-04, CRIT-02).
func TestARestoreRefusesByNameACopyClaimingAGenerationNearTheCap(t *testing.T) {
	lowCost(t)
	for _, g := range []uint64{math.MaxUint64, maxGeneration, maxGeneration - 1, maxGeneration - 2, maxGeneration - generationHeadroom} {
		t.Run(fmt.Sprint(g), func(t *testing.T) {
			out, _, rk, file := sealedArchive(t)
			dropKeyFiles(t, out)
			writeFile(t, out, BackupName, []byte(fmt.Sprintf(`{"generation": %d}`, g)))
			snap := snapshot(out)
			_, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
			code, msg := refusal(err)
			assure.Refused(t, code, msg, assure.Code(1), assure.Names(BackupName, "move it aside"), assure.NoSideEffect(func() bool {
				now := snapshot(out)
				return now[0] == nil && string(now[1]) == string(snap[1])
			}))
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("want ErrCorrupt, got %v", err)
			}
		})
	}
	out, _, rk, file := sealedArchive(t)
	dropKeyFiles(t, out)
	// A clock within the headroom is refused: the restore would leave no room.
	orig := clock
	clock = func() time.Time { return time.Unix(maxGeneration-generationHeadroom, 0) }
	_, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
	clock = orig
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("clock"), assure.NoSideEffect(func() bool { return snapshot(out)[0] == nil }))
	const ahead = uint64(1) << 40
	writeFile(t, out, BackupName, []byte(fmt.Sprintf(`{"generation": %d}`, ahead)))
	p, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
	if err != nil {
		t.Fatalf("a claim far ahead of the clock: %v", err)
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	v, err := Parse(readFile(t, out, FileName))
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, v.Generation == ahead+1, "the restore outranks the claim")
}

// writeOnly makes writeCopy apply f to the file named name and write the rest as
// asked, for the test's duration.
func writeOnly(t *testing.T, f map[string]func(path string, b []byte) error) {
	t.Helper()
	orig := writeCopy
	writeCopy = func(path string, b []byte, pattern string) error {
		if g := f[filepath.Base(path)]; g != nil {
			return g(path, b)
		}
		return orig(path, b, pattern)
	}
	t.Cleanup(func() { writeCopy = orig })
}

// covers: MA-793, MA-791, R37
// Once the primary is written the restore IS in effect, and Commit says so
// whatever follows: it reads back only the primary, so a mirror planted after the
// write cannot turn it into "not in effect"; a primary that does not read back as
// written is ErrUnconfirmed — on the mirror-failed path too, never success; a
// primary that could not be written is neither (the P3-1b pass, OUT-01, CON-03,
// OUT-04, FID-03).
func TestARestoreThatWroteItsKeyFileSaysSoWhateverFollows(t *testing.T) {
	lowCost(t)
	other, _, _ := newArchive(t)
	foreign := readFile(t, other, FileName)
	write := func(path string, b []byte) error { return os.WriteFile(path, b, 0o600) }
	damaged := func(path string, b []byte) error { return os.WriteFile(path, []byte("damaged"), 0o600) }
	planted := func(path string, b []byte) error { return os.WriteFile(path, foreign, 0o600) }
	fails := func(string, []byte) error { return errors.New("the drive went away") }
	// The archive's earlier key file, which parses and authenticates under the same
	// master key, put where the new one was written.
	var earlier []byte
	stale := func(path string, b []byte) error { return os.WriteFile(path, earlier, 0o600) }
	cases := map[string]struct {
		writes           map[string]func(string, []byte) error
		inEffect         bool
		partial, unknown bool
	}{
		"written":                                {writes: map[string]func(string, []byte) error{FileName: write}, inEffect: true},
		"a foreign mirror planted after":         {writes: map[string]func(string, []byte) error{BackupName: planted}, inEffect: true},
		"the primary altered after":              {writes: map[string]func(string, []byte) error{FileName: damaged}, inEffect: true, unknown: true},
		"an earlier key file put in its place":   {writes: map[string]func(string, []byte) error{FileName: stale}, inEffect: true, unknown: true},
		"the mirror failed":                      {writes: map[string]func(string, []byte) error{BackupName: fails}, inEffect: true, partial: true},
		"the mirror failed, the primary altered": {writes: map[string]func(string, []byte) error{FileName: damaged, BackupName: fails}, inEffect: true, partial: true, unknown: true},
		"the primary failed":                     {writes: map[string]func(string, []byte) error{FileName: fails}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			out, _, rk, file := sealedArchive(t)
			earlier = readFile(t, out, FileName)
			dropKeyFiles(t, out)
			p, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
			if err != nil {
				t.Fatal(err)
			}
			writeOnly(t, c.writes)
			err = p.Commit()
			if errors.Is(err, ErrPartiallySaved) != c.partial || errors.Is(err, ErrUnconfirmed) != c.unknown {
				t.Fatalf("partial %v, unconfirmed %v; got %v", c.partial, c.unknown, err)
			}
			if c.inEffect && !c.partial && !c.unknown && err != nil {
				t.Fatalf("a restore in effect reported as failed: %v", err)
			}
			if !c.inEffect && err == nil {
				t.Fatal("a restore whose primary was never written reported success")
			}
			if c.unknown {
				assure.Reached(t, strings.Contains(err.Error(), FileName), "the read-back failure names the key file")
			} else if c.inEffect {
				v, perr := Parse(readFile(t, out, FileName))
				assure.Reached(t, perr == nil && v.Generation > 0, "the restored key file is the primary")
			}
		})
	}
}

// covers: MA-793, MA-791, R37
// A folder renamed away and replaced — by another folder or by a link — between
// Prepare and Commit gets nothing written into it, and the restore is spent (the
// P3-1b pass, OUT-02).
func TestARestoreWritesNothingIntoAFolderPutInPlaceOfTheOnePrepared(t *testing.T) {
	lowCost(t)
	swaps := map[string]func(t *testing.T, out, decoy string){
		"another folder": func(t *testing.T, out, decoy string) {
			if err := os.Rename(decoy, out); err != nil {
				t.Fatal(err)
			}
		},
	}
	if runtime.GOOS != "windows" {
		swaps["a link"] = func(t *testing.T, out, decoy string) {
			if err := os.Symlink(decoy, out); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, swap := range swaps {
		t.Run(name, func(t *testing.T) {
			out, _, rk, file := sealedArchive(t)
			dropKeyFiles(t, out)
			p, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
			if err != nil {
				t.Fatal(err)
			}
			decoy := t.TempDir()
			if err := os.Rename(out, out+"-moved"); err != nil {
				t.Fatal(err)
			}
			swap(t, out, decoy)
			err = p.Commit()
			code, msg := refusal(err)
			assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "no longer"), assure.NoSideEffect(func() bool {
				d, _ := os.ReadDir(out)
				return len(d) == 0
			}))
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("want ErrConflict, got %v", err)
			}
			if err := p.Commit(); !errors.Is(err, ErrPendingUsed) {
				t.Fatalf("a refused restore can still be committed: %v", err)
			}
		})
	}
}

// covers: MA-793, MA-791, R37
// A recovery file asking more than the limit is refused before any derivation,
// naming its cost; within the limit it is used (K14 at a folder this computer has
// not recorded; the P3-1b pass, CRY-05).
func TestARecoveryFileAboveTheLimitIsRefusedBeforeDeriving(t *testing.T) {
	lowCost(t)
	out, _, rk, file := sealedArchive(t)
	dropKeyFiles(t, out)
	e, err := ParseRecoveryFile(file)
	if err != nil {
		t.Fatal(err)
	}
	tight := Params{MemoryKiB: floor.MemoryKiB - 1, Time: floor.Time, Threads: floor.Threads}
	runs := countDerivations(t)
	_, err = PrepareRestore(out, ID{}, RecoveryEntrySource{Entry: e, RecoveryKey: rk.String(), Limit: &tight}, restoredPassword)
	var ce *CostError
	if !errors.As(err, &ce) || ce.Params != floor || len(*runs) != 0 {
		t.Fatalf("want a CostError naming %s before deriving, got %v after %v", floor, err, *runs)
	}
	limit := floor
	p, err := PrepareRestore(out, ID{}, RecoveryEntrySource{Entry: e, RecoveryKey: rk.String(), Limit: &limit}, restoredPassword)
	assure.Reached(t, p, "the restore within the limit")
	if err != nil {
		t.Fatal(err)
	}
	p.Discard()
}

// covers: MA-793, MA-718, R37
// A restore's sources and a pending restore print redacted, whatever the verb (the
// P3-1b pass, CRIT-04).
func TestRestoreSourcesAndAPendingRestorePrintRedacted(t *testing.T) {
	lowCost(t)
	out, _, rk, file := sealedArchive(t)
	dropKeyFiles(t, out)
	src := fromFile(t, file, rk)
	p, err := PrepareRestore(out, ID{}, src, restoredPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Discard()
	secrets := []string{rk.String(), strings.ReplaceAll(rk.String(), "-", ""), p.RecoveryKey().String(),
		restoredPassword, hex.EncodeToString(p.key.master[:])}
	for label, v := range map[string]any{"RecoveryEntrySource": src, "*PendingRestore": p} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			got := fmt.Sprintf(verb, v)
			assure.Reached(t, got, label+" printed")
			for _, s := range secrets {
				if strings.Contains(strings.ToLower(got), strings.ToLower(s)) {
					t.Fatalf("%s printed with %s leaks a secret: %q", label, verb, got)
				}
			}
			if !strings.Contains(got, "redacted") {
				t.Fatalf("%s printed with %s must say it is redacted: %q", label, verb, got)
			}
		}
	}
}

// covers: MA-793, MA-791, R37
// A stated limit, accepted by the owner (2026-10-26): with no copy of the key file
// left on disk a restore's generation is the clock, so a second restore on a clock
// behind the first's is outranked if the first's key file comes back (the P3-1b
// pass, FID-01, CRY-04, CRIT-02).
func TestASecondRestoreOutranksTheFirstWithNoCopyLeft(t *testing.T) {
	t.Skip("stated limit, accepted by the owner 2026-10-26 (P3-1b pass FID-01, CRY-04; the CRIT-02 policy): with no copy left on disk the restore's generation is the clock, so §9.4 and MA-791 claim outranking only while a copy claiming the earlier generation survives or the clocks were right. Closing it needs a floor that survives losing every copy, which a record on this computer cannot give another computer.")
	lowCost(t)
	out, _, rk, file := sealedArchive(t)
	dropKeyFiles(t, out)
	orig := clock
	defer func() { clock = orig }()
	clock = func() time.Time { return releaseFloor.AddDate(10, 0, 0) }
	p, err := PrepareRestore(out, ID{}, fromFile(t, file, rk), restoredPassword)
	if err != nil {
		t.Fatal(err)
	}
	newRK, newFile := p.RecoveryKey(), must(p.RecoveryFile())
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	first := readFile(t, out, FileName)
	dropKeyFiles(t, out)
	clock = func() time.Time { return releaseFloor.Add(time.Hour) }
	p2, err := PrepareRestore(out, ID{}, fromFile(t, newFile, newRK), "a second restored passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if err := p2.Commit(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, out, BackupName, first)
	_, err = Open(out, ID{}, PasswordSecret(restoredPassword))
	assure.Reached(t, errors.Is(err, ErrWrongSecret), "the first restore's password stays withdrawn")
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

// pointerAt writes a record pointer whose header claims format f over junk.
func pointerAt(t *testing.T, out, name string, f uint16) {
	t.Helper()
	raw := append([]byte(pointerMagic), 0, 0)
	binary.BigEndian.PutUint16(raw[4:6], f)
	raw = binary.BigEndian.AppendUint64(raw, 3)
	writeFile(t, out, name, append(raw, "sealed"...))
}
