package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
)

// GraphOptions configures a Microsoft Graph archive run. BaseURL, TokenURL and
// DeviceAuthURL are test overrides (empty = Microsoft production endpoints).
type GraphOptions struct {
	Tenant       string
	ClientID     string
	ClientSecret string
	Mailboxes    []string
	BaseURL      string
	TokenURL     string

	// Auth selects the trust model: "app" (default/empty) is the app-only
	// client-credentials grant against /users/{upn}; "device" is the per-user
	// device-code delegated grant against the signer's OWN mailbox (/me). In
	// device mode ClientSecret is unused, Mailboxes is at most the signer's own
	// address, and TokenCachePath persists the sign-in for later silent runs
	// (design-graph-delegated OR1/OR3).
	Auth           string
	TokenCachePath string
	DeviceAuthURL  string
	Unattended     bool

	// TokenStore, when set, keeps the delegated sign-in token in a vault (Windows
	// Credential Manager) instead of the TokenCachePath file (design-mailarchive-
	// desktop P4). Nil = the file cache (the cross-platform default).
	TokenStore graph.TokenStore

	// Prompt (device mode) receives the verification URL + user code at first
	// sign-in, so a GUI can display the code instead of the default stderr print.
	Prompt func(graph.DeviceAuth)

	// Deleted Items and Junk Email are excluded from the walk by default (T7,
	// operator ruling); these opt them back in. Exclusion is by resolved
	// well-known-folder id, so it is locale-independent (§3.6).
	IncludeDeleted bool
	IncludeJunk    bool

	// Test overrides of the Graph client's bounds and its transport (zero or
	// nil = the client's defaults, which are fixed by design, fault-isolation
	// §4.8.5). The CLI, the GUI and the desktop leave them unset; tests scale
	// the bounds and inject transport faults (MA-540).
	RequestTimeout    time.Duration
	MIMETimeout       time.Duration
	MIMEIdleTimeout   time.Duration
	MIMEHeaderTimeout time.Duration
	RetryAfterMax     time.Duration // each Retry-After sleep (default 60 s)
	RetryAfterBudget  time.Duration // all of one request's Retry-After sleeps (default 5 min)
	BreakerTimeBound  time.Duration // the breaker's cumulative time in failed downloads, per mailbox (default 30 min)
	PageCap           int
	WrapTransport     func(http.RoundTripper) http.RoundTripper
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
	// A paused archive is hard read-only: refuse the capture before the (interactive)
	// sign-in and any write (archive-manager S2-C1/C2).
	if err := seal.RefuseWrite(opts.Out); err != nil {
		return Result{}, err
	}
	if err := RefuseUnfinishedCopy(opts.Out); err != nil {
		return Result{}, err
	}
	// An encrypted archive is captured into only with its key: refuse before the
	// sign-in and any write (phase2a §5); a scheduled capture's refusal is recorded.
	if opts.Key == nil {
		if err := RefuseKeyless("capture into it", opts.Out, manifestDir(opts.Manifest)); err != nil {
			if opts.Unattended {
				RecordRefusedRun(opts, err)
			}
			return Result{}, err
		}
	}
	// A keyed capture proves its key and target before the sign-in, any folder, lock
	// or record: a refused manual capture writes nothing (P2a-C10).
	if opts.Key != nil {
		// A scheduled capture whose archive's drive is not there is told so first, not
		// that its key failed.
		if opts.Unattended {
			if err := RequireExistingOut(opts.Out); err != nil {
				return Result{}, err
			}
		}
		// The sign-in below writes its token cache before the lock: one inside the
		// archive is refused here, first (P2a-C14).
		if err := preflightEncrypted(opts, g.TokenCachePath); err != nil {
			if opts.Unattended {
				RecordRefusedRun(opts, err)
			}
			return Result{}, err
		}
	}
	// Build the Graph client and resolve the mailbox set BEFORE taking the archive
	// lock: a device sign-in is interactive and must not hold the lock, and device
	// mode derives its single mailbox (the signer, read from /me) rather than being
	// told one. App mode returns the given mailboxes unchanged.
	client, mailboxes, err := buildGraphClient(ctx, g, logger)
	if err != nil {
		return Result{}, withKind(state.FailSignIn, cleanErr(asFailure(ctx, err)))
	}
	g.Mailboxes = mailboxes
	if len(g.Mailboxes) == 0 {
		return Result{}, errors.New("no mailboxes specified (-mailbox is required)")
	}
	// PM4: a SCHEDULED M365 capture whose archive -out is gone (an unmounted backup drive) must
	// refuse, never recreate the archive on the local disk and back up into a decoy.
	if opts.Unattended {
		if err := RequireExistingOut(opts.Out); err != nil {
			return Result{}, err
		}
	}
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return Result{}, withKind(state.FailArchiveUnwritable, fmt.Errorf("create output dir: %w", err))
	}
	lockPath := filepath.Join(opts.Out, lockfile.Name)
	lock, err := lockfile.AcquireAs(lockPath, lockVerb(opts, "graph"))
	if err != nil {
		return Result{}, withKind(state.FailArchiveUnwritable, busyRefusal(lockPath, err)) // a held lock is named by ErrHeld first
	}
	defer lock.Release()
	if opts.Key == nil {
		if err := RefuseKeyless("capture into it", opts.Out, manifestDir(opts.Manifest)); err != nil {
			return Result{}, err
		}
	}
	mpath := opts.Manifest
	if mpath == "" {
		mpath = filepath.Join(opts.Out, ".mailarchive-manifest.json")
	}
	// A keyed capture's manifest and unit checks run here, before its "running"
	// record: a refused manual capture writes nothing (P2a-C10), a scheduled one only
	// its minimal failed run (A14).
	var manifest *state.Manifest
	var units *export.Units
	var sealed *container.Store
	var download *programDownload
	encStart := time.Now().UTC()
	if opts.Key != nil {
		owed := func(m *state.Manifest) error { return refuseOwedCollapse(opts, m, g.Mailboxes) }
		if manifest, units, sealed, err = openEncrypted(opts, lock.StillHeld, encStart, logger, owed); err != nil {
			if opts.Unattended {
				writeRefusedRun(opts, err)
			}
			return Result{}, err
		}
		download = startProgramDownload(opts.downloadContext(ctx), sealed) // program design §5, R17
		defer func() {
			if err == nil {
				tellStandingDamage(opts, logger) // P3-8 friction, B19
			}
		}()
	}
	begun := beginRun(opts)
	defer recordRun(opts, begun, &result, &err, logger)

	// What a crashed earlier capture may have left behind (R5): stale temps — a
	// half-written page, zip or manifest. Only temps, never zips: a capture's
	// zip-without-page is not the local runs' crash signature (2a-P).
	if n := export.SweepTemps(opts.Out, time.Now(), logger, programSweepSkip(opts)...); n > 0 {
		logger.Printf("swept %d orphaned temp file(s) from an interrupted run", n)
	}

	if manifest == nil {
		if manifest, err = state.Load(mpath); err != nil {
			return Result{}, withKind(state.FailManifestUnreadable, err)
		}
	}
	manifestBefore := manifest.Len()

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
		Units:            units,
	}
	if exporterSetup != nil {
		exporterSetup(exp)
	}

	// The go-back timeline: an append-only history log records this run's changes
	// (newly-seen / moved / gone), fsync'd before the manifest advances (§3.5).
	// A log that cannot be opened degrades the timeline but must not fail the run.
	// The run's timestamp is its last-run record's start: the stamp every LastSeen
	// and every capture-gap observation carries, so `status` can tell a gap this
	// run attempted from one it did not (FC14).
	runAt := begun.Started
	if sealed != nil {
		sealed.SetRunStamp(runAt)
	}
	// An encrypted archive's timeline is sealed in its record (phase2b §5): no
	// plaintext log is opened.
	var hist state.Timeline
	if sealed != nil {
		hist = sealed.Timeline()
	} else if hw, herr := state.OpenHistory(filepath.Join(opts.Out, state.HistoryName)); herr != nil {
		logger.Printf("warning: %v (this run's go-back timeline will be incomplete)", herr)
	} else {
		hist = hw
	}
	if hist != nil {
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
	var si *sealedIndex
	var indexErrors int
	if !opts.Index && sealed != nil && sealed.HasIndex() {
		// Records this capture commits are not added to the sealed index (P2b-C16).
		sealed.MarkIndexBehind()
	}
	if opts.Index {
		idxPath := filepath.Join(opts.Out, "search.db")
		if sealed != nil {
			if si, err = openSealedIndex(sealed, opts.Out, manifest.Len(), false, logger); err == nil && si != nil {
				idx = si.idx
			}
		} else {
			idx, err = index.Open(idxPath)
		}
		if err != nil {
			return Result{}, withKind(state.FailIndex, indexOpenError(idxPath, opts.Out, err))
		}
	}
	if idx != nil { // nil for a sealed index with no room to work on it: marked behind
		defer closeRunIndex(idx, si != nil, logger)
		if _, rkErr := idx.RepairKeys(manifest.Rekeyed > 0, state.MigrateKey, logger); rkErr != nil {
			if si == nil {
				return Result{}, withKind(state.FailIndex, fmt.Errorf("migrate search index keys: %w", rkErr))
			}
			si.drop(sealed, rkErr, logger) // a sealed index never stops the backup
			idx, si = nil, nil
		}
	}
	if idx != nil {
		// A sealed index marked behind — or none at all beside records — is caught up
		// before the walk (phase2b §6).
		if sealed != nil && (sealed.IndexBehind() || (!sealed.HasIndex() && manifest.Len() > 0)) {
			si.catchUp(manifest, units, sealed, logger)
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
	// An encrypted archive never runs the identity collapse: its content check reads
	// .eml files by logical path, which an encrypted archive has none of, so it
	// would merge distinct messages (P2a-C7). A fresh encrypted archive has nothing
	// to collapse and is simply marked; one that still owes a real collapse was
	// encrypted wrongly (only the A12 conversion, which collapses first, encrypts).
	// (refuseOwedCollapse refused the other case before the run began.)
	if len(collapseTokens) > 0 && opts.Key != nil {
		manifest.MarkCollapsed(collapseTokens...)
		collapseTokens = nil
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
					return Result{}, withKind(state.FailIndex, fmt.Errorf("re-key search index for identity collapse: %w", rkErr))
				}
			}
			logger.Printf("collapsed %d move-duplicate(s) and re-keyed %d record(s) to the mailbox-wide identity (one-time v3→v5 upgrade)", len(losses), len(remap))
		}
		manifest.MarkCollapsed(collapseTokens...)
		// Persist the collapsed manifest + marker now, AFTER the index re-key, so a
		// crash before the walk's end-of-run save leaves a consistent collapsed
		// archive (the manifest never lagging the index).
		if err := manifest.Save(); err != nil {
			return Result{}, withKind(state.FailManifestSave, fmt.Errorf("save manifest after identity collapse: %w", err))
		}
	}
	// Every successful export is a newly-seen (or, in full mode, re-observed)
	// message: feed it to the search index (when enabled) and append a history
	// folder-assertion under its CURRENT folder — the timeline event the fold and
	// serve read (T2). The key is the exporter's own (possibly #fp-qualified) key,
	// so the assertion and the index row agree with the manifest record.
	warns := newIndexWarnings(logger)
	defer warns.flush()
	exp.OnExported = func(store string, folderPath []string, m *model.Message, relPath, key string) {
		if idx != nil {
			if addErr := indexExported(idx, si, sealed, logger, store, folderPath, m, relPath, key); addErr != nil {
				indexErrors++
				warns.add(addErr)
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

	floor := opts.CheckpointEvery
	if floor <= 0 {
		floor = defaultCheckpointEvery
	}
	lastCheckpoint := 0
	var lockLost error
	var volumeFatal *RunFatalError // the archive's volume could no longer be written: the program step is skipped (R1)
	// saveErr is the most recent manifest save's outcome; a run whose last save
	// failed kept no progress and fails (F53), exactly as Run does.
	var saveErr error
	// The run's own state across its mailboxes: what it records of faults and what it
	// closes of gaps. A fault counts as processed for the checkpoint gate below, so a
	// stretch of nothing but faults still saves and still re-checks the lock (FC12).
	lr := &localRun{exp: exp, out: opts.Out, runAt: runAt, logger: logger, lock: lock}
	lr.onLockLost = func(err error) {
		if lockLost == nil {
			lockLost = err
		}
	}
	checkpoint := func() {
		// Gate on messages PROCESSED (written + skipped-as-already-archived), not
		// just Exported: a migration or steady-state run is nearly all skips
		// (SkippedManifest), so an Exported-only gate would never fire and an
		// interrupted large run would never persist its progress and never
		// converge (EC11). Every skip stamps LastSeen / records a move, so skips
		// are real work worth checkpointing.
		// A fault is processed work too (FC12): a run made mostly of faults still saves
		// its progress and re-checks its lock.
		processed := exp.Stats.Exported + exp.Stats.SkippedManifest + lr.faults
		if processed-lastCheckpoint < effectiveEvery(floor, manifest.Len()) {
			return
		}
		lastCheckpoint = processed
		if err := lock.StillHeld(); err != nil && lockLost == nil {
			lockLost = err
			return
		}
		saveErr = commit(hist, idx, manifest, logger)
		stopOnLostLock(saveErr, &lockLost)
	}

	lr.onFault = checkpoint

	// Deleted Items / Junk Email are excluded by default; the flags opt in (T7).
	filter := graph.FolderFilter{IncludeDeleted: g.IncludeDeleted, IncludeJunk: g.IncludeJunk}

	result = Result{Files: len(g.Mailboxes), ManifestBefore: manifestBefore}
	var failures int
	var firstErr error
	unread := 0 // mailboxes a run-level stop left unread
	var lastTrip *GraphBreakerError
	var runBreaker *RunBreakerError
	for i, mbx := range g.Mailboxes {
		runErr := runGraphMailbox(ctx, lr, client, manifest, hist, idx, mbx, filter, g.BreakerTimeBound, checkpoint, func() error { return lockLost })
		if lockLost != nil {
			// Lock removed/replaced mid-run: another run may own this archive now.
			// Write no shared state (manifest/index/README); the deferred recordRun
			// marks the run failed and the next incremental run reconciles the
			// message files already written (INT-CC-2/R5).
			return result, withKind(state.FailLockLost, lockLost)
		}
		if sealed != nil && runErr == nil {
			// The mailbox's walk finished: its records' LastSeen is sealed relative to
			// this run's stamp from here on (P2b-C12).
			sealed.WalkDone(manifest.Token(state.MailboxSourceID(mbx), mbx), runAt)
		}
		saveErr = commit(hist, idx, manifest, logger)
		stopOnLostLock(saveErr, &lockLost)
		// "Cancelled" is decided from RunGraph's own context, never from the
		// error chain (fault-isolation §4.8.6, RR6-C1): a mailbox error can carry
		// a context.Canceled no operator raised (a transport's own), and reading
		// it as an interrupt would skip every later mailbox, record the run
		// cancelled and let the CLI exit 0. It is about a walk the interrupt cut
		// short: a mailbox that finished cleanly stays finished (an interrupt after
		// the last one leaves a complete run), and the next mailbox's first request
		// meets the cancel itself.
		interrupted := ctx.Err() != nil
		if interrupted && runErr != nil {
			result.gapsRecovered, result.gapsClosedGone = lr.recovered, lr.closedGone
			finish(opts.Out, &result, exp, manifest, idx, indexErrors, true, logger)
			if saveErr != nil {
				return result, manifestSaveFailure(mpath, saveErr)
			}
			return result, context.Canceled
		}
		// Two mailboxes in a row that tripped the breaker say the service or the network
		// is down: the run stops, and an outage costs about two mailboxes' budget, not
		// every mailbox's (§4.8.5, FC10).
		var trip *GraphBreakerError
		if !errors.As(runErr, &trip) {
			lastTrip = nil
		}
		if runErr != nil {
			failures++
			if firstErr == nil {
				// The context is live here, so a context.Canceled in the chain is a
				// transport's own: it loses its chain now, not after a later cancel.
				firstErr = runErr
				if errors.Is(runErr, context.Canceled) {
					firstErr = errors.New(runErr.Error())
				}
			}
			logger.Printf("error: mailbox %s: %v", mbx, runErr)
			var fatal *RunFatalError
			if errors.As(runErr, &fatal) {
				// The archive's location or volume can no longer be written: the next
				// mailbox would fail the same way, so the run stops here, with what it
				// kept (§4.6).
				unread = len(g.Mailboxes) - i - 1
				volumeFatal = fatal
				break
			}
			if trip != nil {
				if lastTrip != nil {
					runBreaker = &RunBreakerError{First: lastTrip, Second: trip}
					logger.Printf("error: %v", runBreaker)
					unread = len(g.Mailboxes) - i - 1
					break
				}
				lastTrip = trip
			}
		}
	}

	// When no mailbox succeeded, do not write the browsable scaffold
	// (README.txt / folder index.html): a run that captured nothing must not
	// leave an empty archive that looks real. The manifest and the last-run
	// record are still written (below / by recordRun) so `status` sees the
	// failure. Otherwise apply the nas-02 guard: regenerate pages only when
	// something was exported or index.html is missing.
	allFailed := failures+unread == len(g.Mailboxes)
	// A footer marks a fully-clean run (every mailbox walked, nothing it could not
	// capture): the timeline is complete to here, so a later fold/gone-detection can
	// trust it. A run with any mailbox failure or any gap leaves no footer (§4.8.3).
	// The footer is fsync'd before the manifest advances one last time (crash
	// order, §3.5).
	if failures == 0 && hist != nil && !runPartial(manifest, exp, opts.Out, runAt) {
		if werr := hist.WriteRunFooter(runAt.UnixNano(), time.Now().UTC()); werr != nil {
			logger.Printf("warning: history run footer: %v", werr)
		}
	}
	saveErr = commit(hist, idx, manifest, logger)
	if hErr := healLateDamage(si, sealed, manifest, units, logger); hErr != nil && saveErr == nil {
		saveErr = hErr
	}
	// An encrypted archive has no folder pages: they are its folder tree, subjects and
	// senders in cleartext (phase2a §3); MailArchive's reader renders them (Phase 3).
	if gaps := gapBannerCount(manifest); exp.Units == nil && idx != nil && opts.Pages && !allFailed && pagesNeeded(opts.Out, exp.Stats.Exported, gaps) {
		if pErr := pages.Generate(opts.Out, idx, logger, gaps); pErr != nil {
			logger.Printf("warning: folder pages: %v", pErr)
		}
	}
	result.gapsRecovered, result.gapsClosedGone = lr.recovered, lr.closedGone
	finish(opts.Out, &result, exp, manifest, idx, indexErrors, !allFailed, logger)
	if sealed != nil {
		// No index write may follow the run's last seal (phase2b §6).
		if gErr := guardLateIndexWrite(si, sealed, manifest, logger); gErr != nil && saveErr == nil {
			saveErr = gErr
		}
		result.IndexBehind = opts.Index && sealed.IndexBehind()
		// The program copies, under the lock, after the last save (program design R1).
		if volumeFatal == nil && lockLost == nil && saveErr == nil && ctx.Err() == nil {
			result.Program = programStep(programEnv{ctx: ctx, out: opts.Out, store: sealed, save: manifest.Save,
				stillHeld: lock.StillHeld, logger: logger, runStart: encStart, download: download,
				skip: opts.ProgramSkip, onWait: opts.OnProgramWait})
		}
	}
	if saveErr != nil {
		if failures > 0 && firstErr != nil {
			return result, fmt.Errorf("%d mailbox(es) failed; first error: %w; and %w", failures, firstErr, manifestSaveFailure(mpath, saveErr))
		}
		return result, manifestSaveFailure(mpath, saveErr)
	}
	if failures > 0 {
		// Carry the first per-mailbox error (including any AADSTS code) into the
		// returned error so it lands in the last-run record and `status` can fire
		// the expired-secret remedy (friction #6).
		// A run whose first failure is the breaker's refusal is an outage and is
		// recorded as one (asOutage), not left to the reader of its text (S3-AC5).
		if firstErr != nil {
			if runBreaker != nil {
				return result, withKind(state.FailMailboxFailed, asOutage(fmt.Errorf("%d mailbox(es) failed; first error: %w; %w", failures, asFailure(ctx, firstErr), runBreaker), firstErr))
			}
			return result, withKind(state.FailMailboxFailed, asOutage(fmt.Errorf("%d mailbox(es) failed; first error: %w", failures, asFailure(ctx, firstErr)), firstErr))
		}
		return result, withKind(state.FailMailboxFailed, fmt.Errorf("%d mailbox(es) failed", failures))
	}
	return result, partialOutcome(result)
}

// asFailure is RR6-C1's second guard on what RunGraph returns: while its
// context is live it never returns an error that matches context.Canceled,
// because recordRun and the CLI test the chain and would record the run
// cancelled and exit 0. Such an error keeps its text and loses its chain; any
// other error is returned as it is.
func asFailure(ctx context.Context, err error) error {
	if err != nil && ctx.Err() == nil && errors.Is(err, context.Canceled) {
		return errors.New(err.Error())
	}
	return err
}

// buildGraphClient constructs the Graph client for the selected auth mode and
// resolves the mailbox set. App mode (default) builds the client-credentials
// client and returns the given mailboxes unchanged. Device mode signs in (from a
// cache, or interactively), reads the signer's UPN from /me, and returns that one
// mailbox — refusing a -mailbox that names anyone else, so the operator can never
// believe they are archiving a mailbox other than their own (design P3/D-C1).
func buildGraphClient(ctx context.Context, g GraphOptions, logger *log.Logger) (*graph.Client, []string, error) {
	cfg := graph.Config{
		Tenant:            g.Tenant,
		ClientID:          g.ClientID,
		BaseURL:           g.BaseURL,
		TokenURL:          g.TokenURL,
		RequestTimeout:    g.RequestTimeout,
		MIMETimeout:       g.MIMETimeout,
		MIMEIdleTimeout:   g.MIMEIdleTimeout,
		MIMEHeaderTimeout: g.MIMEHeaderTimeout,
		RetryAfterMax:     g.RetryAfterMax,
		RetryAfterBudget:  g.RetryAfterBudget,
		PageCap:           g.PageCap,
		WrapTransport:     g.WrapTransport,
	}
	if strings.EqualFold(g.Auth, "device") {
		cfg.DeviceAuthURL = g.DeviceAuthURL
		cfg.TokenCachePath = g.TokenCachePath
		cfg.TokenStore = g.TokenStore
		cfg.Unattended = g.Unattended
		cfg.Prompt = g.Prompt
		client, err := graph.NewDelegated(ctx, cfg)
		if err != nil {
			return nil, nil, err
		}
		upn, err := client.Me(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("verify signed-in user: %w", err)
		}
		switch {
		case len(g.Mailboxes) == 0:
			// no -mailbox: adopt the signer.
		case len(g.Mailboxes) == 1 && strings.EqualFold(strings.TrimSpace(g.Mailboxes[0]), upn):
			// -mailbox matches the signer.
		default:
			return nil, nil, fmt.Errorf("signed in as %s, but -mailbox is %s: device mode archives only your own mailbox — omit -mailbox or pass exactly %s", upn, strings.Join(g.Mailboxes, ", "), upn)
		}
		logger.Printf("Signed in as %s (device flow); archiving your own mailbox", upn)
		return client, []string{upn}, nil
	}
	cfg.ClientSecret = g.ClientSecret
	return graph.New(ctx, cfg), g.Mailboxes, nil
}

// applyGraphState overlays the message-state fields carried on the widened
// listing $select (PC16). Graph is authoritative for a mailbox item's
// importance, read state and categories, so these override anything the MIME
// headers carried. A field the tenant omitted stays empty; an omitted isRead
// (nil) leaves the read state unset rather than guessing "unread"; omitted
// categories leave the message with none. Sensitivity rides on the PidTagSensitivity
// extended-property $expand (not $select, MA-284/285) and is overlaid ONLY when a
// value came back — so a tenant that doesn't support the $expand, or a message with
// no sensitivity set, keeps whatever the MIME headers carried rather than being
// wiped (best-effort).
func applyGraphState(m *model.Message, ref graph.MessageRef) {
	m.Importance = graphImportance(ref.Importance)
	if ref.Sensitivity != "" {
		m.Sensitivity = ref.Sensitivity
	}
	if ref.IsRead != nil {
		m.Unread = !*ref.IsRead
	}
	// Graph's receivedDateTime is the authoritative delivery time for a mailbox
	// item; adopt it as Received so the message's Date — and the content
	// fingerprint that folds it — reflects the mailbox delivery time. A tenant
	// that omits it leaves the MIME Date header in place.
	if !ref.Received.IsZero() {
		m.Received = ref.Received
		// A message neither parser could read has the stub's dateless identity terms
		// (NoIdentityDate keeps the walker's display dates out of it). On Graph the stub's
		// identity has always folded the received time, so two such messages are two
		// records: the listing's time is the one date the identity reads (MS2-AI2).
		if t := m.IdentityTerms; t != nil && t.NoIdentityDate {
			dated := *t
			dated.NoIdentityDate = false
			m.IdentityTerms = &dated
		}
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

// recordMove appends the history folder-assertion and issues the body-free index
// folder update for an already-archived message observed in a NEW folder — the
// move's log+index side effects, shared by the pre-download fast-path (with a
// Message-ID) and the exporter's mailbox-wide skip (no Message-ID). The manifest
// field merge (Folder/LastSeen/Present) is the caller's; this is only the two
// stores the exporter cannot reach. Either store may be nil (idx off, or the
// history log could not be opened); a failure of one is logged, not fatal (§3.2).
func recordMove(hist state.Timeline, idx *index.Index, key, folder string, logger *log.Logger) {
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
func runGraphMailbox(ctx context.Context, lr *localRun, client *graph.Client, manifest *state.Manifest, hist state.Timeline, idx *index.Index, mailbox string, filter graph.FolderFilter, breakerTime time.Duration, checkpoint func(), abort func() error) error {
	exp, runAt, logger := lr.exp, lr.runAt, lr.logger
	logger.Printf("Reading mailbox %s via Microsoft Graph", mailbox)
	// The store token for a Graph source is seeded from the CANONICAL mailbox id
	// (lower-cased/trimmed), so a re-run whose -mailbox differs only in case hits
	// the same token — the incremental fast-path key below and the exporter's key
	// still agree and no body is re-downloaded (INT-CC-1/R17). Mailbox addresses
	// are injective, so this is almost always the plain sanitized segment.
	token := manifest.Token(state.MailboxSourceID(mailbox), mailbox)
	if err := refuseStoreIntoEncrypted(exp, token); err != nil {
		return err
	}
	// Deleted Items and Junk Email are dropped from the walk by default (T7): the
	// filter resolves each to its concrete well-known-folder id and skips it and
	// its subtree, so a folder there is never listed and its records — being in an
	// unwalked folder — are never marked gone by the sweep below (§3.4/S38).
	folders, err := client.Folders(ctx, mailbox, filter)
	if err != nil {
		return cleanErr(fmt.Errorf("list folders: %w", err))
	}

	walked := state.NewWalkedFolders()
	w := newMailboxWalk(lr, client, manifest, mailbox, token, breakerTime)
	for _, f := range folders {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		folderKey := strings.Join(f.Path, "/")
		w.folderID, w.mark = f.ID, time.Now()
		err := client.Messages(ctx, mailbox, f.ID, func(ref graph.MessageRef) error {
			defer func() { w.mark = time.Now() }()
			select {
			case <-ctx.Done():
				return &walkAbort{ctx.Err()}
			default:
			}
			// Stop promptly once the run lost its lock — before any download —
			// bounding continued writing to a contended archive (INT-CC-2/R5).
			if abort != nil {
				if e := abort(); e != nil {
					return &walkAbort{e}
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
					// An unparsed stub is terminal for the parser generation that gave up
					// on it: one a newer parser may read is downloaded again, once (FC25).
					// The walk stamps the generation on the stub it still cannot parse,
					// or the exporter heals it in place when this parse succeeds.
					if r, ok := manifest.Get(matchKey); skip && ok && r.StaleUnparsed(state.ParserGenCurrent) {
						skip = false
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
						// The message is archived: a gap an earlier run left for it is
						// answered. Closing it is not a decision the gap takes (P5).
						w.recovered(ref)
						checkpoint() // a mostly-skip run must still persist progress (EC11)
						return nil
					}
				}
			}
			// A folder whose directory could not be created earlier in this run: a
			// message that would need a download is counted under that folder's gap
			// and not downloaded (§4.8.2). One the archive already holds was skipped
			// above and kept its stamp.
			if w.s.unwritable[filepath.Join(append([]string{lr.out, token}, f.Path...)...)] {
				w.folderUnwritable(folderKey, "", w.faultIdentity(ref, nil))
				return nil
			}
			began := time.Now()
			data, fetchErr := client.MIME(ctx, mailbox, ref.ID)
			if fetchErr != nil {
				return w.fetchFault(ctx, ref, folderKey, fetchErr, time.Since(began))
			}
			// A 200 that is not a message (an HTML page, an empty body: a captive portal
			// or a proxy answering for the service) is a fault of this item's download,
			// not a message the parser would accept headerless (S3-AO2). It is not a
			// success, so it does not end the run of consecutive faults either.
			if !looksLikeMessage(data) {
				return w.fetchFault(ctx, ref, folderKey, errNotAMessage, time.Since(began))
			}
			w.br.attempt(attemptOK, 0) // a download that succeeded ends the run of consecutive faults
			m := parseRFC822(data)
			if m == nil {
				// Unreachable today (parseMessage never returns nil), and silent if it
				// were reached: it is an export-failed gap (§4.8 item 6).
				w.observeItem(ref, nil, folderKey, "export-failed", state.ClassFillable, "the message could not be parsed")
				return nil
			}
			stub := parseStub(m) // before applyGraphState gives the stub its received time
			applyGraphState(m, ref)
			if stub && w.stub(ref, m, folderKey) {
				return nil
			}
			// A panic exporting one crafted message must not abort the mailbox (R10).
			return w.exportOne(ref, f, folderKey, m, checkpoint)
		})
		if err != nil {
			var wa *walkAbort
			if errors.As(err, &wa) {
				return wa.err
			}
			// The listing itself failed. A fault that ends the mailbox does; any other
			// costs this folder only, and the walk goes on with the next one (§4.8.4).
			if lerr := w.listingFault(ctx, f, folderKey, err); lerr != nil {
				return lerr
			}
			continue
		}
		// This folder was fully walked (its listing consumed with no error): a
		// message recorded under it that we did NOT re-observe this run has left
		// the live mailbox and is in-scope for the gone sweep below (§3.4).
		walked.Mark(folderKey)
		w.folderRead(f, folderKey)
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
	// A folder that could not be listed defers gone-detection for the whole mailbox: a
	// message that moved into it would be swept from the folder it left (§4.8.3 rule 1,
	// H2, FC4). A fault on one item defers it only for the records that fault could
	// hide (faultIdentity). Deferred, never wrong: the next run that meets no such
	// fault sweeps.
	var gone []string
	if !w.folderFault {
		spare := w.spare
		spare.LegacyPlaceholders = true
		gone = manifest.SweepGoneSparing(token, walked, runAt, spare)
	}
	// A completed walk closes the gaps of the folders it read that it did not meet
	// again: "no longer in the source" (§4.3 step 3). A folder that failed to list
	// may hold an item that moved there, so then it closes nothing.
	if !w.folderFault {
		lr.closedGone += manifest.CloseUnobserved(token, w.covered, runAt, exp.Since)
	}
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
