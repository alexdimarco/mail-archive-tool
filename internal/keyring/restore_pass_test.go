package keyring

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/vault"
)

// The P3-1b adversarial pass's fixes (MA-793).

// covers: MA-793, MA-792, R37
// A restore whose record update fails after the key file was written says it IS in
// effect (ErrRestoredNotRecorded): the new password opens. A record that became
// unusable before the write is refused, writing nothing (the P3-1b pass, CON-02,
// FID-02).
func TestARestoreWhoseRecordCannotBeUpdatedSaysItIsInEffect(t *testing.T) {
	recDir := freshRecord(t)
	out, rk, file, _ := madeWithFile(t, nil)
	dropKeyFiles(t, out)
	p, err := RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	held, err := lockfile.AcquireAs(filepath.Join(recDir, encrecord.LockName), "test")
	if err != nil {
		t.Fatal(err)
	}
	err = p.Commit()
	held.Release()
	if !errors.Is(err, ErrRestoredNotRecorded) || !InEffect(err) {
		t.Fatalf("want ErrRestoredNotRecorded, in effect; got %v", err)
	}
	assure.Reached(t, strings.Contains(err.Error(), "new password"), "the person is told to open it with the new password")
	u, err := Open(out, Password(restoredPassword), OpenOptions{})
	assure.Reached(t, u, "the new password opens")
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()

	// The record unusable before the write: nothing is written.
	out, rk, file, _ = madeWithFile(t, nil)
	dropKeyFiles(t, out)
	p, err = RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	damageRecord(t, recDir)
	err = p.Commit()
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "record of encrypted archives"),
		assure.NoSideEffect(func() bool { return !exists(filepath.Join(out, vault.FileName)) }))
	if InEffect(err) {
		t.Fatalf("a restore that wrote nothing is reported in effect: %v", err)
	}
}

// covers: MA-793, MA-792, R37
// An archive forgotten after its restore was prepared is asked about at Commit, as
// the record is now: no writes nothing; yes puts it back and records it (the P3-1b
// pass, CON-01).
func TestAnArchiveForgottenAfterARestoreWasPreparedIsAskedAbout(t *testing.T) {
	for _, yes := range []bool{false, true} {
		freshRecord(t)
		out, rk, file, a := madeWithFile(t, nil)
		id := a.ArchiveID().String()
		dropKeyFiles(t, out)
		asked := 0
		p, err := RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{
			ConfirmReRecord: func(string) (bool, error) { asked++; return yes, nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		change(t, func(r *encrecord.Record) error { r.ForgetID(id); return nil })
		err = p.Commit()
		if asked != 1 {
			t.Fatalf("asked %d times about re-recording a forgotten archive", asked)
		}
		if !yes {
			code, msg := refusal(err)
			assure.Refused(t, code, msg, assure.Code(1), assure.Names("forget"),
				assure.NoSideEffect(func() bool { return !exists(filepath.Join(out, vault.FileName)) }))
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		assure.Reached(t, recordedID(t, out) == id, "recorded again on a yes")
	}
}

// covers: MA-793, MA-792, R37
// An archive forgotten while its key file is being written is not recorded again
// without the question: the key file is back and says so, the tombstone stays (the
// P3-1b pass, CON-01).
func TestAForgetDuringARestoresWriteIsNotOverriddenSilently(t *testing.T) {
	freshRecord(t)
	out, rk, file, a := madeWithFile(t, nil)
	id := a.ArchiveID().String()
	dropKeyFiles(t, out)
	asked := 0
	p, err := RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{
		ConfirmReRecord: func(string) (bool, error) { asked++; return true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	orig := commitRestore
	commitRestore = func(v *vault.PendingRestore) error {
		change(t, func(r *encrecord.Record) error { r.ForgetID(id); return nil })
		return orig(v)
	}
	defer func() { commitRestore = orig }()
	err = p.Commit()
	if !errors.Is(err, ErrRestoredNotRecorded) || !errors.Is(err, ErrReRecordRefused) || asked != 0 {
		t.Fatalf("want the key file back, not recorded, unasked; got %v (asked %d)", err, asked)
	}
	e := loadRecord(t).Archives[id]
	assure.Reached(t, e != nil && e.Forgotten && len(e.Paths) == 0, "the tombstone kept")
}

// covers: MA-793, MA-792
// What a restore's Commit returns after the key file was written is in effect; a
// refusal before it is not (the P3-1b pass, OUT-01, CON-02).
func TestARestoreErrorSaysWhetherItIsInEffect(t *testing.T) {
	for err, want := range map[error]bool{
		nil: true,
		fmt.Errorf("x: %w", vault.ErrPartiallySaved): true,
		fmt.Errorf("x: %w", vault.ErrUnconfirmed):    true,
		fmt.Errorf("x: %w", ErrRestoredNotRecorded):  true,
		fmt.Errorf("x: %w", vault.ErrConflict):       false,
		errors.New("the drive went away"):            false,
	} {
		if InEffect(err) != want {
			t.Errorf("InEffect(%v) = %v, want %v", err, !want, want)
		}
	}
	assure.Reached(t, InEffect(nil), "a restore with no error is in effect")
}

// covers: MA-793, MA-792, R37
// Commit judges a recorded folder's case again: one that holds other files since
// the restore was prepared gets nothing written (the P3-1b pass, CRIT-05).
func TestARestoreJudgesTheFolderAgainAtCommit(t *testing.T) {
	freshRecord(t)
	out, rk, file, _ := madeWithFile(t, nil)
	dropKeyFiles(t, out)
	p, err := RestoreFromFile(out, file, RecoveryKey(rk), restoredPassword, RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte("another drive's"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = p.Commit()
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out),
		assure.NoSideEffect(func() bool { return !exists(filepath.Join(out, vault.FileName)) }))
	if !errors.Is(err, ErrNotTheArchive) {
		t.Fatalf("want ErrNotTheArchive, got %v", err)
	}
}

// raiseRecoveryFileCost makes a recovery file's entry ask for more memory than
// MailArchive's own (its wrapped key then no longer unwraps).
func raiseRecoveryFileCost(t *testing.T, file []byte) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(file, &m); err != nil {
		t.Fatal(err)
	}
	m["entry"].(map[string]any)["memory_kib"] = float64(vault.CostFloor().MemoryKiB * 2)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// covers: MA-793, MA-792, MA-777, R37
// At a folder this computer has not recorded, a recovery file asking more than
// MailArchive's own cost is asked about before it is derived; no refuses, writing
// nothing; at a recorded folder nothing is asked (K14; the P3-1b pass, CRY-05).
func TestACostlyRecoveryFileIsAskedAboutWhereNothingRecordedIt(t *testing.T) {
	freshRecord(t)
	out, rk, file, b := madeWithFile(t, nil)
	backedUp(t, b)
	costly := raiseRecoveryFileCost(t, file)
	dropKeyFiles(t, out)
	if _, err := RestoreFromFile(out, costly, RecoveryKey(rk), restoredPassword, RestoreOptions{
		ConfirmCost: func(vault.Params) (bool, error) { t.Fatal("asked at a recorded folder"); return false, nil },
	}); !errors.Is(err, vault.ErrWrongSecret) {
		t.Fatalf("at a recorded folder, the altered entry derives and fails: %v", err)
	}
	change(t, func(r *encrecord.Record) error { delete(r.Archives, b.ArchiveID().String()); return nil })
	var asked []vault.Params
	_, err := RestoreFromFile(out, costly, RecoveryKey(rk), restoredPassword, RestoreOptions{
		ConfirmCost: func(p vault.Params) (bool, error) { asked = append(asked, p); return false, nil },
	})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out),
		assure.NoSideEffect(func() bool { return !exists(filepath.Join(out, vault.FileName)) }))
	if !errors.Is(err, ErrCostRefused) || len(asked) != 1 || asked[0].MemoryKiB != vault.CostFloor().MemoryKiB*2 {
		t.Fatalf("want one question naming the cost, then ErrCostRefused; asked %v, got %v", asked, err)
	}
	_, err = RestoreFromFile(out, costly, RecoveryKey(rk), restoredPassword, RestoreOptions{
		ConfirmCost: func(vault.Params) (bool, error) { return true, nil },
	})
	if !errors.Is(err, vault.ErrWrongSecret) {
		t.Fatalf("after a yes, the entry is derived: %v", err)
	}
}

// covers: MA-793, MA-777, R37
// The cost question never lets a raw vault.CostError out: past two questions, or
// with no one to ask, it is ErrCostRefused naming the cost (the P3-1b pass, REG-02).
func TestTheCostQuestionNeverLetsARawCostErrorOut(t *testing.T) {
	asked := 0
	tries := 0
	err := askingCost("/an/archive", vault.ID{}, func(vault.Params) (bool, error) { asked++; return true, nil },
		func(limit *vault.Params) error {
			tries++
			return &vault.CostError{Params: vault.Params{MemoryKiB: limit.MemoryKiB + 1, Time: limit.Time, Threads: limit.Threads}}
		})
	var ce *vault.CostError
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("/an/archive"))
	if errors.As(err, &ce) || !errors.Is(err, ErrCostRefused) || asked != 2 || tries != 3 {
		t.Fatalf("want ErrCostRefused after two questions and three tries, got %v (asked %d, tried %d)", err, asked, tries)
	}
	// A yes raises the limit to the most of each setting, never lowering one.
	var limits []vault.Params
	askingCost("/an/archive", vault.ID{}, func(vault.Params) (bool, error) { return true, nil }, func(limit *vault.Params) error {
		limits = append(limits, *limit)
		if len(limits) == 1 {
			return &vault.CostError{Params: vault.Params{MemoryKiB: limit.MemoryKiB - 1, Time: limit.Time + 1, Threads: limit.Threads}}
		}
		return nil
	})
	floor := vault.CostFloor()
	assure.Reached(t, len(limits) == 2 && limits[1] == vault.Params{MemoryKiB: floor.MemoryKiB, Time: floor.Time + 1, Threads: floor.Threads},
		"the limit raised in time and kept in memory, not lowered to what was asked")
}

// blockingStore is a secret store whose Get waits until released, as an OS
// keychain waits on the person.
type blockingStore struct {
	vault.Store
	entered, release chan struct{}
}

func (s blockingStore) Get(account string) (string, error) {
	close(s.entered)
	<-s.release
	return s.Store.Get(account)
}

// covers: MA-793, MA-792, R37
// A restore from the remembered key does not hold the derivation lock while the
// secret store waits on the person: another open in this process goes ahead (the
// P3-1b pass, CON-04).
func TestARememberedRestoreDoesNotHoldOtherOpensBehindTheStore(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, _, _, _ := madeWithFile(t, s)
	other, _, _, _ := madeWithFile(t, nil)
	dropKeyFiles(t, out)
	bs := blockingStore{Store: s, entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		p, err := RestoreFromRemembered(out, bs, restoredPassword, RestoreOptions{})
		if err == nil {
			p.Discard()
		}
		done <- err
	}()
	<-bs.entered
	opened := make(chan error, 1)
	go func() {
		u, err := Open(other, Password(testPassword), OpenOptions{})
		if err == nil {
			u.Destroy()
		}
		opened <- err
	}()
	select {
	case err := <-opened:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		close(bs.release)
		t.Fatal("an open waited behind the remembered restore's store")
	}
	close(bs.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, true, "the open went ahead while the store waited")
}

// covers: MA-793, MA-792, R37
// Damaged key files at a recorded folder — none parses, or both parse and do not
// authenticate — are told how to put them back (case 3; the P3-1b pass, FID-05).
func TestDamagedKeyFilesAtARecordedFolderOfferTheRestore(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, path string){
		"none parses": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("damaged"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"none authenticates": func(t *testing.T, path string) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			mac := m["mac"].(string)
			m["mac"] = map[bool]string{true: "1", false: "0"}[mac[0] == '0'] + mac[1:]
			b, _ := json.Marshal(m)
			if err := os.WriteFile(path, b, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			freshRecord(t)
			out, _, _, _ := madeWithFile(t, nil)
			for _, f := range []string{vault.FileName, vault.BackupName} {
				damage(t, filepath.Join(out, f))
			}
			_, err := Open(out, Password(testPassword), OpenOptions{})
			code, msg := refusal(err)
			assure.Refused(t, code, msg, assure.Code(1), assure.Names("restore-key-file", out))
		})
	}
}
