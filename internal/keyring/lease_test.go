package keyring

import (
	"errors"
	"sync"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

// usable reports whether k can still seal (a destroyed key refuses, MA-718).
func usable(k *vault.Key) bool {
	_, err := k.UnitName(vault.UnitMessage, "probe")
	return err == nil
}

// covers: MA-778, R37
func TestALeaseIsAnIndependentKeyThatOutlivesTheLock(t *testing.T) {
	freshRecord(t)
	out, _, u := made(t, nil)
	l, err := u.Lease()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Destroy()
	u.Lock()
	if _, err := u.Lease(); !errors.Is(err, ErrLocked) {
		t.Fatalf("a locked archive leased: %v", err)
	}
	var opts app.Options
	opts.Out = out
	done, err := Apply(&opts, l)
	if err != nil {
		t.Fatalf("the lease, after the archive it came from was locked: %v", err)
	}
	assure.Reached(t, usable(opts.Key) && opts.RecordedID == u.ArchiveID(), "the lease's own key")
	done()
}

// covers: MA-778, R37
func TestALockDuringALeaseInFlightWins(t *testing.T) {
	freshRecord(t)
	_, _, u := made(t, nil)
	// Positive twin: no lock, the lease succeeds.
	l, err := u.Lease()
	if err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	l.Destroy()

	var leased *vault.Opened
	orig := openVault
	openVault = func(out string, want vault.ID, s vault.Secret) (*vault.Opened, error) {
		o, err := orig(out, want, s)
		leased = o
		return o, err
	}
	defer func() { openVault = orig }()
	leaseHook = func() { u.Lock() }
	defer func() { leaseHook = func() {} }()

	l, err = u.Lease()
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("locked"),
		assure.NoSideEffect(func() bool { return l == nil && leased != nil && !usable(leased.Key) }))
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
}

// covers: MA-778, R37
func TestALockDestroysTheKeyOnlyWhenItsLastUseEnds(t *testing.T) {
	freshRecord(t)
	out, _, u := made(t, nil)
	var opts app.Options
	opts.Out = out
	done, err := Apply(&opts, u)
	if err != nil {
		t.Fatal(err)
	}
	u.Lock()
	if !usable(opts.Key) {
		t.Fatal("a lock destroyed a key a run was still using")
	}
	if _, err := Apply(&app.Options{Out: out}, u); !errors.Is(err, ErrLocked) {
		t.Fatalf("a locked archive started a run: %v", err)
	}
	done()
	assure.Reached(t, !usable(opts.Key), "the key destroyed when the run ended")
}

// covers: MA-778, R37
func TestLeasesAndLocksRaceSafely(t *testing.T) {
	freshRecord(t)
	_, _, u := made(t, nil)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := u.Lease()
			if err == nil {
				l.Destroy()
			}
			results <- err
		}()
	}
	u.Lock()
	wg.Wait()
	close(results)
	n := 0
	for err := range results {
		if err != nil && !errors.Is(err, ErrLocked) {
			t.Fatalf("a lease failed other than by the lock: %v", err)
		}
		n++
	}
	assure.Reached(t, n, "every lease answered")
	if _, err := u.Lease(); !errors.Is(err, ErrLocked) {
		t.Fatalf("leased after the lock: %v", err)
	}
}

// covers: MA-778, R37
func TestAReadOpenCannotRunABackup(t *testing.T) {
	freshRecord(t)
	out, _, _ := made(t, nil)
	r, err := Open(out, Password(testPassword), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	opts := app.Options{Out: out}
	_, err = Apply(&opts, r)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("opened to read"), assure.NoSideEffect(func() bool { return opts.Key == nil }))

	w, err := Open(out, Password(testPassword), OpenOptions{Write: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Destroy()
	other := app.Options{Out: t.TempDir()}
	_, err = Apply(&other, w)
	code, msg = refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, other.Out), assure.NoSideEffect(func() bool { return other.Key == nil }))
}
