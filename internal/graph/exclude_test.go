package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// folderPathSet maps each discovered folder's "/"-joined display path to true.
func folderPathSet(fs []Folder) map[string]bool {
	set := map[string]bool{}
	for _, f := range fs {
		set[strings.Join(f.Path, "/")] = true
	}
	return set
}

// covers: MA-215, R17, S38
// Deleted Items and Junk Email are ABSENT from the walked folder set by default:
// the client resolves their well-known-folder ids (deletedItems / junkemail) via
// Graph's well-known-folder endpoint and skips each folder AND its subtree by
// that id — never by display name — so the exclusion is locale-independent and a
// nested subfolder under Deleted Items is never even listed. Setting the
// FolderFilter opts a folder back in. Every request is a GET (read-only, R17).
func TestGraphExcludesDeletedAndJunkByDefault(t *testing.T) {
	var mu sync.Mutex
	var reqs []string
	record := func(r *http.Request) { mu.Lock(); reqs = append(reqs, r.Method+" "+r.URL.Path); mu.Unlock() }

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"t","token_type":"Bearer","expires_in":3600}`))
	})
	// Top-level: Inbox, Deleted Items (with a child subtree), Junk Email.
	mux.HandleFunc("/users/u1/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, `{"value":[
			{"id":"F_INBOX","displayName":"Inbox","childFolderCount":0},
			{"id":"F_DEL","displayName":"Papierkorb","childFolderCount":1},
			{"id":"F_JUNK","displayName":"Junk Email","childFolderCount":0}]}`)
	})
	// Well-known resolution: id, not display name (the German "Papierkorb" proves
	// a display-name match would miss it, an id match cannot).
	mux.HandleFunc("/users/u1/mailFolders/deletedItems", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, `{"id":"F_DEL","displayName":"Papierkorb"}`)
	})
	mux.HandleFunc("/users/u1/mailFolders/junkemail", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, `{"id":"F_JUNK","displayName":"Junk Email"}`)
	})
	// The Deleted-Items subtree: it must NOT be walked when excluded.
	mux.HandleFunc("/users/u1/mailFolders/F_DEL/childFolders", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, `{"value":[{"id":"F_DEL_SUB","displayName":"Old","childFolderCount":0}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)
	ctx := context.Background()

	// Default: both excluded. Only Inbox is walked.
	got, err := c.Folders(ctx, "u1", FolderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	set := folderPathSet(got)
	if set["Papierkorb"] || set["Junk Email"] {
		t.Errorf("default walk must exclude Deleted Items/Junk Email; got %v", set)
	}
	if !set["Inbox"] {
		t.Errorf("default walk dropped Inbox; got %v", set)
	}

	mu.Lock()
	seenDel, seenJunk, walkedSubtree := false, false, false
	for _, p := range reqs {
		if !strings.HasPrefix(p, http.MethodGet+" ") {
			t.Errorf("non-GET Graph request (client must be read-only): %s", p)
		}
		switch {
		case strings.HasSuffix(p, "/mailFolders/deletedItems"):
			seenDel = true
		case strings.HasSuffix(p, "/mailFolders/junkemail"):
			seenJunk = true
		case strings.Contains(p, "F_DEL/childFolders"):
			walkedSubtree = true
		}
	}
	mu.Unlock()
	if !seenDel || !seenJunk {
		t.Errorf("the excluded folders were not resolved by well-known id (deletedItems=%v junkemail=%v)", seenDel, seenJunk)
	}
	if walkedSubtree {
		t.Error("the excluded Deleted-Items subtree was walked; exclusion must drop the whole subtree by id")
	}

	// Opt in to both: all three appear.
	got, err = c.Folders(ctx, "u1", FolderFilter{IncludeDeleted: true, IncludeJunk: true})
	if err != nil {
		t.Fatal(err)
	}
	set = folderPathSet(got)
	for _, want := range []string{"Inbox", "Papierkorb", "Junk Email"} {
		if !set[want] {
			t.Errorf("with -include-deleted/-include-junk, %q must be walked; got %v", want, set)
		}
	}

	// Opt in to Deleted only: Deleted present, Junk still excluded.
	got, err = c.Folders(ctx, "u1", FolderFilter{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	set = folderPathSet(got)
	if !set["Papierkorb"] {
		t.Errorf("-include-deleted must keep the Deleted Items folder; got %v", set)
	}
	if set["Junk Email"] {
		t.Errorf("Junk Email must stay excluded when only -include-deleted is set; got %v", set)
	}
}

// covers: MA-215, R17, S38
// A mailbox that lacks a well-known folder (its resolution 404s) has nothing to
// skip: the walk proceeds and the default exclusion is a silent no-op rather than
// a run-ending error. WellKnownFolderID returns ("", nil) on 404.
func TestGraphWellKnownResolutionTolerates404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"t","token_type":"Bearer","expires_in":3600}`))
	})
	// Only the top-level listing exists; the well-known endpoints 404.
	mux.HandleFunc("/users/u1/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"value":[{"id":"F_INBOX","displayName":"Inbox","childFolderCount":0}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)
	ctx := context.Background()

	id, err := c.WellKnownFolderID(ctx, "u1", WellKnownDeletedItems)
	if err != nil {
		t.Fatalf("a 404 well-known resolution must not error: %v", err)
	}
	if id != "" {
		t.Errorf("a missing well-known folder must resolve to \"\"; got %q", id)
	}

	got, err := c.Folders(ctx, "u1", FolderFilter{})
	if err != nil {
		t.Fatalf("default walk must survive a mailbox with no Deleted Items/Junk: %v", err)
	}
	if set := folderPathSet(got); !set["Inbox"] || len(set) != 1 {
		t.Errorf("walk over a mailbox with no excluded folders: want just Inbox, got %v", set)
	}
}
