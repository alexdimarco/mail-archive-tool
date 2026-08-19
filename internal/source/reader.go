// Package source opens Outlook data files (.pst/.ost) and yields normalized
// messages, hiding the go-pst API behind a small callback-based walk.
package source

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	charsets "github.com/emersion/go-message/charset"
	pst "github.com/mooijtech/go-pst/v6/pkg"
	"github.com/mooijtech/go-pst/v6/pkg/properties"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/util"
)

// MAPI property IDs read directly from the property context. go-pst's generated
// struct getters only decode Unicode/integer/time/boolean properties and skip
// binary ones, so binary-stored text (notably PidTagHtml in modern Outlook)
// must be read this way to avoid silently losing it.
const (
	pidTagDisplayName = 12289 // 0x3001, message-store display name
	pidTagBody        = 4096  // 0x1000, PidTagBody (plain text)
	pidTagHtml        = 4115  // 0x1013, PidTagHtml
)

// registerCharsets wires go-message's charset catalogue into go-pst once, so
// legacy (non-Unicode) String8 properties decode correctly.
var registerCharsets = sync.OnceFunc(func() {
	pst.ExtendCharsets(func(name string, enc encoding.Encoding) {
		charsets.RegisterEncoding(name, enc)
	})
})

// syntheticFolders are container folders Outlook creates that carry no useful
// name; their segment is dropped from the mirrored path.
var syntheticFolders = map[string]bool{
	"ROOT_FOLDER":              true,
	"Root":                     true,
	"Root - Mailbox":           true,
	"Root Container":           true,
	"Top of Personal Folders":  true,
	"Top of Outlook data file": true,
	"Top of Information Store": true,
	"IPM_SUBTREE":              true,
}

// Reader is an open Outlook data file. It implements Source.
type Reader struct {
	file   *pst.File
	closer io.Closer
	store  string
}

// openPST opens the .pst/.ost file at path.
func openPST(path string) (*Reader, error) {
	registerCharsets()

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	pstFile, err := pst.New(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	r := &Reader{
		file:   pstFile,
		closer: f,
		store:  pstStoreName(pstFile, path),
	}
	return r, nil
}

// StoreName returns the display name of the mail store.
func (r *Reader) StoreName() string { return r.store }

// Close releases the underlying file.
func (r *Reader) Close() error {
	r.file.Cleanup()
	return r.closer.Close()
}

// Walk traverses every folder depth-first, invoking handler for each mail item.
// Non-mail items (appointments, contacts, tasks) are skipped in this version.
func (r *Reader) Walk(handler MessageHandler) error {
	root, err := r.file.GetRootFolder()
	if err != nil {
		return fmt.Errorf("get root folder: %w", err)
	}
	return r.walk(&root, nil, handler)
}

func (r *Reader) walk(folder *pst.Folder, path []string, handler MessageHandler) error {
	folderPath := path
	if name := strings.TrimSpace(folder.Name); name != "" && !syntheticFolders[name] {
		folderPath = append(append([]string{}, path...), util.SanitizeSegment(name))
	}

	if folder.MessageCount > 0 {
		if err := r.walkMessages(folder, folderPath, handler); err != nil {
			return err
		}
	}

	subFolders, err := folder.GetSubFolders()
	if err != nil {
		return fmt.Errorf("get sub-folders of %q: %w", folder.Name, err)
	}
	for i := range subFolders {
		if err := r.walk(&subFolders[i], folderPath, handler); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reader) walkMessages(folder *pst.Folder, folderPath []string, handler MessageHandler) error {
	it, err := folder.GetMessageIterator()
	if errors.Is(err, pst.ErrMessagesNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("message iterator for %q: %w", folder.Name, err)
	}

	for it.Next() {
		msg, err := safeConvertMessage(it.Value())
		if err != nil {
			return fmt.Errorf("convert message in %q: %w", folder.Name, err)
		}
		if msg == nil {
			continue // not a mail item
		}
		if err := handler(folderPath, msg); err != nil {
			return err
		}
	}
	return it.Err()
}

// safeConvertMessage wraps convertMessage so a panic deep in the PST binary
// parser on one crafted node becomes a visible stub instead of aborting the
// whole archive (R10 — robust parsing).
func safeConvertMessage(m *pst.Message) (out *model.Message, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = &model.Message{Subject: "(unreadable message)"}, nil
		}
	}()
	return convertMessage(m)
}

// convertMessage maps a go-pst message into a model.Message, or returns nil for
// non-mail items.
func convertMessage(m *pst.Message) (*model.Message, error) {
	mp, ok := m.Properties.(*properties.Message)
	if !ok {
		return nil, nil
	}

	// Read the HTML body straight from the property context: modern Outlook
	// stores PidTagHtml as binary, which go-pst's GetBodyHtml() does not decode.
	htmlBody := readTextProperty(m, pidTagHtml)
	plainBody := mp.GetBody()
	if strings.TrimSpace(plainBody) == "" {
		plainBody = readTextProperty(m, pidTagBody)
	}

	msg := &model.Message{
		Subject:           mp.GetSubject(),
		SenderName:        mp.GetSenderName(),
		SenderEmail:       mp.GetSenderEmailAddress(),
		To:                mp.GetDisplayTo(),
		Cc:                mp.GetDisplayCc(),
		InternetMessageID: mp.GetInternetMessageId(),
		HTMLBody:          htmlBody,
		PlainBody:         plainBody,
	}
	if t := mp.GetMessageDeliveryTime(); t != 0 {
		msg.Received = time.Unix(0, t).UTC()
	}
	if t := mp.GetClientSubmitTime(); t != 0 {
		msg.Sent = time.Unix(0, t).UTC()
	}

	// Only pay the cost of decompressing RTF when there is no better body.
	if strings.TrimSpace(msg.HTMLBody) == "" && strings.TrimSpace(msg.PlainBody) == "" {
		if rtf, err := m.GetBodyRTF(); err == nil {
			msg.RTFBody = rtf
		}
	}

	atts, err := collectAttachments(m)
	if err != nil {
		return nil, err
	}
	msg.Attachments = atts
	return msg, nil
}

// readTextProperty reads a text-valued property directly from the message's
// property context, handling both Unicode (PtypString, UTF-16LE) and binary
// (PtypBinary) storage. It returns "" when the property is absent or unreadable.
func readTextProperty(m *pst.Message, propertyID uint16) string {
	r, err := m.PropertyContext.GetPropertyReader(propertyID, m.LocalDescriptors)
	if err != nil {
		return ""
	}

	// Unicode strings decode correctly through the library helper.
	if r.Property.Type == pst.PropertyTypeString {
		if s, err := r.GetString(); err == nil {
			return s
		}
	}

	size := r.Size()
	if size <= 0 {
		return ""
	}
	buf := make([]byte, size)
	if _, err := r.ReadAt(buf, 0); err != nil {
		return ""
	}
	return decodeBytes(buf)
}

// decodeBytes turns raw binary text bytes into a Go string. Modern Outlook
// stores PidTagHtml as UTF-8 bytes; legacy messages use a single-byte codepage,
// for which Windows-1252 is the safe western default.
func decodeBytes(buf []byte) string {
	if utf8.Valid(buf) {
		return string(buf)
	}
	if s, err := charmap.Windows1252.NewDecoder().String(string(buf)); err == nil {
		return s
	}
	return string(buf)
}

func collectAttachments(m *pst.Message) ([]model.Attachment, error) {
	it, err := m.GetAttachmentIterator()
	if errors.Is(err, pst.ErrAttachmentsNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []model.Attachment
	idx := 0
	for it.Next() {
		a := it.Value() // fresh *Attachment per iteration, safe to capture

		name := a.GetAttachLongFilename()
		if name == "" {
			name = a.GetAttachFilename()
		}
		if name == "" {
			if ext := a.GetAttachExtension(); ext != "" {
				name = fmt.Sprintf("attachment-%d%s", idx, ext)
			}
		}

		out = append(out, model.Attachment{
			Filename:  name,
			MimeType:  a.GetAttachMimeTag(),
			ContentID: a.GetAttachContentId(),
			WriteTo:   func(w io.Writer) (int64, error) { return a.WriteTo(w) },
		})
		idx++
	}
	if it.Err() != nil {
		return nil, it.Err()
	}
	return out, nil
}

// pstStoreName derives a human-readable label for the data file, preferring the
// message-store display name and falling back to the file's base name.
func pstStoreName(f *pst.File, path string) string {
	if pc, err := f.GetMessageStore(); err == nil {
		if reader, err := pc.GetPropertyReader(pidTagDisplayName, nil); err == nil {
			if name, err := reader.GetString(); err == nil && strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name)
			}
		}
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
