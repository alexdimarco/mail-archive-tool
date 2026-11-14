package desktop

import (
	"net/http"
	"os"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// covers: MA-1307, R45, R36
// The Desktop's Gmail answer takes the guards the other writers of this computer's settings take
// (the B3 adversarial pass's A6): Remove from list's settings hold, so it is refused, nothing
// kept, while an uninstall removes the settings; Pause's unfinished-delete check, so no answer is
// kept for a half-deleted folder; and Save settings' lock on the job file, so the two never write
// it at once. Positive twin first: another archive's answer is kept while a delete of the first
// is not finished.
func TestTheGmailAnswerTakesTheSettingsGuards(t *testing.T) {
	e := newDeleteEnv(t)
	other := e.plainArchive("Other")
	gmailWaits(t, other, "gmail-store")
	stopped := e.plainArchive("Stopped")
	gmailWaits(t, stopped, "gmail-store")
	stopAt2(t, e, stopped)

	if _, m := e.post("/api/gmail-answer", map[string]any{"path": other, "answer": "not-now", "archive": state.ArchiveID(other)}); m["ok"] != true || !gmailanswer.Read(other).NotNow {
		t.Fatalf("positive twin: another archive's Not now: %v", m)
	}

	// A delete of the folder that did not finish.
	sp, _ := gmailanswer.Path(stopped)
	_, s := e.post("/api/gmail-answer", map[string]any{"path": stopped, "answer": "yes", "archive": state.ArchiveID(stopped)})
	assure.Refused(t, http.StatusOK, replyErr(s), assure.Code(http.StatusOK),
		assure.Names("Delete not finished", "your answer was not kept", "nothing was changed"),
		assure.NoSideEffect(func() bool { _, err := os.Lstat(sp); return s["ok"] == false && os.IsNotExist(err) }))

	// An uninstall removing the settings.
	op, _ := gmailanswer.Path(other)
	before, _ := os.ReadFile(op)
	if _, why := e.d.running.block(uninstallingText); why != "" {
		t.Fatal(why)
	}
	_, u := e.post("/api/gmail-answer", map[string]any{"path": other, "answer": "yes", "archive": state.ArchiveID(other)})
	assure.Refused(t, http.StatusOK, replyErr(u), assure.Code(http.StatusOK), assure.Names(uninstallingText),
		assure.NoSideEffect(func() bool { after, _ := os.ReadFile(op); return u["ok"] == false && string(after) == string(before) }))
	e.d.running.mu.Lock()
	e.d.running.blocked = ""
	e.d.running.mu.Unlock()

	// Save settings' lock on the job file: the answer waits for it.
	jobFileMu.Lock()
	done := make(chan map[string]any, 1)
	go func() {
		_, m := e.post("/api/gmail-answer", map[string]any{"path": other, "answer": "yes", "archive": state.ArchiveID(other)})
		done <- m
	}()
	select {
	case m := <-done:
		jobFileMu.Unlock()
		t.Fatalf("the answer was kept while Save settings held the job file: %v", m)
	case <-time.After(300 * time.Millisecond):
	}
	jobFileMu.Unlock()
	if m := <-done; m["ok"] != true || gmailanswer.Read(other).NotNow {
		t.Fatalf("the answer after Save settings let go: %v", m)
	}
}
