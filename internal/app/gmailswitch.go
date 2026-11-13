package app

// The Gmail switch in a backup run (Q-B; docs/design-gmail-dedup.md §7.4 fold c and its F1
// note; slice B3, wired to slice B2's state.Manifest.GmailDecision). A store is switched on to
// the Gmail rule only by the person's yes, or when the archive is new: an existing archive stays
// exactly as it is (version 6) until then. The run decides for each Thunderbird store that
// B1's thunderbird.GmailForStore calls Gmail, before any of its mail is read, so a yes counts
// from this run's first message of that store.

import (
	"log"

	"mail-archive-tool/internal/state"
)

// GmailOutcome is what a run did about the Gmail switch.
type GmailOutcome struct {
	// Switched are the store tokens this run switched on (a new archive, or the person's yes).
	Switched []string
	// Off are the Gmail stores of this existing archive the run read that are still off, the
	// person not having said yes: the question waits for them (Question).
	Off []string
	// Question is the words to ask, the encrypted sentence added for an encrypted archive.
	Question string
	// NotNow says the person answered Not now: in this run (the terminal's question), or
	// before it, kept on this computer (Options.GmailNotNow).
	NotNow bool
	// Counts are the archive's switched-on Gmail stores' counts at the run's end, for the run
	// summary's line (§8.1): a plaintext archive's only (an encrypted archive's run summary may
	// go to a plain log file, GD-3).
	Counts state.GmailCounts
}

// Waiting reports whether the person should be asked: an existing archive's Gmail store is
// still off and nobody answered Not now in this run.
func (g GmailOutcome) Waiting() bool { return len(g.Off) > 0 && !g.NotNow }

// gmailRun is a run's state for the switch.
type gmailRun struct {
	fresh     bool // the archive held no records when the run began: a new archive
	encrypted bool
	yes       bool            // the person's yes, given before the run (Options.GmailYes)
	yesFor    map[string]bool // the yes for these stores only (Options.GmailYesStores)
	notNow    bool            // the person's Not now, kept on this computer (Options.GmailNotNow)
	ask       func(string) bool
	logger    *log.Logger

	asked    bool
	answered bool // the asked person said yes
	out      GmailOutcome
}

func newGmailRun(m *state.Manifest, opts Options, logger *log.Logger) gmailRun {
	g := gmailRun{fresh: m.Len() == 0, encrypted: opts.Key != nil, yes: opts.GmailYes, notNow: opts.GmailNotNow, ask: opts.AskGmail, logger: logger}
	g.out.NotNow = opts.GmailNotNow // an answer kept on this computer: nothing waits to be asked
	for _, t := range opts.GmailYesStores {
		if g.yesFor == nil {
			g.yesFor = map[string]bool{}
		}
		g.yesFor[t] = true
	}
	return g
}

// store decides the switch for the Gmail store token of the run. GmailDecision is B2's rule;
// "new" is taken at the run's start, so a new archive's later sources are switched on too.
func (g *gmailRun) store(m *state.Manifest, token string) {
	if m.DedupOn(token) {
		return
	}
	switchOn := func(why string) {
		m.SwitchOnGmail(token)
		if m.DedupOn(token) {
			g.out.Switched = append(g.out.Switched, token)
			// Never logged for an encrypted archive: its log is plain text, and nothing about its
			// Gmail accounts is written beside it (§8.1 fold, GD-3).
			if g.logger != nil && !g.encrypted {
				g.logger.Printf("Gmail: %s — from now on each email is stored once and shown in all of its folders", why)
			}
		}
	}
	if g.fresh {
		switchOn("a new archive")
		return
	}
	if g.yes || g.answered || g.yesFor[token] {
		switchOn("you said yes")
		return
	}
	attended := g.ask != nil && !g.asked
	switch m.GmailDecision([]string{token}, g.notNow, attended) {
	case state.GmailSwitchNew:
		switchOn("a new archive")
		return
	case state.GmailAskPerson:
		g.asked = true
		if g.ask(state.GmailSwitchQuestion(g.encrypted)) {
			g.answered = true
			switchOn("you said yes")
			return
		}
		g.out.NotNow = true
	}
	g.out.Off = append(g.out.Off, token)
}

// outcome is the run's GmailOutcome, with the question's words when one waits.
func (g *gmailRun) outcome() GmailOutcome {
	o := g.out
	if len(o.Off) > 0 {
		o.Question = state.GmailSwitchQuestion(g.encrypted)
	}
	return o
}
