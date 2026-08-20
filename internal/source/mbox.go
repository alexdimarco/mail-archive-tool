package source

import (
	"bytes"
	"fmt"
	"io"
	netmail "net/mail"
	"os"
	"path/filepath"
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

// safeParseMessage wraps parseMessage so a panic in the MIME/charset stack on a
// single crafted message becomes a visible stub instead of aborting the whole
// archive (R10 — robust parsing; the item is never silently dropped).
func safeParseMessage(data []byte) (m *model.Message) {
	defer func() {
		if r := recover(); r != nil {
			m = &model.Message{Subject: "(unreadable message)"}
		}
	}()
	return parseMessage(data)
}

// parseMessage converts one raw RFC 5322 message into a model.Message, falling
// back to a header-only parse if full MIME parsing fails (nothing is dropped).
func parseMessage(data []byte) *model.Message {
	mr, err := mail.CreateReader(bytes.NewReader(data))
	if err != nil {
		return fallbackParse(data)
	}
	h := mr.Header

	msg := &model.Message{}
	msg.Subject, _ = h.Subject()
	if id, err := h.MessageID(); err == nil {
		msg.InternetMessageID = id
	}
	if d, err := h.Date(); err == nil && !d.IsZero() {
		msg.Received = d.UTC()
	}
	if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
		msg.SenderName = from[0].Name
		msg.SenderEmail = from[0].Address
	}
	msg.To = addressList(&h, "To")
	msg.Cc = addressList(&h, "Cc")

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
		return &model.Message{Subject: "(unparseable message)", PlainBody: string(data)}
	}
	body, _ := io.ReadAll(m.Body)
	msg := &model.Message{
		Subject:           m.Header.Get("Subject"),
		InternetMessageID: strings.Trim(m.Header.Get("Message-Id"), "<>"),
		To:                m.Header.Get("To"),
		Cc:                m.Header.Get("Cc"),
		PlainBody:         string(body),
	}
	if addrs, err := m.Header.AddressList("From"); err == nil && len(addrs) > 0 {
		msg.SenderName = addrs[0].Name
		msg.SenderEmail = addrs[0].Address
	}
	if d, err := m.Header.Date(); err == nil {
		msg.Received = d.UTC()
	}
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

func bytesWriterTo(data []byte) func(io.Writer) (int64, error) {
	return func(w io.Writer) (int64, error) {
		n, err := w.Write(data)
		return int64(n), err
	}
}
