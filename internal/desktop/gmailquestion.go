package desktop

// The Gmail question on an archive's card (Q-B; docs/design-gmail-dedup.md §7.4 fold c; slice
// B3). A backup that reads a Gmail account of an existing archive that is not switched on keeps
// the account's store folders in its last-run record (state.LastRun.GmailOff, plaintext only); the
// card then asks, beside the archive, without blocking anything:
//
//	Show Sent Mail and your other Gmail folders in full, and store each email once? Every
//	computer that backs up into this archive then needs MailArchive 1.8 or later.
//
// with Yes and Not now. Both answers are kept on this computer, in the archive's job file, never in
// the archive: Yes names the stores, and the archive's next backup switches them on (so the archive
// is version 7 from that backup's first save); Not now means this computer does not ask again by
// itself. A scheduled backup never asks and never switches without the yes.

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/state"
)

// The card's words around the question (for the friction review).
const (
	gmailYesWaits   = "Gmail: you said yes. From this archive's next backup on, each Gmail email is stored once and shown in all of its folders."
	gmailYesNote    = "Yes. From this archive's next backup on, each Gmail email is stored once and shown in all of its folders."
	gmailNotNowNote = "Not now. Nothing changed, and MailArchive will not ask again on this computer."
	gmailNoQuestion = "There is no Gmail question for this archive now. Reload the page."
	gmailNoJob      = "MailArchive could not keep your answer: this archive has no backup settings on this computer. Back it up once from this page, then answer."
)

// gmailOffAt is the Gmail stores the last backup of the plaintext archive at out found off: its
// last-run record's, each a safe store name, read as every card line is (bounded, never through a
// link). None for an encrypted archive, a record of an encrypted one, or a backup that did not end
// ok.
func gmailOffAt(out string, plaintext bool) []string {
	if !plaintext || encmark.MustMinimise(out) {
		return nil
	}
	lr, st, _ := state.ReadLastRun(out)
	if st != state.LastRunPresent || lr.Status != state.RunOK || lr.Encrypted {
		return nil
	}
	var off []string
	for _, t := range lr.GmailOff {
		if state.SafeToken(t) {
			off = append(off, t)
		}
	}
	return off
}

// gmailJob is the archive's job file on this computer and its path; ok false when there is none
// or it cannot be read.
func gmailJob(out string) (job.Job, string, bool) {
	p, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		return job.Job{}, "", false
	}
	if _, err := os.Lstat(p); err != nil {
		return job.Job{}, p, false
	}
	j, err := job.Read(p)
	return j, p, err == nil
}

// gmailCard is the card's question for the archive at out, or the line saying a yes waits for
// the next backup; both "" when nothing is to be said: no Gmail store off, Not now kept here.
func gmailCard(out string, plaintext bool) (question, yesWaits string) {
	off := gmailOffAt(out, plaintext)
	if len(off) == 0 {
		return "", ""
	}
	j, _, _ := gmailJob(out)
	if j.GmailNotNow {
		return "", ""
	}
	said := map[string]bool{}
	for _, t := range j.GmailYes {
		said[t] = true
	}
	for _, t := range off {
		if !said[t] {
			return state.GmailSwitchQuestion(false), ""
		}
	}
	return "", gmailYesWaits
}

// gmailAnswer is the card's Yes or Not now: a loopback-only, CSRF-tokened POST of
// {"path": …, "answer": "yes" | "not-now"}. The stores are read again from the archive's last-run
// record here, never taken from the page.
func (d *dashboard) gmailAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, csrfRefused, http.StatusForbidden)
		return
	}
	var in struct{ Path, Answer string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	path := strings.TrimSpace(in.Path)
	if path == "" || (in.Answer != "yes" && in.Answer != "not-now") {
		writeJSON(w, map[string]any{"ok": false, "error": "The answer is Yes or Not now."})
		return
	}
	release, ok := d.holdAgainstDelete(w, path, pausingRefusal)
	if !ok {
		return
	}
	defer release()
	kind := archindex.Kind(path)
	off := gmailOffAt(path, kind != "" && kind != archindex.KindEncrypted)
	if len(off) == 0 {
		writeJSON(w, map[string]any{"ok": false, "error": gmailNoQuestion})
		return
	}
	j, p, ok := gmailJob(path)
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error": gmailNoJob})
		return
	}
	note := gmailNotNowNote
	if in.Answer == "yes" {
		have := map[string]bool{}
		for _, t := range j.GmailYes {
			have[t] = true
		}
		for _, t := range off {
			if !have[t] {
				j.GmailYes = append(j.GmailYes, t)
			}
		}
		j.GmailNotNow = false
		note = gmailYesNote
	} else {
		j.GmailNotNow = true
	}
	if err := job.Write(p, j); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "MailArchive could not keep your answer: " + err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "note": note})
}
