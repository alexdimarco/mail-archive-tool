package export

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/util"
)

// WriteZip writes every attachment not in skip into a zip archive at zipPath.
// Attachments that produce zero bytes (e.g. embedded messages, or content not
// downloaded from an IMAP server) are omitted; their names are returned in
// `empty` for verification. If nothing is written, no file is left on disk. It
// returns the number of attachments archived plus the empties.
func WriteZip(zipPath string, atts []model.Attachment, skip map[int]bool) (written int, empty []string, err error) {
	f, err := os.Create(zipPath)
	if err != nil {
		return 0, nil, fmt.Errorf("create zip: %w", err)
	}

	zw := zip.NewWriter(f)
	usedNames := map[string]int{}

	for i := range atts {
		if skip[i] {
			continue
		}

		// Buffer one attachment at a time so we can skip empties without
		// leaving a zero-byte entry in the archive.
		var buf bytes.Buffer
		n, werr := atts[i].WriteTo(&buf)
		if werr != nil {
			zw.Close()
			f.Close()
			return written, empty, fmt.Errorf("read attachment %q: %w", atts[i].Filename, werr)
		}
		if n == 0 {
			empty = append(empty, attachmentLabel(atts[i], i))
			continue
		}

		name := uniqueName(util.SanitizeFilename(atts[i].Filename, i), usedNames)
		w, cerr := zw.Create(name)
		if cerr != nil {
			zw.Close()
			f.Close()
			return written, empty, fmt.Errorf("create zip entry %q: %w", name, cerr)
		}
		if _, werr := w.Write(buf.Bytes()); werr != nil {
			zw.Close()
			f.Close()
			return written, empty, fmt.Errorf("write zip entry %q: %w", name, werr)
		}
		written++
	}

	if err := zw.Close(); err != nil {
		f.Close()
		return written, empty, fmt.Errorf("finalize zip: %w", err)
	}
	if err := f.Close(); err != nil {
		return written, empty, fmt.Errorf("close zip: %w", err)
	}

	if written == 0 {
		os.Remove(zipPath)
	}
	return written, empty, nil
}

// attachmentLabel is a human-readable name for an attachment in reports.
func attachmentLabel(a model.Attachment, index int) string {
	if a.Filename != "" {
		return a.Filename
	}
	if a.ContentID != "" {
		return "cid:" + a.ContentID
	}
	return fmt.Sprintf("attachment-%d", index)
}

// uniqueName disambiguates repeated file names within a single archive by
// appending " (n)" before the extension.
func uniqueName(name string, used map[string]int) string {
	if _, ok := used[name]; !ok {
		used[name] = 1
		return name
	}
	ext := ""
	base := name
	if dot := lastDot(name); dot > 0 {
		ext = name[dot:]
		base = name[:dot]
	}
	for {
		used[name]++
		candidate := fmt.Sprintf("%s (%d)%s", base, used[name]-1, ext)
		if _, ok := used[candidate]; !ok {
			used[candidate] = 1
			return candidate
		}
	}
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
		if s[i] == '/' || s[i] == '\\' {
			return -1
		}
	}
	return -1
}
