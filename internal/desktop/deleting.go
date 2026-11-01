package desktop

import (
	"context"
	"os"
	"sync"
)

// The deletes under way in this process (design-archive-delete §5.3, slice 3 server side;
// del-s3a-A2-03, A2-04, A3-01, A3-02, A4-03, MA-979). A delete runs inside its request for as
// long as it takes — minutes for a large archive on a USB or network drive — and until this
// record nothing said so: the row showed "Delete not finished" with Delete offered, a second
// Delete was told a backup was running, an export or a preview of the folder could start while
// its files were removed, and Shut down said the archives were untouched while it cut the delete
// off. Each delete is recorded from before its first step until its result is held.

// deletesUnderWay are the deletes running now.
type deletesUnderWay struct {
	mu    sync.Mutex
	next  int
	items map[int]*deleteUnderWay
}

// deleteUnderWay is one delete: the folder planned, the row, the folder's identity at the start,
// and done, closed when it ends.
type deleteUnderWay struct {
	folder, row string
	fi          os.FileInfo
	done        chan struct{}
}

func newDeletesUnderWay() *deletesUnderWay {
	return &deletesUnderWay{items: map[int]*deleteUnderWay{}}
}

// tryBegin records a delete of folder (planned from row) as under way, unless one of that folder
// is (ok false); end, called once it has ended, takes it off.
func (u *deletesUnderWay) tryBegin(folder, row string) (end func(), ok bool) {
	it := &deleteUnderWay{folder: folder, row: row, fi: statOrNil(folder), done: make(chan struct{})}
	u.mu.Lock()
	for _, o := range u.items {
		if o.is(folder, it.fi) || o.is(row, nil) {
			u.mu.Unlock()
			return nil, false
		}
	}
	u.next++
	id := u.next
	u.items[id] = it
	u.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			u.mu.Lock()
			delete(u.items, id)
			u.mu.Unlock()
			close(it.done)
		})
	}, true
}

// is reports whether this delete is of the folder at path (fi: its identity, nil when unknown):
// by its spelling as planned or as its row, or by file identity.
func (it *deleteUnderWay) is(path string, fi os.FileInfo) bool {
	if path == "" {
		return false
	}
	_, key := keyOf(path)
	if _, k := keyOf(it.folder); k == key {
		return true
	}
	if _, k := keyOf(it.row); it.row != "" && k == key {
		return true
	}
	return fi != nil && it.fi != nil && os.SameFile(fi, it.fi)
}

// of is the delete under way of the folder at path, or nil: the same folder by its spelling
// (keyOf) as planned or as its row, or by file identity while it can be read.
func (u *deletesUnderWay) of(path string) *deleteUnderWay {
	if u == nil || path == "" {
		return nil
	}
	u.mu.Lock()
	items := make([]*deleteUnderWay, 0, len(u.items))
	for _, it := range u.items {
		items = append(items, it)
	}
	u.mu.Unlock()
	if len(items) == 0 {
		return nil
	}
	fi := statOrNil(path)
	for _, it := range items {
		if it.is(path, fi) {
			return it
		}
	}
	return nil
}

// any is a delete under way, or nil.
func (u *deletesUnderWay) any() *deleteUnderWay {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, it := range u.items {
		return it
	}
	return nil
}

// wait waits until no delete is under way, or ctx ends.
func (u *deletesUnderWay) wait(ctx context.Context) error {
	for {
		it := u.any()
		if it == nil {
			return nil
		}
		select {
		case <-it.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// The words of a delete under way (builder's words, for the operator).
const (
	deletingRowWhy   = "This archive is being deleted. Its result shows on this page when the delete ends."
	deletingRefusal  = "This archive is being deleted. Wait for that delete's result on this page; nothing else was changed."
	deletingShutdown = "A delete is running (%s). Shut down when its result shows on this page: stopping MailArchive now would leave that delete not finished. Nothing was stopped."
	deletingExport   = "This archive is being deleted, so it cannot be exported. Nothing was exported."
	deletingPreview  = "This archive is being deleted, so it cannot be previewed. Nothing was read."
	shuttingDownText = "MailArchive Desktop is shutting down, so nothing was deleted."
)
