package desktop

import (
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/seal"
)

// This file wires the folder-only archive index (internal/archindex, the
// archive-manager Slice-1 leaf) into the Desktop. The Desktop is the index's sole
// maintainer (operator C2 decision, docs/design-archive-manager.md rev 2 §4/§5.1,
// SD-6): it folds the folders it already knows about into the index and reads the
// list back for the "Your archives" surface (UX-1, §13). Every per-archive fact
// shown here — whether an archive is the current one, whether it is reachable, what
// it was made from — is derived LIVE, never cached in the index, so nothing here can
// drift against the archive itself.

// ArchiveRow is one row of the Desktop's archive list: a known archive folder plus
// the three facts derived live for it. Current follows the active pointer
// (desktop-settings out), NEVER a field in the index — the active-archive invariant.
// Available and Kind come from archindex's SAFE bounded read of the archive.
type ArchiveRow struct {
	Path      string
	Current   bool   // this folder is the archive in use (effectiveOut)
	Available bool   // reachable right now and still a readable archive
	Sealed    bool   // paused (hard read-only) — show Resume, not Switch/Pause
	Kind      string // local | m365 | mixed | empty; "" when unavailable
}

// KindLabel is the short, plain-words form of Kind for the archive list ("" when
// the archive's kind is unknown, e.g. the folder is unavailable).
func (r ArchiveRow) KindLabel() string {
	switch r.Kind {
	case kindLocal:
		return "local mail files"
	case kindM365:
		return "Microsoft 365"
	case kindMixed:
		return "Microsoft 365 + local files"
	case kindEmpty:
		return "empty"
	}
	return ""
}

// archivePointers are the archive folders the Desktop already knows about from its
// own config, gathered for a reconcile: the archive in use (effectiveOut), the
// wizard's last-touched breadcrumb (Config.LastArchive) and the folder a weekly
// backup is named for (Settings.BackupFor). archindex reads none of these itself —
// it is handed the list, staying a leaf. Blank and duplicate pointers are harmless
// (Reconcile skips them).
func (cfg Config) archivePointers() []string {
	var ps []string
	add := func(p string) {
		if p = strings.TrimSpace(p); p != "" {
			ps = append(ps, p)
		}
	}
	add(cfg.effectiveOut())
	if cfg.LastArchive != nil {
		add(cfg.LastArchive())
	}
	add(cfg.settings().BackupFor)
	return ps
}

// reconcileArchives folds the Desktop's known pointers into the folder-only index
// and returns it. The Desktop is the index's sole maintainer (SD-6): it writes the
// file back ONLY when the reconcile changed the membership, so rendering the list
// does not churn the file (or fsync) on every page load — Reconcile is monotone, so
// a longer entry list is the one signal that a new archive folder was imported. A
// write failure is returned but never loses the in-memory list the caller renders
// from. IndexPath empty (the informational shell, a test that opts out) means no
// index: an empty one is returned and nothing is written.
func (cfg Config) reconcileArchives(now time.Time) (archindex.Index, error) {
	if strings.TrimSpace(cfg.IndexPath) == "" {
		return archindex.Index{Version: archindex.Version}, nil
	}
	before, err := archindex.ReadFile(cfg.IndexPath)
	if err != nil {
		return archindex.Index{Version: archindex.Version}, err
	}
	after := archindex.Reconcile(before, cfg.archivePointers(), now)
	if len(after.Entries) != len(before.Entries) {
		if werr := archindex.WriteFile(cfg.IndexPath, after); werr != nil {
			return after, werr
		}
	}
	return after, nil
}

// archiveRows turns an index into the list the Desktop shows. The row whose folder
// is the active pointer (effectiveOut) is the only Current one, so changing out
// moves Current with no index write; a row whose folder is unreachable or no longer
// an archive reads Available=false (shown greyed, never dropped — SD-6) with an
// empty Kind; an available row's Kind is derived live by archindex's safe read
// (state.LoadFrom inside an os.Root), never the Desktop's state.Load-based
// archiveKind, which would resolve an untrusted manifest's store ids on disk (SD-1).
func (cfg Config) archiveRows(idx archindex.Index) []ArchiveRow {
	current := cleanPath(cfg.effectiveOut())
	rows := make([]ArchiveRow, 0, len(idx.Entries))
	for _, e := range idx.Entries {
		avail := archindex.Available(e)
		kind := ""
		if avail {
			kind = archindex.Kind(e.Path)
		}
		rows = append(rows, ArchiveRow{
			Path:      e.Path,
			Current:   current != "" && cleanPath(e.Path) == current,
			Available: avail,
			Sealed:    seal.IsSealed(e.Path),
			Kind:      kind,
		})
	}
	return rows
}

// cleanPath normalizes a folder path for identity comparison, matching archindex's
// own cleaning so a Desktop pointer and an index entry for the same folder compare
// equal.
func cleanPath(p string) string {
	if p = strings.TrimSpace(p); p == "" {
		return ""
	}
	return filepath.Clean(p)
}
