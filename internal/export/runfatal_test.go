package export

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-320, R1, R6, R10, R5, R12, S46
// Export's errors are typed (§4.7.1) so the local run can tell a folder it cannot
// create (FolderError) from a message it cannot render or write (ItemError), and
// their text is exactly what it was: a folder whose directory cannot be created
// (here a regular file in its place, the N1 shape) is a *FolderError naming the
// directory; a write the exporter's fault hook refuses is an *ItemError naming the
// file; an unsafe store token is ErrUnsafeToken, run-fatal, naming the token.
func TestExportErrorsAreTyped(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	e := incExporter(out, manifest)

	// Positive twin: a healthy export writes and returns nil.
	if ok, err := e.Export("store", []string{"Inbox"}, gapMsg("Healthy", "<ok@x>", nil)); err != nil || !ok {
		t.Fatalf("positive twin: Export = %v, %v", ok, err)
	}

	if err := os.WriteFile(filepath.Join(out, "store", "Blocked"), []byte("a file, not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := e.Export("store", []string{"Blocked"}, gapMsg("Into a blocked folder", "<b@x>", nil))
	var fe *FolderError
	if !errors.As(err, &fe) {
		t.Fatalf("a folder that cannot be created returned %T %v; want *FolderError", err, err)
	}
	if want := filepath.Join(out, "store", "Blocked"); fe.Dir != want {
		t.Errorf("FolderError.Dir = %q, want %q", fe.Dir, want)
	}
	if !strings.HasPrefix(err.Error(), "create output dir "+fe.Dir+": ") {
		t.Errorf("FolderError text changed: %q", err)
	}
	if IsRunFatal(err) {
		t.Errorf("ENOTDIR on one folder is that folder's fault, not run-fatal: %v", err)
	}

	e.BeforeWrite = func(path string) error {
		if strings.HasSuffix(path, ".html") {
			return &fs.PathError{Op: "write", Path: path, Err: syscall.EACCES}
		}
		return nil
	}
	_, err = e.Export("store", []string{"Inbox"}, gapMsg("Refused write", "<w@x>", nil))
	var ie *ItemError
	if !errors.As(err, &ie) || !strings.HasSuffix(ie.Path, ".html") {
		t.Fatalf("a refused write returned %T %v; want *ItemError naming the page", err, err)
	}
	if !strings.HasPrefix(err.Error(), "write "+ie.Path+": ") || !errors.Is(err, syscall.EACCES) {
		t.Errorf("ItemError text or cause changed: %q", err)
	}
	if IsRunFatal(err) {
		t.Errorf("EACCES on one file is that item's fault, not run-fatal: %v", err)
	}

	e.BeforeWrite = nil
	_, err = e.Export("../escape", []string{"Inbox"}, gapMsg("Unsafe token", "<u@x>", nil))
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names("../escape", "not a single safe path segment"))
	if !errors.Is(err, ErrUnsafeToken) || !IsRunFatal(err) {
		t.Errorf("a SafeToken refusal must be ErrUnsafeToken and run-fatal: %v", err)
	}
}

// covers: MA-320, R1, R6, R10, R5, R12, S46
// IsRunFatal says which write errors stop the run instead of costing one folder or
// item (§4.6, FC12): a full disk, an exceeded quota, a read-only file system
// (errno injection), a vanished or failing volume, and the refusal of an unsafe
// token — and nothing else: a permission error or a name too long is the item's or
// the folder's own. The Windows codes are a table over their numbers, so the
// classification is proven on every OS: 112, 39, 19, 21, 53, 55, 64, 1167 and 1295,
// and 3 (path not found) only when it is the archive root's own.
func TestIsRunFatalClassifiesTheVolumeErrors(t *testing.T) {
	fatal := runFatalErrnos
	notFatal := notRunFatalErrnos
	for _, en := range fatal {
		for _, wrap := range []func(error) error{
			func(e error) error { return e },
			func(e error) error { return &fs.PathError{Op: "mkdir", Path: "/x", Err: e} },
			func(e error) error {
				return &FolderError{Dir: "/x", Err: &fs.PathError{Op: "mkdir", Path: "/x", Err: e}}
			},
			func(e error) error { return &ItemError{Path: "/x/y.html", msg: "write /x/y.html", Err: e} },
		} {
			if err := wrap(en); !IsRunFatal(err) {
				t.Errorf("%v (%T) is not classified run-fatal", en, err)
			}
		}
	}
	for _, en := range notFatal {
		if err := error(&FolderError{Dir: "/x", Err: &fs.PathError{Op: "mkdir", Path: "/x", Err: en}}); IsRunFatal(err) {
			t.Errorf("%v classified run-fatal: it costs one folder", en)
		}
	}
	if IsRunFatal(nil) || IsRunFatal(errors.New("some other failure")) {
		t.Error("nil and an unclassified error are not run-fatal")
	}

	for _, code := range []uintptr{112, 39, 19, 21, 53, 55, 64, 1167, 1295} {
		if !windowsRunFatal(code, false) {
			t.Errorf("Windows error %d must be run-fatal", code)
		}
	}
	if windowsRunFatal(3, false) || !windowsRunFatal(3, true) {
		t.Error("Windows error 3 (path not found) is run-fatal on the archive root only")
	}
	for _, code := range []uintptr{5, 2, 123, 206, 183} { // access denied, not found, bad name, name too long, exists
		if windowsRunFatal(code, true) {
			t.Errorf("Windows error %d is one item's or folder's own, not run-fatal", code)
		}
	}
}
