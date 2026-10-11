package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
func graphLocator(ref graph.MessageRef) string { return "graph:" + ref.ID }

// runPartial says, before finish has built the run's Result, whether the run
// observed something it could not capture (§4.1): a fillable or terminal gap its
// walk met, or a partly-read record. The history footer asks it, because the
// footer is written ahead of the manifest's last save (crash order, §3.5).
func runPartial(m *state.Manifest, exp *export.Exporter, out string, runAt time.Time) bool {
	return summarizeGaps(m, out, runAt, time.Now().UTC(), 24*time.Hour).Observed > 0 || exp.Stats.Unreadable > 0
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
	mailbox  string
	token    string

	walkedGap   map[string]bool // folders whose listing was consumed this run, in the ledger's spelling: the scope of closing a gap
	folderFault bool            // a folder-scope fault: the mailbox's sweep and the closing of gaps are deferred
	itemFault   bool            // an item fault: the mailbox's sweep is deferred
}

func newMailboxWalk(lr *localRun, manifest *state.Manifest, mailbox, token string) *mailboxWalk {
	return &mailboxWalk{
		lr: lr, s: &sourceRun{lr: lr, token: token, path: mailbox},
		manifest: manifest, mailbox: mailbox, token: token, walkedGap: map[string]bool{},
	}
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

// observeItem records ref's gap and defers the mailbox's sweep.
func (w *mailboxWalk) observeItem(ref graph.MessageRef, folderKey, reason, class, detail string) {
	w.itemFault = true
	w.s.observe(graphItemGap(ref, folderKey, reason, class, detail))
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
// on. In Full mode a message whose Message-ID is already archived under this
// mailbox keeps its archived capture: the failed refresh is an info gap.
func (w *mailboxWalk) fetchFault(ctx context.Context, ref graph.MessageRef, folderKey string, err error) error {
	reason, class, fatal, _ := classifyFetch(err, ctx)
	if fatal {
		return &walkAbort{fmt.Errorf("fetch message %s: %w", ref.ID, err)}
	}
	if w.lr.exp.Mode == export.Full && ref.InternetMessageID != "" && len(w.sameToken("mid:"+ref.InternetMessageID)) > 0 {
		reason, class = "refresh-failed", state.ClassInfo
	}
	w.observeItem(ref, folderKey, reason, class, err.Error())
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
	if fatal, _ := classifyListing(err, ctx); fatal {
		return err
	}
	w.folderFault = true
	w.s.observe(state.CaptureGap{Folder: folderKey, Scope: state.GapScopeFolder, Locator: "folder:" + f.ID,
		Reason: "folder-list-failed", Class: state.ClassFillable, Detail: err.Error()})
	return nil
}

// folderRead notes that folder f's listing was consumed: a gap its listing left
// earlier is answered, and the folder is in the scope of closing gaps.
func (w *mailboxWalk) folderRead(f graph.Folder, folderKey string) {
	w.walkedGap[state.GapFolder(folderKey)] = true
	if w.manifest.CloseGapReason(state.GapKey(w.token, state.GapScopeFolder, "folder:"+f.ID), "folder-list-failed") {
		w.lr.recovered++
	}
}

// covered says whether this run covered a gap: the folder it sits in had its
// listing consumed (§4.3 step 3).
func (w *mailboxWalk) covered(g state.CaptureGap) bool { return w.walkedGap[g.Folder] }

// folderUnwritable records a message that would need a download into a folder
// whose directory could not be created earlier in this run: it is counted under
// the folder's gap and not downloaded (§4.8.2).
func (w *mailboxWalk) folderUnwritable(folderKey string, detail string) {
	w.itemFault = true
	w.s.observe(state.CaptureGap{Folder: folderKey, Scope: state.GapScopeFolder, Locator: "folder:" + folderKey,
		Reason: "folder-unwritable", Class: state.ClassFillable, Detail: detail, Skipped: 1})
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
			w.observeItem(ref, folderKey, "export-panic", state.ClassTerminal, fmt.Sprint(r))
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
			if w.manifest.CloseGapReason(state.GapKey(w.token, state.GapScopeFolder, "folder:"+folderKey), "folder-unwritable") {
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
		w.folderUnwritable(folderKey, exportErr.Error())
	case errors.As(exportErr, &ie):
		if perr := lr.probe(s); perr != nil {
			return &walkAbort{perr}
		}
		lr.exp.Stats.ExportFaults++
		w.observeItem(ref, folderKey, "export-failed", state.ClassFillable, exportErr.Error())
	default:
		return &walkAbort{exportErr}
	}
	s.streak++
	if s.streak >= writeBreaker {
		return &walkAbort{&SourceStoppedError{Err: exportErr, Msg: cleanText(fmt.Sprintf("stopped reading %s after %d writes in a row failed (last: %v) — check that the archive drive is connected and writable, then re-run.", s.path, writeBreaker, exportErr))}}
	}
	return nil
}
