package desktop

import (
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// gmailWaits writes the last-run record of a backup that ended ok and found the Gmail stores
// off, as a plaintext run does.
func gmailWaits(t *testing.T, out string, off ...string) {
	t.Helper()
	now := time.Now().UTC()
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now, Finished: now, GmailOff: off}); err != nil {
		t.Fatal(err)
	}
}

// cardRow is the archive's row on the card, as the page draws it.
func cardRow(t *testing.T, e *delEnv, out string) ArchiveRow {
	t.Helper()
	idx, err := archindex.ReadFile(e.cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range e.cfg.archiveRows(idx) {
		if r.Path == out {
			return r
		}
	}
	t.Fatalf("no row for %s", out)
	return ArchiveRow{}
}

// covers: MA-1236, R45, S82
// The Gmail question on the archive's card (slice B3's Desktop surface). Positive twin first: an
// archive whose last backup found no Gmail store off asks nothing. One whose last backup did asks
// the question in its exact words, with Yes and Not now, beside the archive (the page carries
// them); the archive itself is never written by the card. Not now is kept in the archive's job
// file on this computer and the card asks no more (a Yes sent after it, from a page left open,
// replaces it). Yes names the stores the record holds (never
// the page's), is kept in the job file for the next backup, which the job's run options carry,
// and the card then says the yes waits. A planted store name that is not a safe one is never
// taken. With no job file the answer is refused, with words that say why and what to do, and
// nothing is written. The card never asks for an encrypted archive (nothing beside it says it
// holds a Gmail account).
func TestTheCardAsksTheGmailQuestionAndKeepsTheAnswerHere(t *testing.T) {
	e := newDeleteEnv(t)
	out := e.plainArchive("Gmail")
	if r := cardRow(t, e, out); r.Gmail != "" || r.GmailYes != "" {
		t.Fatalf("positive twin: no Gmail store off, yet the card says %q / %q", r.Gmail, r.GmailYes)
	}
	gmailWaits(t, out, "gmail-store", "../escape")
	manifestBefore, _ := os.ReadFile(out + "/.mailarchive-manifest.json")

	// No job file: amended by the B3 friction review's F2 (MA-1292). The answer is no longer
	// refused: it is kept on this computer beside the job files, no job file is made, and the
	// archive is not written. The answer is then taken back so the job-file half below starts clean.
	jp, _ := job.PathFor(schedule.DefaultNameFor(out))
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "not-now"}); m["ok"] != true {
		t.Fatalf("Not now with no job file: %v", m)
	}
	if _, err := os.Lstat(jp); !os.IsNotExist(err) || !gmailanswer.Read(out).NotNow {
		t.Fatalf("Not now with no job file: job file %v, kept %+v", err, gmailanswer.Read(out))
	}
	if ap, _ := gmailanswer.Path(out); os.Remove(ap) != nil {
		t.Fatal("the answer kept beside the job files is not there")
	}

	if err := job.Write(jp, job.Job{Name: "gmail", Out: out, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	r := cardRow(t, e, out)
	if r.Gmail != state.GmailQuestion {
		t.Fatalf("the card asks %q, want the question's words", r.Gmail)
	}
	if page := e.page(); !strings.Contains(page, `data-gmail-answer="yes"`) || !strings.Contains(page, `data-gmail-answer="not-now"`) || !strings.Contains(page, ">Not now</button>") {
		t.Fatal("the page does not offer Yes and Not now beside the question")
	}

	// Yes: the record's safe stores, kept for the next backup.
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "yes"}); m["ok"] != true || m["note"] != gmailYesNote {
		t.Fatalf("Yes: %v", m)
	}
	j, err := job.Read(jp)
	if err != nil || !reflect.DeepEqual(j.GmailYes, []string{"gmail-store"}) || j.GmailNotNow {
		t.Fatalf("after Yes the job file holds %+v (%v)", j, err)
	}
	// The yes line, with what starts the next backup (F15, MA-1294).
	if r := cardRow(t, e, out); r.Gmail != "" || !strings.HasPrefix(r.GmailYes, gmailYesWaits) {
		t.Fatalf("after Yes the card says %q / %q", r.Gmail, r.GmailYes)
	}

	// Not now: kept; the card asks no more.
	j.GmailYes = nil
	job.Write(jp, j)
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "not-now"}); m["ok"] != true || m["note"] != gmailNotNowNote {
		t.Fatalf("Not now: %v", m)
	}
	if j, _ := job.Read(jp); !j.GmailNotNow || len(j.GmailYes) != 0 {
		t.Fatalf("after Not now the job file holds %+v", j)
	}
	if r := cardRow(t, e, out); r.Gmail != "" || r.GmailYes != "" {
		t.Fatalf("after Not now the card says %q / %q", r.Gmail, r.GmailYes)
	}
	if after, _ := os.ReadFile(out + "/.mailarchive-manifest.json"); string(after) != string(manifestBefore) {
		t.Fatal("answering on the card changed the archive's manifest")
	}

	// A Yes after a Not now (a page left open) replaces it: the stores are kept and Not now goes.
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "yes"}); m["ok"] != true {
		t.Fatalf("Yes after Not now: %v", m)
	}
	if j, _ := job.Read(jp); j.GmailNotNow || !reflect.DeepEqual(j.GmailYes, []string{"gmail-store"}) {
		t.Fatalf("after Yes following Not now the job file holds %+v", j)
	}

	// An encrypted archive's card never asks.
	if q, y := gmailCard(out, false, false); q != "" || y != "" {
		t.Fatalf("a card told it is not plaintext asks %q / %q", q, y)
	}
	// A bad answer is refused.
	jb, _ := os.ReadFile(jp)
	_, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "maybe"})
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names("Yes or Not now"),
		assure.NoSideEffect(func() bool { after, _ := os.ReadFile(jp); return string(after) == string(jb) }))
}
