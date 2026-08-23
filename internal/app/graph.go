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
func RunGraph(ctx context.Context, g GraphOptions, opts Options, logger *log.Logger) (Result, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	if len(g.Mailboxes) == 0 {
		return Result{}, errors.New("no mailboxes specified (-mailbox is required)")
	}
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return Result{}, fmt.Errorf("create output dir: %w", err)
	}

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
	}

	var idx *index.Index
	if opts.Index {
		idxPath := filepath.Join(opts.Out, "search.db")
		idx, err = index.Open(idxPath)
		if err != nil {
			return Result{}, fmt.Errorf("open search index: %w", err)
		}
		defer idx.Close()
		exp.OnExported = func(store string, folderPath []string, m *model.Message, relPath, key string) {
			if addErr := idx.Add(store, folderPath, m, relPath, key); addErr != nil {
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

	result := Result{Files: len(g.Mailboxes)}
	var failures int
	for _, mbx := range g.Mailboxes {
		runErr := runGraphMailbox(ctx, client, exp, manifest, opts.Mode, mbx, logger)

		if saveErr := manifest.Save(); saveErr != nil {
			logger.Printf("warning: could not save manifest: %v", saveErr)
		}
		if idx != nil {
			if flushErr := idx.Flush(); flushErr != nil {
				logger.Printf("warning: index flush: %v", flushErr)
			}
		}
		if errors.Is(runErr, context.Canceled) {
			result.Stats = exp.Stats
			result.ManifestSize = manifest.Len()
			result.Indexed = indexCount(idx)
			return result, context.Canceled
		}
		if runErr != nil {
			failures++
			logger.Printf("error: mailbox %s: %v", mbx, runErr)
		}
	}

	if idx != nil && opts.Pages {
		if pErr := pages.Generate(opts.Out, idx, logger); pErr != nil {
			logger.Printf("warning: folder pages: %v", pErr)
		}
	}
	if len(exp.Issues) > 0 {
		reportPath := filepath.Join(opts.Out, "attachments-report.tsv")
		if wErr := writeIssuesReport(reportPath, exp.Issues); wErr != nil {
			logger.Printf("warning: could not write verification report: %v", wErr)
		} else {
			result.ReportPath = reportPath
			logger.Printf("Verification: %d attachment/inline issue(s) recorded in %s", len(exp.Issues), reportPath)
		}
	}

	result.Stats = exp.Stats
	result.ManifestSize = manifest.Len()
	result.Indexed = indexCount(idx)
	result.Issues = len(exp.Issues)
	if failures > 0 {
		return result, fmt.Errorf("%d mailbox(es) failed", failures)
	}
	return result, nil
}

// runGraphMailbox walks one mailbox's folders and messages, exporting each.
func runGraphMailbox(ctx context.Context, client *graph.Client, exp *export.Exporter, manifest *state.Manifest, mode export.Mode, mailbox string, logger *log.Logger) error {
	logger.Printf("Reading mailbox %s via Microsoft Graph", mailbox)
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
			// Incremental fast-path: skip a message already archived, matched by
			// its Internet-Message-ID, WITHOUT downloading the body (R17). Only
			// possible when the id is present; otherwise fall through and let the
			// exporter dedup on the parsed content hash.
			if mode == export.Incremental && ref.InternetMessageID != "" {
				if manifest.Has(state.Key(folderKey, "mid:"+ref.InternetMessageID)) {
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
			// A panic exporting one crafted message must not abort the mailbox (R10).
			return func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						logger.Printf("warning: recovered on a message in %s (skipped): %v", folderKey, r)
						err = nil
					}
				}()
				_, exportErr := exp.Export(mailbox, f.Path, m)
				return exportErr
			}()
		})
		if err != nil {
			return err
		}
	}
	return nil
}
