package export

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/util"
)

// ZipResult reports what WriteZip did: how many attachments were archived, the
// labels of those that produced zero bytes (e.g. content not downloaded from an
// IMAP server — recoverable by syncing), and the labels of those whose stream
// FAILED mid-read (a torn fetch, a read error). Neither kind is ever silently
// dropped (R1): both are recorded as verification issues by the exporter.
type ZipResult struct {
	Written int
	Empty   []string
	Failed  []string
}

// WriteZip writes every attachment not in skip into a zip archive at zipPath.
// The archive is built in a uniquely named temp file beside zipPath and renamed
// into place only when finished, so a crash or error can never leave a partial
// zip under the final name and a previously good zip survives (R5). If nothing
// is written, no file is left on disk (and a stale zip at zipPath is removed).
func WriteZip(zipPath string, atts []model.Attachment, skip map[int]bool) (ZipResult, error) {
	var res ZipResult
	tmp, err := createTemp(filepath.Dir(zipPath))
	if err != nil {
		return res, fmt.Errorf("create zip: %w", err)
	}
	abort := func(werr error) (ZipResult, error) {
		tmp.Close()
		os.Remove(tmp.Name())
		return res, werr
	}

	zw := zip.NewWriter(tmp)
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
			// A torn stream loses only this attachment, never the rest of the
			// message's archive; it is reported, not swallowed.
			res.Failed = append(res.Failed, attachmentLabel(atts[i], i))
			continue
		}
		if n == 0 {
			res.Empty = append(res.Empty, attachmentLabel(atts[i], i))
			continue
		}

		name := uniqueName(util.SanitizeFilename(atts[i].Filename, i), usedNames)
		w, cerr := zw.Create(name)
		if cerr != nil {
			zw.Close()
			return abort(fmt.Errorf("create zip entry %q: %w", name, cerr))
		}
		if _, werr := w.Write(buf.Bytes()); werr != nil {
			zw.Close()
			return abort(fmt.Errorf("write zip entry %q: %w", name, werr))
		}
		res.Written++
	}

	if err := zw.Close(); err != nil {
		return abort(fmt.Errorf("finalize zip: %w", err))
	}
	if res.Written == 0 {
		tmp.Close()
		os.Remove(tmp.Name())
		os.Remove(zipPath) // a stale archive from an earlier capture must not outlive it
		return res, nil
	}
	if err := commitTemp(tmp, zipPath); err != nil {
		return res, fmt.Errorf("commit zip: %w", err)
	}
	return res, nil
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
