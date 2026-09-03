package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-107, R17, R18
// When every Graph mailbox fails, the first per-mailbox error (with its AADSTS
// code) is recorded in the last-run error so `status` can fire the expired-
// secret remedy, and no browsable scaffold (README.txt / index.html) is written
// for an archive that captured nothing — while the manifest and the last-run
// record still are, so `status` has state to read (friction #6).
func TestRunGraphAllMailboxesFail(t *testing.T) {
	out := tmpDir(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"t","token_type":"Bearer","expires_in":3600}`))
	})
	// Folder listing fails with an expired-credential error carrying an AADSTS code.
	mux.HandleFunc("/users/u1/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":"InvalidAuthenticationToken","message":"AADSTS700082: the credential has expired"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	g := GraphOptions{
		Tenant: "t", ClientID: "c", ClientSecret: "s",
		Mailboxes: []string{"u1"},
		BaseURL:   srv.URL, TokenURL: srv.URL + "/token",
	}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	_, err := RunGraph(context.Background(), g, opts, log.New(io.Discard, "", 0))
	if err == nil {
		t.Fatal("expected the run to fail when every mailbox fails")
	}

	lr, st, rerr := state.ReadLastRun(out)
	if rerr != nil || st != state.LastRunPresent {
		t.Fatalf("no last-run record after an all-failed run: state=%v err=%v", st, rerr)
	}
	if lr.Status != state.RunFailed || !strings.Contains(lr.Error, "AADSTS") {
		t.Errorf("failed record lacks the AADSTS reason status needs: %+v", lr)
	}
	for _, name := range []string{"README.txt", "index.html"} {
		if _, serr := os.Stat(filepath.Join(out, name)); !os.IsNotExist(serr) {
			t.Errorf("a zero-capture run wrote the empty scaffold file %s", name)
		}
	}
	if _, serr := os.Stat(filepath.Join(out, ".mailarchive-manifest.json")); serr != nil {
		t.Errorf("manifest not written on an all-failed run (status needs it): %v", serr)
	}
}
