package wizard

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/ncruces/zenity"
)

// covers: MA-1406, R50
// The wizard reached from the launcher choice does not ask the choice's question again
// (the S1 friction review, F10). Positive twin first: the standalone wizard's first list
// asks "What would you like to do?". The wizard in the running MailArchive (RunIn) asks
// "What mail do you want to add or back up?" over the launcher's items.
func TestMA1406_TheLauncherWizardAsksItsOwnQuestion(t *testing.T) {
	orig := firstListDialog
	t.Cleanup(func() { firstListDialog = orig })
	var prompts []string
	var lists [][]string
	firstListDialog = func(text string, items []string, _ ...zenity.Option) (string, error) {
		prompts = append(prompts, text)
		lists = append(lists, items)
		return "", zenity.ErrCanceled
	}
	if err := wizard(); !errors.Is(err, zenity.ErrCanceled) {
		t.Fatalf("the standalone wizard's first list: %v", err)
	}
	if len(prompts) != 1 || prompts[0] != "What would you like to do?" {
		t.Fatalf("the standalone wizard asked %q", prompts)
	}
	if _, ok := dialogPreflight(runtime.GOOS, dialogToolOnPath(), displayIsSet()); !ok {
		// No dialog tool or display (a headless Linux run): RunIn refuses before any list;
		// the question is proven on macOS and Windows.
		if code := RunIn(nil); code != 1 || len(prompts) != 1 {
			t.Errorf("RunIn with no dialogs possible: exit %d, lists %q; want 1 and none", code, prompts)
		}
		return
	}
	if code := RunIn(nil); code != 0 {
		t.Fatalf("RunIn with its first list dismissed: exit %d", code)
	}
	if len(prompts) != 2 || prompts[1] != "What mail do you want to add or back up?" || prompts[1] == prompts[0] {
		t.Errorf("the launcher's wizard asked %q, want its own question", prompts)
	}
	if len(lists) == 2 && len(lists[1]) != len(launcherChoices()) {
		t.Errorf("the launcher's wizard offered %q", lists[1])
	}
}

// covers: MA-1424, MA-1422, R52
// An open wizard is work, and a stop waits for the wizard's own writes (the S1
// adversarial pass, A4; friction F5 and F9). Positive twin first: with no wizard held
// and no write running, OpenHere is false and the wait returns at once. While this
// process holds the wizard lock OpenHere is true, and false again once it lets go (a
// second release changes nothing). A wizard that only waits for an answer is not waited
// for; one of its writes after the export (the job file and schedule install) is, until
// it ends or the stop's bound.
func TestMA1424_AnOpenWizardIsWorkAndItsWritesAreWaitedFor(t *testing.T) {
	if OpenHere() || WaitExports(context.Background()) != nil {
		t.Fatalf("idle: open %v", OpenHere())
	}
	dir := t.TempDir()
	release, held := LockWizardIn(dir)
	if held {
		t.Fatal("the wizard lock was held")
	}
	if !OpenHere() {
		t.Errorf("OpenHere is false while this process holds the wizard lock")
	}
	short, c := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer c()
	if err := WaitExports(short); err != nil {
		t.Errorf("a wizard waiting for an answer was waited for: %v", err)
	}
	writeStarted()
	short2, c2 := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer c2()
	if err := WaitExports(short2); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the wait with a write running: %v, want the bound's error", err)
	}
	done := make(chan error, 1)
	go func() { done <- WaitExports(context.Background()) }()
	time.Sleep(50 * time.Millisecond)
	writeEnded()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the wait after the write ended: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the wait did not end when the write ended")
	}
	release()
	release()
	if OpenHere() {
		t.Errorf("OpenHere is still true after the wizard let go")
	}
}
