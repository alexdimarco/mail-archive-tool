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
	// Messages counted for certain, the STRICT count (deleted copies carrying 0x0008 already
	// left out, §2.4; in an mbox file only pieces that begin at a separator Thunderbird writes
	// and carry X-Mozilla-Status, the C2 pass round 2's R2-04).
	Messages int64
	// Keys are the counted messages' identity keys (source.IdentityKey: the Message-ID, else the
	// content without Thunderbird's per-copy lines), and NoKey how many had none (Gmail's X).
	Keys  [][16]byte
	NoKey int64
	// Loose: pieces at a separator with no X-Mozilla-Status (source.MailboxCount.Loose), with
	// their keys and how many had none. The LENIENT count adds them.
	Loose      int64
	LooseKeys  [][16]byte
	LooseNoKey int64
	// Unseparated: pieces the mbox reader split off at a "From " line of another shape (the C2
	// pass's ADV-C2-03): the mailbox is "split differently".
	Unseparated int64
	// Glued: pieces at a separator-shaped line with no empty line before it (R2-05): the mailbox
	// is split differently and the account is never told behind.
	Glued int64
}

// OnDiskFor is the walk's count as the verdict reads it for the store s (the C1b note: keyed by
// Entry.Path exactly as the reader gives it). A mailbox is the folder whose Path names it, each
// part compared as this OS compares names (FolderIdentity); its containers must end in ".sbd", as
// Thunderbird names them. Then:
//
//   - Folders holds each such folder's count (a folder with no mailbox has none: 0);
//   - for Gmail, Distinct is the distinct count over the mailboxes of folders kept on this
//     computer (the Offline flag): one per distinct identity key, never one per copy (the C2
//     pass's ADV-C2-02); a message with no key is left out of X and counted in NoKey, which
//     keeps the account from saying complete. A mailbox of a folder that is not kept never adds
//     to X: its messages are outside Y (the builder's reading of §3, beside L3; it fails toward
//     "behind");
//   - a keyed mailbox with pieces that do not begin at a separator Thunderbird writes is counted
//     in SplitMailboxes (the C2 pass's ADV-C2-03), and also in GluedMailboxes when some begin
//     at a separator-shaped line with no empty line before it (R2-05);
//   - the LENIENT count (R2-04) is kept beside the strict one: Loose holds each folder's pieces
//     with no X-Mozilla-Status, and for Gmail LooseDistinct is how many more X could be: the
//     Loose pieces' identities not already counted, and every message with no identity key,
//     strict or loose (each may be one more distinct email). Strict plus lenient is never less
//     than what is on disk, so a gap by it is certain;
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
	d := OnDisk{Folders: map[string]int64{}, Loose: map[string]int64{}}
	seen := map[[16]byte]bool{}
	var looseKeys [][16]byte
	for i, mb := range walked {
		id := ids[i]
		e, keyed := folders[id]
		switch {
		case id != "" && oldOnly[id] && len(byFolder[id]) == 1:
			continue // M2: left out of X and Y
		case id == "" || !keyed || len(byFolder[id]) > 1:
			d.UnkeyedMailboxes++
			d.UnkeyedMessages += mb.Messages + mb.Loose // its loose pieces are on disk too (TC-2's "lists no mail")
			continue
		}
		d.Folders[e.Path] = mb.Messages
		if mb.Loose > 0 {
			d.Loose[e.Path] = mb.Loose
			d.NoStatus += mb.Loose
		}
		if mb.Unseparated > 0 || mb.Glued > 0 {
			d.SplitMailboxes++
		}
		if mb.Glued > 0 {
			d.GluedMailboxes++
		}
		if !gmail || !e.Has(FlagOffline) {
			continue
		}
		for _, k := range mb.Keys {
			if !seen[k] {
				seen[k] = true
				d.Distinct++
			}
		}
		d.NoKey += mb.NoKey
		looseKeys = append(looseKeys, mb.LooseKeys...)
		d.LooseDistinct += mb.LooseNoKey
	}
	// After every strict key is seen, so a loose piece that is a copy of a counted email adds
	// nothing.
	for _, k := range looseKeys {
		if !seen[k] {
			seen[k] = true
			d.LooseDistinct++
		}
	}
	d.LooseDistinct += d.NoKey
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
