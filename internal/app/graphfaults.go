package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// The Graph walk's handling of what goes wrong (fault-isolation §4.8, FC4, FC10,
// FC11, FC12). A fault in fetching or exporting ONE message costs that message: it
// is an entry in the capture-gap ledger (never a manifest record, H3), and the walk
// goes on. A fault in listing ONE folder costs that folder. A fault of the mailbox
// itself (a cancel, a lost lock, a refused sign-in, an archive that can no longer be
// written) ends the mailbox, and the run stops where it ends the run.

// parseRFC822 is how the walk parses a message's MIME: source.ParseRFC822, a
// variable so a test can make one parse fail the way a crafted message would.
var parseRFC822 = source.ParseRFC822

// errNotAMessage is what a 200 $value that is no RFC 5322 message becomes: the
// item's fetch-failed fault (S3-AO2).
var errNotAMessage = errors.New("the answer to the download is not an e-mail message (it has no RFC 5322 header)")

// looksLikeMessage says whether data starts like an RFC 5322 message: its first
// line is a header field (a name of printable characters, then a colon). Every
// message Graph converts opens with headers; an HTML page, an empty body or a
// plain sentence does not. It is a check that an answer is a message at all, not
// that it parses.
func looksLikeMessage(data []byte) bool {
	line := data
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	i := bytes.IndexByte(line, ':')
	if i <= 0 {
		return false
	}
	for _, c := range line[:i] {
		if c < '!' || c > '~' {
			return false
		}
	}
	return true
}

// classifyFetch sorts a failed Graph request for one message (§4.8.1). It wraps
// graph.Classify, which both consumers share, with capture's own readings: a
// cancelled walk, a refused sign-in (401, a failed token refresh) and a 403
// (owner ruling OR7: a revoked grant must not become thousands of gaps) are fatal
// for the mailbox and record no gap; every other fault is the item's, with the
// reason and class of the table of §4.1, and transport says whether it is about
// the service rather than the item (what the breaker counts).
func classifyFetch(err error, walk context.Context) (reason, class string, fatal, transport bool) {
	if err == nil {
		return "", "", false, false
	}
	if walk != nil && walk.Err() != nil {
		return "", "", true, false
	}
	// A refused redirect is an answer, not a dropped connection: graph.Classify would
	// read the *url.Error it travels in as the network failing.
	var re *graph.RedirectError
	if errors.As(err, &re) {
		return "fetch-failed", state.ClassFillable, false, false
	}
	c, transport := graph.Classify(err, walk)
	switch c {
	case graph.ClassCancelled, graph.ClassAuth, graph.ClassForbidden:
		return "", "", true, false
	case graph.ClassNotFound:
		return "vanished-at-fetch", state.ClassFillable, false, false
	case graph.ClassTransport:
		return "fetch-transport", state.ClassFillable, false, transport
	case graph.ClassTimeout:
		return "fetch-timeout", state.ClassFillable, false, transport
	case graph.ClassTooLarge:
		return "fetch-too-large", state.ClassTerminal, false, false
	}
	return "fetch-failed", state.ClassFillable, false, false
}

// classifyListing sorts a failed listing request (a folder's message pages): the
// same fatal faults end the mailbox, and any other fault is the folder's own
// folder-list-failed gap (§4.8.4), counted by the breaker when it is about the
// service.
func classifyListing(err error, walk context.Context) (fatal, transport bool) {
	_, _, fatal, transport = classifyFetch(err, walk)
	return fatal, transport
}

// graphLocator is the ledger locator of a Graph message: graph:<id>. When the
// tenant honours the immutable-id preference the id the listing returns IS the
// immutable id, which survives a move and a folder rename, so a moved item
// re-observes its own entry instead of stranding it (§4.8.2, FC8(5), FC14);
// otherwise it is the id the listing gave.
//
// The locator is built through state.BoundLocator, so the key every lookup builds
// (recovery, the known-poison read) is the key the ledger stored the entry under,
// whatever the server's id looks like, and two ids never share an entry (S3-AO9).
func graphLocator(ref graph.MessageRef) string { return state.BoundLocator("graph:" + ref.ID) }

// folderLocator is the ledger locator of a folder-scope gap: folder:<id or path>.
func folderLocator(s string) string { return state.BoundLocator("folder:" + s) }

// runPartial says, before finish has built the run's Result, whether the run
// observed something it could not capture (§4.1): a fillable or terminal gap its
// walk met, or a partly-read record. The history footer asks it, because the
// footer is written ahead of the manifest's last save (crash order, §3.5).
func runPartial(m *state.Manifest, exp *export.Exporter, out string, runAt time.Time) bool {
	return summarizeGaps(m, out, runAt, time.Now().UTC(), 24*time.Hour).Observed > 0 || exp.Stats.Unreadable > 0
}

// parseStub says whether a parse gave up on the message: the constant
// "(unreadable message)" stub safeParseMessage leaves when the MIME stack
// panics, or a message that carries the unparsed label (§4.8.2).
func parseStub(m *model.Message) bool {
	return m.Identity() == model.LegacyStubIdentity() || slices.Contains(m.ReadGaps, state.LabelUnparsed)
}

// stub is the Graph stub rule (§4.8.2, FC5, FC27) for a message whose parse gave
// up. Its identity is a content hash that folds the received time, which the fast
// path never recognises, so without a Message-ID the stub would be downloaded again
// on every run. With the listing's Message-ID the stub's identity is mid:<id> and
// the fast path skips it, but only when the mailbox holds no record for that
// Message-ID or for the item's immutable id: a worse read of a message already
// archived (a Full-mode refresh, a re-issued id) is a degraded capture that never
// replaces it (P15). It is held back: nothing is written, the archived record is
// stamped seen, it adopts no new immutable id, and an info refresh-failed gap says
// so. It reports whether the capture was held back. Where the stub goes on, a
// legacy Graph stub this message left under its old content-hash identity is
// adopted, so the change of identity does not leave it to the sweep.
func (w *mailboxWalk) stub(ref graph.MessageRef, m *model.Message, folderKey string) (held bool) {
	if ref.InternetMessageID == "" {
		return false
	}
	if w.hasRecord(ref) {
		w.faultIdentity(ref, nil)
		w.lr.exp.Stats.Kept++
		w.s.observe(graphItemGap(ref, folderKey, "refresh-failed", state.ClassInfo, "a newer read of this item could not be parsed; the archived copy was kept unchanged"))
		return true
	}
	if n := w.manifest.AdoptLegacyGraphStubs(w.token, ref.PhysID, ref.Received, w.lr.runAt, legacyStubIdentity); n > 0 {
		w.lr.logger.Printf("re-classed %d legacy placeholder(s); this version records each unreadable message separately", n)
	}
	m.InternetMessageID = ref.InternetMessageID
	return false
}

// cleanedError is an error whose text is cleaned (control and bidi characters
// stripped) and whose chain is kept: errors.Is and errors.As still reach the
// typed fault inside. The words of a Graph error are the endpoint's own and go to
// the operator's terminal, the run log and the returned error, so a fault that ends
// a mailbox is cleaned where it is built, as S2A's are (S3-AO7).
type cleanedError struct{ err error }

func (e *cleanedError) Error() string { return cleanText(e.err.Error()) }
func (e *cleanedError) Unwrap() error { return e.err }

// cleanErr is err with clean text: err itself when its text is clean already.
func cleanErr(err error) error {
	if err == nil || cleanText(err.Error()) == err.Error() {
		return err
	}
	return &cleanedError{err}
}

// legacyStubIdentity is the identity an older build gave the Graph stub of a
// message it could not parse, received at t: the content hash of the placeholder
// subject and t (§4.8.2, FC27). The adoption of such a stub matches on it.
func legacyStubIdentity(t time.Time) string {
	return (&model.Message{Subject: state.LegacyPlaceholderSubject, Received: t}).Identity()
}

// walkAbort wraps an error that ends the mailbox's walk, so the walk's own caller
// can tell it from a listing error, which costs only that folder.
type walkAbort struct{ err error }

func (e *walkAbort) Error() string { return e.err.Error() }
func (e *walkAbort) Unwrap() error { return e.err }

// mailboxWalk is one mailbox's walk: what it records of faults and what it
// leaves undecided about gone-detection.
type mailboxWalk struct {
	lr       *localRun
	s        *sourceRun // the mailbox as a source: its token, its writes, its unwritable folders
	manifest *state.Manifest
	client   *graph.Client
	mailbox  string
	token    string

	walkedGap   map[string]bool     // folders whose listing was consumed this run, in the ledger's spelling: the scope of closing a gap
	folderFault bool                // a folder-scope fault: the mailbox's sweep and the closing of gaps are deferred
	spare       state.SweepSpare    // the records an item fault the run could not match to a record keeps out of this run's sweep
	physKeys    map[string][]string // the token's records by immutable id, read on the first fault that needs it

	br       *graphBreaker // this mailbox's counts of faults about the service (§4.8.5)
	folderID string        // the folder being listed: the control request's target
	mark     time.Time     // when the walk last finished with an item, or began the listing: a listing fault's time is what has passed since
}

func newMailboxWalk(lr *localRun, client *graph.Client, manifest *state.Manifest, mailbox, token string, breakerTime time.Duration) *mailboxWalk {
	return &mailboxWalk{
		lr: lr, s: &sourceRun{lr: lr, token: token, path: mailbox}, client: client,
		manifest: manifest, mailbox: mailbox, token: token, walkedGap: map[string]bool{},
		br: newGraphBreaker(breakerTime),
	}
}

// breakerCheck is what happens when a counted fault crossed one of the breaker's
// bounds (fired says which): one control request, unrelated to the failing items,
// decides (§4.8.5). If it fails too, the service or the network is down and the
// mailbox ends with *GraphBreakerError, its gaps kept, no sweep and no closings.
// If it is answered, the failing items are the items' own, the counts start again
// and the walk goes on.
func (w *mailboxWalk) breakerCheck(ctx context.Context, fired string, last error) error {
	if fired == "" {
		return nil
	}
	perr := w.client.ProbeFolder(ctx, w.mailbox, w.folderID)
	if perr == nil {
		w.br.reset()
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &GraphBreakerError{Mailbox: w.mailbox, Threshold: fired, Last: boundText(last.Error()), Out: w.lr.out}
}

// boundText cuts a fault's text for a refusal that carries it.
func boundText(s string) string {
	if r := []rune(s); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

// graphItemGap is the ledger entry of a message that could not be captured: its
// locator is the immutable id when the tenant gave one, and its subject and
// received date come from the listing, so the operator can find it (FC13).
func graphItemGap(ref graph.MessageRef, folderKey, reason, class, detail string) state.CaptureGap {
	g := state.CaptureGap{Folder: folderKey, Scope: state.GapScopeItem, Locator: graphLocator(ref), Reason: reason, Class: class, Subject: ref.Subject, Detail: detail}
	if !ref.Received.IsZero() {
		g.Received = ref.Received.UTC().Format(time.RFC3339)
	}
	return g
}

// observeItem records ref's gap and what the fault does to the mailbox's sweep
// (faultIdentity). m is the parsed message when the fault came after the parse.
func (w *mailboxWalk) observeItem(ref graph.MessageRef, m *model.Message, folderKey, reason, class, detail string) {
	g := graphItemGap(ref, folderKey, reason, class, detail)
	g.DefersGone = w.faultIdentity(ref, m)
	w.s.observe(g)
}

// faultIdentity is what a fault on one item does to this run's gone-detection
// (§4.8.3, FC4, owner ruling OR10): the item was listed but not captured, so
// "LastSeen is stale" would call a record gone that is still there. Every same-token
// record that carries the item's identity is stamped seen: the Message-ID the
// listing gave, the parsed message's identity when the fault came after the parse,
// and, where this run's listing confirmed immutable ids, every record that holds
// the item's immutable id, which survives a move. A fault whose identity matched
// no record at all might be the message of any record the run cannot tell from it,
// so the records it could be are spared this run's sweep instead: every one with no
// immutable id (OR10; the stricter rule, which includes a pre-v6 record whose
// listing and parsed Message-IDs differ), and, where this run's ids are not
// immutable, every content-hash record too. The folder stays swept for the rest.
// It reports whether it deferred anything (the fault matched no record): the gap
// records it, so `status` can say gone-detection waits while the gap persists.
func (w *mailboxWalk) faultIdentity(ref graph.MessageRef, m *model.Message) (deferred bool) {
	matched := false
	stamp := func(identity string) {
		for _, sib := range w.sameToken(identity) {
			w.manifest.TouchSeen(sib.Key, w.lr.runAt)
			matched = true
		}
	}
	if ref.InternetMessageID != "" {
		stamp("mid:" + ref.InternetMessageID)
	}
	if m != nil {
		stamp(m.Identity())
	}
	if ref.PhysID != "" {
		for _, k := range w.physIDKeys()[ref.PhysID] {
			w.manifest.TouchSeen(k, w.lr.runAt)
			matched = true
		}
	}
	if !matched {
		w.spare.NoPhysID = true
		if ref.PhysID == "" {
			// This run's ids are not immutable (or the item has none), so no record can
			// be told from the fault's item by its id: a record that holds an immutable
			// id and whose listing and parsed Message-IDs differ is as likely to be it
			// as a content-hash one (S3-AI1, S3-AO4).
			w.spare.ShaIdentity = true
			w.spare.AllRecords = true
		}
	}
	return !matched
}

// physIDKeys is the token's records by immutable id as they stood at the first fault
// that asked: the faulting items were not exported, so the records they could match
// are all there already.
func (w *mailboxWalk) physIDKeys() map[string][]string {
	if w.physKeys == nil {
		w.physKeys = w.manifest.PhysIDKeys(w.token)
	}
	return w.physKeys
}

// hasRecord says whether this mailbox already holds a record of the item: one for the
// listing's Message-ID, or one that holds its immutable id.
func (w *mailboxWalk) hasRecord(ref graph.MessageRef) bool {
	if ref.InternetMessageID != "" && len(w.sameToken("mid:"+ref.InternetMessageID)) > 0 {
		return true
	}
	return ref.PhysID != "" && len(w.physIDKeys()[ref.PhysID]) > 0
}

// recovered closes the gap an item's capture (or its being archived already)
// answers: nothing is written or deleted, the entry just goes (§4.3 step 2).
func (w *mailboxWalk) recovered(ref graph.MessageRef) {
	if w.manifest.CloseGap(state.GapKey(w.token, state.GapScopeItem, graphLocator(ref))) {
		w.lr.recovered++
	}
}

// fetchFault is what a failed $value costs (§4.8.2): a fatal fault ends the
// mailbox with today's error; any other is the item's own gap and the walk goes
// on. In order:
//  1. a run with -since, a message received before it that has no record: it was
//     not requested, so it is counted (SkippedDate) and records no gap. A gap an
//     earlier run left for it is not touched: it is retained, never closed by this
//     run's absence (FC13);
//  2. in Full mode, a message whose Message-ID is already archived under this
//     mailbox keeps its archived capture: the failed refresh is an info gap;
//  3. otherwise its reason and class from classifyFetch.
func (w *mailboxWalk) fetchFault(ctx context.Context, ref graph.MessageRef, folderKey string, err error, took time.Duration) error {
	reason, class, fatal, transport := classifyFetch(err, ctx)
	if fatal {
		return &walkAbort{cleanErr(fmt.Errorf("fetch message %s: %w", ref.ID, err))}
	}
	// Known poison is read before this fault is recorded, which would stamp the gap.
	kind := attemptItemFault
	if transport && !w.knownPoison(ref, reason) {
		kind = attemptCounted
	}
	if since := w.lr.exp.Since; !since.IsZero() && !ref.Received.IsZero() && ref.Received.Before(since) && !w.hasRecord(ref) {
		w.lr.exp.Stats.SkippedDate++
		w.faultIdentity(ref, nil)
	} else {
		if w.lr.exp.Mode == export.Full && ref.InternetMessageID != "" && len(w.sameToken("mid:"+ref.InternetMessageID)) > 0 {
			reason, class = "refresh-failed", state.ClassInfo
		}
		w.observeItem(ref, nil, folderKey, reason, class, err.Error())
	}
	if berr := w.breakerCheck(ctx, w.br.attempt(kind, took), err); berr != nil {
		return &walkAbort{berr}
	}
	return nil
}

// sameToken is the keys of this mailbox's records that carry identity.
func (w *mailboxWalk) sameToken(identity string) []state.IdentRef {
	var out []state.IdentRef
	prefix := w.token + "\x00"
	for _, sib := range w.manifest.KeysForIdentity(identity) {
		if strings.HasPrefix(sib.Key, prefix) {
			out = append(out, sib)
		}
	}
	return out
}

// listingFault is what a failed listing of one folder costs (§4.8.4): a fatal
// fault ends the mailbox, any other is the folder's gap. The folder is not
// walked, and the mailbox's sweep and closings wait for a run that reads it.
func (w *mailboxWalk) listingFault(ctx context.Context, f graph.Folder, folderKey string, err error) error {
	fatal, transport := classifyListing(err, ctx)
	if fatal {
		return cleanErr(err)
	}
	w.folderFault = true
	w.s.observe(state.CaptureGap{Folder: folderKey, Scope: state.GapScopeFolder, Locator: folderLocator(f.ID),
		Reason: "folder-list-failed", Class: state.ClassFillable, Detail: err.Error()})
	// A listing the service could not answer counts toward the breaker as a fault
	// of the service; one it refused or one the guard stopped is the folder's own.
	if !transport {
		return nil
	}
	return w.breakerCheck(ctx, w.br.attempt(attemptCounted, time.Since(w.mark)), err)
}

// folderRead notes that folder f's listing was consumed: a gap its listing left
// earlier is answered, and the folder is in the scope of closing gaps.
func (w *mailboxWalk) folderRead(f graph.Folder, folderKey string) {
	w.walkedGap[state.GapFolder(folderKey)] = true
	if w.manifest.CloseGapReason(state.GapKey(w.token, state.GapScopeFolder, folderLocator(f.ID)), "folder-list-failed") {
		w.lr.recovered++
	}
}

// covered says whether this run covered a gap: the folder it sits in had its
// listing consumed (§4.3 step 3).
func (w *mailboxWalk) covered(g state.CaptureGap) bool { return w.walkedGap[g.Folder] }

// folderUnwritable records a message that would need a download into a folder
// whose directory could not be created earlier in this run: it is counted under
// the folder's gap and not downloaded (§4.8.2).
//
// defers is what faultIdentity said of the message: the gap records that the sweep
// was deferred because of a message it counted (S3-AI3), and the ledger keeps it
// when any observation of the run says so.
func (w *mailboxWalk) folderUnwritable(folderKey string, detail string, defers bool) {
	w.s.observe(state.CaptureGap{Folder: folderKey, Scope: state.GapScopeFolder, Locator: folderLocator(folderKey),
		Reason: "folder-unwritable", Class: state.ClassFillable, Detail: detail, Skipped: 1, DefersGone: defers})
}

// exportOne exports one parsed message and sorts what goes wrong by whose fault
// it is, as the local run does (§4.6, FC12): a panic is a terminal export-panic
// gap, never only a log line (F33); a drive that is full, read-only or gone ends the
// run; a folder the archive could not create is a folder-unwritable gap; a failed
// write of one message is an export-failed gap; 50 failed writes in a row end the
// mailbox.
func (w *mailboxWalk) exportOne(ref graph.MessageRef, f graph.Folder, folderKey string, m *model.Message, checkpoint func()) (err error) {
	lr, s := w.lr, w.s
	defer func() {
		if r := recover(); r != nil {
			// The folder name and the panic's words can both be the mailbox's own bytes.
			lr.logger.Printf("warning: recovered from panic on a message in %s (skipped): %s", cleanText(folderKey), cleanText(fmt.Sprint(r)))
			lr.exp.Stats.ExportPanics++
			w.observeItem(ref, m, folderKey, "export-panic", state.ClassTerminal, fmt.Sprint(r))
			err = nil
		}
	}()
	written, exportErr := lr.exp.Export(w.token, f.Path, m)
	checkpoint()
	if exportErr == nil {
		if written {
			s.wrote, s.streak = true, 0
			w.recovered(ref)
			// A folder the archive could not write earlier is writable now: its gap is
			// answered, as the first message that lands in it shows.
			if w.manifest.CloseGapReason(state.GapKey(w.token, state.GapScopeFolder, folderLocator(folderKey)), "folder-unwritable") {
				lr.recovered++
			}
		}
		return nil
	}

	tokenDir := filepath.Join(lr.out, w.token)
	if kind := export.FatalKindAt(exportErr, lr.out, tokenDir); kind != export.FatalNone {
		return &walkAbort{runFatalFor(lr.out, kind, exportErr)}
	}
	var fe *export.FolderError
	var ie *export.ItemError
	switch {
	case errors.As(exportErr, &fe):
		if perr := lr.probe(s); perr != nil {
			return &walkAbort{perr}
		}
		if s.unwritable == nil {
			s.unwritable = map[string]bool{}
		}
		s.unwritable[fe.Dir] = true
		lr.exp.Stats.ExportFaults++
		w.folderUnwritable(folderKey, exportErr.Error(), w.faultIdentity(ref, m))
	case errors.As(exportErr, &ie):
		if perr := lr.probe(s); perr != nil {
			return &walkAbort{perr}
		}
		lr.exp.Stats.ExportFaults++
		w.observeItem(ref, m, folderKey, "export-failed", state.ClassFillable, exportErr.Error())
	default:
		return &walkAbort{exportErr}
	}
	s.streak++
	if s.streak >= writeBreaker {
		return &walkAbort{&SourceStoppedError{Err: exportErr, Msg: cleanText(fmt.Sprintf("stopped reading %s after %d writes in a row failed (last: %v) — check that the archive drive is connected and writable, then re-run.", s.path, writeBreaker, exportErr))}}
	}
	return nil
}
