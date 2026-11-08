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
	// Messages counted for certain (the STRICT count, the C2 pass round 2's R2-04): every
	// message read, but those carrying the Expunged bit; in an mbox file only the pieces that
	// begin at a separator Thunderbird writes (separatorShape) AND whose header holds
	// X-Mozilla-Status, the line Thunderbird writes at the top of every offline copy
	// (nsImapMailFolder.cpp's SetupMsgWriteStream). Only this count may make an account
	// "caught up". A maildir file is one message whatever its header (no separator to mistake).
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
	// Loose is how many pieces of an mbox file begin at a separator Thunderbird writes but
	// carry no X-Mozilla-Status (a body line of exactly the separator's shape after an empty
	// line, or a real message Thunderbird wrote without the line: the owner's profile holds 2 of
	// 318,856). They are archived as before; the LENIENT count is Messages + Loose, and an
	// account is "behind" only if it is short even by the lenient count, so a piece without
	// the line can neither make an account complete nor make it behind.
	Loose int64
	// LooseKeys and LooseNoKey (when Distinct) are the Loose pieces' identity keys and how
	// many had none, as Keys and NoKey are the counted messages'.
	LooseKeys  [][16]byte
	LooseNoKey int64
	// Unseparated is how many pieces the mbox reader handed on that begin at a "From " line of
	// another shape (a body line that the message's writer did not escape, a git patch's first
	// line, for example). They are archived as before, never counted; a mailbox with any is
	// "split differently" and its account can't be told complete. Such a piece cannot hold a
	// message Thunderbird wrote (that would begin at a line of separatorShape), so it does not
	// stop a certain gap from being told "behind".
	Unseparated int64
	// Glued is how many pieces begin at a line of separatorShape with no empty line before it.
	// Thunderbird writes an empty line before every separator (the owner's profile: every one
	// of 318,856), so such a piece may be a message the count could not take, or a body line:
	// its mailbox is split differently and its account is never told "behind" (R2-05).
	Glued int64
	// Misaligned: the mbox reader handed on a different number of pieces than the separator scan
	// saw "From " lines (the C2 pass round 3's R3-01, the lead's call: go-mbox reads a line through
	// a 4,096-byte buffer, and a "From " line longer than that can end one more piece at a later
	// chunk). Then no piece is matched to a separator: Messages, Loose, Expunged, Glued,
	// Unseparated and the keys stay 0, Uncounted is how many pieces were handed on (archived as
	// before), and the mailbox is split differently with its account never told "behind". The
	// count never depends on where the reader's chunks fall.
	Misaligned bool
	Uncounted  int64
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

// pieceCount is what the count reads of one message's bytes, whatever its place in the file.
type pieceCount struct {
	expunged, status, keyed bool
	key                     [16]byte
}

// read is what the count takes of raw: its X-Mozilla-Status (and the Expunged bit), and, when
// the walk keeps identities, its identity key.
func (w *localWalk) read(raw []byte) pieceCount {
	var p pieceCount
	st, status := headerValue(headerSection(raw), "X-Mozilla-Status")
	p.status = status
	if status {
		if v, err := strconv.ParseUint(strings.TrimSpace(st), 16, 32); err == nil && v&mozillaExpunged != 0 {
			p.expunged = true
			return p
		}
	}
	if w.opts.Counts.Distinct {
		p.key, p.keyed = IdentityKey(raw)
	}
	return p
}

// count counts one message read into mb (nil: not counted). mbox says it is a piece of an mbox
// file that began at a separator: it is counted for certain only when its header holds
// X-Mozilla-Status, else it is Loose.
func (w *localWalk) count(mb *MailboxCount, raw []byte, mbox bool) {
	if mb == nil {
		return
	}
	w.add(mb, w.read(raw), mbox)
}

// add adds one message's reading p to mb, as count says.
func (w *localWalk) add(mb *MailboxCount, p pieceCount, mbox bool) {
	if p.expunged {
		mb.Expunged++
		return
	}
	loose := mbox && !p.status
	if loose {
		mb.Loose++
	} else {
		mb.Messages++
	}
	if !w.opts.Counts.Distinct {
		return
	}
	switch {
	case loose && p.keyed:
		mb.LooseKeys = append(mb.LooseKeys, p.key)
	case loose:
		mb.LooseNoKey++
	case p.keyed:
		mb.Keys = append(mb.Keys, p.key)
	default:
		mb.NoKey++
	}
}

// settlePieces counts an mbox file's pieces into mb once the reader has handed on its last (the
// C2 pass round 3's R3-01): each piece is matched to the scan's note of the same place only when
// the reader handed on exactly as many pieces as the scan saw "From " lines. go-mbox ends a piece
// at every line the scan notes, and may end more (a fresh piece reader forgets it was in the
// middle of a line longer than its buffer), never fewer, so equal numbers are equal splits.
// Otherwise the mailbox is Misaligned and no piece is counted. Then each matched piece is counted
// by how it begins: at a separator Thunderbird writes, as count says; at a separator-shaped line
// with no empty line before it, Glued; at a "From " line of another shape, Unseparated.
func (w *localWalk) settlePieces(mb *MailboxCount, pieces []pieceCount, seps *separatorScan) {
	if mb == nil {
		return
	}
	if len(pieces) != len(seps.marks) {
		mb.Misaligned, mb.Uncounted = true, int64(len(pieces))
		return
	}
	for i, p := range pieces {
		switch seps.marks[i] {
		case startSeparator:
			w.add(mb, p, true)
		case startGlued:
			mb.Glued++
		default:
			mb.Unseparated++
		}
	}
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
	w.count(mb, raw, false)
}

// IdentityKey is a message's identity for Gmail's distinct count (the C2 pass's rule, the
// completeness design §2.4 adjustment 2 as amended): the first 16 bytes of the SHA-256 of its
// Message-ID (read case-blind, white space allowed before the colon, unfolded, trimmed of white
// space and angle brackets) when it has one; else the first 16 bytes of the SHA-256 of
// "content", a NUL, and its bytes without the lines Thunderbird rewrites per copy
// (X-Mozilla-Status, X-Mozilla-Status2 and X-Mozilla-Keys of its top header block, with their
// continuation lines: the gmail-dedup design's §4.4 split), with every CRLF read as LF so an LF
// and a CRLF copy of one email have one key (the C2 pass round 2's R2-03; for counting only: the
// gmail-dedup design merges byte-identical copies only, and keeps its stricter identity). The
// Message-ID is read only from a header block that ends within the 64 KiB bound (headerWithin,
// R2-01): from a block cut at the bound a Message-ID line may be cut too, and two copies whose
// per-copy lines differ in length would get two keys. ok is false when neither can be told: no
// Message-ID read, and no header block end within the bound of a longer message, or more of
// Thunderbird's per-copy lines than the split's bound (8 lines, 1 KiB).
func IdentityKey(raw []byte) ([16]byte, bool) {
	var key [16]byte
	if hb, ok := headerWithin(raw); ok {
		id, _ := headerValue(hb, "Message-ID")
		id = strings.TrimSpace(id)
		id = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(id, "<"), ">"))
		if id != "" {
			sum := sha256.Sum256([]byte(id))
			copy(key[:], sum[:16])
			return key, true
		}
	}
	h := sha256.New()
	h.Write([]byte("content\x00"))
	lf := &lfWriter{w: h}
	if !writeWithoutMozillaLines(lf, raw) {
		return key, false
	}
	lf.flush()
	copy(key[:], h.Sum(nil)[:16])
	return key, true
}

// headerWithin is raw's header block when its end lies within headerBound: the bytes before its
// first empty line ("" when it begins with one), or the whole of a message no longer than the
// bound that has none. ok is false otherwise: the block may run past the bound.
func headerWithin(raw []byte) (string, bool) {
	if bytes.HasPrefix(raw, []byte("\n")) || bytes.HasPrefix(raw, []byte("\r\n")) {
		return "", true
	}
	if hb := headerBlock(raw); hb != "" {
		return hb, true
	}
	if len(raw) <= headerBound {
		return string(raw), true
	}
	return "", false
}

// lfWriter writes to w what it is given with every CRLF written as LF (a CR before anything else
// is kept). flush writes a CR still held at the end.
type lfWriter struct {
	w  io.Writer
	cr bool
}

func (l *lfWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		if l.cr {
			l.cr = false
			if p[0] != '\n' {
				l.w.Write([]byte{'\r'})
			}
		}
		i := bytes.IndexByte(p, '\r')
		if i < 0 {
			l.w.Write(p)
			break
		}
		l.w.Write(p[:i])
		l.cr, p = true, p[i+1:]
	}
	return n, nil
}

func (l *lfWriter) flush() {
	if l.cr {
		l.w.Write([]byte{'\r'})
		l.cr = false
	}
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

// separatorShape is the "From " line Thunderbird writes and only it (the C2 pass round 2's
// R2-04): "From - " and the time as C's ctime gives it, its newline cut
// (nsImapMailFolder.cpp's SetupMsgWriteStream: `ct[24] = 0; result = "From - "; result += ct;`).
// The day of the month is ctime's: space-padded ("Mar  3", glibc and macOS) or two digits
// ("Mar 03", the Windows C library writes it zero-padded: the builder's reading, not checked on
// a Windows store). Nothing else: no other sender word (a git patch's first line, "From <sha>
// Mon Sep 17 00:00:00 2001", is not one), no time zone, no trailing text. A CR before the line's
// LF is taken off before matching. The owner's real macOS profile (counts only, 2026-11-08):
// every one of its 318,856 lines starting "From " is of this shape after an empty line or at the
// file's start.
var separatorShape = regexp.MustCompile(`^From - (?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) (?: \d|\d\d) \d\d:\d\d:\d\d \d{4}$`)

// maxSeparatorLine bounds the bytes of a line the scan keeps: a separator is far shorter.
const maxSeparatorLine = 256

// pieceStart is how a piece the mbox reader split off begins.
type pieceStart uint8

const (
	startBody      pieceStart = iota // a "From " line of another shape (or none the scan saw)
	startSeparator                   // a separator Thunderbird writes, at the file's start or after an empty line
	startGlued                       // a line of separatorShape with no empty line before it
)

// separatorScan watches the bytes the mbox reader reads (go-mbox, which starts a new piece at
// every line beginning "From ") and notes, for each such line in order, how it begins a piece:
// a separator Thunderbird writes (of separatorShape, at the file's start or after an empty
// line), a glued one (of the shape, no empty line before it), or another line. The reader's n-th
// piece begins at the n-th note only when the reader handed on as many pieces as there are notes
// (settlePieces checks it: R3-01). It changes no byte.
type separatorScan struct {
	r         io.Reader
	marks     []pieceStart
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
		start := startBody
		if !long && separatorShape.Match(content) {
			start = startGlued
			if s.prevBlank {
				start = startSeparator
			}
		}
		s.marks = append(s.marks, start)
	}
	s.prevBlank = !long && len(content) == 0
	s.line, s.lineLen = s.line[:0], 0
}

// start is how the reader's n-th piece (from 1) begins; a piece the scan saw no line for is
// startBody, never counted.
func (s *separatorScan) start(n int) pieceStart {
	if n < 1 || n > len(s.marks) {
		return startBody
	}
	return s.marks[n-1]
}
