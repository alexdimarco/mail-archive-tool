package desktop

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
)

// The destination chooser (extraction design §4.8): the OS folder picker, opened in this
// process, plus the typed path. No endpoint lists directories: the picker adds no new
// read surface (Q1). zenity is new to the mailarchive-desktop binary, so on Windows its
// in-process IFileOpenDialog loads shell extensions into the process that holds the token
// and runs the export: Browse is therefore refused while an export runs, and a typed path
// still works (XC31, MA-529).

// pickerBudget is how long one folder dialog may stay open.
const pickerBudget = 10 * time.Minute

// pickFolder is the zenity seam: the OS folder dialog, dismissed through ctx.
var pickFolder = func(ctx context.Context, title string) (string, error) {
	return zenity.SelectFile(zenity.Directory(), zenity.Title(title), zenity.Context(ctx))
}

// browseAction opens the native folder picker and returns {path}, {canceled} or
// {unavailable}. One dialog at a time; never while an export runs.
func (d *dashboard) browseAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
	}
	if !d.readBody(w, r, &in) {
		return
	}
	if d.exportLive() {
		d.reply(w, map[string]any{"ok": false, "disabled": true, "error": "Browse is off while an export runs — type the folder's path in the box instead."})
		return
	}
	ws := d.ws
	ws.mu.Lock()
	if ws.pickerBusy {
		ws.mu.Unlock()
		d.reply(w, map[string]any{"ok": false, "busy": true, "error": "A folder picker is already open — it may be behind this window."})
		return
	}
	ws.pickerBusy = true
	ws.mu.Unlock()
	defer func() {
		ws.mu.Lock()
		ws.pickerBusy = false
		ws.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(d.life.Context(), pickerBudget)
	defer cancel()
	stop := context.AfterFunc(r.Context(), cancel) // the page went away: dismiss the dialog
	defer stop()
	title := strings.TrimSpace(in.Title)
	if title == "" || len(title) > 120 {
		title = "Choose the folder to export into"
	}
	path, err := d.pick(ctx, title)
	switch {
	case err == nil && path != "":
		d.reply(w, map[string]any{"ok": true, "path": path})
	case errors.Is(err, zenity.ErrCanceled), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), err == nil:
		d.reply(w, map[string]any{"ok": true, "canceled": true})
	default:
		ref := refusalN()
		d.reply(w, map[string]any{"ok": false, "unavailable": true, "id": ref.ID, "error": ref.Text})
	}
}

// mkdirAction is Create folder: exactly one new, sanitized directory under an existing
// destination outside the archive, made through the handle on the parent its checks
// opened (design §4.8, W1).
func (d *dashboard) mkdirAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if !d.readBody(w, r, &in) {
		return
	}
	// With another archive folder named as the source [ruling W1], a parent inside it, or an alias of
	// it, is refused as one inside the configured archive is: Create folder's mkdir is a write too. The
	// folders the owner has named count from the moment they are named (not only while an archive
	// session for one is loaded), and the newest is the one a refusal speaks of (E4-AI4).
	archives, other := d.archives(), ""
	for _, o := range d.ws.otherPaths() {
		archives, other = append(archives, o), o
	}
	// And whatever the parent is, or lies inside, that is an archive folder, named or not: the folders the
	// workspace remembers are a handful, and a folder it forgot is still one (E4-AI4).
	for _, o := range app.ArchivesAbove(strings.TrimSpace(in.Parent)) {
		archives, other = append(archives, o), o
	}
	path, err := d.copyEnv().CreateFolderOther(strings.TrimSpace(in.Parent), in.Name, archives, other)
	if err != nil {
		d.refuse(w, *copyRefusal(err))
		return
	}
	d.reply(w, map[string]any{"ok": true, "path": path})
}

var _ = app.CopyRefusal{}
