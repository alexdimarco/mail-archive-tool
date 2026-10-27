package desktop

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
)

// Forget this encrypted archive here (design-encrypted-archives-phase3 §4.6; the P3-3
// plan, 3e), from its row in Your archives: what forgetting would remove is shown
// first and kept under a token; the forget itself refuses if the folder or this
// computer's record changed since. A folder that may have been tampered with is
// forgotten only with its path typed back. The archive is locked here first.

// forgetRequest is what a row's Forget sends.
type forgetRequest struct {
	Path  string `json:"path"`
	Token string `json:"token"`
	Typed string `json:"typed"`
	Whole bool   `json:"whole"`
	Keys  bool   `json:"keys"`
}

func readForgetRequest(w http.ResponseWriter, r *http.Request) (forgetRequest, bool) {
	var in forgetRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSecretBody)).Decode(&in); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "That request was not understood — reload the page and try again."})
		return in, false
	}
	return in, true
}

// forgetPlanAction shows what forgetting the row's folder would remove.
func (d *dashboard) forgetPlanAction(w http.ResponseWriter, r *http.Request) {
	in, ok := readForgetRequest(w, r)
	if !ok {
		return
	}
	abs, key := keyOf(strings.TrimSpace(in.Path))
	p, err := keyring.PlanForget(abs, d.cfg.Store)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// The folder is shown as its row names it, not in the record's case-folded form,
	// since the person may be asked to type it back (P3-8 friction review, B27); the
	// record matches either.
	lines := []string{fmt.Sprintf("This computer recorded an encrypted archive at %s.", p.Out)}
	for _, o := range p.Others {
		lines = append(lines, fmt.Sprintf("Its other folder %s (recorded %s) stays protected.", o.Spelling, o.Recorded.Local().Format("2006-01-02")))
	}
	lines = append(lines, "Another copy of this archive that used this same folder path, such as another drive mounted at the same place, loses this computer's protection at this path too.")
	if p.Strong {
		lines = append(lines, "This folder may have been tampered with: it no longer holds the archive this computer made there.")
	}
	token, ok := d.keep(w, &pendingItem{key: key, out: abs, plan: &p})
	if !ok {
		return
	}
	writeJSON(w, map[string]any{"ok": true, "token": token, "lines": lines, "strong": p.Strong, "spelling": p.Spelling, "last": p.Last, "remembered": p.Remembered})
}

// forgetAllWord is what the person types to forget every encrypted archive here.
const forgetAllWord = "FORGET"

// forgetAllAction forgets every encrypted archive this computer recorded, and every
// key it remembers for them (§4.6, K8), beside uninstall, which keeps them: the
// archives are not touched, but one whose password and recovery key are gone can
// never be opened again — so it is done only with the word typed. Every archive open
// here is locked first.
func (d *dashboard) forgetAllAction(w http.ResponseWriter, r *http.Request) {
	in, ok := readForgetRequest(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(in.Typed) != forgetAllWord {
		writeJSON(w, map[string]any{"ok": false, "error": "Nothing was forgotten: type " + forgetAllWord + " to go ahead."})
		return
	}
	// Every archive stays closed for the whole forget — no remembered key re-opens one
	// meanwhile (CRIT-02) — and every key change prepared here goes first: a key file
	// put back afterwards would record a forgotten archive again unasked (CRIT-01).
	release := d.cfg.Keys.hush()
	defer release()
	d.pending.discardFor("")
	n, err := keyring.ForgetAll(d.cfg.Store)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "lines": []string{fmt.Sprintf("This computer forgot %d encrypted archive(s) and the keys it remembered for them. The archives themselves were not changed.", n)}})
}

// forgetAction carries out the forget the person confirmed.
func (d *dashboard) forgetAction(w http.ResponseWriter, r *http.Request) {
	in, ok := readForgetRequest(w, r)
	if !ok {
		return
	}
	it := d.pending.take(in.Token, isForgetPlan)
	if it == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "That forget is no longer waiting — click Forget again."})
		return
	}
	p := *it.plan
	if p.Strong && !encrecord.Names(strings.TrimSpace(in.Typed), p.Spelling) {
		writeJSON(w, map[string]any{"ok": false, "error": "The path typed does not match; nothing was changed."})
		return
	}
	o := keyring.ForgetOptions{Whole: in.Whole && p.Last}
	if o.Whole && in.Keys && p.Remembered {
		o.Keys, o.Store = true, d.cfg.Store
	}
	d.cfg.Keys.lock(p.Out) // locked here first (§4.6)
	if err := keyring.Forget(p, o); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	done := fmt.Sprintf("This computer no longer protects %s as an encrypted archive.", p.Out)
	if o.Whole {
		done = "This computer forgot the encrypted archive."
	}
	writeJSON(w, map[string]any{"ok": true, "lines": []string{done}})
}
