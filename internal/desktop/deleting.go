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
	// blocked, when not "", is why no delete may begin: an uninstall is removing (or has
	// removed) the settings folder deletions.log lives in (del-s5-A2-02).
	blocked string
	// holds are the Pause and Resume actions running now, each on its folder: no delete of that
	// folder begins until it ends (the residual fixes' adversarial pass, finding 2).
	holds map[int]*deleteUnderWay
}

// hold records an action on the folder at path (Pause, Resume) as running, unless a delete of
// that folder is under way (ok false); release, called once it has ended, takes it off. It is
// taken under the lock tryBegin takes, so a check and the action it guards are one step: a
// delete beginning after the check is refused while the action runs, never run beside it.
func (u *deletesUnderWay) hold(path string) (release func(), ok bool) {
	if u == nil {
		return func() {}, true
	}
	h := &deleteUnderWay{folder: path, fi: statOrNil(path)}
	u.mu.Lock()
	for _, it := range u.items {
		if it.is(path, h.fi) {
			u.mu.Unlock()
			return nil, false
		}
	}
	if u.holds == nil {
		u.holds = map[int]*deleteUnderWay{}
	}
	u.next++
	id := u.next
	u.holds[id] = h
	u.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			u.mu.Lock()
			delete(u.holds, id)
			u.mu.Unlock()
		})
	}, true
}

// heldOn reports whether a Pause or Resume of the folder (planned from row) is running now.
func (u *deletesUnderWay) heldOn(folder, row string) bool {
	fi := statOrNil(folder)
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.heldLocked(folder, row, fi)
}

// heldLocked is heldOn under u.mu.
func (u *deletesUnderWay) heldLocked(folder, row string, fi os.FileInfo) bool {
	for _, h := range u.holds {
		if h.is(folder, fi) || h.is(row, nil) {
			return true
		}
	}
	return false
}

// block stops any delete from beginning, saying why, unless one is under way (that delete is
// returned) or deletes are already blocked (that reason is returned): then nothing is set. An
// uninstall's Remove takes it before it removes anything, so a second Remove at the same moment,
// or one from a tab still showing the plan after an uninstall ended, is refused and cannot lift
// the block another holds (del-s5 re-verification, residual 1).
func (u *deletesUnderWay) block(why string) (*deleteUnderWay, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.blocked != "" {
		return nil, u.blocked
	}
	for _, it := range u.items {
		return it, ""
	}
	u.blocked = why
	return nil, ""
}

// unblock lets deletes begin again after an uninstall that was refused or stopped. It lifts only
// uninstallingText: once an uninstall has removed the settings (uninstalledText), deletes stay
// refused for as long as this Desktop runs.
func (u *deletesUnderWay) unblock() {
	u.mu.Lock()
	if u.blocked == uninstallingText {
		u.blocked = ""
	}
	u.mu.Unlock()
}

// uninstalled blocks every delete for good: the uninstall holding the block removed the settings.
func (u *deletesUnderWay) uninstalled() {
	u.mu.Lock()
	u.blocked = uninstalledText
	u.mu.Unlock()
}

// blockedWhy is why no delete may begin now, "" when one may.
func (u *deletesUnderWay) blockedWhy() string {
	if u == nil {
		return ""
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.blocked
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
// is, or a Pause or Resume of it is running (ok false); end, called once it has ended, takes it off.
func (u *deletesUnderWay) tryBegin(folder, row string) (end func(), ok bool) {
	it := &deleteUnderWay{folder: folder, row: row, fi: statOrNil(folder), done: make(chan struct{})}
	u.mu.Lock()
	if u.blocked != "" {
		u.mu.Unlock()
		return nil, false
	}
	for _, o := range u.items {
		if o.is(folder, it.fi) || o.is(row, nil) {
			u.mu.Unlock()
			return nil, false
		}
	}
	if u.heldLocked(folder, row, it.fi) {
		u.mu.Unlock()
		return nil, false
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
	items := u.list()
	if len(items) == 0 {
		return nil
	}
	return match(items, path, statOrNil(path))
}

// ofStat is of with the folder's identity already asked of the file system (fi, nil when it
// cannot be read): it asks nothing of the disk, so it is the check made under a lock other
// requests wait on — an offline network folder then stalls only the request that asked it
// (del-s3a re-verification, R5).
func (u *deletesUnderWay) ofStat(path string, fi os.FileInfo) *deleteUnderWay {
	if u == nil || path == "" {
		return nil
	}
	return match(u.list(), path, fi)
}

// list are the deletes under way now.
func (u *deletesUnderWay) list() []*deleteUnderWay {
	u.mu.Lock()
	defer u.mu.Unlock()
	items := make([]*deleteUnderWay, 0, len(u.items))
	for _, it := range u.items {
		items = append(items, it)
	}
	return items
}

// match is the one of items that is the folder at path (fi: its identity, nil when unknown).
func match(items []*deleteUnderWay, path string, fi os.FileInfo) *deleteUnderWay {
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
	// A Pause or Resume of the folder running when Delete permanently is clicked; the plan waits.
	pausingRefusal = "This archive is being paused or resumed. Click Delete permanently again when that ends; nothing was deleted."
	// An uninstall and a delete never overlap (del-s5-A2-02): uninstall removes deletions.log and
	// the settings a delete writes.
	uninstallingText  = "MailArchive is being uninstalled from this computer, so nothing was deleted or changed."
	uninstalledText   = "MailArchive was uninstalled from this computer, so nothing was deleted or changed. Install MailArchive again first."
	deletingUninstall = "A delete is running (%s). Uninstall when its result shows on this page: uninstalling now would remove the deletion log that delete writes. Nothing was removed."
)
