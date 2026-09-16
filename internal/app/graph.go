package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// GraphOptions configures a Microsoft Graph app-only archive run. BaseURL and
// TokenURL are test overrides (empty = Microsoft production endpoints).
type GraphOptions struct {
	Tenant       string
	ClientID     string
	ClientSecret string
	Mailboxes    []string
	BaseURL      string
	TokenURL     string

	// Deleted Items and Junk Email are excluded from the walk by default (T7,
	// operator ruling); these opt them back in. Exclusion is by resolved
	// well-known-folder id, so it is locale-independent (§3.6).
	IncludeDeleted bool
	IncludeJunk    bool
}

// RunGraph archives the given mailboxes server-side via Microsoft Graph (app-only
// Mail.Read). It reuses the whole export/index/pages/manifest pipeline: each
// message's raw MIME is fetched and parsed exactly as a local store's would be.
// Incremental runs skip messages already recorded in the manifest by their
// Internet-Message-ID, without downloading the body — so re-runs over a large
// mailbox are cheap.
func RunGraph(ctx context.Context, g GraphOptions, opts Options, logger *log.Logger) (result Result, err error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	if len(g.Mailboxes) == 0 {
		return Result{}, errors.New("no mailboxes specified (-mailbox is required)")
	}
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output dir: %w", err)
	}
	lock, err := lockfile.AcquireAs(filepath.Join(opts.Out, lockfile.Name), "graph")
	if err != nil {
		return Result{}, err
	}
	defer lock.Release()
	defer recordRun(opts, beginRun(opts), &result, &err)

	mpath := opts.Manifest
	if mpath == "" {
		mpath = filepath.Join(opts.Out, ".mailarchive-manifest.json")
	}
	manifest, err := state.Load(mpath)
	if err != nil {
		return Result{}, err
	}

	exp := &export.Exporter{
		OutDir:   opts.Out,
		Manifest: manifest,
		Mode:     opts.Mode,
		Since:    opts.Since,
		Log:      logger,
		KeepRaw:  opts.KeepRaw,
		// One GET returns the whole MIME: a gap can never be filled by
		// re-fetching, so it is recorded terminal and never retried (R17 keeps
		// its no-re-download guarantee).
		SourceComplete: true,
		// The live/repeat path stores one copy per mailbox keyed by identity, so a
		// message that moves between folders is one record, not a second archived
		// copy (R3 reworded, §3.1/§3.6). Set ONLY here — a one-shot local import
		// keeps the folder-scoped key.
		DedupMailboxWide: true,
	}

	// The go-back timeline: an append-only history log records this run's changes
	// (newly-seen / moved / gone), fsync'd before the manifest advances (§3.5).
	// A log that cannot be opened degrades the timeline but must not fail the run.
	runAt := time.Now().UTC()
	var hist *state.HistoryWriter
	if hw, herr := state.OpenHistory(filepath.Join(opts.Out, state.HistoryName)); herr != nil {
		logger.Printf("warning: %v (this run's go-back timeline will be incomplete)", herr)
	} else {
		hist = hw
		defer hist.Close()
		if werr := hist.WriteRunHeader(runAt.UnixNano(), runAt, g.Mailboxes); werr != nil {
			logger.Printf("warning: history run header: %v", werr)
		}
	}

	// One-time upgrade logs BEFORE the index repair, so cause (the manifest
	// re-scope) precedes effect (the index-key migration) — matching reindex and
	// the README sample (friction #17).
	if manifest.StoresMigrated > 0 {
		logger.Printf("canonicalized %d store path%s (one-time upgrade)",
			manifest.StoresMigrated, plural(manifest.StoresMigrated, "", "s"))
	}
	if manifest.Rekeyed > 0 {
		logger.Printf("re-scoped %d manifest entr%s by store (one-time upgrade; cost scales with archive size)",
			manifest.Rekeyed, plural(manifest.Rekeyed, "y", "ies"))
	}

	var idx *index.Index
	var indexErrors int
	if opts.Index {
		idxPath := filepath.Join(opts.Out, "search.db")
		idx, err = index.Open(idxPath)
		if err != nil {
			return Result{}, fmt.Errorf("open search index: %w", err)
		}
		defer idx.Close()
		if _, rkErr := idx.RepairKeys(manifest.Rekeyed > 0, state.MigrateKey, logger); rkErr != nil {
			return Result{}, fmt.Errorf("migrate search index keys: %w", rkErr)
		}
	}
	// One-time v3→v5 identity-collapse (option D, design rev-4 §4), BEFORE the walk
	// so the walk's Message-ID membership skip finds the collapsed LiveKeys. For
	// each mailbox we are about to archive that still owes it, unify every v3
	// folder-scoped move-duplicate into one mailbox-wide record and re-key the
	// search index to match. Scoped to this run's tokens and skipped once a token
	// is marked (EC4); the index re-key is durable BEFORE the manifest is saved,
	// and the whole thing is idempotent, so a crash mid-migration re-runs cleanly.
	var collapseTokens []string
	for _, mbx := range g.Mailboxes {
		if tok := manifest.Token(state.MailboxSourceID(mbx), mbx); manifest.OwesCollapse(tok) {
			collapseTokens = append(collapseTokens, tok)
		}
	}
	// If a search index EXISTS on disk but is not open this run (-index=false), a
	// collapse would re-key the manifest to LiveKeys while leaving the index at its
	// v3 folder-scoped keys — a permanent desync (a later reindex redaction would
	// miss the stale search rows, and search would keep the un-collapsed duplicate).
	// Defer the collapse (leave the token owing it) to a run that can re-key the
	// index in step.
	if len(collapseTokens) > 0 && idx == nil {
		if _, statErr := os.Stat(filepath.Join(opts.Out, "search.db")); statErr == nil {
			logger.Printf("deferring the one-time v3→v5 identity-collapse: a search index exists but is not open this run — re-run with -index so the manifest and index are re-keyed together")
			collapseTokens = nil
		}
	}
	if len(collapseTokens) > 0 {
		// Decide whether two same-(token,identity) records are one move-duplicate
		// (merge) or two distinct reuses (keep both). The rendered .html is NOT a
		// valid discriminator — the same message rendered in two folders differs
		// (folder breadcrumb, depth-dependent relative paths, key-derived attachment
		// names), so comparing it would wrongly split genuine move-duplicates (R3).
		// Compare the PRESERVED .eml instead — the raw wire bytes, identical for one
		// message wherever it is filed. When a -raw .eml is absent for either copy
		// we cannot content-compare, so we MERGE (the common move-duplicate case);
		// the residual — two distinct same-envelope/different-body reuses in a
		// non-raw archive — is rare and closed by the deferred ImmutableId closure.
		sameContent := func(relA, relB string) bool { return sameArchivedEML(opts.Out, relA, relB) }
		losses, remap := manifest.CollapseByIdentity(sameContent, collapseTokens...)
		if len(remap) > 0 || len(losses) > 0 {
			if idx != nil {
				drop := make([]string, len(losses))
				for i, l := range losses {
					drop[i] = l.LoserKey
				}
				if rkErr := idx.Rekey(remap, drop); rkErr != nil {
					return Result{}, fmt.Errorf("re-key search index for identity collapse: %w", rkErr)
				}
			}
			logger.Printf("collapsed %d move-duplicate(s) and re-keyed %d record(s) to the mailbox-wide identity (one-time v3→v5 upgrade)", len(losses), len(remap))
		}
		manifest.MarkCollapsed(collapseTokens...)
		// Persist the collapsed manifest + marker now, AFTER the index re-key, so a
		// crash before the walk's end-of-run save leaves a consistent collapsed
		// archive (the manifest never lagging the index).
		if err := manifest.Save(); err != nil {
			return Result{}, fmt.Errorf("save manifest after identity collapse: %w", err)
		}
	}
	// Every successful export is a newly-seen (or, in full mode, re-observed)
	// message: feed it to the search index (when enabled) and append a history
	// folder-assertion under its CURRENT folder — the timeline event the fold and
	// serve read (T2). The key is the exporter's own (possibly #fp-qualified) key,
	// so the assertion and the index row agree with the manifest record.
	exp.OnExported = func(store string, folderPath []string, m *model.Message, relPath, key string) {
		if idx != nil {
			if addErr := idx.Add(store, folderPath, m, relPath, key); addErr != nil {
				indexErrors++
				logger.Printf("warning: index: %v", addErr)
			}
		}
		if hist != nil {
			if herr := hist.WriteFolder(key, strings.Join(folderPath, "/")); herr != nil {
				logger.Printf("warning: history event: %v", herr)
			}
		}
	}
	// Every mailbox-wide observation stamps LastSeen=runAt (§3.4); the exporter
	// does that on a skip via RunAt, so a with-mid fast-path skip and a no-mid
	// exporter skip in one run carry the identical run timestamp.
	exp.RunAt = runAt
	// A no-Message-ID message cannot be recognised before download, so its move is
	// discovered at the exporter's mailbox-wide skip rather than the pre-download
	// fast-path. The exporter has already merged the record's Folder/LastSeen; here
	// we record the side effects only the live path can reach — the history
	// folder-assertion and the body-free index folder update — exactly as the
	// fast-path does for a with-mid move (§3.2/§3.6).
	exp.OnManifestSkip = func(key string, folderPath []string, moved bool) {
		if moved {
			recordMove(hist, idx, key, strings.Join(folderPath, "/"), logger)
		}
	}

	client := graph.New(ctx, graph.Config{
		Tenant:       g.Tenant,
		ClientID:     g.ClientID,
		ClientSecret: g.ClientSecret,
		BaseURL:      g.BaseURL,
		TokenURL:     g.TokenURL,
	})

	floor := opts.CheckpointEvery
	if floor <= 0 {
		floor = defaultCheckpointEvery
	}
	lastCheckpoint := 0
	var lockLost error
	checkpoint := func() {
		// Gate on messages PROCESSED (written + skipped-as-already-archived), not
		// just Exported: a migration or steady-state run is nearly all skips
		// (SkippedManifest), so an Exported-only gate would never fire and an
		// interrupted large run would never persist its progress and never
		// converge (EC11). Every skip stamps LastSeen / records a move, so skips
		// are real work worth checkpointing.
		processed := exp.Stats.Exported + exp.Stats.SkippedManifest
		if processed-lastCheckpoint < effectiveEvery(floor, manifest.Len()) {
			return
		}
		lastCheckpoint = processed
		if err := lock.StillHeld(); err != nil && lockLost == nil {
			lockLost = err
			return
		}
		commit(hist, idx, manifest, logger)
	}

	// Deleted Items / Junk Email are excluded by default; the flags opt in (T7).
	filter := graph.FolderFilter{IncludeDeleted: g.IncludeDeleted, IncludeJunk: g.IncludeJunk}

	result = Result{Files: len(g.Mailboxes)}
	var failures int
	var firstErr error
	for _, mbx := range g.Mailboxes {
		runErr := runGraphMailbox(ctx, client, exp, manifest, hist, idx, runAt, mbx, filter, logger, checkpoint, func() error { return lockLost })
		if lockLost != nil {
			// Lock removed/replaced mid-run: another run may own this archive now.
			// Write no shared state (manifest/index/README); the deferred recordRun
			// marks the run failed and the next incremental run reconciles the
			// message files already written (INT-CC-2/R5).
			return result, lockLost
		}
		commit(hist, idx, manifest, logger)
		if errors.Is(runErr, context.Canceled) {
			finish(opts.Out, &result, exp, manifest, idx, indexErrors, true, logger)
			return result, context.Canceled
		}
		if runErr != nil {
			failures++
			if firstErr == nil {
				firstErr = runErr
			}
			logger.Printf("error: mailbox %s: %v", mbx, runErr)
		}
	}

	// When no mailbox succeeded, do not write the browsable scaffold
	// (README.txt / folder index.html): a run that captured nothing must not
	// leave an empty archive that looks real. The manifest and the last-run
	// record are still written (below / by recordRun) so `status` sees the
	// failure. Otherwise apply the nas-02 guard: regenerate pages only when
	// something was exported or index.html is missing.
	allFailed := failures == len(g.Mailboxes)
	// A footer marks a fully-clean run (every mailbox walked): the timeline is
	// complete to here, so a later fold/gone-detection can trust it. A run with
	// any mailbox failure leaves no footer. The footer is fsync'd before the
	// manifest advances one last time (crash order, §3.5).
	if failures == 0 && hist != nil {
		if werr := hist.WriteRunFooter(runAt.UnixNano(), time.Now().UTC()); werr != nil {
			logger.Printf("warning: history run footer: %v", werr)
		}
	}
	commit(hist, idx, manifest, logger)
	if idx != nil && opts.Pages && !allFailed && pagesNeeded(opts.Out, exp.Stats.Exported) {
		if pErr := pages.Generate(opts.Out, idx, logger); pErr != nil {
			logger.Printf("warning: folder pages: %v", pErr)
		}
	}
	finish(opts.Out, &result, exp, manifest, idx, indexErrors, !allFailed, logger)
	if failures > 0 {
		// Carry the first per-mailbox error (including any AADSTS code) into the
		// returned error so it lands in the last-run record and `status` can fire
		// the expired-secret remedy (friction #6).
		if firstErr != nil {
			return result, fmt.Errorf("%d mailbox(es) failed; first error: %w", failures, firstErr)
		}
		return result, fmt.Errorf("%d mailbox(es) failed", failures)
	}
	return result, nil
}

// applyGraphState overlays the message-state fields carried on the widened
// listing $select (PC16). Graph is authoritative for a mailbox item's
// importance, sensitivity, read state and categories, so these override
// anything the MIME headers carried. A field the tenant omitted stays empty; an
// omitted isRead (nil) leaves the read state unset rather than guessing
// "unread"; omitted categories leave the message with none.
func applyGraphState(m *model.Message, ref graph.MessageRef) {
	m.Importance = graphImportance(ref.Importance)
	m.Sensitivity = graphSensitivity(ref.Sensitivity)
	if ref.IsRead != nil {
		m.Unread = !*ref.IsRead
	}
	// Graph's receivedDateTime is the authoritative delivery time for a mailbox
	// item; adopt it as Received so the message's Date — and the content
	// fingerprint that folds it — reflects the mailbox delivery time. A tenant
	// that omits it leaves the MIME Date header in place.
	if !ref.Received.IsZero() {
		m.Received = ref.Received
	}
	// Categories ride on the same widened listing; Graph is authoritative for a
	// mailbox item's classification. Copied so the message owns its slice.
	if len(ref.Categories) > 0 {
		m.Categories = append([]string(nil), ref.Categories...)
	}
	// The per-physical-message immutable id (empty unless the run obtained one),
	// carried to the exporter as the PhysID capture field (closure rev-6 §1/§4).
	m.PhysID = ref.PhysID
}

// graphImportance maps Graph's importance ("low"/"normal"/"high") to the
// model's convention; normal/omitted is the empty state.
func graphImportance(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return "high"
	case "low":
		return "low"
	}
	return ""
}

// graphSensitivity maps Graph's sensitivity ("normal"/"personal"/"private"/
// "confidential") to the model's convention; normal/omitted is the empty state.
func graphSensitivity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "personal":
		return "personal"
	case "private":
		return "private"
	case "confidential":
		return "confidential"
	}
	return ""
}

// recordMove appends the history folder-assertion and issues the body-free index
// folder update for an already-archived message observed in a NEW folder — the
// move's log+index side effects, shared by the pre-download fast-path (with a
// Message-ID) and the exporter's mailbox-wide skip (no Message-ID). The manifest
// field merge (Folder/LastSeen/Present) is the caller's; this is only the two
// stores the exporter cannot reach. Either store may be nil (idx off, or the
// history log could not be opened); a failure of one is logged, not fatal (§3.2).
func recordMove(hist *state.HistoryWriter, idx *index.Index, key, folder string, logger *log.Logger) {
	if hist != nil {
		if herr := hist.WriteFolder(key, folder); herr != nil {
			logger.Printf("warning: history event: %v", herr)
		}
	}
	if idx != nil {
		if uerr := idx.UpdateFolder(key, folder); uerr != nil {
			logger.Printf("warning: index folder update: %v", uerr)
		}
	}
}

// runGraphMailbox walks one mailbox's folders and messages, exporting each. hist
// and idx (either may be nil) receive the go-back timeline events and the
// body-free index folder update the move fast-path issues; runAt is the run's
// timestamp, stamped as LastSeen on every observation (§3.2/§3.4).
func runGraphMailbox(ctx context.Context, client *graph.Client, exp *export.Exporter, manifest *state.Manifest, hist *state.HistoryWriter, idx *index.Index, runAt time.Time, mailbox string, filter graph.FolderFilter, logger *log.Logger, checkpoint func(), abort func() error) error {
	logger.Printf("Reading mailbox %s via Microsoft Graph", mailbox)
	// The store token for a Graph source is seeded from the CANONICAL mailbox id
	// (lower-cased/trimmed), so a re-run whose -mailbox differs only in case hits
	// the same token — the incremental fast-path key below and the exporter's key
	// still agree and no body is re-downloaded (INT-CC-1/R17). Mailbox addresses
	// are injective, so this is almost always the plain sanitized segment.
	token := manifest.Token(state.MailboxSourceID(mailbox), mailbox)
	// Deleted Items and Junk Email are dropped from the walk by default (T7): the
	// filter resolves each to its concrete well-known-folder id and skips it and
	// its subtree, so a folder there is never listed and its records — being in an
	// unwalked folder — are never marked gone by the sweep below (§3.4/S38).
	folders, err := client.Folders(ctx, mailbox, filter)
	if err != nil {
		return fmt.Errorf("list folders: %w", err)
	}

	walked := state.NewWalkedFolders()
	for _, f := range folders {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		folderKey := strings.Join(f.Path, "/")
		err := client.Messages(ctx, mailbox, f.ID, func(ref graph.MessageRef) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			// Stop promptly once the run lost its lock — before any download —
			// bounding continued writing to a contended archive (INT-CC-2/R5).
			if abort != nil {
				if e := abort(); e != nil {
					return e
				}
			}
			// Live fast-path (incremental): a message already archived under its
			// mailbox-wide identity is recognised by Message-ID MEMBERSHIP and NOT
			// re-downloaded (R17); a MOVE is recorded from the listing with no
			// download; every observation stamps LastSeen=thisRun so gone-detection
			// is accurate (§3.4); a legacy "unknown" record is resolved here without
			// a download. Full mode re-downloads everything (R2) and dedups at the
			// exporter. The option-D mechanics are in the block below.
			if exp.Mode == export.Incremental && ref.InternetMessageID != "" {
				identity := "mid:" + ref.InternetMessageID
				// Option D (design rev-4): skip the download when this Message-ID is
				// already archived ANYWHERE in the mailbox (membership), whatever
				// folder it is now in — no envelope signature is computed or compared
				// (that was rev-2's retired mechanism). This is the cheap re-run and
				// the move fast-path in one: recognised by identity, folder recorded
				// from the LISTING, body never re-fetched (R17). A genuinely DISTINCT
				// message that reuses this Message-ID is the bounded #8 residual on
				// the floor: the membership skip never downloads it, so a fresh reuse
				// cannot be detected here (the deferred Graph ImmutableId closure
				// detects it pre-download; the id-reuse log lands in a later slice).
				// Same-TOKEN siblings only: KeysForIdentity is archive-wide (the
				// token is stripped from the index), so a Message-ID shared with
				// ANOTHER mailbox archived into the same -out must NOT make this
				// mailbox skip — each mailbox keeps its own physical copy (R6). Filter
				// to this token; if none, fall through and download.
				var sameToken []state.IdentRef
				tokenPrefix := token + "\x00"
				for _, sib := range manifest.KeysForIdentity(identity) {
					if strings.HasPrefix(sib.Key, tokenPrefix) {
						sameToken = append(sameToken, sib)
					}
				}
				if len(sameToken) > 0 {
					// Skip-vs-download decision (design closure rev-6.1 §5, content-hash
					// arbiter). The floor (no immutable id, ref.PhysID == "") skips by
					// Message-ID MEMBERSHIP — this mail is archived somewhere in THIS
					// mailbox, whatever folder it is in now (R17). With an immutable id:
					//   - a same-token sibling already stores THIS id -> SKIP (the same
					//     physical message; matchKey = that sibling, HC6);
					//   - no id match, and EVERY same-token sibling carries a content hash
					//     (a v6-managed identity) -> DOWNLOAD and let the exporter's
					//     content-hash arbiter place it (adopt a churn / split a distinct
					//     reuse — #8 closed);
					//   - no id match, but ANY sibling lacks a content hash (a pre-v6 /
					//     floor record) -> FLOOR membership skip: the closure cannot tell a
					//     distinct reuse from the archived message without a comparable hash,
					//     so it does NOT bind the listed id to a record chosen by anything
					//     but content (HC4) and does NOT re-download to backfill legacy
					//     records (no storm). Such an archive closes #8 only for messages
					//     captured fresh under v6.
					matchKey := state.LiveKey(token, identity)
					if _, ok := manifest.Get(matchKey); !ok {
						matchKey = sameToken[0].Key
					}
					skip := true
					idMatched := false
					if ref.PhysID != "" {
						for _, sib := range sameToken {
							// Match the id against the record's WHOLE content-equal set
							// (primary PhysID or any AltPhysIDs), so a message copied into
							// several folders — same Message-ID, identical content, distinct
							// Graph ids — skips every copy after the first run instead of
							// re-downloading and flapping the id (rev-6.2).
							if manifest.HasPhysID(sib.Key, ref.PhysID) {
								matchKey, idMatched, skip = sib.Key, true, true
								break
							}
						}
						if !idMatched {
							// Download only when the whole identity is content-comparable;
							// otherwise stay on the floor (skip).
							comparable := true
							for _, sib := range sameToken {
								if r, ok := manifest.Get(sib.Key); !ok || r.ContentHash == "" {
									comparable = false
									break
								}
							}
							skip = !comparable
						}
					}
					if skip {
						rec, _ := manifest.Get(matchKey)
						if rec.Unknown() && manifest.Resolve(matchKey) {
							exp.Stats.Resolved++
						}
						manifest.MergeFields(matchKey, folderKey, runAt, true)
						// A folder-assertion is emitted when the observed folder differs
						// from the record's last known folder (a MOVE) OR the record was
						// gone and is now seen again (present-again, Present false->true):
						// both are one folder-assertion shape, and the fold reads an
						// assertion after a gone event as present-again (§3.3). rec is the
						// pre-observation snapshot (MergeFields does not mutate this copy).
						// An unchanged re-observation (same folder, still present) emits
						// nothing and downloads nothing (R17).
						if rec.Folder != folderKey || !rec.Present {
							recordMove(hist, idx, matchKey, folderKey, logger)
						}
						// Fan-out LastSeen unless we identified the EXACT sibling by its
						// immutable id: a floor/uncomparable skip cannot say WHICH #fp
						// sibling of a reused Message-ID this listing entry is, so advance
						// LastSeen on EVERY same-token sibling, else an un-stamped
						// still-present sibling is phantom-marked gone by SweepGone (R21).
						// An exact id match stamps only that sibling, so a genuinely
						// departed distinct reuse is correctly gone-detected (rev-6.1).
						if !idMatched {
							for _, sib := range sameToken {
								if sib.Key != matchKey {
									manifest.TouchSeen(sib.Key, runAt)
								}
							}
						}
						// If this identity already carries MORE THAN ONE distinct
						// fingerprint in this mailbox, a reuse is known — log it (deduped,
						// HC12). A fresh distinct reuse is captured on the download path;
						// the note remains an audit trail.
						exp.NoteIDReuse(identity, len(sameToken))
						exp.Stats.SkippedManifest++
						checkpoint() // a mostly-skip run must still persist progress (EC11)
						return nil
					}
				}
			}
			data, fetchErr := client.MIME(ctx, mailbox, ref.ID)
			if fetchErr != nil {
				return fmt.Errorf("fetch message %s: %w", ref.ID, fetchErr)
			}
			m := source.ParseRFC822(data)
			if m == nil {
				return nil
			}
			applyGraphState(m, ref)
			// A panic exporting one crafted message must not abort the mailbox (R10).
			return func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						logger.Printf("warning: recovered on a message in %s (skipped): %v", folderKey, r)
						err = nil
					}
				}()
				_, exportErr := exp.Export(token, f.Path, m)
				checkpoint()
				return exportErr
			}()
		})
		if err != nil {
			return err
		}
		// This folder was fully walked (its listing consumed with no error): a
		// message recorded under it that we did NOT re-observe this run has left
		// the live mailbox and is in-scope for the gone sweep below (§3.4).
		walked.Mark(folderKey)
	}
	// gone-detection by full reconciliation (§3.4). Control reaches here only
	// after EVERY folder of the mailbox was fully walked (any folder error or ctx
	// cancel returns above). Re-check abort() so a run whose lock was lost or
	// replaced at a checkpoint DURING this walk stops WITHOUT concluding anything
	// is gone — a run that may no longer own the archive must not sweep. gone is
	// therefore computed once, at the walk's end, NEVER at a checkpoint.
	if abort != nil {
		if e := abort(); e != nil {
			return e
		}
	}
	gone := manifest.SweepGone(token, walked, runAt)
	for _, k := range gone {
		if hist != nil {
			// The {k,gone} event is appended (and fsync'd by the caller's commit)
			// so the log carries the gone before the manifest (the trailing anchor)
			// is saved with Present=false (crash order §3.5). If the append FAILS,
			// UNDO SweepGone's in-memory Present=false, so the saved manifest never
			// records a gone the log lacks — the manifest must never lead the log
			// (#13). The record keeps its prior Folder/LastSeen and is re-detected
			// gone (event retried) next run. When there is no log at all (hist nil)
			// the manifest still reflects reality for the current view; there is no
			// log to lead.
			if herr := hist.WriteGone(k); herr != nil {
				logger.Printf("warning: history gone event (leaving message present until next run): %v", herr)
				manifest.SetPresent(k, true)
			}
		}
	}
	if len(gone) > 0 {
		logger.Printf("mailbox %s: %d message(s) gone from the live mailbox (files kept)", mailbox, len(gone))
	}
	return nil
}

// sameArchivedEML reports whether two archived messages (given by their .html
// paths relative to out) are the SAME message — by comparing their preserved
// .eml (raw wire bytes, identical wherever a message is filed). The rendered
// .html must NOT be used: it embeds the folder breadcrumb, depth-dependent
// relative paths and key-derived attachment names, so one message's two
// folder-copies differ. When a -raw .eml is absent for either copy there is
// nothing to compare, so they are treated as a move-duplicate (the common case);
// the residual — two distinct same-envelope/different-body reuses in a non-raw
// archive — is closed by the deferred ImmutableId closure.
func sameArchivedEML(out, relHTMLa, relHTMLb string) bool {
	emlA := filepath.Join(out, filepath.FromSlash(strings.TrimSuffix(relHTMLa, ".html")+".eml"))
	emlB := filepath.Join(out, filepath.FromSlash(strings.TrimSuffix(relHTMLb, ".html")+".eml"))
	fa, ea := os.Stat(emlA)
	fb, eb := os.Stat(emlB)
	if ea != nil || eb != nil {
		return true // no raw bytes to compare → treat as a move-duplicate
	}
	if fa.Size() != fb.Size() {
		return false
	}
	ba, e1 := os.ReadFile(emlA)
	bb, e2 := os.ReadFile(emlB)
	return e1 == nil && e2 == nil && bytes.Equal(ba, bb)
}
