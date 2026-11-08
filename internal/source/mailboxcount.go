package source

import (
	"bytes"
	"crypto/sha256"
	"io"
	"regexp"
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
	// Distinct also keeps each counted message's identity key (Gmail's X, §3: distinct
	// messages across the account, the C2 pass's identity rule).
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
	// Messages counted: every message read, but those carrying the Expunged bit, and (an
	// mbox file) only the pieces that begin at a separator Thunderbird writes (Unseparated).
	Messages int64
	// Expunged is how many were left out for the Expunged bit.
	Expunged int64
	// Keys (when Distinct) is one identity key per counted message that has one (the C2
	// pass's rule, IdentityKey): the same email in two folders has one key, so Gmail's X counts
	// it once.
	Keys [][16]byte
	// NoKey (when Distinct) is how many counted messages have no identity key: none is
	// expected; each is left out of X, and the account may not say complete.
	NoKey int64
	// Unseparated is how many pieces the mbox reader handed on that do not begin at a
	// separator Thunderbird writes (a body line starting "From " that the message's writer did
	// not escape, for example). They are archived as before, never counted; a mailbox with any
	// is "split differently" and its account can't be told complete.
	Unseparated int64
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
	if key, ok := IdentityKey(raw); ok {
		mb.Keys = append(mb.Keys, key)
		return
	}
	mb.NoKey++
}

// countPiece counts one piece of an mbox file: separated says it begins at a separator
// Thunderbird writes (separatorScan); one that does not is noted, never counted.
func (w *localWalk) countPiece(mb *MailboxCount, raw []byte, separated bool) {
	if mb == nil {
		return
	}
	if !separated {
		mb.Unseparated++
		return
	}
	w.count(mb, raw)
}

// countMaildirFile counts one maildir message file named name, leaving out of the count what
// is no message of Thunderbird's maildir store: a name starting with "." (a Finder .DS_Store,
// an editor's or a sync tool's own file) and an empty file. Such files are archived as before;
// tmp/ is never read at all. Not checked against Thunderbird's maildir store source, which the
// copies under .claude do not include (the C2 pass's LOW item 4).
func (w *localWalk) countMaildirFile(mb *MailboxCount, name string, raw []byte) {
	if mb == nil || strings.HasPrefix(name, ".") || len(raw) == 0 {
		return
	}
	w.count(mb, raw)
}

// IdentityKey is a message's identity for Gmail's distinct count (the C2 pass's rule, the
// completeness design §2.4 adjustment 2 as amended): the first 16 bytes of the SHA-256 of its
// Message-ID (read case-blind, white space allowed before the colon, unfolded, trimmed of white
// space and angle brackets) when it has one; else the first 16 bytes of the SHA-256 of
// "content", a NUL, and its bytes without the lines Thunderbird rewrites per copy
// (X-Mozilla-Status, X-Mozilla-Status2 and X-Mozilla-Keys of its top header block, with their
// continuation lines: the gmail-dedup design's §4.4 split). ok is false when neither can be
// told: no Message-ID, and no header block end within the 64 KiB bound of a longer message, or
// more of those lines than the split's bound (8 lines, 1 KiB).
func IdentityKey(raw []byte) ([16]byte, bool) {
	var key [16]byte
	id, _ := headerValue(headerSection(raw), "Message-ID")
	id = strings.TrimSpace(id)
	id = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(id, "<"), ">"))
	if id != "" {
		sum := sha256.Sum256([]byte(id))
		copy(key[:], sum[:16])
		return key, true
	}
	h := sha256.New()
	h.Write([]byte("content\x00"))
	if !writeWithoutMozillaLines(h, raw) {
		return key, false
	}
	copy(key[:], h.Sum(nil)[:16])
	return key, true
}

// The bounds of the content identity's split (the gmail-dedup design §4.4 step 1).
const (
	headerBound       = 64 << 10
	mozillaLinesMax   = 8
	mozillaLinesBytes = 1 << 10
)

// writeWithoutMozillaLines writes raw to w without the X-Mozilla-Status, X-Mozilla-Status2 and
// X-Mozilla-Keys lines of its top header block (with their continuation lines). false when the
// block's end is not within headerBound of a message longer than it, or the lines pass their
// bound.
func writeWithoutMozillaLines(w io.Writer, raw []byte) bool {
	lines, size := 0, 0
	removing, ended := false, false
	start := 0 // the first byte not yet written
	pos := 0
	for pos < len(raw) {
		if pos >= headerBound {
			return false // no header end within the bound
		}
		end := len(raw)
		if i := bytes.IndexByte(raw[pos:], '\n'); i >= 0 {
			end = pos + i + 1
		}
		line := raw[pos:end]
		content := bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
		if len(content) == 0 {
			ended = true // the empty line: the header block ends
			break
		}
		switch {
		case content[0] == ' ' || content[0] == '\t':
			// a continuation line: removed with its field
		default:
			removing = isMozillaLine(content)
		}
		if removing {
			lines++
			size += len(line)
			if lines > mozillaLinesMax || size > mozillaLinesBytes {
				return false
			}
			w.Write(raw[start:pos])
			start = end
		}
		pos = end
	}
	if !ended && len(raw) > headerBound {
		return false // all header, longer than the bound: its end is not within it
	}
	w.Write(raw[start:])
	return true
}

// isMozillaLine reports whether a header line is one of Thunderbird's per-copy lines.
func isMozillaLine(line []byte) bool {
	name, _, ok := bytes.Cut(line, []byte(":"))
	if !ok {
		return false
	}
	name = bytes.TrimRight(name, " \t")
	for _, f := range []string{"X-Mozilla-Status", "X-Mozilla-Status2", "X-Mozilla-Keys"} {
		if strings.EqualFold(string(name), f) {
			return true
		}
	}
	return false
}

// headerSection is raw's header section (headerBlock), or, when no blank line ends it within
// headerBlock's bound, the bound's leading bytes (as headerBlockHas reads them).
func headerSection(raw []byte) string {
	if hb := headerBlock(raw); hb != "" {
		return hb
	}
	if len(raw) > headerBound {
		raw = raw[:headerBound]
	}
	return string(raw)
}

// headerValue is the first field named field (compared case-blind, white space allowed
// before the colon as RFC 5322's obsolete syntax allows) in the header section hb, its
// continuation lines joined, and whether there was one.
func headerValue(hb, field string) (string, bool) {
	name := strings.ToLower(field)
	lines := strings.Split(hb, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\r")
		if len(line) < len(name) || strings.ToLower(line[:len(name)]) != name {
			continue
		}
		rest := strings.TrimLeft(line[len(name):], " \t")
		if !strings.HasPrefix(rest, ":") {
			continue
		}
		v := rest[1:]
		for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t")) {
			i++
			v += " " + strings.TrimSpace(strings.TrimSuffix(lines[i], "\r"))
		}
		return v, true
	}
	return "", false
}

// separatorShape is the "From " line Thunderbird writes ("From - " and the time as C's ctime
// gives it, nsImapMailFolder.cpp's SetupMsgWriteStream) and the classic mbox form the repo's
// fixtures use ("From sender Mon Mar  3 09:00:00 2025"), a time zone allowed beside the year.
var separatorShape = regexp.MustCompile(`^From \S+ +(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) +(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) +\d{1,2} +\d{1,2}:\d{2}(?::\d{2})?(?: +(?:[A-Z]{1,5}|[+-]\d{4}))? +\d{4}(?: +(?:[A-Z]{1,5}|[+-]\d{4}))? *$`)

// maxSeparatorLine bounds the bytes of a line the scan keeps: a separator is far shorter.
const maxSeparatorLine = 256

// separatorScan watches the bytes the mbox reader reads (go-mbox, which starts a new piece at
// every line beginning "From ") and notes, for each such line in order, whether it is a
// separator Thunderbird writes: at the file's start or after an empty line, and of
// separatorShape. The reader's n-th piece begins at the n-th note. It changes no byte.
type separatorScan struct {
	r         io.Reader
	marks     []bool
	line      []byte
	lineLen   int
	prevBlank bool
}

func newSeparatorScan(r io.Reader) *separatorScan {
	return &separatorScan{r: r, prevBlank: true}
}

func (s *separatorScan) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	for _, b := range p[:n] {
		if b == '\n' {
			s.endLine()
			continue
		}
		if s.lineLen < maxSeparatorLine {
			s.line = append(s.line, b)
		}
		s.lineLen++
	}
	if err != nil && s.lineLen > 0 {
		s.endLine() // the last line has no newline
	}
	return n, err
}

func (s *separatorScan) endLine() {
	content := bytes.TrimSuffix(s.line, []byte("\r"))
	long := s.lineLen > maxSeparatorLine
	if bytes.HasPrefix(s.line, []byte("From ")) {
		s.marks = append(s.marks, s.prevBlank && !long && separatorShape.Match(content))
	}
	s.prevBlank = !long && len(content) == 0
	s.line, s.lineLen = s.line[:0], 0
}

// separated reports whether the reader's n-th piece (from 1) begins at a separator
// Thunderbird writes; a piece the scan saw no line for is not.
func (s *separatorScan) separated(n int) bool {
	return n >= 1 && n <= len(s.marks) && s.marks[n-1]
}
