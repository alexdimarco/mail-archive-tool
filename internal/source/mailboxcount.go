package source

import (
	"crypto/sha256"
	"strconv"
	"strings"
)

// MailboxCounts is what a local store's walk counted of each mailbox it read, for the
// Thunderbird completeness check (docs/design-thunderbird-completeness.md §2.4, slice C2).
// It changes nothing the walk reads or delivers: the handler gets every message as before,
// and what is archived is the same whether or not the walk counts.
//
// The count is taken as each message is read, before the handler sees it, so before any
// manifest skip ("already done") or date filter: an incremental run counts what a first run
// counts. A message Thunderbird has marked deleted but not yet compacted away (the Expunged
// bit 0x0008 of its X-Mozilla-Status, nsMsgMessageFlags.idl) is left out of the count, and
// still archived (owner default Q7).
type MailboxCounts struct {
	// Count asks for the per-mailbox counts. Without it only Msf is noted, so a store that
	// no account owns costs no header scan.
	Count bool
	// Distinct also keeps each counted message's distinctness key (Gmail's X, §3: distinct
	// messages by Message-ID across the account).
	Distinct bool

	// Mailboxes are the mailboxes read, in the order the walk read them.
	Mailboxes []*MailboxCount
	// Msf: a directory entry whose name ends in ".msf", in any letter case, was in a listing
	// the walk made of the store or of a folder under it (the M6-residual fold: a sign the
	// folder is one Thunderbird keeps). Seen by name only; never opened or followed.
	Msf bool
}

// MailboxCount is one mailbox's count.
type MailboxCount struct {
	// Rel is the mailbox's place under the store as it is on disk, slash-separated, its
	// containers with their ".sbd" ending ("INBOX", "[Gmail].sbd/All Mail"); "" for a store
	// that is itself one maildir folder. For a mailbox reached through a followed link it is
	// where the link is.
	Rel string
	// Messages counted: every message read, but those carrying the Expunged bit.
	Messages int64
	// Expunged is how many were left out for the Expunged bit.
	Expunged int64
	// IDs (when Distinct) is one key per counted message that has a Message-ID: the first
	// 16 bytes of the SHA-256 of the Message-ID as written, trimmed of white space and angle
	// brackets.
	IDs [][16]byte
	// NoID (when Distinct) is how many counted messages have no Message-ID: each counts once
	// (§2.4 adjustment 2).
	NoID int64
}

// mozillaExpunged is the Expunged bit of X-Mozilla-Status (nsMsgMessageFlags.idl,
// nsMsgMessageFlags::Expunged).
const mozillaExpunged = 0x0008

// startMailbox adds a mailbox at rel to the walk's counts and returns it, or nil when the
// walk does not count.
func (w *localWalk) startMailbox(rel string) *MailboxCount {
	c := w.opts.Counts
	if c == nil || !c.Count {
		return nil
	}
	mb := &MailboxCount{Rel: rel}
	c.Mailboxes = append(c.Mailboxes, mb)
	return mb
}

// noteEntry notes a directory entry the walk listed: a name ending in ".msf" sets Msf.
func (w *localWalk) noteEntry(name string) {
	if c := w.opts.Counts; c != nil && strings.HasSuffix(strings.ToLower(name), ".msf") {
		c.Msf = true
	}
}

// count counts one message read into mb (nil: not counted).
func (w *localWalk) count(mb *MailboxCount, raw []byte) {
	if mb == nil {
		return
	}
	hb := headerSection(raw)
	if st, ok := headerValue(hb, "X-Mozilla-Status"); ok {
		if v, err := strconv.ParseUint(strings.TrimSpace(st), 16, 32); err == nil && v&mozillaExpunged != 0 {
			mb.Expunged++
			return
		}
	}
	mb.Messages++
	if !w.opts.Counts.Distinct {
		return
	}
	id, _ := headerValue(hb, "Message-ID")
	id = strings.TrimSpace(id)
	id = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(id, "<"), ">"))
	if id == "" {
		mb.NoID++
		return
	}
	sum := sha256.Sum256([]byte(id))
	var key [16]byte
	copy(key[:], sum[:16])
	mb.IDs = append(mb.IDs, key)
}

// headerSection is raw's header section (headerBlock), or, when no blank line ends it within
// headerBlock's bound, the bound's leading bytes (as headerBlockHas reads them).
func headerSection(raw []byte) string {
	if hb := headerBlock(raw); hb != "" {
		return hb
	}
	const limit = 64 << 10
	if len(raw) > limit {
		raw = raw[:limit]
	}
	return string(raw)
}

// headerValue is the first field named field (compared case-blind) in the header section
// hb, its continuation lines joined, and whether there was one.
func headerValue(hb, field string) (string, bool) {
	prefix := strings.ToLower(field) + ":"
	lines := strings.Split(hb, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		if len(line) < len(prefix) || strings.ToLower(line[:len(prefix)]) != prefix {
			continue
		}
		v := line[len(prefix):]
		for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t")) {
			i++
			v += " " + strings.TrimSpace(strings.TrimSuffix(lines[i], "\r"))
		}
		return v, true
	}
	return "", false
}
