package lockfile

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// covers: MA-431, R5, S25
// EW-AC9 tombstone. Held, which the copy export calls before every message and on every
// breaker tick, probes by taking the archive's exclusive lock for an instant, and Acquire
// never waited: a scheduled capture whose Acquire landed in that instant was refused as "in
// use" though nobody held the archive, and its backup was skipped until the next schedule.
// Acquire (and a probe that meets another probe) now retries for a short grace before it
// refuses. Two goroutines hammer Held on a lock nobody holds while thousands of Acquires are
// made: none is refused. The twin: a lock that IS held is still refused, and soon.
func TestAcquireIsNotRefusedByAProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	l, err := Acquire(path) // creates the file, so Held has one to open
	if err != nil {
		t.Fatal(err)
	}
	// The twin: a real holder refuses a second Acquire with ErrHeld, and promptly.
	began := time.Now()
	if _, err := Acquire(path); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("a held lock was acquired twice: %v", err)
	}
	if d := time.Since(began); d > 2*time.Second {
		t.Errorf("a held lock took %v to refuse", d)
	}
	if !Held(path) {
		t.Error("Held does not see a held lock")
	}
	l.Release()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var probes atomic.Int64
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				Held(path)
				probes.Add(1)
			}
		}()
	}
	refused := 0
	const attempts = 3000
	for i := 0; i < attempts; i++ {
		l, err := Acquire(path)
		if err != nil {
			refused++
			continue
		}
		l.Release()
	}
	close(stop)
	wg.Wait()
	if refused != 0 {
		t.Errorf("%d of %d Acquires were refused as held while only probes touched the lock (%d probes)", refused, attempts, probes.Load())
	}
}
