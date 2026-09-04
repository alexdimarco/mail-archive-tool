package source

import (
	"bytes"
	"fmt"
	"io"
	netmail "net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gombox "github.com/emersion/go-mbox"
	_ "github.com/emersion/go-message/charset" // register charsets for MIME decoding
	"github.com/emersion/go-message/mail"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/util"
)

// mboxReader reads Thunderbird / mbox mail stores. It implements Source.
type mboxReader struct {
	root  string // a mail directory (store) or a single mbox file
	isDir bool
	store string
}

func openMboxDir(dir string) (*mboxReader, error) {
	clean := strings.TrimRight(dir, string(os.PathSeparator))
	return &mboxReader{root: dir, isDir: true, store: filepath.Base(clean)}, nil
}

func openMboxFile(path string) (*mboxReader, error) {
	base := filepath.Base(path)
	return &mboxReader{root: path, isDir: false, store: strings.TrimSuffix(base, filepath.Ext(base))}, nil
}

func (r *mboxReader) StoreName() string { return r.store }
func (r *mboxReader) Close() error      { return nil }

func (r *mboxReader) Walk(handler MessageHandler) error {
	if !r.isDir {
		// A single mbox file is its own store; messages go directly under it
		// (no redundant <name>/<name> nesting).
		return r.walkFile(r.root, nil, handler)
	}
	// A directory that is itself a maildir folder is a single store.
	if isMaildir(r.root) {
		return readMaildir(r.root, nil, handler)
	}
	return r.walkDir(r.root, nil, handler)
}

// walkDir recurses a mail directory. Each folder is either an mbox file or a
// maildir directory; its subfolders live in a sibling "<name>.sbd" directory
// (Thunderbird's layout).
func (r *mboxReader) walkDir(dir string, prefix []string, handler MessageHandler) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read mail dir %s: %w", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dir, name)

		if e.IsDir() {
			if strings.HasSuffix(name, ".sbd") {
				continue // subfolder container, reached via its parent folder
			}
			if isMaildir(full) {
				folderPath := append(append([]string{}, prefix...), util.SanitizeSegment(name))
				if err := readMaildir(full, folderPath, handler); err != nil {
					return err
				}
				if err := r.walkSbd(dir, name, folderPath, handler); err != nil {
					return err
				}
			}
			continue
		}

		if !isMailboxCandidate(name) || !looksLikeMbox(full) {
			continue
		}
		folderPath := append(append([]string{}, prefix...), util.SanitizeSegment(name))
		if err := r.walkFile(full, folderPath, handler); err != nil {
			return err
		}
		if err := r.walkSbd(dir, name, folderPath, handler); err != nil {
			return err
		}
	}
	return nil
}

// walkSbd recurses into a "<name>.sbd" subfolder container if it exists.
func (r *mboxReader) walkSbd(dir, name string, folderPath []string, handler MessageHandler) error {
	sbd := filepath.Join(dir, name+".sbd")
	if fi, err := os.Stat(sbd); err == nil && fi.IsDir() {
		return r.walkDir(sbd, folderPath, handler)
	}
	return nil
}

// isMaildir reports whether dir is a maildir folder (has a "cur" or "new" subdir).
func isMaildir(dir string) bool {
	for _, sub := range []string{"cur", "new"} {
		if fi, err := os.Stat(filepath.Join(dir, sub)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// readMaildir reads every message file in a maildir folder's new/ and cur/ dirs.
// It is shared by the Thunderbird/mbox reader and the Evolution reader (both
// store folders as maildirs).
func readMaildir(dir string, folderPath []string, handler MessageHandler) error {
	for _, sub := range []string{"new", "cur"} {
		if err := readMaildirDir(filepath.Join(dir, sub), folderPath, handler); err != nil {
			return err
		}
	}
	return nil
}

// readMaildirDir reads every message file under a maildir new/ or cur/ directory.
// Standard maildirs store messages as flat files; Evolution sub-buckets them into
// two-hex-character shard directories (e.g. cur/04/<msg>), so we descend into
// subdirectories rather than skipping them.
func readMaildirDir(dir string, folderPath []string, handler MessageHandler) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // absent new/ or cur/: nothing to read
	}
	for _, fe := range entries {
		full := filepath.Join(dir, fe.Name())
		if fe.IsDir() {
			if err := readMaildirDir(full, folderPath, handler); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return fmt.Errorf("read maildir message %s: %w", fe.Name(), err)
		}
		if m := safeParseMessage(data); m != nil {
			// Read state for a maildir message is the "S" (Seen) info flag in its
			// filename — UNLESS the message also carries an X-Mozilla-Status
			// header (a Thunderbird artifact parseMessage already honoured, which
			// wins per the design). Threaded here because only the maildir layout
			// puts the read flag in the path.
			if !headerBlockHas(data, "X-Mozilla-Status") {
				m.Unread = !maildirSeen(fe.Name())
			}
			if err := handler(folderPath, m); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *mboxReader) walkFile(path string, folderPath []string, handler MessageHandler) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open mbox %s: %w", path, err)
	}
	defer f.Close()

	mr := gombox.NewReader(f)
	for {
		msgReader, err := mr.NextMessage()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read mbox %s: %w", path, err)
		}
		data, err := io.ReadAll(msgReader)
		if err != nil {
			return fmt.Errorf("read message in %s: %w", path, err)
		}
		if m := safeParseMessage(data); m != nil {
			if err := handler(folderPath, m); err != nil {
				return err
			}
		}
	}
}

// ParseRFC822 parses a raw RFC 5322 message into a normalized Message via the
// same tolerant, panic-safe path the mbox/maildir readers use. The Graph source
// uses it, since it fetches each message's raw MIME rather than reading a store.
func ParseRFC822(data []byte) *model.Message { return safeParseMessage(data) }

// safeParseMessage wraps parseMessage so a panic in the MIME/charset stack on a
// single crafted message becomes a visible stub instead of aborting the whole
// archive (R10 — robust parsing; the item is never silently dropped).
func safeParseMessage(data []byte) (m *model.Message) {
	defer func() {
		if r := recover(); r != nil {
			m = &model.Message{Subject: "(unreadable message)"}
		}
	}()
	m = parseMessage(data)
	if m != nil {
		m.TransportHeaders = headerBlock(data)
	}
	return m
}

// headerBlock returns a raw message's internet header section — the bytes
// before the first blank line — as stored, for display as transport headers.
// It looks only within the first 64 KiB: if no blank line (CRLFCRLF or LFLF)
// terminates the headers there, it returns "" rather than a misleading partial
// block, so a body-only blob or a pathologically long header run yields
// nothing. Shared by every raw-bytes source (mbox, maildir, Evolution) and the
// Graph MIME path, which reach it through ParseRFC822.
func headerBlock(raw []byte) string {
	const limit = 64 << 10 // 64 KiB
	scan := raw
	if len(scan) > limit {
		scan = scan[:limit]
	}
	end := -1
	if i := bytes.Index(scan, []byte("\r\n\r\n")); i >= 0 {
		end = i
	}
	if i := bytes.Index(scan, []byte("\n\n")); i >= 0 && (end < 0 || i < end) {
		end = i
	}
	if end < 0 {
		return ""
	}
	return string(raw[:end])
}

// parseMessage converts one raw RFC 5322 message into a model.Message, falling
// back to a header-only parse if full MIME parsing fails (nothing is dropped).
func parseMessage(data []byte) *model.Message {
	mr, err := mail.CreateReader(bytes.NewReader(data))
	if err != nil {
		return fallbackParse(data)
	}
	h := mr.Header

	msg := &model.Message{Raw: data}
	msg.Subject, _ = h.Subject()
	if id, err := h.MessageID(); err == nil {
		msg.InternetMessageID = id
	}
	if d, err := h.Date(); err == nil && !d.IsZero() {
		msg.Received = d // keep the original offset: it is part of the record
	}
	if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
		msg.SenderName = from[0].Name
		msg.SenderEmail = from[0].Address
	}
	msg.To = addressList(&h, "To")
	msg.Cc = addressList(&h, "Cc")
	msg.Bcc = addressList(&h, "Bcc")
	msg.ReplyTo = addressList(&h, "Reply-To")
	msg.InReplyTo = strings.Trim(h.Get("In-Reply-To"), "<> ")
	msg.References = strings.TrimSpace(h.Get("References"))

	// Message state from headers. Read from X-Mozilla-Status wins when present
	// (Thunderbird mbox); otherwise Unread is left for the maildir "S" flag or
	// stays false for a plain mbox with no read-state signal.
	msg.Importance = importanceFromHeaders(h.Get("Importance"), h.Get("X-Priority"))
	msg.Sensitivity = sensitivityFromHeader(h.Get("Sensitivity"))
	if unread, ok := mozillaReadFlag(h.Get("X-Mozilla-Status")); ok {
		msg.Unread = unread
	}
	// Categories come ONLY from Thunderbird's local tagging (X-Mozilla-Keys),
	// never the sender-settable RFC Keywords header (QC6).
	msg.Categories = mozillaCategories(h.Get("X-Mozilla-Keys"))

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		body, _ := io.ReadAll(p.Body)
		switch ph := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := ph.ContentType()
			switch {
			case strings.HasPrefix(ct, "text/html"):
				if msg.HTMLBody == "" {
					msg.HTMLBody = string(body)
				}
			case strings.HasPrefix(ct, "text/plain"):
				if msg.PlainBody == "" {
					msg.PlainBody = string(body)
				}
			default:
				// Inline non-text (e.g. an embedded image referenced by cid).
				cid := strings.Trim(ph.Get("Content-Id"), "<>")
				name := cid
				if name == "" {
					name = "inline"
				}
				msg.Attachments = append(msg.Attachments, model.Attachment{
					Filename: name, MimeType: ct, ContentID: cid, WriteTo: bytesWriterTo(body),
				})
			}
		case *mail.AttachmentHeader:
			filename, _ := ph.Filename()
			ct, _, _ := ph.ContentType()
			msg.Attachments = append(msg.Attachments, model.Attachment{
				Filename:  filename,
				MimeType:  ct,
				ContentID: strings.Trim(ph.Get("Content-Id"), "<>"),
				WriteTo:   bytesWriterTo(body),
			})
		}
	}
	return msg
}

// fallbackParse handles messages that are not full MIME (rare): keep the headers
// and the raw body as plain text so the message is still exported and indexed.
func fallbackParse(data []byte) *model.Message {
	m, err := netmail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return &model.Message{Subject: "(unparseable message)", PlainBody: string(data), Raw: data}
	}
	body, _ := io.ReadAll(m.Body)
	msg := &model.Message{
		Subject:           m.Header.Get("Subject"),
		InternetMessageID: strings.Trim(m.Header.Get("Message-Id"), "<>"),
		To:                m.Header.Get("To"),
		Cc:                m.Header.Get("Cc"),
		Bcc:               m.Header.Get("Bcc"),
		ReplyTo:           m.Header.Get("Reply-To"),
		InReplyTo:         strings.Trim(m.Header.Get("In-Reply-To"), "<> "),
		References:        strings.TrimSpace(m.Header.Get("References")),
		PlainBody:         string(body),
		Raw:               data,
	}
	if addrs, err := m.Header.AddressList("From"); err == nil && len(addrs) > 0 {
		msg.SenderName = addrs[0].Name
		msg.SenderEmail = addrs[0].Address
	}
	if d, err := m.Header.Date(); err == nil {
		msg.Received = d
	}
	msg.Importance = importanceFromHeaders(m.Header.Get("Importance"), m.Header.Get("X-Priority"))
	msg.Sensitivity = sensitivityFromHeader(m.Header.Get("Sensitivity"))
	if unread, ok := mozillaReadFlag(m.Header.Get("X-Mozilla-Status")); ok {
		msg.Unread = unread
	}
	msg.Categories = mozillaCategories(m.Header.Get("X-Mozilla-Keys"))
	return msg
}

func addressList(h *mail.Header, key string) string {
	addrs, err := h.AddressList(key)
	if err != nil || len(addrs) == 0 {
		return strings.TrimSpace(h.Get(key))
	}
	var parts []string
	for _, a := range addrs {
		if a.Name != "" {
			parts = append(parts, fmt.Sprintf("%s <%s>", a.Name, a.Address))
		} else {
			parts = append(parts, a.Address)
		}
	}
	return strings.Join(parts, ", ")
}

// importanceFromHeaders maps the Importance header ("high"/"normal"/"low") or,
// absent that, X-Priority (1-2 high, 4-5 low, 3 normal) to the model's
// convention; normal/unknown is the empty state.
func importanceFromHeaders(importance, xPriority string) string {
	switch strings.ToLower(strings.TrimSpace(importance)) {
	case "high":
		return "high"
	case "low":
		return "low"
	case "normal":
		return ""
	}
	// X-Priority is a digit, sometimes with a trailing word ("1 (Highest)").
	switch firstToken(xPriority) {
	case "1", "2":
		return "high"
	case "4", "5":
		return "low"
	}
	return ""
}

// sensitivityFromHeader maps the RFC 2156 Sensitivity header to the model's
// convention. "Company-Confidential" and "Confidential" both map to
// "confidential"; "normal"/absent is empty.
func sensitivityFromHeader(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "personal":
		return "personal"
	case "private":
		return "private"
	case "company-confidential", "confidential":
		return "confidential"
	}
	return ""
}

// mozillaReadFlag reads Thunderbird's X-Mozilla-Status (four hex digits); bit
// 0x0001 is MSG_FLAG_READ, so its absence means unread. ok is false when the
// header is absent or unparseable, letting the maildir "S" flag decide instead.
func mozillaReadFlag(s string) (unread, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return false, false
	}
	return v&0x0001 == 0, true
}

// mozillaCategories parses Thunderbird's X-Mozilla-Keys header — the local tag
// keys, whitespace-separated and often trailing-space-padded — into categories,
// control-stripped, de-duplicated and order-stable. Only this local-tagging
// header is a category source; the sender-settable RFC Keywords header is NOT
// read (QC6), so a sender cannot inject "categories". Absent/blank → nil.
func mozillaCategories(s string) []string {
	// Bound the untrusted header BEFORE strings.Fields (which allocates a token
	// per word up front) and cap the token count, mirroring the PST path's
	// blob/value bounds — only maxCategories can ever render, so scanning more is
	// pointless and a multi-MB X-Mozilla-Keys must not amplify memory.
	const maxScan = 64 << 10 // bytes scanned before splitting
	const maxTokens = 256    // >> the 64 the page renders
	if len(s) > maxScan {
		s = s[:maxScan]
	}
	var out []string
	seen := map[string]bool{}
	for _, tok := range strings.Fields(s) {
		if len(out) >= maxTokens {
			break
		}
		tok = strings.TrimSpace(util.StripControl(tok))
		if tok == "" || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// maildirSeen reports whether a maildir filename carries the "S" (Seen) info
// flag. The info section is "<unique>:2,<flags>"; on filesystems where ':' is
// illegal (Windows/NTFS) tools substitute another separator, so we locate the
// "2," version marker rather than the colon. A message in new/ (no info) or one
// without the flag is unseen (unread).
func maildirSeen(name string) bool {
	i := strings.LastIndex(name, "2,")
	if i < 0 {
		return false
	}
	return strings.ContainsRune(name[i+2:], 'S')
}

// headerBlockHas reports whether raw's header section (the lines before the
// first blank line) carries a header field with the given name
// (case-insensitive). Used to decide whether a maildir message already carries
// an X-Mozilla-Status header that should win over the filename's "S" flag.
func headerBlockHas(raw []byte, field string) bool {
	hb := headerBlock(raw)
	if hb == "" {
		// No blank line within the bound: scan the leading bytes anyway, so a
		// header-only maildir file (no body) is still recognised.
		const limit = 64 << 10
		if len(raw) > limit {
			raw = raw[:limit]
		}
		hb = string(raw)
	}
	prefix := strings.ToLower(field) + ":"
	for _, line := range strings.Split(hb, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimLeft(line, " \t")), prefix) {
			return true
		}
	}
	return false
}

func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i]
	}
	return s
}

func bytesWriterTo(data []byte) func(io.Writer) (int64, error) {
	return func(w io.Writer) (int64, error) {
		n, err := w.Write(data)
		return int64(n), err
	}
}
