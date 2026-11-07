// Package interchange writes preserved message bytes out in the standard mail
// interchange formats a migration target understands: mboxrd (one file per
// folder) and byte-exact .eml (one file per message). It only ever copies bytes
// that were preserved at capture (`-raw`); it never re-serializes or synthesizes
// a message, so nothing it writes is a forgery.
//
// The mbox flavour is mboxrd (design G4/PC12): each message is preceded by a
// synthesized "From " postmark and every body line matching `>*From ` is
// `>`-quoted. A reader that unquotes `>From ` recovers the exact original bytes;
// an mboxo reader does not; for a consumer of unknown flavour, `-format eml` is
// the byte-exact format.
package interchange

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"mail-archive-tool/internal/util"
)

// fromPrefix is the mbox message-boundary marker: a line beginning with
// "From " (at column 0) starts a new message. mboxrd quotes any body line that
// is a run of '>' followed by "From " so it can never be mistaken for a
// boundary, and un-quotes it on the way back.
const fromPrefix = "From "

// isFromQuoteLine reports whether line (a single body line, without its trailing
// newline) is `>*From ` — the set of lines mboxrd must `>`-quote so a genuine
// boundary is unambiguous.
func isFromQuoteLine(line []byte) bool {
	i := 0
	for i < len(line) && line[i] == '>' {
		i++
	}
	return bytes.HasPrefix(line[i:], []byte(fromPrefix))
}

// WriteMessage writes one message to w in mboxrd form: a synthesized "From "
// postmark line derived from the message's own From/Date headers, the raw bytes
// with every `>*From ` line `>`-quoted, and a trailing blank line so the next
// postmark starts a clean boundary. Line endings in raw are preserved verbatim
// (a CRLF message stays CRLF); a final line with no newline is terminated with
// one so the blank-line separator is unambiguous. It returns any write error
// from w so the caller can discard a torn temp file (ENOSPC) and refuse.
func WriteMessage(w io.Writer, raw []byte) error {
	if _, err := io.WriteString(w, Postmark(raw)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	i := 0
	for i < len(raw) {
		j := bytes.IndexByte(raw[i:], '\n')
		var line []byte
		if j < 0 {
			line = raw[i:]
			i = len(raw)
		} else {
			line = raw[i : i+j]
			i += j + 1
		}
		if isFromQuoteLine(line) {
			if _, err := io.WriteString(w, ">"); err != nil {
				return err
			}
		}
		if _, err := w.Write(line); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	// The blank line that separates this message from the next postmark.
	_, err := io.WriteString(w, "\n")
	return err
}

// WriteMessageMatched is WriteMessage for Restore to Thunderbird
// (docs/design-restore-thunderbird.md §4.3): the two lines it adds — the "From "
// line's ending and the blank line after the message — and the line ending it adds
// to a final line that has none use the message's own line ending (CRLF when its
// first line ends in CRLF, else LF), so a file never mixes them. WriteMessage, which
// extract uses, is unchanged. added is how many bytes of line ending it put after a
// final line that had none (0, 1 or 2), so a check can recover the original exactly.
func WriteMessageMatched(w io.Writer, raw []byte) (added int, err error) {
	eol := "\n"
	if i := bytes.IndexByte(raw, '\n'); i > 0 && raw[i-1] == '\r' {
		eol = "\r\n"
	}
	if _, err := io.WriteString(w, Postmark(raw)+eol); err != nil {
		return 0, err
	}
	i := 0
	for i < len(raw) {
		j := bytes.IndexByte(raw[i:], '\n')
		var line []byte
		end := "\n"
		if j < 0 {
			line = raw[i:]
			i = len(raw)
			end, added = eol, len(eol)
		} else {
			line = raw[i : i+j]
			i += j + 1
		}
		if isFromQuoteLine(line) {
			if _, err := io.WriteString(w, ">"); err != nil {
				return 0, err
			}
		}
		if _, err := w.Write(line); err != nil {
			return 0, err
		}
		if _, err := io.WriteString(w, end); err != nil {
			return 0, err
		}
	}
	_, err = io.WriteString(w, eol)
	return added, err
}

// ReadMboxrd reads an mbox written by WriteMessageMatched (or WriteMessage) back
// into its messages, by the read rules Thunderbird's own reader follows
// (MboxMsgInputStream, design §4.1–§4.3; a port of those rules, not Thunderbird): a
// line beginning "From " at column 0 starts a message (no blank line is required
// before it); within a message one '>' is removed from a line that is a run of '>'
// followed by "From " (mboxrd's unquoting); the one blank line — LF or CRLF — before
// the next "From " line or the end of the file is the separator, not the message's.
// Lines before the first "From " line are not a message. fn gets each message's bytes
// in file order; an error from fn stops the read and is returned.
func ReadMboxrd(r io.Reader, fn func(msg []byte) error) error {
	var cur []byte
	return ScanMboxrd(r, MboxrdHandler{
		Start: func() error { cur = cur[:0]; return nil },
		Data:  func(p []byte) error { cur = append(cur, p...); return nil },
		End:   func() error { return fn(cur) },
	})
}

// MboxrdHandler receives ScanMboxrd's messages: Start at each "From " line, Data with
// the message's bytes in order (unquoted, the separator's blank line held back and not
// given), End when the message ends. Any error stops the scan.
type MboxrdHandler struct {
	Start func() error
	Data  func([]byte) error
	End   func() error
}

// ScanMboxrd is ReadMboxrd streamed, for files of any size: it holds at most one line
// back (the line that may be the separator's blank line). A line longer than its
// buffer is passed through in pieces; only its start is looked at, for "From " and
// mboxrd's quoting.
func ScanMboxrd(r io.Reader, h MboxrdHandler) error {
	br := bufio.NewReaderSize(r, 64<<10)
	in := false
	var pending []byte // the last whole short line, held back: it may be the separator
	hasPending := false
	emitPending := func() error {
		if hasPending {
			hasPending = false
			return h.Data(pending)
		}
		return nil
	}
	end := func() error {
		if !in {
			return nil
		}
		if hasPending {
			b := bytes.TrimSuffix(bytes.TrimSuffix(pending, []byte("\n")), []byte("\r"))
			isBlank := len(b) == 0 && bytes.HasSuffix(pending, []byte("\n"))
			if !isBlank {
				if err := emitPending(); err != nil {
					return err
				}
			}
			hasPending = false
		}
		return h.End()
	}
	midLine := false // the previous read ended inside a long line
	for {
		chunk, err := br.ReadSlice('\n')
		if len(chunk) > 0 {
			if midLine {
				if e := emitPending(); e != nil {
					return e
				}
				if in {
					if e := h.Data(chunk); e != nil {
						return e
					}
				}
			} else {
				line := chunk
				body := bytes.TrimSuffix(line, []byte("\n"))
				switch {
				case bytes.HasPrefix(body, []byte(fromPrefix)):
					if e := end(); e != nil {
						return e
					}
					in = true
					if e := h.Start(); e != nil {
						return e
					}
					if errors.Is(err, bufio.ErrBufferFull) {
						// a "From " line longer than the buffer: the rest of it is not the message's
						for errors.Is(err, bufio.ErrBufferFull) {
							_, err = br.ReadSlice('\n')
						}
						if err != nil && !errors.Is(err, io.EOF) {
							return err
						}
						if errors.Is(err, io.EOF) {
							return end()
						}
						continue
					}
				case !in:
				default:
					if len(body) > 0 && body[0] == '>' && isFromQuoteLine(body) {
						line = line[1:]
					}
					if e := emitPending(); e != nil {
						return e
					}
					if errors.Is(err, bufio.ErrBufferFull) {
						if e := h.Data(line); e != nil {
							return e
						}
					} else {
						pending = append(pending[:0], line...)
						hasPending = true
					}
				}
			}
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			midLine = true
		case err == nil:
			midLine = false
		case errors.Is(err, io.EOF):
			return end()
		default:
			return err
		}
	}
}

// Postmark synthesizes the "From " postmark line for a message from its own
// header block: the address from the first From: header and the time from the
// first Date: header, both parsed leniently. It never fails — an unparseable or
// absent header falls back to a stable placeholder ("MAILER-DAEMON" / the Unix
// epoch) so the output is deterministic (a re-run produces identical bytes) and
// a malformed message can still be framed (R10). The postmark's only job is to
// mark a boundary; its content is not part of the preserved message.
func Postmark(raw []byte) string {
	addr := headerAddress(firstHeader(raw, "from"))
	if addr == "" {
		addr = "MAILER-DAEMON"
	}
	when := time.Unix(0, 0).UTC()
	if d := firstHeader(raw, "date"); d != "" {
		if t, err := parseDate(d); err == nil {
			when = t.UTC()
		}
	}
	// asctime form, as generated by mbox writers: "Mon Jan _2 15:04:05 2006".
	return fmt.Sprintf("%s%s %s", fromPrefix, addr, when.Format("Mon Jan _2 15:04:05 2006"))
}

// parseDate parses an RFC 5322 Date header, accepting the common variants
// (with and without a leading weekday, numeric or named zone).
func parseDate(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	layouts := []string{
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 MST",
		"2 Jan 2006 15:04:05 -0700",
		"2 Jan 2006 15:04:05 MST",
		time.RFC1123Z,
		time.RFC1123,
	}
	var firstErr error
	for _, l := range layouts {
		if t, err := time.Parse(l, v); err == nil {
			return t, nil
		} else if firstErr == nil {
			firstErr = err
		}
	}
	return time.Time{}, firstErr
}

// firstHeader returns the unfolded value of the first occurrence of the named
// header (case-insensitive) in raw's header block — the bytes up to the first
// blank line. It stops at the header/body boundary so a body line that looks
// like a header is never read. Returns "" when the header is absent.
func firstHeader(raw []byte, name string) string {
	lname := strings.ToLower(name)
	i := 0
	for i < len(raw) {
		j := bytes.IndexByte(raw[i:], '\n')
		var line []byte
		if j < 0 {
			line = raw[i:]
			i = len(raw)
		} else {
			line = raw[i : i+j]
			i += j + 1
		}
		trimmed := bytes.TrimRight(line, "\r")
		if len(trimmed) == 0 {
			break // end of the header block
		}
		colon := bytes.IndexByte(trimmed, ':')
		if colon < 0 {
			continue
		}
		if strings.ToLower(string(trimmed[:colon])) != lname {
			continue
		}
		value := string(bytes.TrimSpace(trimmed[colon+1:]))
		// Fold any continuation lines (leading whitespace) into the value.
		for i < len(raw) {
			k := bytes.IndexByte(raw[i:], '\n')
			var next []byte
			if k < 0 {
				next = raw[i:]
			} else {
				next = raw[i : i+k]
			}
			nt := bytes.TrimRight(next, "\r")
			if len(nt) == 0 || (nt[0] != ' ' && nt[0] != '\t') {
				break
			}
			value += " " + string(bytes.TrimSpace(nt))
			if k < 0 {
				i = len(raw)
			} else {
				i += k + 1
			}
		}
		return value
	}
	return ""
}

// headerAddress extracts a bare email address from a From header value
// ("Name <a@b>" → "a@b"; "a@b" → "a@b"), control-stripped so the postmark line
// stays a single clean line. Returns "" when no plausible address is present. The
// rule is util.LenientAddress's, which the page's sender shares, so the postmark and
// the page always agree (design-mime-fidelity MC35): an address is the contents of
// the last <…> or a whole "x@y" value, with no whitespace in it.
func headerAddress(v string) string {
	_, email := util.LenientAddress(v)
	return email
}
