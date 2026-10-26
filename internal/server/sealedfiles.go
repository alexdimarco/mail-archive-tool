package server

import (
	"bytes"
	"context"
	"errors"
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
			if _, release, err := open(r, strings.Join(segs, "/")+"/index.html"); err == nil {
				release()
				localRedirect(w, r, segs[len(segs)-1]+"/")
				return
			}
			http.NotFound(w, r)
			return
		}
		data, release, err := open(r, strings.Join(segs, "/"))
		switch {
		case err == nil:
		case errors.Is(err, ErrLocked):
			serveLocked(w, lockedNote())
			return
		case errors.Is(err, fs.ErrNotExist):
			http.NotFound(w, r)
			return
		case errors.Is(err, memguard.ErrTooLarge):
			http.Error(w, "This message is "+strings.TrimPrefix(err.Error(), "memguard: ")+".", http.StatusServiceUnavailable)
			return
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return
		default:
			http.Error(w, "This message could not be read from the archive: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer release()
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(writeTimeout))
		http.ServeContent(w, r, segs[len(segs)-1], time.Time{}, bytes.NewReader(data))
	}))
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
