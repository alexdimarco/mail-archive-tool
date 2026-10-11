package app

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-531, MA-435, MA-512, R29, S52
// The folder tree of a manifest handed over is bounded (E4-AO2, E4-AC1): a record whose folder path
// is deep, or has a long name, or a manifest that lists many folders, made the tree build
// quadratic (every prefix of the path is a node whose id is the whole path: 2,000 levels cost
// 0.4 s and 770 MB, 20,000 levels a minute and 98 GB, and a Go out-of-memory kills the dashboard
// whatever export is running). The tree is limited as the Graph tree is (Exchange's 300 levels,
// 10,000 folders) and in the bytes its ids add up to, and a folder over a limit is R-p ("can't be
// read") before the tree is built and having allocated almost nothing. The positive twins first: a
// path exactly at the depth limit, and a manifest just under the folder limit, load.
func TestArchiveSourceBoundsTheFolderTreeOfAHostileManifest(t *testing.T) {
	deep := func(levels int, name string) string {
		segs := make([]string, levels)
		for i := range segs {
			segs[i] = name
		}
		return strings.Join(segs, "/")
	}
	open := func(t *testing.T, a *ceArchive) (*ArchiveSource, error, uint64, time.Duration) {
		t.Helper()
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		start := time.Now()
		s, err := OpenArchive(a.dir, ArchiveOptions{Other: true})
		took := time.Since(start)
		runtime.ReadMemStats(&m1)
		if s != nil {
			t.Cleanup(func() { s.Close() })
		}
		return s, err, m1.TotalAlloc - m0.TotalAlloc, took
	}

	t.Run("a path exactly at the depth limit loads", func(t *testing.T) {
		a := buildCEArchive(t)
		a.setRec("hello", func(r *state.Record) { r.Folder = deep(300, "a") })
		s, err, _, _ := open(t, a)
		if err != nil {
			t.Fatalf("a folder 300 levels deep (Exchange's own limit): %v", err)
		}
		if n := len(s.Tree()); n != 1+300+1+1 { // alice, its 300 levels, alice's other folder (Clients), bob
			t.Errorf("the tree has %d nodes, want 303", n)
		}
	})
	t.Run("a manifest just under the folder limit loads", func(t *testing.T) {
		a := buildCEArchive(t)
		a.edit(func(m *state.Manifest) {
			for i := 0; i < 9000; i++ {
				f := fmt.Sprintf("f%05d", i)
				m.Add(state.Key("alice", f, "id"+f), state.Record{Path: "alice/" + f + "/x.html", Folder: f})
			}
		})
		if _, err, _, _ := open(t, a); err != nil {
			t.Fatalf("a manifest of 9,000 folders: %v", err)
		}
	})

	for _, c := range []struct {
		name   string
		folder func() string
		count  int // extra folders, instead of one deep path
		names  []string
	}{
		{"a path one level over the depth limit", func() string { return deep(301, "a") }, 0, []string{"nested more than 300 levels deep"}},
		{"a path 2,000 levels deep", func() string { return deep(2000, "a") }, 0, []string{"nested more than 300 levels deep"}},
		{"a folder name over the length limit", func() string { return strings.Repeat("n", 1025) }, 0, []string{"longer than 1,024 bytes"}},
		{"a path whose ids add up to more than the byte budget", func() string { return deep(300, strings.Repeat("n", 1000)) }, 0, []string{"folder names add up to more than 8 MiB"}},
		{"more folders than the folder limit", nil, 10001, []string{"more than 10,000 folders"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := buildCEArchive(t)
			if c.folder != nil {
				folder := c.folder()
				a.setRec("hello", func(r *state.Record) { r.Folder = folder })
			}
			if c.count > 0 {
				a.edit(func(m *state.Manifest) {
					for i := 0; i < c.count; i++ {
						f := fmt.Sprintf("f%05d", i)
						m.Add(state.Key("alice", f, "id"+f), state.Record{Path: "alice/" + f + "/x.html", Folder: f})
					}
				})
			}
			s, err, alloc, took := open(t, a)
			var ref *CopyRefusal
			if !errors.As(err, &ref) || ref.ID != "R-p" {
				t.Fatalf("%s: opened (%v) in %v having allocated %d MiB, want R-p before the tree is built", c.name, err, took, alloc>>20)
			}
			if s != nil {
				t.Errorf("a refused archive still returned a source")
			}
			assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names(append([]string{"manifest can't be read", "nothing was written"}, c.names...)...))
			if alloc > 96<<20 || took > 10*time.Second {
				t.Errorf("%s: refusing took %v and allocated %d MiB: the tree was built before it was refused", c.name, took, alloc>>20)
			}
		})
	}
}

// covers: MA-531, MA-435, MA-512, R29
// The count of folders and the bytes of their ids are limits of another archive folder (E4-AO2, re-verified):
// they were applied to the owner's configured archive too, and across all its stores together, so a healthy
// archive of several accounts (or an IMAP cache) over 10,000 folders was refused as "can't be read" with no way
// to export from it, where before it loaded. R-o4's own remedy for a folder one trusts is to make it the
// archive location in Settings, and that archive is the owner's, written by their own captures; what a
// path may spell (300 levels, 1,024 bytes a name) stays bounded for both, as a path no filesystem holds.
// The positive twin: the same manifest handed over as another archive folder is still refused.
func TestArchiveSourceTreeCountLimitsAreForAnotherFolderNotTheConfiguredArchive(t *testing.T) {
	a := buildCEArchive(t)
	a.edit(func(m *state.Manifest) {
		for i := 0; i < 10001; i++ {
			f := fmt.Sprintf("f%05d", i)
			m.Add(state.Key("alice", f, "id"+f), state.Record{Path: "alice/" + f + "/x.html", Folder: f})
		}
		long := strings.Join(slicesOf(300, strings.Repeat("n", 1000)), "/") // ids of 45 MB together
		m.Add(state.Key("bob", long, "idlong"), state.Record{Path: "bob/long/x.html", Folder: long})
	})
	other, err := OpenArchive(a.dir, ArchiveOptions{Other: true})
	var ref *CopyRefusal
	if !errors.As(err, &ref) || ref.ID != "R-p" || other != nil {
		t.Fatalf("another archive folder with 10,001 folders: %v, want R-p", err)
	}
	s, err := OpenArchive(a.dir, ArchiveOptions{})
	if err != nil {
		t.Fatalf("the configured archive with 10,001 folders and 45 MB of folder ids: %v, want it loaded", err)
	}
	defer s.Close()
	if n := len(s.Tree()); n < 10001+300 {
		t.Errorf("the configured archive's tree has %d nodes, want every folder", n)
	}
}

func slicesOf(n int, s string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// covers: MA-435, MA-531, R29
// A folder path with an empty segment ("/Sub", "a//b", "a/") is read as the folder its names
// spell (E4-AC8): it used to give a node whose id was the store's own token, which overwrote the
// store in the page's tree and hid the store with all its folders. Every node id is unique, every
// folder's parent is in the tree, the records are listed under the folder, and the message names
// the same folders.
func TestArchiveSourceReadsAnEmptyFolderSegmentAsTheFolderItSpells(t *testing.T) {
	a := buildCEArchive(t)
	a.setRec("hello", func(r *state.Record) { r.Folder = "/Sub" })
	a.setRec("second", func(r *state.Record) { r.Folder = "Sub/" })
	a.setRec("client-mail", func(r *state.Record) { r.Folder = "Clients//Deep" })
	s := a.open()
	ids := map[string]ArchiveNode{}
	for _, n := range s.Tree() {
		if _, dup := ids[n.ID]; dup {
			t.Errorf("two nodes have the id %q: %+v", n.ID, s.Tree())
		}
		ids[n.ID] = n
	}
	if n := ids["alice"]; !n.Store || n.Parent != "" || n.Name != "alice" {
		t.Errorf("the store node: %+v", n)
	}
	for _, n := range s.Tree() {
		if !n.Store {
			if _, ok := ids[n.Parent]; !ok {
				t.Errorf("node %+v has a parent that is not in the tree", n)
			}
			if n.Name == "" {
				t.Errorf("node %+v has an empty name", n)
			}
		}
	}
	if n, ok := ids["alice/Sub"]; !ok || n.Count != 2 || n.Parent != "alice" || n.Name != "Sub" {
		t.Errorf("the folder the two paths spell: %+v (%v), want one folder Sub with both records", n, ok)
	}
	if n, ok := ids["alice/Clients/Deep"]; !ok || n.Count != 1 || n.Parent != "alice/Clients" || n.Name != "Deep" {
		t.Errorf("the folder 'Clients//Deep' spells: %+v (%v)", n, ok)
	}
	if tok, folder, ok := s.Locate("alice/Sub"); !ok || tok != "alice" || folder != "Sub" {
		t.Errorf("Locate(alice/Sub) = %q %q %v", tok, folder, ok)
	}
	res, err := s.Messages(t.Context(), []string{a.key("hello")}, nil, nil)
	if err != nil || len(res.Messages) != 1 {
		t.Fatalf("Messages: %v %v", res, err)
	}
	var got []string
	for _, f := range res.Messages[0].Folder {
		got = append(got, f.ID+"="+f.Name)
	}
	if strings.Join(got, ";") != "alice=alice;alice/Sub=Sub" {
		t.Errorf("the message's folders %v, want the store and Sub", got)
	}
}
