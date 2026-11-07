package tbrestore

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"strings"
	"time"
)

// The two hidden files inside a restore's container (design §3.4 notes): the marker,
// one short JSON line naming the restore, and the made-list, one JSON line per entry
// the restore makes, each appended and fsynced BEFORE that entry is created, so after
// any crash it names every entry the restore made and nothing it did not try to make
// (foldcheck M1). Both are dot-named, so neither Thunderbird nor a backup takes them
// for a mailbox.
const (
	MarkerName   = ".mailarchive-restore"
	MadeListName = ".mailarchive-restore-made"
	// markerMax is the marker read's bound, the backup's 4 KiB (§7.2 note).
	markerMax = 4 << 10
	// madeListMax bounds the made-list read (rule 1).
	madeListMax = 16 << 20
)

// Marker is a restore's marker.
type Marker struct {
	MailArchive string `json:"mailarchive"`
	ID          string `json:"id"`
	Started     string `json:"started"`
	Profile     string `json:"profile"`
}

func markerLine(id, profileKey string, started time.Time) []byte {
	b, _ := json.Marshal(Marker{MailArchive: "restore", ID: id, Started: started.UTC().Format(time.RFC3339), Profile: profileKey})
	return append(b, '\n')
}

// readMarker reads the marker in container: ok only for a regular file of at most 4 KiB
// that parses as a restore's marker. Never through a link.
func readMarker(f FS, container string) (Marker, bool) {
	p := container + "/" + MarkerName
	fi, err := f.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > markerMax {
		return Marker{}, false
	}
	r, err := f.Open(p)
	if err != nil {
		return Marker{}, false
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, markerMax+1))
	if err != nil || len(data) > markerMax {
		return Marker{}, false
	}
	var m Marker
	if json.Unmarshal(bytes.TrimSpace(data), &m) != nil || m.MailArchive != "restore" || m.ID == "" {
		return Marker{}, false
	}
	return m, true
}

// madeEntry is one line of the made-list: "f" a file, "d" a folder; the path relative to
// the container, parts joined by "/".
type madeEntry struct {
	K string `json:"k"`
	P string `json:"p"`
}

func madeLine(kind, path string) []byte {
	b, _ := json.Marshal(madeEntry{K: kind, P: path})
	return append(b, '\n')
}

// errListUnreadable is a made-list that cannot be parsed apart from a torn last line.
var errListUnreadable = errors.New("the list of what the restore wrote cannot be read")

// readMadeList reads container's made-list (rule 1): bounded at 16 MiB; a missing list
// (a crash between the marker and the list) lists nothing; a torn last line is passed
// over (its entry was never created: the create comes after the fsync); every path must
// be relative with no empty, ".", ".." part and no "\" or ":", and every kind "f" or
// "d", or the whole list is unreadable.
func readMadeList(f FS, container string) ([]madeEntry, error) {
	p := container + "/" + MadeListName
	fi, err := f.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > madeListMax {
		return nil, errListUnreadable
	}
	r, err := f.Open(p)
	if err != nil {
		return nil, errListUnreadable
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, madeListMax+1))
	if err != nil || len(data) > madeListMax {
		return nil, errListUnreadable
	}
	lines := bytes.Split(data, []byte("\n"))
	var out []madeEntry
	for i, b := range lines {
		if len(b) == 0 {
			continue
		}
		var e madeEntry
		if json.Unmarshal(b, &e) != nil {
			if i == len(lines)-1 {
				continue // the torn last line: no newline after it
			}
			return nil, errListUnreadable
		}
		if (e.K != "f" && e.K != "d") || !safeRel(e.P) {
			return nil, errListUnreadable
		}
		out = append(out, e)
	}
	return out, nil
}

// safeRel reports a relative path of plain parts.
func safeRel(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\:\x00") || strings.HasPrefix(p, "/") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
