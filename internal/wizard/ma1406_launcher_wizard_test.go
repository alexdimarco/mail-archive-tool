package wizard

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// covers: MA-1406, R50
// The wizard's first list from the launcher choice or a hand-off (Q2, design-menubar
// §6.2). Positive twin first: the standalone `wizard` verb keeps all four items. From the
// launcher, only the "open MailArchive Desktop" item is dropped; the two tasks that finish
// on a Desktop page stay, in order, the local item first; the list never has fewer than
// two items.
func TestMA1406_TheLauncherWizardDropsOnlyTheDesktopItem(t *testing.T) {
	if !reflect.DeepEqual(surfaceChoices, []string{surfaceLocalItem, surfaceDesktopItem, surfaceOpenItem, surfaceEncryptItem}) {
		t.Fatalf("the standalone wizard's list changed: %q", surfaceChoices)
	}
	got := launcherChoices()
	want := []string{surfaceLocalItem, surfaceOpenItem, surfaceEncryptItem}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the launcher's list %q, want %q", got, want)
	}
	for _, c := range got {
		if c == surfaceDesktopItem {
			t.Errorf("the launcher's list still offers %q", c)
		}
	}
	if len(got) < 2 {
		t.Errorf("the launcher's list has %d items", len(got))
	}
}

// covers: MA-1404, MA-1430, R50
// The `wizard` verb hands off first (design-menubar D5): when the running MailArchive
// takes the wizard, Run ends with exit 0 before any dialog, the preflight included (a
// launcher with no dialog tool still reaches a running copy that has one). The wizard
// lock is one per account across processes and inside one: a second take is held until
// the first lets go.
func TestMA1404_TheWizardVerbHandsOffFirstAndTheLockIsOne(t *testing.T) {
	orig := HandOff
	t.Cleanup(func() { HandOff = orig })
	asked := 0
	HandOff = func() bool { asked++; return true }
	if code := Run(); code != 0 || asked != 1 {
		t.Errorf("Run with a running copy that takes the wizard: exit %d, asked %d; want 0 and asked once", code, asked)
	}

	dir := t.TempDir()
	release, held := LockWizardIn(dir)
	if held || release == nil {
		t.Fatalf("the first take of the wizard lock was refused")
	}
	if _, again := LockWizardIn(dir); !again {
		t.Errorf("a second wizard took the lock while the first holds it")
	}
	release()
	r2, held := LockWizardIn(dir)
	if held {
		t.Errorf("the lock was not free after the first let go")
	} else {
		r2()
	}
}

// covers: MA-1424, R52
// The wizard's export inside the running MailArchive is bound to its life and waited for
// (design-menubar MB-2 item 4). Positive twin first: with no export running, the wait
// returns at once. An export running is waited for until it ends, or until the stop's
// bound, which returns the bound's error; an export's context is a child of the
// instance's life, so a stop cancels it.
func TestMA1424_TheWizardsExportIsWaitedFor(t *testing.T) {
	if err := WaitExports(context.Background()); err != nil || ExportsRunning() {
		t.Fatalf("no export: %v, running %v", err, ExportsRunning())
	}
	life, stop := context.WithCancel(context.Background())
	BindExports(life)
	t.Cleanup(func() { BindExports(context.Background()) })
	ctx, cancel := exportContext()
	defer cancel()
	exportStarted()
	if !ExportsRunning() {
		t.Fatalf("an export started but ExportsRunning is false")
	}
	short, c2 := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer c2()
	if err := WaitExports(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the wait with an export running: %v, want the bound's error", err)
	}
	stop()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatalf("a stop of the instance did not cancel the export's context")
	}
	done := make(chan error, 1)
	go func() { done <- WaitExports(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	exportEnded()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the wait after the export ended: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the wait did not end when the export ended")
	}
}
