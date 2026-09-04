package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

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
		exp.OnExported = func(store string, folderPath []string, m *model.Message, relPath, key string) {
			if addErr := idx.Add(store, folderPath, m, relPath, key); addErr != nil {
				indexErrors++
				logger.Printf("warning: index: %v", addErr)
			}
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
		if exp.Stats.Exported-lastCheckpoint < effectiveEvery(floor, manifest.Len()) {
			return
		}
		lastCheckpoint = exp.Stats.Exported
		if err := lock.StillHeld(); err != nil && lockLost == nil {
			lockLost = err
			return
		}
		commit(manifest, idx, logger)
	}

	result = Result{Files: len(g.Mailboxes)}
	var failures int
	var firstErr error
	for _, mbx := range g.Mailboxes {
		runErr := runGraphMailbox(ctx, client, exp, manifest, opts.Mode, mbx, logger, checkpoint, func() error { return lockLost })
		if lockLost != nil {
			// Lock removed/replaced mid-run: another run may own this archive now.
			// Write no shared state (manifest/index/README); the deferred recordRun
			// marks the run failed and the next incremental run reconciles the
			// message files already written (INT-CC-2/R5).
			return result, lockLost
		}
		commit(manifest, idx, logger)
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
	// Categories ride on the same widened listing; Graph is authoritative for a
	// mailbox item's classification. Copied so the message owns its slice.
	if len(ref.Categories) > 0 {
		m.Categories = append([]string(nil), ref.Categories...)
	}
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

// runGraphMailbox walks one mailbox's folders and messages, exporting each.
func runGraphMailbox(ctx context.Context, client *graph.Client, exp *export.Exporter, manifest *state.Manifest, mode export.Mode, mailbox string, logger *log.Logger, checkpoint func(), abort func() error) error {
	logger.Printf("Reading mailbox %s via Microsoft Graph", mailbox)
	// The store token for a Graph source is seeded from the CANONICAL mailbox id
	// (lower-cased/trimmed), so a re-run whose -mailbox differs only in case hits
	// the same token — the incremental fast-path key below and the exporter's key
	// still agree and no body is re-downloaded (INT-CC-1/R17). Mailbox addresses
	// are injective, so this is almost always the plain sanitized segment.
	token := manifest.Token(state.MailboxSourceID(mailbox), mailbox)
	folders, err := client.Folders(ctx, mailbox)
	if err != nil {
		return fmt.Errorf("list folders: %w", err)
	}

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
			// Incremental fast-path: skip a message already archived, matched by
			// its Internet-Message-ID, WITHOUT downloading the body (R17). Only
			// possible when the id is present; otherwise fall through and let the
			// exporter dedup on the parsed content hash. A legacy "unknown" record
			// is resolved here without a download: Graph delivers a message whole,
			// so the earlier capture is as complete as the source allows.
			if mode == export.Incremental && ref.InternetMessageID != "" {
				key := state.Key(token, folderKey, "mid:"+ref.InternetMessageID)
				if _, seen := manifest.Get(key); seen {
					if manifest.Resolve(key) {
						exp.Stats.Resolved++
					}
					exp.Stats.SkippedManifest++
					return nil
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
	}
	return nil
}
