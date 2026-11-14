package desktop

// The Gmail question on an archive's card and in Settings (Q-B; docs/design-gmail-dedup.md §7.4
// fold c; slice B3, and the B3 friction review's F1, F2, F3, F5, F6 and F15). A backup that reads
// a Gmail account of an existing archive that is not switched on keeps the account's store folders
// in its last-run record (state.LastRun.GmailOff, or GmailHeld when a Not now holds them back;
// plaintext only); the card then asks, in a box of its own with a heading, beside the archive,
// without blocking anything, and Settings keeps the same question for the current archive, so a
// Not now can be undone there. Both answers are kept on this computer (gmailanswer: the archive's
// job file when it has one, otherwise a small file beside the job files), never in the archive:
// Yes names the stores, and the archive's next backup switches them on; Not now means the card
// does not ask again by itself. A scheduled backup never asks and never switches without the yes.
// An encrypted archive is never asked here (the review's F18, the owner's design call: open).

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/state"
)

// The card's words around the question (for the owner's sign-off).
const (
	gmailYesWaits   = "Gmail: you said yes. From this archive's next backup on, each Gmail email is stored once and shown in all of its folders."
	gmailYesNote    = "Yes. From this archive's next backup on, each Gmail email is stored once and shown in all of its folders."
	gmailNotNowNote = "Not now. Nothing changed, and MailArchive will not ask again on this computer. To say yes later, make this the current archive and answer the same question in Settings."
	gmailNoQuestion = "There is no Gmail question for this archive now. Reload the page."
	// What starts the next backup, after gmailYesWaits (F15): an archive with no schedule never
	// backs up by itself.
	gmailRunNowLead  = "Nothing backs it up by itself: "
	gmailRunNowAct   = "click Run now on the Scheduled backup card to start that backup."
	gmailNoJobLead   = "It has no backup settings on this computer: to back it up from here, "
	gmailNoJobAct    = "choose How often in Settings and click Save settings."
	gmailSwitchFirst = "click Switch to this archive, then "
	// Settings' line after a Not now kept here (F1).
	gmailNotNowKeptLine = "You said Not now on this computer. You can still say yes here."
)

// gmailWords are the question's words for the page (one constant each, in state).
var gmailWords = map[string]string{
	"heading":    state.GmailQuestionHeading,
	"yes":        state.GmailYesButton,
	"notnow":     state.GmailNotNowButton,
	"notnowkept": gmailNotNowKeptLine,
	"setup":      state.GmailSetupLine,
	"needs":      state.GmailNeedsLine,
}

// gmailWord is the template's way to the question's words.
func gmailWord(name string) string { return gmailWords[name] }

// gmailBox is what the page's question box draws (F5): the archive, the question, and whether a
// Not now is kept here (Settings says so beside it).
type gmailBox struct {
	Path, Question string
	NotNowKept     bool
}

func gmailAskBox(path, question string, notNowKept bool) gmailBox {
	return gmailBox{Path: path, Question: question, NotNowKept: notNowKept}
}

// gmailStores is what the last backup of the plaintext archive at out found about its Gmail
// stores: off (waiting for the person), held (off, held back by a Not now) and on. Each a safe
// store name, read as every card line is (bounded, never through a link). None for an encrypted
// archive, a record of an encrypted one, or a backup that did not end ok.
type gmailStores struct{ off, held, on []string }

func gmailStoresAt(out string, plaintext bool) gmailStores {
	var g gmailStores
	if !plaintext || encmark.MustMinimise(out) {
		return g
	}
	lr, st, _ := state.ReadLastRun(out)
	if st != state.LastRunPresent || lr.Status != state.RunOK || lr.Encrypted {
		return g
	}
	safe := func(in []string) []string {
		var keep []string
		for _, t := range in {
			if state.SafeToken(t) {
				keep = append(keep, t)
			}
		}
		return keep
	}
	return gmailStores{off: safe(lr.GmailOff), held: safe(lr.GmailHeld), on: safe(lr.GmailOn)}
}

// anyOff is every store still off, waiting or held.
func (g gmailStores) anyOff() []string {
	all := append([]string(nil), g.off...)
	for _, t := range g.held {
		if !slices.Contains(all, t) {
			all = append(all, t)
		}
	}
	return all
}

// gmailOffAt is every Gmail store the last backup of the plaintext archive at out found off.
func gmailOffAt(out string, plaintext bool) []string { return gmailStoresAt(out, plaintext).anyOff() }

// gmailUnanswered says whether the stores still need a yes: some store is not named by the yes
// kept here.
func gmailUnanswered(off []string, kept gmailanswer.Answer) bool {
	for _, t := range off {
		if !slices.Contains(kept.Yes, t) {
			return true
		}
	}
	return false
}

// gmailYesWaitsFor is the line that says a yes waits for the archive's next backup, with what
// starts that backup when nothing starts it by itself (F15).
func gmailYesWaitsFor(out string, current bool) string {
	first := ""
	if !current {
		first = gmailSwitchFirst
	}
	if d, err := schedule.ReadDescriptor(out); err == nil && !d.IsManual() {
		return gmailYesWaits
	}
	if p, err := job.PathFor(schedule.DefaultNameFor(out)); err == nil {
		if _, err := job.Read(p); err == nil {
			return gmailYesWaits + " " + gmailRunNowLead + first + gmailRunNowAct
		}
	}
	return gmailYesWaits + " " + gmailNoJobLead + first + gmailNoJobAct
}

// gmailCard is the card's question for the archive at out, or the line saying a yes waits for
// the next backup; both "" when nothing is to be said: no Gmail store off, or Not now kept here.
func gmailCard(out string, plaintext, current bool) (question, yesWaits string) {
	off := gmailOffAt(out, plaintext)
	if len(off) == 0 {
		return "", ""
	}
	kept := gmailanswer.Read(out)
	if kept.NotNow {
		return "", ""
	}
	if gmailUnanswered(off, kept) {
		return state.GmailSwitchQuestion(false), ""
	}
	return "", gmailYesWaitsFor(out, current)
}

// gmailSettings is Settings' Gmail lines for the current archive (F1, F6).
type gmailSettings struct {
	Question   string // the question, also after a Not now kept here, so it can be undone
	NotNowKept bool   // the person said Not now on this computer
	YesWaits   string // a yes waits for the next backup
	On         bool   // a Gmail store of the archive is switched on
	Path       string
}

// gmailSettingsFor works out Settings' Gmail lines for the plaintext archive at out.
func gmailSettingsFor(out string, plaintext bool) gmailSettings {
	g := gmailStoresAt(out, plaintext)
	s := gmailSettings{On: len(g.on) > 0, Path: out}
	off := g.anyOff()
	if len(off) == 0 {
		return s
	}
	kept := gmailanswer.Read(out)
	if gmailUnanswered(off, kept) {
		s.Question, s.NotNowKept = state.GmailSwitchQuestion(false), kept.NotNow
		return s
	}
	s.YesWaits = gmailYesWaitsFor(out, true)
	return s
}

// gmailAnswer is the card's and Settings' Yes or Not now: a loopback-only, CSRF-tokened POST of
// {"path": …, "answer": "yes" | "not-now"}. The stores are read again from the archive's last-run
// record here, never taken from the page. The answer is kept on this computer whether or not the
// archive has backup settings here (F2).
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
	kept := gmailanswer.Read(path)
	note := gmailNotNowNote
	if in.Answer == "yes" {
		for _, t := range off {
			if !slices.Contains(kept.Yes, t) {
				kept.Yes = append(kept.Yes, t)
			}
		}
		kept.NotNow = false
		note = gmailYesNote
	} else {
		kept.NotNow = true
	}
	if err := gmailanswer.Keep(path, kept); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "MailArchive could not keep your answer: " + err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "note": note})
}
