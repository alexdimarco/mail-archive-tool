//go:build windows

package outlookcom

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// defaultSyncWait bounds the pre-copy Send/Receive when a caller asks to sync but
// gives no explicit budget.
const defaultSyncWait = 5 * time.Minute

// olStoreUnicode selects the modern large-capacity Unicode PST format for
// Namespace.AddStoreEx (OlStoreType.olStoreUnicode).
const olStoreUnicode = 3

// withCOM runs fn on a COM-initialized, OS-locked thread (Outlook requires a
// single-threaded apartment) and turns any panic in the COM layer into an error
// so a quirk in one account never crashes the whole tool.
func withCOM(fn func() error) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Ignore the result: S_FALSE ("already initialized on this thread") is benign,
	// and a hard failure surfaces on the first CreateObject below.
	_ = ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	defer ole.CoUninitialize()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("Outlook automation failed unexpectedly: %v", r)
		}
	}()
	return fn()
}

// Detect reports the installed classic Outlook version and whether COM
// automation is available.
func Detect() (version string, available bool) {
	_ = withCOM(func() error {
		unk, err := oleutil.CreateObject("Outlook.Application")
		if err != nil {
			return err // no classic Outlook registered
		}
		defer unk.Release()
		app, err := unk.QueryInterface(ole.IID_IDispatch)
		if err != nil {
			return err
		}
		defer app.Release()
		available = true
		if v, err := oleutil.GetProperty(app, "Version"); err == nil {
			version = v.ToString()
		}
		return nil
	})
	return version, available
}

// CreatePSTs drives Outlook to copy each non-PST mail account into a fresh .pst
// under outDir, returning the files created. Accounts already stored as a .pst on
// disk are skipped (they can be archived directly). If opts.Sync is set it runs a
// Send/Receive first (bounded by opts.SyncWait) so cached accounts are current.
func CreatePSTs(outDir string, opts Options, logger *log.Logger) ([]Store, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	var stores []Store
	err := withCOM(func() error {
		unk, err := oleutil.CreateObject("Outlook.Application")
		if err != nil {
			return fmt.Errorf("%w (starting Outlook: %v)", ErrUnsupported, err)
		}
		defer unk.Release()
		app, err := unk.QueryInterface(ole.IID_IDispatch)
		if err != nil {
			return err
		}
		defer app.Release()

		nsV, err := oleutil.CallMethod(app, "GetNamespace", "MAPI")
		if err != nil {
			return fmt.Errorf("get MAPI namespace: %w", err)
		}
		ns := nsV.ToIDispatch()
		defer ns.Release()

		if opts.Sync {
			wait := opts.SyncWait
			if wait <= 0 {
				wait = defaultSyncWait
			}
			syncAndWait(ns, wait, logger)
		}

		srcStoresV, err := oleutil.GetProperty(ns, "Stores")
		if err != nil {
			return fmt.Errorf("list Outlook stores: %w", err)
		}
		srcStores := srcStoresV.ToIDispatch()
		defer srcStores.Release()
		countV, err := oleutil.GetProperty(srcStores, "Count")
		if err != nil {
			return err
		}
		count := int(countV.Val)
		logger.Printf("Outlook: %d mail store(s) found", count)

		for i := 1; i <= count; i++ {
			if s, ok := exportOneStore(ns, srcStores, i, outDir, logger); ok {
				stores = append(stores, s)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(stores) == 0 {
		return nil, errors.New("no Outlook accounts could be exported to a PST (an account already stored as a .pst can be archived directly)")
	}
	return stores, nil
}

// syncAndWait starts every Send/Receive group and waits a bounded time for the
// downloads to run. The Outlook Object Model exposes no reliable "sync finished"
// signal, so the wait is time-bounded rather than event-driven; any items still
// only on the server are fetched on demand when CopyTo reads them (online).
func syncAndWait(ns *ole.IDispatch, wait time.Duration, logger *log.Logger) {
	soV, err := oleutil.GetProperty(ns, "SyncObjects")
	if err != nil {
		logger.Printf("Outlook: could not access Send/Receive groups: %v", err)
		return
	}
	so := soV.ToIDispatch()
	defer so.Release()
	countV, err := oleutil.GetProperty(so, "Count")
	if err != nil {
		return
	}
	count := int(countV.Val)
	if count == 0 {
		logger.Printf("Outlook: no Send/Receive groups configured; skipping sync")
		return
	}

	started := 0
	for i := 1; i <= count; i++ {
		itV, err := oleutil.CallMethod(so, "Item", i)
		if err != nil {
			continue
		}
		it := itV.ToIDispatch()
		if _, err := oleutil.CallMethod(it, "Start"); err != nil {
			logger.Printf("Outlook: Send/Receive group %d failed to start: %v", i, err)
		} else {
			started++
		}
		it.Release()
	}
	if started == 0 {
		return
	}

	logger.Printf("Outlook: Send/Receive started (%d group(s)); waiting up to %s for downloads.", started, wait)
	const step = 20 * time.Second
	for elapsed := time.Duration(0); elapsed < wait; {
		d := step
		if remaining := wait - elapsed; remaining < step {
			d = remaining
		}
		time.Sleep(d)
		elapsed += d
		logger.Printf("Outlook: syncing… (%s elapsed of %s)", elapsed.Round(time.Second), wait)
	}
	logger.Printf("Outlook: proceeding; any remaining items download on demand as they are copied.")
}

// exportOneStore creates a PST for the i-th source store and copies its folders
// into it. It returns false (with a logged reason) rather than aborting the run
// if one account can't be exported.
func exportOneStore(ns, srcStores *ole.IDispatch, i int, outDir string, logger *log.Logger) (Store, bool) {
	itemV, err := oleutil.CallMethod(srcStores, "Item", i)
	if err != nil {
		logger.Printf("Outlook: store %d: %v", i, err)
		return Store{}, false
	}
	store := itemV.ToIDispatch()
	defer store.Release()

	name := "account"
	if v, err := oleutil.GetProperty(store, "DisplayName"); err == nil {
		if s := strings.TrimSpace(v.ToString()); s != "" {
			name = s
		}
	}

	// An account already backed by a .pst on disk needs no conversion.
	if v, err := oleutil.GetProperty(store, "FilePath"); err == nil {
		if fp := strings.ToLower(strings.TrimSpace(v.ToString())); strings.HasSuffix(fp, ".pst") {
			logger.Printf("Outlook: %q is already a PST file; skipping (archive it directly)", name)
			return Store{}, false
		}
	}

	pstPath := filepath.Join(outDir, pstFileName(name))
	_ = os.Remove(pstPath) // AddStoreEx wants a fresh, unmounted target

	if _, err := oleutil.CallMethod(ns, "AddStoreEx", pstPath, olStoreUnicode); err != nil {
		logger.Printf("Outlook: could not create PST for %q: %v", name, err)
		return Store{}, false
	}

	pstRoot, ok := findCreatedStoreRoot(ns, pstPath)
	if !ok {
		logger.Printf("Outlook: created a PST for %q but could not open it", name)
		return Store{}, false
	}
	defer func() {
		// Detach the PST from the Outlook profile, leaving the file on disk.
		if _, err := oleutil.CallMethod(ns, "RemoveStore", pstRoot); err != nil {
			logger.Printf("Outlook: could not detach the temporary PST for %q: %v", name, err)
		}
		pstRoot.Release()
	}()

	srcRootV, err := oleutil.CallMethod(store, "GetRootFolder")
	if err != nil {
		logger.Printf("Outlook: %q root folder: %v", name, err)
		return Store{}, false
	}
	srcRoot := srcRootV.ToIDispatch()
	defer srcRoot.Release()

	copied := copyChildFolders(srcRoot, pstRoot, name, logger)
	if copied == 0 {
		logger.Printf("Outlook: %q had no copyable folders", name)
		return Store{}, false
	}
	logger.Printf("Outlook: exported %q (%d folder tree(s)) -> %s", name, copied, pstPath)
	return Store{Name: name, Path: pstPath}, true
}

// copyChildFolders copies each top-level folder (with its subfolders and items)
// from srcRoot into destRoot, returning how many were copied.
func copyChildFolders(srcRoot, destRoot *ole.IDispatch, name string, logger *log.Logger) int {
	foldersV, err := oleutil.GetProperty(srcRoot, "Folders")
	if err != nil {
		return 0
	}
	folders := foldersV.ToIDispatch()
	defer folders.Release()
	countV, err := oleutil.GetProperty(folders, "Count")
	if err != nil {
		return 0
	}
	count := int(countV.Val)

	copied := 0
	for i := 1; i <= count; i++ {
		fV, err := oleutil.CallMethod(folders, "Item", i)
		if err != nil {
			continue
		}
		f := fV.ToIDispatch()
		fname := "folder"
		if v, err := oleutil.GetProperty(f, "Name"); err == nil {
			fname = v.ToString()
		}
		if resV, err := oleutil.CallMethod(f, "CopyTo", destRoot); err != nil {
			logger.Printf("Outlook: skipped folder %q of %q: %v", fname, name, err)
		} else {
			if d := resV.ToIDispatch(); d != nil {
				d.Release() // CopyTo returns the new folder
			}
			copied++
		}
		f.Release()
	}
	return copied
}

// findCreatedStoreRoot returns the root folder of the mounted store whose file
// path matches pstPath (the PST we just added).
func findCreatedStoreRoot(ns *ole.IDispatch, pstPath string) (*ole.IDispatch, bool) {
	storesV, err := oleutil.GetProperty(ns, "Stores")
	if err != nil {
		return nil, false
	}
	stores := storesV.ToIDispatch()
	defer stores.Release()
	countV, err := oleutil.GetProperty(stores, "Count")
	if err != nil {
		return nil, false
	}
	count := int(countV.Val)

	for i := 1; i <= count; i++ {
		itemV, err := oleutil.CallMethod(stores, "Item", i)
		if err != nil {
			continue
		}
		st := itemV.ToIDispatch()
		fp := ""
		if v, err := oleutil.GetProperty(st, "FilePath"); err == nil {
			fp = v.ToString()
		}
		if fp != "" && strings.EqualFold(fp, pstPath) {
			rootV, err := oleutil.CallMethod(st, "GetRootFolder")
			st.Release()
			if err != nil {
				return nil, false
			}
			return rootV.ToIDispatch(), true
		}
		st.Release()
	}
	return nil, false
}
