package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"strings"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
)

// Readable HTML (design §4.11, slice E5): with the option on, each message whose original
// was written also gets <stem>.html beside it, made from the original's own bytes read
// back from the destination and rendered by export.RenderWith, the archive's sanitizing
// renderer, never by a path that skips it. The readable copy is a convenience: the .eml
// is the export, so a copy that cannot be made is a note on a message that is still
// exported (status exported-original-only, counted as "exported without a readable copy",
// never as "not exported", not offered for retry; XC35). The one exception is a read-back
// that does not match what was written: the original itself is then not what the run
// recorded, and the message is listed as failed.

const (
	// copyRenderBound is the render bound [RR-2b RR2b-C2]: the most predicted rendered HTML
	// (export.PredictedRenderSize) one readable copy may be made from. The size cap bounds
	// the bytes downloaded, not what the renderer makes of them, and Go's out-of-memory is
	// fatal, not a panic, so no recovery could catch it.
	copyRenderBound = 32 << 20

	// noteReadableTooLarge is the entry's note for a message over the render bound.
	noteReadableTooLarge = "too large for a readable copy"
	// noteReadableFailed starts the entry's note for a copy that could not be made.
	noteReadableFailed = "readable copy could not be made: "

	// reasonOriginalChanged is why a message whose .eml no longer matches what the run
	// wrote is listed as failed.
	reasonOriginalChanged = "the original changed on the destination after it was written"
)

// errOriginalChanged is the read-back finding: the .eml is not the bytes whose hash and size
// the run recorded when it wrote it (replaced, shortened, grown, removed, or no longer a
// regular file).
var errOriginalChanged = errors.New(reasonOriginalChanged)

// readableRender is the renderer the readable copy goes through: export.RenderWith. It is a
// variable only so a test can make it panic (a render panic is recovered); production
// always uses RenderWith.
var readableRender = export.RenderWith

// copyFileCountFor is how many files a message puts in its directory with opts on: the
// archive's files (copyFileCount) and a readable copy beside an original.
func copyFileCountFor(m *CopyMessage, opts CopyOptions) int {
	n := copyFileCount(m)
	if opts.ReadableHTML && m.NoOrig == "" {
		n++
	}
	return n
}

// readBack reads the .eml the run wrote, back from the destination into one buffer sized to
// what was recorded, and checks it is those bytes: its SHA-256 and size must equal what the
// run recorded when it wrote the original (XC1). It reads at most one byte more than the
// recorded size (and never past the MIME cap), so a file grown since is refused without
// being read whole.
func (r *CopyRun) readBack(orig ceFileRec) ([]byte, error) {
	if orig.Size < 0 || orig.Size > r.env.cap() {
		return nil, errOriginalChanged
	}
	f, err := r.fs.Open(orig.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errNotRegular) {
			return nil, errOriginalChanged // gone, or something else stands where the original was
		}
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, orig.Size+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, err
	}
	if int64(n) != orig.Size {
		return nil, errOriginalChanged
	}
	sum := sha256.Sum256(buf[:n])
	if hex.EncodeToString(sum[:]) != orig.SHA256 {
		return nil, errOriginalChanged
	}
	return buf[:n], nil
}

// parseOriginal parses the original's bytes, once, for both derived files (the readable copy
// and the extracted attachments). It returns the message or, when none can be made, why. A
// panic in the parse is recovered (a panic only: out of memory is fatal, and the size cap is
// what bounds the bytes).
func parseOriginal(eml []byte) (msg *model.Message, why string) {
	defer func() {
		if p := recover(); p != nil {
			msg, why = nil, panicReason(p)
		}
	}()
	msg = source.ParseRFC822(eml)
	if msg == nil {
		return nil, "the message could not be read"
	}
	return msg, ""
}

// buildReadable renders the parsed original's readable page. It returns the page and, when
// the mail's HTML could not be shown as HTML, the renderer's reason (the page then shows its
// source as text); or, when no copy can be made, the entry's note and no page. attachDir is
// the name of the directory its attachments were extracted into, "" when there is none. A
// panic in the render is recovered (a panic only: out of memory is fatal, and the render
// bound is what keeps a render from reaching it).
func buildReadable(msg *model.Message, m *CopyMessage, emlName, attachDir string) (page []byte, fallback, note string) {
	defer func() {
		if p := recover(); p != nil {
			page, fallback, note = nil, "", noteReadableFailed+panicReason(p)
		}
	}()
	if st := m.State; st != nil { // Graph is authoritative for state, as for the archive's own pages
		applyGraphState(msg, graph.MessageRef{Importance: st.Importance, IsRead: st.IsRead, Categories: st.Categories,
			Sensitivity: st.Sensitivity, Received: m.Received})
	}
	if export.PredictedRenderSize(msg) > copyRenderBound {
		return nil, "", noteReadableTooLarge
	}
	ctx := export.RenderContext{RawName: url.PathEscape(emlName)}
	if attachDir != "" {
		ctx.AttachDirName = url.PathEscape(attachDir)
	}
	rr, err := readableRender(msg, ctx)
	if err != nil {
		return nil, "", noteReadableFailed + plainText(err.Error())
	}
	return rr.HTML, rr.BodyFallback, ""
}

// panicReason is a recovered panic as a note's reason: its message only, with no stack,
// control-stripped and bounded.
func panicReason(p any) string {
	s := plainText(fmt.Sprint(p))
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.ToValidUTF8(s, "") + " (an internal error)"
}

// originalOnly records that m's original is exported without a readable copy, and why.
func originalOnly(line *ceMsgLine, note string) {
	line.Status = statusOriginalOnly
	line.Notes = append(line.Notes, note)
}

// addDerived makes the derived files of the message whose original was just placed as
// dir/emlName (recorded as orig) and adds them to the message's progress line: the extracted
// attachments (role derived-attachment, slice E6) and the readable copy (role derived-html,
// slice E5), or notes, and for a readable copy that could not be made the status
// exported-original-only. The original is read back and parsed once for both, and the
// attachments come first, so the page can link their directory. It returns errOriginalChanged
// only when the read-back finds the original is not what was written; the caller then lists
// the message as failed and removes what it placed (nothing else is placed by then).
func (r *CopyRun) addDerived(ctx context.Context, n int, m *CopyMessage, dir, emlName string, orig ceFileRec, line *ceMsgLine) error {
	opts := r.plan.Options
	r.env.step(fmt.Sprintf("W5read:%d", n))
	eml, err := r.readBack(orig)
	if err != nil {
		if errors.Is(err, errOriginalChanged) {
			return err
		}
		why := "the original could not be read back (" + leaveName(err) + ")"
		if opts.Attachments {
			line.Notes = append(line.Notes, attachNotePrefix+"the attachments were not extracted: "+why+"; "+attachAuthorityEML)
		}
		if opts.ReadableHTML {
			originalOnly(line, noteReadableFailed+why)
		}
		return nil
	}
	msg, why := parseOriginal(eml)
	stem := strings.TrimSuffix(emlName, ".eml")
	attachDir := ""
	if opts.Attachments {
		attachDir = r.addAttachments(ctx, n, dir, stem, msg, why, line)
	}
	if opts.ReadableHTML {
		r.addReadable(ctx, n, m, dir, emlName, msg, why, attachDir, line)
	}
	return nil
}

// addReadable makes the readable copy of the parsed original (why says it could not be
// parsed) and adds it to the message's line: its file with the role derived-html, or a note
// and the status exported-original-only.
func (r *CopyRun) addReadable(ctx context.Context, n int, m *CopyMessage, dir, emlName string, msg *model.Message, why, attachDir string, line *ceMsgLine) {
	if why != "" {
		originalOnly(line, noteReadableFailed+why)
		return
	}
	page, fallback, note := buildReadable(msg, m, emlName, attachDir)
	if note != "" {
		originalOnly(line, note)
		return
	}
	rec, err := r.writeReadable(ctx, dir, strings.TrimSuffix(emlName, ".eml"), page)
	if err != nil {
		originalOnly(line, noteReadableFailed+"it could not be written ("+leaveName(err)+")")
		return
	}
	line.Files = append(line.Files, rec)
	if fallback != "" {
		line.Notes = append(line.Notes, "the readable copy shows this message's HTML as text ("+fallback+")")
	}
	r.env.step(fmt.Sprintf("W5html:%d", n))
}

// writeReadable writes page as dir/<stem>.html: checked against what is left on the
// destination first (derived files are checked against the budget, design §4.13), then a
// temp file, fsync and a no-replace rename, hashed as written.
func (r *CopyRun) writeReadable(ctx context.Context, dir, stem string, page []byte) (ceFileRec, error) {
	budget, err := r.budget()
	if err != nil {
		return ceFileRec{}, err
	}
	if int64(len(page)) > budget {
		return ceFileRec{}, errors.New("no space left for it")
	}
	f, tmp, err := r.fs.CreateTemp(dir)
	if err != nil {
		return ceFileRec{}, err
	}
	hw := &ceHashWriter{w: f, h: sha256.New(), watch: r.watch, total: &r.bytes}
	_, werr := hw.Write(page)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		r.fs.Remove(ceJoin(dir, tmp))
		return ceFileRec{}, werr
	}
	placed, err := r.placeFinalAs(ctx, dir, tmp, stem, ".html")
	if err != nil {
		r.fs.Remove(ceJoin(dir, tmp))
		return ceFileRec{}, err
	}
	return ceFileRec{Path: ceJoin(dir, placed), Role: roleDerivedHTML, SHA256: hex.EncodeToString(hw.h.Sum(nil)), Size: int64(len(page))}, nil
}
