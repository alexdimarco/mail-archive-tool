package server

import (
	"bytes"
	"context"
	"errors"
	"html"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"mail-archive-tool/internal/memguard"
)

// SealedOpen opens one archive-relative path of an encrypted archive for a
// request: its bytes, and a release to call once they are served, which gives back
// their share of the memory budget. A path the archive does not hold is
// fs.ErrNotExist; an archive locked meanwhile is ErrLocked; one the budget could
// never hold is memguard.ErrTooLarge.
type SealedOpen func(r *http.Request, rel string) (data []byte, release func(), err error)

// SealedFiles serves /files/ (the prefix stripped) for an encrypted archive's
// source (design-encrypted-archives-phase3 §7.2): the very name rules a plaintext
// folder's reader applies (allowedPath, fileAllowed), a folder served through its
// index.html — a path ending in index.html sent to the folder, a folder named
// without its slash sent to it with one, as http.FileServer does — each file's type
// pinned, and the bytes served from memory under a write deadline, so a client
// that stops reading cannot hold the memory budget past it (§3.2).
func SealedFiles(open SealedOpen, lockedNote func() string, writeTimeout time.Duration) http.Handler {
	return pinFileTypes(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		segs, ok := allowedPath(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		folder := len(segs) == 0 || strings.HasSuffix(name, "/")
		switch {
		case folder:
			segs = append(segs, "index.html")
		case segs[len(segs)-1] == "index.html":
			localRedirect(w, r, "./")
			return
		case !fileAllowed(len(segs), segs[len(segs)-1]):
			// A folder named without its slash is sent to it, as FileServer does.
			_, release, err := open(r, strings.Join(segs, "/")+"/index.html")
			if err == nil {
				release()
				localRedirect(w, r, segs[len(segs)-1]+"/")
				return
			}
			if !errors.Is(err, fs.ErrNotExist) && sealedError(w, err, lockedNote) {
				return
			}
			http.NotFound(w, r)
			return
		}
		data, release, err := open(r, strings.Join(segs, "/"))
		if err != nil {
			sealedError(w, err, lockedNote)
			return
		}
		defer release()
		data = withDownloadLine(segs[len(segs)-1], data)
		http.ServeContent(progressWriter{w, http.NewResponseController(w), writeTimeout}, r, segs[len(segs)-1], time.Time{}, bytes.NewReader(data))
	}))
}

// DownloadNotEncrypted is K9's line (design-encrypted-archives-phase3 K9): what a
// reader of an encrypted archive is told beside a download — an attachment zip or an
// original .eml leaves the archive as a plain file (P3-8, B8).
const DownloadNotEncrypted = "A downloaded file is not encrypted: it is an ordinary copy on this computer."

// withDownloadLine is a page of an encrypted archive with K9's line where it offers a
// download. A message page gets it beside each of its own download links — the
// attachment zip and the original .eml the page sits beside, as the export writes them
// (internal/export: base.html, base-attachments.zip, base.eml, each href HTML-escaped).
// A folder page that lists attachment zips (internal/pages' paperclip column) gets it
// once, above its list. Any other file is returned as it is. The line is fixed text of
// ours; a mail that spells the same link only gets the line beside it too.
func withDownloadLine(leaf string, data []byte) []byte {
	base, ok := strings.CutSuffix(leaf, ".html")
	if !ok {
		return data
	}
	page := string(data)
	if base == "index" || isFolderPageName(leaf) {
		// Everything before the list is the page's own markup: the folder's name in it is
		// escaped, so it cannot spell the wrap div.
		const zipCell, wrap = `title="attachments">📎</a>`, `<div class="wrap">`
		i := strings.Index(page, wrap)
		if i < 0 || !strings.Contains(page, zipCell) {
			return data
		}
		i += len(wrap)
		return []byte(page[:i] + `<p class="muted mailarchive-k9">` + DownloadNotEncrypted + `</p>` + page[i:])
	}
	note := ` <span class="mailarchive-k9">(` + DownloadNotEncrypted + `)</span>`
	changed := false
	for _, link := range []string{
		`<a href="` + html.EscapeString(base+"-attachments.zip") + `">download zip</a>`,
		`<a href="` + html.EscapeString(base+".eml") + `">Original message (.eml)</a>`,
	} {
		if strings.Contains(page, link) {
			page = strings.ReplaceAll(page, link, link+note)
			changed = true
		}
	}
	if !changed {
		return data
	}
	return []byte(page)
}

// isFolderPageName reports a folder page's own name, index-N.html: a page of links,
// never a message's.
func isFolderPageName(leaf string) bool {
	n, ok := strings.CutPrefix(strings.TrimSuffix(leaf, ".html"), "index-")
	return ok && n != "" && strings.Trim(n, "0123456789") == ""
}

// sealedError answers a sealed open that failed, in its own words: the locked
// page; 404; a message too large; a reason it is unavailable now; a cancelled
// request nothing; anything else a message that could not be read. It reports
// whether it answered.
func sealedError(w http.ResponseWriter, err error, lockedNote func() string) bool {
	switch {
	case errors.Is(err, ErrLocked):
		serveLocked(w, lockedNote())
	case errors.Is(err, fs.ErrNotExist):
		http.Error(w, "404 page not found", http.StatusNotFound)
	case errors.Is(err, memguard.ErrTooLarge):
		http.Error(w, "This message is "+strings.TrimPrefix(err.Error(), "memguard: ")+".", http.StatusServiceUnavailable)
	case errors.Is(err, ErrUnavailable):
		http.Error(w, strings.TrimPrefix(err.Error(), ErrUnavailable.Error()+": "), http.StatusServiceUnavailable)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
	default:
		http.Error(w, "This message could not be read from the archive: "+err.Error(), http.StatusInternalServerError)
	}
	return true
}

// progressWriter renews the write deadline before each write, so a response
// that makes progress runs as long as it needs, while one whose client stops
// reading ends within timeout and gives back its memory budget (§3.2).
type progressWriter struct {
	http.ResponseWriter
	rc      *http.ResponseController
	timeout time.Duration
}

func (p progressWriter) Write(b []byte) (int, error) {
	_ = p.rc.SetWriteDeadline(time.Now().Add(p.timeout))
	return p.ResponseWriter.Write(b)
}

// localRedirect sends a relative redirect, as http.FileServer does, keeping the
// query.
func localRedirect(w http.ResponseWriter, r *http.Request, to string) {
	if q := r.URL.RawQuery; q != "" {
		to += "?" + q
	}
	w.Header().Set("Location", to)
	w.WriteHeader(http.StatusMovedPermanently)
}
