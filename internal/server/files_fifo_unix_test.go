//go:build unix

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-374, R19, S22, S45
// A FIFO swapped in for a page after the checks is refused and never blocks the
// handler: the open must not wait for a writer. (A FIFO already present is
// refused by the checks, as TestFilesServesOnlyExportedContent plants none; this
// is the swap between the check and the open.) Positive twin: with nothing
// swapped the page is served. (Adversarial pass AC2.)
func TestFilesNeverBlocksOnAFifoSwappedInAfterTheCheck(t *testing.T) {
	h, dir := filesFixture(t)
	pageRel := filepath.FromSlash(msgStem + ".html")
	page := filepath.Join(dir, pageRel)
	restore := afterCheck
	t.Cleanup(func() { afterCheck = restore })

	reached := 0
	afterCheck = func(rel string) {
		if rel == pageRel {
			reached++
		}
	}
	expectServed(t, h, msgStem+".html", "MESSAGE-PAGE")
	assure.Reached(t, reached, "the fault-injection point between the checks and the open")

	afterCheck = func(rel string) {
		if rel != pageRel {
			return
		}
		if err := os.Rename(page, page+".orig"); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(page, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// If the open blocks, releasing it at cleanup keeps the goroutine from outliving the test.
	t.Cleanup(func() {
		if f, err := os.OpenFile(page, os.O_RDWR, 0); err == nil {
			f.Close()
		}
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- getFile(h, msgStem+".html") }()
	select {
	case rec := <-done:
		assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusNotFound), assure.Forbid("MESSAGE-PAGE"))
	case <-time.After(3 * time.Second):
		t.Fatal("the request did not return in 3s: the open of a swapped-in FIFO blocks the handler")
	}
}
