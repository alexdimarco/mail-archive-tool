package desktop

import (
	"context"
	"fmt"
	"os"
	"strings"
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
	// partWay records that an uninstall's Remove stopped after it had begun removing (the
	// delete residuals' security pass, CON-06): the settings folder may be half removed, so
	// lifting a later Remove's block puts uninstallUnfinishedText back, never "".
	partWay bool
	// holds are the actions running now that a delete must not meet, each on its folder — Pause
	// and Resume (the residual fixes' adversarial pass, finding 2), a Switch or Save settings
	// naming it, the Scheduled backup card's Install (the delete residuals' security pass,
	// CON-01, CON-02), and Remove from list (CON-06): no delete of that folder begins until it
	// ends. Remove from list's hold also keeps an uninstall's Remove from beginning (settings).
	holds map[int]*deleteUnderWay
}

// hold records an action on the folder at path as running, unless a delete of that folder is
// under way (ok false); release, called once it has ended, takes it off. why is the delete's
// refusal while it runs. It is taken under the lock tryBegin takes, so a check and the action it
// guards are one step: a delete beginning after the check is refused while the action runs,
// never run beside it. An empty path holds nothing.
func (u *deletesUnderWay) hold(path, why string) (release func(), ok bool) {
	if u == nil || strings.TrimSpace(path) == "" {
		return func() {}, true
	}
	release, refusal := u.holdLocked(path, why, false)
	return release, refusal == ""
}

// holdSettings is hold for an action that writes the list and the settings an uninstall removes
// — Remove from list (the delete residuals' security pass, CON-06): it is refused, with the
// reason, while an uninstall blocks deletes (refusal is that reason) or a delete of the folder is
// under way (deletingRefusal); and while it runs an uninstall's Remove is refused (block), so the
// check and the writes it guards are one step, never split by an uninstall beginning between.
func (u *deletesUnderWay) holdSettings(path, why string) (release func(), refusal string) {
	if u == nil {
		return func() {}, ""
	}
	return u.holdLocked(path, why, true)
}

// holdLocked is hold and holdSettings: settings marks a hold an uninstall must wait for.
func (u *deletesUnderWay) holdLocked(path, why string, settings bool) (release func(), refusal string) {
	h := &deleteUnderWay{folder: path, fi: statOrNil(path), why: why, settings: settings}
	u.mu.Lock()
	if settings && u.blocked != "" {
		why := u.blocked
		u.mu.Unlock()
		return nil, why
	}
	for _, it := range u.items {
		if it.is(path, h.fi) {
			u.mu.Unlock()
			return nil, deletingRefusal
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
	}, ""
}

// heldWhy is the refusal of a hold on the folder (planned from row) running now — a Pause or
// Resume, a Switch, an Install — or "" when none is.
func (u *deletesUnderWay) heldWhy(folder, row string) string {
	fi := statOrNil(folder)
	u.mu.Lock()
	defer u.mu.Unlock()
	if h := u.heldLocked(folder, row, fi); h != nil {
		return h.why
	}
	return ""
}

// heldLocked is the hold on the folder (planned from row), or nil, under u.mu.
func (u *deletesUnderWay) heldLocked(folder, row string, fi os.FileInfo) *deleteUnderWay {
	for _, h := range u.holds {
		if h.is(folder, fi) || h.is(row, nil) {
			return h
		}
	}
	return nil
}

// block stops any delete from beginning, saying why, unless one is under way (that delete is
// returned), Remove from list is running (its refusal is returned), or deletes are already
// blocked (that reason is returned): then nothing is set. An uninstall's Remove takes it before it
// removes anything, so a second Remove at the same moment, or one from a tab still showing the
// plan after an uninstall ended, is refused and cannot lift the block another holds (del-s5
// re-verification, residual 1). The one block a Remove takes over is an uninstall that stopped
// part way (CON-06): running Uninstall again is how it is finished.
func (u *deletesUnderWay) block(why string) (*deleteUnderWay, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.blocked != "" && u.blocked != uninstallUnfinishedText {
		return nil, u.blocked
	}
	for _, it := range u.items {
		return it, ""
	}
	for _, h := range u.holds {
		if h.settings {
			return nil, fmt.Sprintf(unlistingUninstall, h.folder)
		}
	}
	u.blocked = why
	return nil, ""
}

// unblock lets deletes begin again after an uninstall that was refused or stopped before it
// removed anything. It lifts only uninstallingText: once an uninstall has removed the settings
// (uninstalledText), deletes stay refused for as long as this Desktop runs; and once one stopped
// part way (CON-06), lifting a later Remove's block puts uninstallUnfinishedText back.
func (u *deletesUnderWay) unblock() {
	u.mu.Lock()
	if u.blocked == uninstallingText {
		u.blocked = ""
		if u.partWay {
			u.blocked = uninstallUnfinishedText
		}
	}
	u.mu.Unlock()
}

// stoppedPartWay blocks every delete for as long as this Desktop runs, as uninstalled does, after
// an uninstall's Remove stopped once it had begun removing (the delete residuals' security pass,
// CON-06): schedules, the sign-in, working copies or settings files, deletions.log among them,
// may be gone, and a delete would write the log and the settings again into a half-removed
// settings folder. Uninstall's Remove may run again to finish it.
func (u *deletesUnderWay) stoppedPartWay() {
	u.mu.Lock()
	u.partWay = true
	if u.blocked != uninstalledText {
		u.blocked = uninstallUnfinishedText
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
// and done, closed when it ends. A hold uses folder, fi and why: the delete's refusal while it runs.
type deleteUnderWay struct {
	folder, row string
	fi          os.FileInfo
	done        chan struct{}
	why         string
	// settings marks a hold an uninstall's Remove waits for (holdSettings).
	settings bool
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
	if u.heldLocked(folder, row, it.fi) != nil {
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
	// A Switch to the folder, or a Save settings that keeps it, being saved (CON-01).
	switchingRefusal = "Settings naming this archive are being saved (a switch to it, or Save settings). Click Delete permanently again when that ends; nothing was deleted."
	// The Scheduled backup card's Install or Repair of the folder running (CON-02).
	installingRefusal = "A scheduled backup of this archive is being installed. Click Delete permanently again when that ends; nothing was deleted."
	// An uninstall and a delete never overlap (del-s5-A2-02): uninstall removes deletions.log and
	// the settings a delete writes.
	uninstallingText  = "MailArchive is being uninstalled from this computer, so nothing was deleted or changed."
	uninstalledText   = "MailArchive was uninstalled from this computer, so nothing was deleted or changed. Install MailArchive again first."
	deletingUninstall = "A delete is running (%s). Uninstall when its result shows on this page: uninstalling now would remove the deletion log that delete writes. Nothing was removed."
	// An uninstall's Remove that stopped after it began removing (CON-06).
	uninstallUnfinishedText = "An uninstall of MailArchive did not finish on this computer, and some of its settings may already be removed, so nothing was deleted or changed. Run Uninstall again to finish it, or install MailArchive again first."
	// Remove from list running when Delete permanently is clicked (CON-06).
	unlistingRefusal = "This archive is being removed from Your archives, so nothing was deleted."
	// Remove from list running when an uninstall's Remove is clicked (CON-06).
	unlistingUninstall = "Remove from list is running (%s). Uninstall when it ends: uninstalling now would remove the list and settings it writes. Nothing was removed."
)
