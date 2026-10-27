package server

import (
	"errors"
	"html/template"
	"net/http"
	"path/filepath"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// Source is the archive a reader serves (design-encrypted-archives-phase3 §7.2):
// a plaintext archive's folder (Folder), or an encrypted archive's sealed record,
// which internal/keyring builds so that no key leaves it.
type Source interface {
	// UseIndex runs f on the search index (nil when there is none) while the
	// archive can be read; once it was locked it returns ErrLocked and f does not
	// run.
	UseIndex(f func(*index.Index) error) error
	// Files serves the archive's pages, originals and attachment zips under
	// /files/ (the prefix stripped) — and nothing else the archive holds.
	Files() http.Handler
	// GoBack is what the go-back view reads; ErrLocked once the archive was locked.
	GoBack() (GoBack, error)
	// LockedNote is what a locked archive's reader says, in plain words, and how to
	// unlock it (§7.1); "" while the archive can be read.
	LockedNote() string
}

// ErrLocked is a read of an encrypted archive that was locked.
var ErrLocked = errors.New("server: this encrypted archive is locked")

// ErrUnavailable is a read an encrypted archive cannot answer now, for a reason
// its text gives in plain words — search unavailable, a backup running — never a
// message's damage.
var ErrUnavailable = errors.New("server: unavailable")

// Timeline is how much of the history timeline a go-back view can use.
type Timeline int

const (
	TimelineAvailable   Timeline = iota
	TimelinePartial              // damaged or lost in part: past dates may be incomplete
	TimelineUnavailable          // none: only the current mailbox is shown
)

// GoBack is what the go-back view reads from a Source.
type GoBack struct {
	Timeline Timeline
	Events   []state.HistoryEvent
	// Records is the archive's manifest; nil when it could not be read.
	Records *state.Manifest
	// Present reports whether a record's message is in the archive — the
	// intersection that keeps a removed message out of every date (R21).
	Present func(state.Record) bool
	// Encrypted words the notes for an encrypted archive, which has no timeline
	// file to name.
	Encrypted bool
}

// Folder is a plaintext archive's folder, searched through ix: today's reader,
// unchanged (MA-81..84, R19, P9).
func Folder(outDir string, ix *index.Index) Source { return folder{outDir, ix} }

type folder struct {
	dir string
	ix  *index.Index
}

func (f folder) UseIndex(fn func(*index.Index) error) error { return fn(f.ix) }

func (f folder) Files() http.Handler {
	return pinFileTypes(http.FileServer(allowedFiles{dir: f.dir}))
}

func (f folder) LockedNote() string { return "" }

func (f folder) GoBack() (GoBack, error) {
	hpath := filepath.Join(f.dir, state.HistoryName)
	stat, statErr := state.HistoryStatus(hpath)
	g := GoBack{Present: func(r state.Record) bool { return onDisk(f.dir, r.Path) }}
	switch {
	case !stat.Exists:
		g.Timeline = TimelineUnavailable
	case statErr != nil || !stat.Clean():
		g.Timeline = TimelinePartial
	}
	// A read error yields no events: the availability above already tells the
	// reader the timeline is partial or unavailable.
	g.Events, _ = state.ReadHistory(hpath)
	if m, err := state.Load(filepath.Join(f.dir, ".mailarchive-manifest.json")); err == nil {
		g.Records = m
	}
	return g, nil
}

// serveLocked answers a request to a locked archive: 503, the source's note, and
// no script.
func serveLocked(w http.ResponseWriter, note string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = lockedTmpl.Execute(w, note)
}

var lockedTmpl = template.Must(template.New("locked").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Locked — Archive</title>
<style>:root{color-scheme:light dark}body{margin:0;font:15px system-ui,-apple-system,Segoe UI,Roboto,sans-serif}main{max-width:640px;margin:60px auto;padding:0 16px}</style>
</head><body><main><h1>This encrypted archive is locked</h1><p>{{.}}</p></main></body></html>`))
