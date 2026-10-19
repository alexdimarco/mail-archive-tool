package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// covers: MA-435, R20, R29, S52
// The archive source's tree, counts and list (design §4.3, §4.6): the tree and counts come from the
// manifest (stores, then folders by current Folder, each store labelled with its mailbox, XC21); the
// list comes from index.ListFolder, which is closed after each request (a following Rebuild runs when
// the injected "handle held" check says no handle is held); gone records are counted apart and
// included by default (ruling Q4); the preview reports with-original against without; the archive's
// own gaps are carried into notes, a v1 record's UnknownSentinel never reaching them; a record whose
// files were removed is left out and counted, its subject and sender absent from the export.
func TestArchiveSourceTreeCountsAndList(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	a.edit(func(m *state.Manifest) { // a Graph mailbox is a store whose source id is "mailbox:<address>"
		for id, tok := range m.Stores {
			if tok == "alice" {
				delete(m.Stores, id)
				m.Stores["mailbox:alice@contoso.org"] = "alice"
			}
		}
	})
	s := a.open()

	nodes := s.Tree()
	want := []struct {
		id, parent, name string
		count            int
		store            bool
	}{{"alice", "", "alice", 2, true}, {"alice/Clients", "alice", "Clients", 1, false}, {"bob", "", "bob", 3, true}}
	if len(nodes) != len(want) {
		t.Fatalf("the tree has %d nodes, want %d: %+v", len(nodes), len(want), nodes)
	}
	for i, w := range want {
		n := nodes[i]
		if n.ID != w.id || n.Parent != w.parent || n.Name != w.name || n.Count != w.count || n.Store != w.store {
			t.Errorf("node %d = %+v, want %+v", i, n, w)
		}
	}
	if nodes[0].Mailbox != "alice@contoso.org" || nodes[2].Mailbox != "" {
		t.Errorf("the stores' mailboxes: %q and %q, want the Graph store's address and none for a maildir store", nodes[0].Mailbox, nodes[2].Mailbox)
	}
	if tok, folder, ok := s.Locate("alice/Clients"); !ok || tok != "alice" || folder != "Clients" {
		t.Errorf("Locate(alice/Clients) = %q %q %v", tok, folder, ok)
	}
	if _, _, ok := s.Locate("alice/../../etc"); ok {
		t.Error("a node id that is not in the snapshot resolves: ids are validated against it, never used to build a path")
	}

	// the list comes from the index, one request at a time, and is closed at its end
	h, err := s.OpenList()
	if err != nil {
		t.Fatal(err)
	}
	rows, total, err := h.ListFolder("alice", "", 0, 1000)
	if err != nil || total != 2 {
		t.Fatalf("alice's own folder: total %d, %v", total, err)
	}
	for _, r := range assure.Reached(t, rows, "alice's rows") {
		if k, ok := s.MapKey(h.Version, r); !ok || k != r.Key {
			t.Errorf("row %q does not map to a manifest key (%q, %v)", r.Subject, k, ok)
		}
	}
	h.Close()
	handleHeld := func() bool { return index.OpenListHandles() > 0 }
	if handleHeld() {
		t.Fatal("the index is still open after the request that listed it")
	}
	if _, err := Rebuild(a.dir, discard()); err != nil { // the rename the workspace must never be inside
		t.Errorf("a Rebuild after the list: %v", err)
	}

	// gone, gaps and the sentinel
	a.setRec("hello", func(r *state.Record) {
		r.FirstSeen, r.LastSeen, r.Present = time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC), false
	})
	a.setRec("second", func(r *state.Record) { r.Missing = []string{"doc.pdf"} })
	a.setRec("client-mail", func(r *state.Record) { r.Terminal = []string{state.MissingBody}; r.Unresolved = []string{"logo@x"} })
	a.setRec("third-page", func(r *state.Record) { r.Missing = []string{state.UnknownSentinel} })
	a.setRec("plain-page", func(r *state.Record) { r.Subject = "SECRET-SUBJECT" })
	pagePlain, emlNone, _ := a.files("plain-page")
	_ = emlNone
	os.Remove(pagePlain) // a redaction whose manifest and index were not yet updated
	s = a.open()
	keys := allKeys(s)
	p, res := a.planWith(e, s, keys, nil)
	if res.Skipped != 1 || p.Archive.Skipped != 1 || len(res.Messages) != 5 {
		t.Fatalf("resolution: skipped %d, %d messages; want the record with no page left out and counted", res.Skipped, len(res.Messages))
	}
	if c := p.Archive; c.Gone != 1 || c.WithOriginal != 3 || c.WithoutOriginal != 2 {
		t.Errorf("the preview counts %+v: want one gone (a local record with no timeline is not gone), 3 with an original and 2 without", c)
	}
	exp := a.run(e, p, s)
	if exp.Counts.Exported != 5 {
		t.Fatalf("the run: %+v", exp.Counts)
	}
	man := manifestOf(t, exp.Root)
	if got := notesOf(entryOf(t, man, a.key("second"))); !strings.Contains(got, "the archive records attachment 'doc.pdf' as never downloaded") {
		t.Errorf("second's notes: %q", got)
	}
	cm := notesOf(entryOf(t, man, a.key("client-mail")))
	if !strings.Contains(cm, "message's body as never downloaded") || !strings.Contains(cm, "inline image (cid:logo@x)") {
		t.Errorf("client-mail's notes: %q", cm)
	}
	tp := notesOf(entryOf(t, man, a.key("third-page")))
	if !strings.Contains(tp, "completeness was not tracked by the version that captured this message") || strings.Contains(tp, "unknown") {
		t.Errorf("a v1 record's notes: %q, want the sentinel put in words and never the literal", tp)
	}
	if ent := entryOf(t, man, a.key("hello")); ent["status"] != "exported" {
		t.Errorf("a record gone from the mailbox is included by default: %v", ent)
	}
	if ent := entryOf(t, man, a.key("hello")); ent["mailbox"] != "alice@contoso.org" {
		t.Errorf("each entry carries its store's mailbox (XC21): %v", ent)
	}
	srcStores := man["source"].(map[string]any)["stores"].([]any)
	if len(srcStores) != 2 || srcStores[0].(map[string]any)["mailbox"] != "alice@contoso.org" {
		t.Errorf("the manifest's source stores: %v", srcStores)
	}
	for name := range treeFiles(t, exp.Root) {
		b, _ := os.ReadFile(filepath.Join(exp.Root, filepath.FromSlash(name)))
		if strings.Contains(string(b), "SECRET-SUBJECT") || strings.Contains(string(b), "plain-page") {
			t.Errorf("%s names the record whose files were removed: its metadata must never be written into the export", name)
		}
	}
	readme := readText(t, exp.Root, "README.txt")
	if !strings.Contains(readme, "alice (alice@contoso.org)") || !strings.Contains(readme, "bob") {
		t.Errorf("README does not name every store and its mailbox: %s", readme)
	}
}

// covers: MA-435, MA-512, R20, R29, S52
// A manifest or an index a newer MailArchive wrote is refused with R-p, in the loader's own
// wording and naming the format, and a folder that holds no archive is R-o (the configured
// archive) or R-o3 (another folder): each refusal before anything is read past the check.
func TestArchiveSourceRefusesWhatItCannotRead(t *testing.T) {
	a := buildCEArchive(t)
	if s, err := OpenArchive(a.dir, ArchiveOptions{}); err != nil { // the positive twin
		t.Fatalf("a healthy archive: %v", err)
	} else {
		s.Close()
	}

	// a newer manifest
	raw := a.read(a.manifestPath())
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["version"] = json.RawMessage("99")
	newer, _ := json.Marshal(doc)
	os.WriteFile(a.manifestPath(), newer, 0o644)
	_, err := OpenArchive(a.dir, ArchiveOptions{})
	var ref *CopyRefusal
	if !errors.As(err, &ref) || ref.ID != "R-p" {
		t.Fatalf("a newer manifest: %v, want R-p", err)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names("newer MailArchive", "format 99", "update MailArchive Desktop"))
	os.WriteFile(a.manifestPath(), raw, 0o644)

	// a newer index
	db, err := sql.Open("sqlite", filepath.Join(a.dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET version = 99`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s := a.open()
	if _, err := s.OpenList(); !errors.Is(err, index.ErrNewerFormat) {
		t.Errorf("a newer index: %v, want index.ErrNewerFormat", err)
	}
	var ni *index.NewerIndexError
	if err := func() error { _, e := s.OpenList(); return e }(); !errors.As(err, &ni) || ni.Version != 99 {
		t.Errorf("a newer index names its version: %v", err)
	}

	// no archive
	empty := tmpDir(t)
	_, err = OpenArchive(empty, ArchiveOptions{})
	if !errors.As(err, &ref) || ref.ID != "R-o" {
		t.Fatalf("a configured archive location with no archive: %v, want R-o", err)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names(empty, "no archive", "run Archive now first", "choose Microsoft 365"))
	_, err = OpenArchive(empty, ArchiveOptions{Other: true})
	if !errors.As(err, &ref) || ref.ID != "R-o3" {
		t.Fatalf("another folder with no manifest: %v, want R-o3", err)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names(empty, "isn't a MailArchive archive", archiveManifestName, "index.html and README.txt"))
	if _, err := OpenArchive(filepath.Join(empty, "nowhere"), ArchiveOptions{Other: true}); !errors.As(err, &ref) || ref.ID != "R-o3" {
		t.Errorf("a folder that does not exist: %v, want R-o3", err)
	}
}

// covers: MA-435, R20, R29
// A v2-era archive (a manifest of version 2 with folder-and-identity keys, and an index with no meta
// table whose rows carry the same keys) lists through the migrated keys: the loader re-scopes the
// manifest in memory, the list maps each row's key the same way, and every message of it can be
// selected and exported.
func TestArchiveSourceListsAV2EraArchive(t *testing.T) {
	a := buildCEArchive(t)
	var doc struct {
		Version int                        `json:"version"`
		Entries map[string]json.RawMessage `json:"entries"`
		Stores  map[string]string          `json:"stores"`
	}
	if err := json.Unmarshal(a.read(a.manifestPath()), &doc); err != nil {
		t.Fatal(err)
	}
	v2 := map[string]json.RawMessage{}
	for k, v := range doc.Entries {
		_, rest, _ := strings.Cut(k, "\x00") // token\0folder\0identity -> folder\0identity
		v2[rest] = v
	}
	out, _ := json.Marshal(map[string]any{"version": 2, "entries": v2, "stores": doc.Stores})
	os.WriteFile(a.manifestPath(), out, 0o644)
	db, err := sql.Open("sqlite", filepath.Join(a.dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT id, key FROM docs`)
	if err != nil {
		t.Fatal(err)
	}
	type idKey struct {
		id  int64
		key string
	}
	var all []idKey
	for rows.Next() {
		var r idKey
		rows.Scan(&r.id, &r.key)
		all = append(all, r)
	}
	rows.Close()
	for _, r := range all {
		_, rest, _ := strings.Cut(r.key, "\x00")
		if _, err := db.Exec(`UPDATE docs SET key = ? WHERE id = ?`, rest, r.id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DROP TABLE meta`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s := a.open()
	if s.Rekeyed() != 6 {
		t.Fatalf("the loader re-scoped %d records in memory, want 6", s.Rekeyed())
	}
	h, err := s.OpenList()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if h.Version != 1 {
		t.Fatalf("a pre-meta index reads as version %d, want 1", h.Version)
	}
	listed, _, err := h.ListFolder("alice", "Clients", 0, 100)
	if err != nil || len(listed) != 1 {
		t.Fatalf("the list of alice/Clients: %d rows, %v", len(listed), err)
	}
	k, ok := s.MapKey(h.Version, listed[0])
	if !ok || !strings.HasPrefix(k, "alice\x00Clients\x00") {
		t.Fatalf("the v2 key %q maps to %q (%v), want the store-qualified key", listed[0].Key, k, ok)
	}
	if _, in := s.man.Get(k); !in {
		t.Errorf("the mapped key is not a manifest key")
	}
	e := newCEEnv(t)
	p := a.plan(e, s, []string{k})
	if res := a.run(e, p, s); res.Counts.Exported != 1 {
		t.Errorf("the run of the migrated key: %+v", res.Counts)
	}
}
