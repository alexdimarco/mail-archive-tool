package tbcomplete

// This file is slice C2's part of the package: what the backup's own walk counted, put in the
// spelling the verdict reads (OnDisk), for one store. It is pure: no file is read.

import (
	"runtime"
	"strings"
)

// Mailbox is one mailbox the walk read, as it counted it (the source package's MailboxCount,
// handed over by the run).
type Mailbox struct {
	// Rel is its place under the store as on disk, slash-separated, its containers with
	// their ".sbd" ending ("[Gmail].sbd/All Mail").
	Rel string
	// Messages counted (deleted copies carrying 0x0008 already left out, §2.4).
	Messages int64
	// IDs are the counted messages' Message-ID keys, and NoID how many had none (Gmail's X).
	IDs  [][16]byte
	NoID int64
}

// OnDiskFor is the walk's count as the verdict reads it for the store s (the C1b note: keyed by
// Entry.Path exactly as the reader gives it). A mailbox is the folder whose Path names it, each
// part compared as this OS compares names (FolderIdentity); its containers must end in ".sbd", as
// Thunderbird names them. Then:
//
//   - Folders holds each such folder's count (a folder with no mailbox has none: 0);
//   - for Gmail, Distinct is the distinct count over the mailboxes of folders kept on this
//     computer (the Offline flag): one per distinct Message-ID, plus one per message with none.
//     A mailbox of a folder that is not kept never adds to X: its messages are outside Y (the
//     builder's reading of §3, beside L3; it fails toward "behind");
//   - a mailbox with no folder of s (no key, a container not named ".sbd", or the store's own
//     maildir) is counted in UnkeyedMailboxes and UnkeyedMessages and never in X (L3);
//   - two mailboxes that name one folder are both unkeyed (neither can be told to be it);
//   - a mailbox of a folder with only an old-path key (s.OldPathOnly, M2) is in neither: that
//     folder is left out of X and Y, and the verdict says so.
func (s Store) OnDiskFor(walked []Mailbox, gmail bool) OnDisk {
	return s.onDiskFor(walked, gmail, runtime.GOOS)
}

func (s Store) onDiskFor(walked []Mailbox, gmail bool, goos string) OnDisk {
	folders := map[string]Entry{}
	for _, e := range s.Folders {
		folders[folderIdentity(e.Path, goos)] = e
	}
	oldOnly := map[string]bool{}
	for _, e := range s.OldPathOnly {
		oldOnly[folderIdentity(e.Path, goos)] = true
	}
	byFolder := map[string][]int{}
	ids := make([]string, len(walked))
	for i, mb := range walked {
		id, ok := walkedIdentity(mb.Rel, goos)
		if !ok {
			continue
		}
		ids[i] = id
		byFolder[id] = append(byFolder[id], i)
	}
	d := OnDisk{Folders: map[string]int64{}}
	seen := map[[16]byte]bool{}
	for i, mb := range walked {
		id := ids[i]
		e, keyed := folders[id]
		switch {
		case id != "" && oldOnly[id] && len(byFolder[id]) == 1:
			continue // M2: left out of X and Y
		case id == "" || !keyed || len(byFolder[id]) > 1:
			d.UnkeyedMailboxes++
			d.UnkeyedMessages += mb.Messages
			continue
		}
		d.Folders[e.Path] = mb.Messages
		if !gmail || !e.Has(FlagOffline) {
			continue
		}
		for _, k := range mb.IDs {
			if !seen[k] {
				seen[k] = true
				d.Distinct++
			}
		}
		d.Distinct += mb.NoID
	}
	return d
}

// walkedIdentity is the identity of the folder a walked mailbox at rel would be: its parts with
// the containers' ".sbd" taken off, compared as goos compares names. ok is false for the store's
// own maildir ("") and for a mailbox under a container not named ".sbd" (a followed link's own
// name, which no key of Thunderbird's names).
func walkedIdentity(rel, goos string) (string, bool) {
	if rel == "" {
		return "", false
	}
	parts := strings.Split(rel, "/")
	for i := range parts[:len(parts)-1] {
		name, ok := strings.CutSuffix(parts[i], ".sbd")
		if !ok || name == "" {
			return "", false
		}
		parts[i] = name
	}
	if parts[len(parts)-1] == "" {
		return "", false
	}
	return folderIdentity(strings.Join(parts, "/"), goos), true
}

// FolderIdentity is a folder Path as this OS compares it, part by part (NFC; case-folded on
// Windows and macOS): two Paths with one identity are one folder. The TC-19 fingerprint of the
// not-kept folders is taken over it.
func FolderIdentity(path string) string { return folderIdentity(path, runtime.GOOS) }

func folderIdentity(path, goos string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = same(p, goos)
	}
	return strings.Join(parts, "/")
}
