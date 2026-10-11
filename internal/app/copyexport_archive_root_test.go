package app

import (
	"context"
	"testing"
)

// covers: MA-435, MA-436, MA-420, R29, S52
// An archive that is loaded for the page holds no directory handle on the folder between requests
// (E4-AI9): the session kept an os.Root on the archive for up to 30 idle minutes, which on Windows (no share
// mode that allows a delete) keeps the folder and every folder above it from being renamed, moved or deleted,
// and on macOS and Linux makes Eject and unmount say "in use" for the drive that holds another archive
// folder, while nothing is being read. Every request opens the root it reads through and closes it at its end
// (as the list closes the index, XC20); a source opened to run holds its root for the run, and Close releases
// it. The reads are the same through a root opened for the request (every other test of the source).
func TestArchiveSessionHoldsNoRootBetweenRequests(t *testing.T) {
	a := buildCEArchive(t)
	base := ArchiveRootsOpen()
	held := func() int { return ArchiveRootsOpen() - base }

	s, err := OpenArchive(a.dir, ArchiveOptions{Other: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if n := held(); n != 0 {
		t.Errorf("a loaded archive holds %d root(s) on the folder", n)
	}
	// every kind of request, none of which may leave one behind
	if _, _, err := s.StampNow(); err != nil {
		t.Fatal(err)
	}
	s.WALBeside()
	res, err := s.Messages(context.Background(), allKeys(s), nil, nil)
	if err != nil || len(res.Messages) != 6 {
		t.Fatalf("Messages through a root opened for the request: %d messages, %v", len(res.Messages), err)
	}
	h, err := s.OpenList()
	if err != nil {
		t.Fatal(err)
	}
	h.Close()
	if n := held(); n != 0 {
		t.Errorf("after a list, a stamp and a resolution the archive holds %d root(s)", n)
	}
	if got := s.Changed(&res.Messages[0]); got {
		t.Errorf("Changed says a record of an archive nobody touched has changed")
	}
	if n := held(); n != 0 {
		t.Errorf("after Changed the archive holds %d root(s)", n)
	}

	// a source opened to run holds its root for the run, and only for it
	rs, err := OpenArchiveForRun(a.dir, ArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n := held(); n != 1 {
		t.Errorf("a source opened for a run holds %d root(s), want 1", n)
	}
	rs.Close()
	rs.Close() // closing twice is not a second release
	if n := held(); n != 0 {
		t.Errorf("a closed source holds %d root(s)", n)
	}
}
