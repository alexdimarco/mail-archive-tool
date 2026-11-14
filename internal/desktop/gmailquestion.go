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
// An encrypted archive is asked too (the owner's ruling on F18, 2026-11-13), only while it is
// unlocked here and after its Run now found a Gmail store off, with the encrypted add-on; its
// answer is kept in memory on the open archive only, never in a file (O1), and a Lock forgets it.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/state"
)

// The card's words around the question (the question and its buttons are the owner's approved
// words, in state; these notes around them were not in that sign-off).
const (
	gmailYesWaits   = "Gmail: you said yes. From this archive's next backup on, each Gmail email is stored once and shown in all of its folders."
	gmailNotNowNote = "Not now. Nothing changed, and MailArchive will not ask again on this computer. To say yes later, make this the current archive and answer the same question in Settings."
	// gmailNotNowSettingsNote is Not now given in Settings, where the person already is (U6).
	gmailNotNowSettingsNote = "Not now. Nothing changed. You can say yes here any time."
	// After a Not now, Settings shows one short line with a button that opens the question (U10).
	gmailCollapsedLine   = "Gmail folders: not switched on."
	gmailCollapsedButton = "Turn on…"
	gmailNoQuestion      = "There is no Gmail question for this archive now. Reload the page."
	// gmailQuestionChanged refuses a Yes from a page drawn for other Gmail accounts than the ones
	// off now (round 3's H5): reloaded, the page asks about them all.
	gmailQuestionChanged = "This archive's Gmail question changed since this page was drawn. Reload the page."
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

// The encrypted archive's words (the owner's ruling on F18): its answer is kept in memory only.
const (
	gmailYesWaitsEncrypted   = "Gmail: you said yes. This archive's next Run now, while it stays unlocked here, stores each Gmail email once and shows it in all of its folders. If you lock it first, MailArchive asks again after its next Run now."
	gmailNotNowEncryptedNote = "Not now. Nothing changed. This computer keeps no answer for an encrypted archive, so MailArchive asks again after you lock it and click Run now."
	gmailNoQuestionLocked    = "Unlock this encrypted archive and click Run now: its Gmail question is asked only while it is unlocked here."
)

// keepGmailOutcome keeps an encrypted Run now's Gmail outcome on the open archive's entry, in
// memory only (F18, O1): the stores it found off ask the question; a store it switched on spends
// the yes. Nothing is written anywhere.
func (d *dashboard) keepGmailOutcome(out string, g app.GmailOutcome) {
	if d.cfg.Keys == nil {
		return
	}
	e := d.cfg.Keys.get(out)
	if e == nil {
		return
	}
	lastSyncMu.Lock()
	defer lastSyncMu.Unlock()
	e.gmailOff = nil
	if g.Waiting() {
		e.gmailOff = append([]string(nil), g.Off...)
	}
	// A store the run switched on spends the yes for it; the others still wait (G6).
	e.gmailYes = slices.DeleteFunc(e.gmailYes, func(t string) bool { return slices.Contains(g.Switched, t) })
}

// gmailYesForRunNow says the open encrypted archive at out has a yes waiting for its Run now.
func (d *dashboard) gmailYesForRunNow(out string) bool {
	return len(d.gmailYesStoresForRunNow(out)) > 0
}

// gmailYesStoresForRunNow is the yes waiting for the open encrypted archive at out's Run now: the
// stores its question named (G6); none when none waits.
func (d *dashboard) gmailYesStoresForRunNow(out string) []string {
	if d.cfg.Keys == nil {
		return nil
	}
	e := d.cfg.Keys.get(out)
	if e == nil {
		return nil
	}
	lastSyncMu.Lock()
	defer lastSyncMu.Unlock()
	return append([]string(nil), e.gmailYes...)
}

func (e *keyEntry) gmailYesWaits() bool {
	lastSyncMu.Lock()
	defer lastSyncMu.Unlock()
	return len(e.gmailYes) > 0
}

// encryptedGmailCard is the row's question, or its yes line, for an encrypted archive open in
// this Desktop: asked when its last Run now here found a Gmail store off and nobody answered.
func (cfg Config) encryptedGmailCard(out string) (question, yesWaits string) {
	if cfg.Keys == nil {
		return "", ""
	}
	e := cfg.Keys.get(out)
	if e == nil {
		return "", ""
	}
	lastSyncMu.Lock()
	defer lastSyncMu.Unlock()
	switch {
	case len(e.gmailYes) > 0:
		return "", gmailYesWaitsEncrypted
	case len(e.gmailOff) > 0 && !e.gmailNotNow:
		return state.GmailSwitchQuestion(true), ""
	}
	return "", ""
}

// gmailAnswerEncrypted keeps the answer for an encrypted archive open here, in memory only. A Yes
// covers the stores the page asked about (asked, their fingerprint) only (H5).
func (d *dashboard) gmailAnswerEncrypted(w http.ResponseWriter, path, answer, asked string) {
	var e *keyEntry
	if d.cfg.Keys != nil {
		e = d.cfg.Keys.get(path)
	}
	if e == nil {
		writeJSON(w, map[string]any{"ok": false, "error": gmailNoQuestionLocked})
		return
	}
	lastSyncMu.Lock()
	if len(e.gmailOff) == 0 {
		lastSyncMu.Unlock()
		writeJSON(w, map[string]any{"ok": false, "error": gmailNoQuestion})
		return
	}
	if answer == "yes" && strings.TrimSpace(asked) != gmailAsked(e.gmailOff) {
		lastSyncMu.Unlock()
		writeJSON(w, map[string]any{"ok": false, "error": gmailQuestionChanged})
		return
	}
	note := gmailNotNowEncryptedNote
	if answer == "yes" {
		// For the stores the question named (G6).
		e.gmailYes, e.gmailNotNow, note = append([]string(nil), e.gmailOff...), false, gmailYesWaitsEncrypted
	} else {
		e.gmailYes, e.gmailNotNow = nil, true
	}
	lastSyncMu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "note": note})
}

// gmailWords are the question's words for the page (one constant each, in state).
var gmailWords = map[string]string{
	"heading":    state.GmailQuestionHeading,
	"yes":        state.GmailYesButton,
	"notnow":     state.GmailNotNowButton,
	"notnowkept": gmailNotNowKeptLine,
	"setup":      state.GmailSetupLine,
	"needs":      state.GmailNeedsLine,
	"collapsed":  gmailCollapsedLine,
	"open":       gmailCollapsedButton,
}

// gmailWord is the template's way to the question's words.
func gmailWord(name string) string { return gmailWords[name] }

// gmailBox is what the page's question box draws (F5): the archive, the question, whether a
// Not now is kept here (Settings says so beside it), and whether it is Settings' copy (its Not
// now note says so, U6; after a Not now it is drawn shut behind one line, U10).
// Archive is the id of the archive the question was read for (a plaintext archive's; "" for an
// encrypted one): the page sends it with the answer (G5). Asked is the fingerprint of the Gmail
// stores it was drawn for (gmailAsked), which the page sends too (round 3's H5).
type gmailBox struct {
	Path, Archive, Asked, Question string
	NotNowKept, Settings           bool
}

func gmailAskBox(path, archive, asked, question string, notNowKept, settings bool) gmailBox {
	return gmailBox{Path: path, Archive: archive, Asked: asked, Question: question, NotNowKept: notNowKept, Settings: settings}
}

// gmailAsked is the fingerprint of the Gmail stores a question is drawn for (the B3 adversarial
// pass, round 3's H5): the page carries it beside the archive's id and sends it with a Yes, which
// then covers those stores only; when the stores off now are others (an account added to
// Thunderbird since the page was drawn), the Yes is refused and the page is told to reload. A
// digest of the set, so the page names no store (an encrypted archive's stores included).
func gmailAsked(stores []string) string {
	set := slices.Clone(stores)
	slices.Sort(set)
	sum := sha256.Sum256([]byte(strings.Join(slices.Compact(set), "\n")))
	return hex.EncodeToString(sum[:12])
}

// encryptedGmailAsked is the fingerprint of the stores the open encrypted archive at out's question
// is drawn for (its last Run now's, kept in memory); "" when none is kept.
func (cfg Config) encryptedGmailAsked(out string) string {
	if cfg.Keys == nil {
		return ""
	}
	e := cfg.Keys.get(out)
	if e == nil {
		return ""
	}
	lastSyncMu.Lock()
	defer lastSyncMu.Unlock()
	if len(e.gmailOff) == 0 {
		return ""
	}
	return gmailAsked(e.gmailOff)
}

// gmailStores is what the last backup of the plaintext archive at out found about its Gmail
// stores: off (waiting for the person), held (off, held back by a Not now) and on. Each a safe
// store name, read as every card line is (bounded, never through a link). None for an encrypted
// archive, a record of an encrypted one, or a backup that did not end ok. archive is the id that
// record carries: an answer is kept for that archive only (the B3 adversarial pass's A1).
type gmailStores struct {
	off, held, on []string
	archive       string
}

func gmailStoresAt(out string, plaintext bool) gmailStores {
	var g gmailStores
	if !plaintext || encmark.MustMinimise(out) {
		return g
	}
	lr, st, _ := state.ReadLastRun(out)
	if st != state.LastRunPresent || lr.Status != state.RunOK || lr.Encrypted || lr.Archive == "" {
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
	return gmailStores{off: safe(lr.GmailOff), held: safe(lr.GmailHeld), on: safe(lr.GmailOn), archive: lr.Archive}
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
	Archive    string // the id of the archive the question was read for (G5)
	Asked      string // the fingerprint of the stores it was drawn for (H5)
}

// gmailSettingsFor works out Settings' Gmail lines for the plaintext archive at out.
func gmailSettingsFor(out string, plaintext bool) gmailSettings {
	g := gmailStoresAt(out, plaintext)
	s := gmailSettings{On: len(g.on) > 0, Path: out, Archive: g.archive}
	off := g.anyOff()
	if len(off) == 0 {
		return s
	}
	kept := gmailanswer.Read(out)
	if gmailUnanswered(off, kept) {
		s.Question, s.NotNowKept, s.Asked = state.GmailSwitchQuestion(false), kept.NotNow, gmailAsked(off)
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
	var in struct{ Path, Answer, From, Archive, Asked string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	path := strings.TrimSpace(in.Path)
	if path == "" || (in.Answer != "yes" && in.Answer != "not-now") {
		writeJSON(w, map[string]any{"ok": false, "error": "The answer is Yes or Not now."})
		return
	}
	// The settings hold, as Remove from list takes (the B3 adversarial pass's A6): refused while
	// an uninstall removes the settings this writes, or a delete of the folder runs; and while it
	// runs, neither an uninstall's Remove nor a delete of the folder begins.
	release, why := d.running.holdSettings(path, answeringRefusal)
	if why != "" {
		writeJSON(w, map[string]any{"ok": false, "error": why})
		return
	}
	defer release()
	// A delete of it that did not finish: no answer is kept for a half-deleted folder (A6, as
	// Pause and Resume refuse, MA-1015).
	if why := d.unfinishedActionRefusal(path, "your answer was not kept"); why != "" {
		writeJSON(w, map[string]any{"ok": false, "error": why})
		return
	}
	// One writer of the archive's job file at a time in this Desktop: Save settings reads, then
	// writes it, and so does Keep (A6).
	jobFileMu.Lock()
	defer jobFileMu.Unlock()
	kind := archindex.Kind(path)
	if kind == archindex.KindEncrypted {
		d.gmailAnswerEncrypted(w, path, in.Answer, in.Asked) // memory only (F18)
		return
	}
	g := gmailStoresAt(path, kind != "" && kind != archindex.KindEncrypted)
	off := g.anyOff()
	// The answer is for the archive the page drew the question for (the B3 adversarial pass, round
	// 2's G5): a page left open while the folder came to hold another archive (a drive of a rotation
	// swapped at the same letter) is told to reload, and nothing is kept for the archive there now.
	if len(off) == 0 || strings.TrimSpace(in.Archive) != g.archive {
		writeJSON(w, map[string]any{"ok": false, "error": gmailNoQuestion})
		return
	}
	// A Yes is for the Gmail stores the page asked about (round 3's H5): one drawn before a later
	// backup found another store off is told to reload, so the person is asked about them all.
	// Not now keeps no yes, so it needs no such check.
	if in.Answer == "yes" && strings.TrimSpace(in.Asked) != gmailAsked(off) {
		writeJSON(w, map[string]any{"ok": false, "error": gmailQuestionChanged})
		return
	}
	// The answer is for the archive whose record the question was read from (A1): what this
	// computer kept for another archive in the folder is not carried into it.
	kept := gmailanswer.Read(path)
	if kept.Archive != g.archive {
		kept = gmailanswer.Answer{}
	}
	kept.Archive = g.archive
	note := gmailNotNowNote
	if in.From == "settings" {
		note = gmailNotNowSettingsNote // the person is in Settings already (the B3 UX walk's U6)
	}
	if in.Answer == "yes" {
		for _, t := range off {
			if !slices.Contains(kept.Yes, t) {
				kept.Yes = append(kept.Yes, t)
			}
		}
		kept.NotNow = false
		// What starts the backup that applies it, said now, as the row says after a reload (the
		// B3 UX walk's U1).
		current := d.cfg.effectiveOut() != "" && cleanPath(d.cfg.effectiveOut()) == cleanPath(path)
		note = gmailYesWaitsFor(path, current)
	} else {
		// Not now takes back a yes still waiting for this archive (G2): the note says "Nothing
		// changed", and the next backup then switches nothing.
		kept.NotNow, kept.Yes = true, nil
	}
	if err := gmailanswer.Keep(path, kept); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "MailArchive could not keep your answer: " + err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "note": note})
}
