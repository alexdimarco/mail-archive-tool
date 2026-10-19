package source

import (
	"sort"
	"strconv"
	"strings"
)

// The lenient parameter tokenizer of design-mime-fidelity §4.3 (P5, C1-4). The strict
// parser (mime.ParseMediaType, through go-message) returns NO parameters at all on any
// error, so one unquoted "=" in a boundary, an unquoted start=<x@y>, a comment after a
// charset or a filename with a space loses the boundary of a multipart and every part
// in it. This tokenizer runs only after a strict failure and recovers what is there.
//
// It is bounded and linear: its input is one header (the walker caps every header at
// 1 MiB), it keeps at most maxLenientParams parameters, and an RFC 2231 continuation
// index is only ever an ordering key (a sparse sort), never a slice length, so an index
// of 4294967295 costs nothing.
const (
	maxLenientParams = 64 // parameters read from one header
	maxContinuation  = 64 // the largest RFC 2231 continuation index kept
)

// lenientMediaType reads a Content-Type or Content-Disposition value the strict parser
// rejected. mediaType is the first segment, lower-cased (it may be empty); params maps a
// lower-cased name to its value, the first occurrence of a name winning. It never fails.
//
// The rules: segments are split on ";" outside double quotes; a name runs to the first
// "="; a quoted value runs to its closing quote (backslash escapes honoured, the rest of
// the segment ignored); an unquoted value runs to the next ";", may contain "=" and
// spaces, is trimmed and loses one trailing "(comment)". RFC 2231 "name*=charset'lang'pct"
// and "name*N" / "name*N*" continuations are joined in index order and decoded by
// decodeText.
func lenientMediaType(v string) (mediaType string, params map[string]string) {
	params = map[string]string{}
	segs := splitUnquoted(v, ';')
	if len(segs) == 0 {
		return "", params
	}
	mediaType = strings.ToLower(strings.TrimSpace(segs[0]))

	type piece struct {
		idx int
		ext bool // percent-encoded (name*N*)
		val string
	}
	pieces := map[string][]piece{}
	var order []string // names with continuations, in first-seen order

	taken := 0
	for _, seg := range segs[1:] {
		eq := strings.IndexByte(seg, '=')
		if eq < 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(seg[:eq]))
		if name == "" {
			continue
		}
		if taken >= maxLenientParams {
			break
		}
		taken++
		val := lenientValue(seg[eq+1:])

		// name*, name*N and name*N* are RFC 2231 forms; a plain name is a plain value.
		ext := strings.HasSuffix(name, "*")
		base := strings.TrimSuffix(name, "*")
		idx := -1
		if star := strings.IndexByte(base, '*'); star >= 0 {
			digits := base[star+1:]
			base = base[:star]
			n, err := strconv.Atoi(digits)
			if err != nil || len(digits) > 4 || n < 0 || n > maxContinuation {
				continue // not a number, or a continuation beyond the bound: dropped
			}
			idx = n
		} else if !ext {
			if _, dup := params[base]; !dup {
				params[base] = val
			}
			continue
		}
		if _, seen := pieces[base]; !seen {
			order = append(order, base)
		}
		pieces[base] = append(pieces[base], piece{idx: idx, ext: ext, val: val})
	}

	for _, base := range order {
		if _, dup := params[base]; dup {
			continue // the first occurrence of a name wins
		}
		ps := pieces[base]
		sort.SliceStable(ps, func(i, j int) bool { return ps[i].idx < ps[j].idx })
		var raw []byte
		charset := ""
		for i, p := range ps {
			val := p.val
			if p.ext {
				if i == 0 {
					// charset'language'value: the charset names how the bytes are to be read.
					if q1 := strings.IndexByte(val, '\''); q1 >= 0 {
						if q2 := strings.IndexByte(val[q1+1:], '\''); q2 >= 0 {
							charset, val = val[:q1], val[q1+1+q2+1:]
						}
					}
				}
				raw = append(raw, pctDecode(val)...)
			} else {
				raw = append(raw, val...)
			}
		}
		text, _ := decodeText(charset, raw)
		params[base] = text
	}
	return mediaType, params
}

// lenientValue is one parameter's value text: a quoted string to its closing quote, else
// the trimmed text with one trailing "(comment)" removed.
func lenientValue(s string) string {
	s = strings.TrimLeft(s, " \t")
	if strings.HasPrefix(s, `"`) {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch c := s[i]; c {
			case '\\':
				if i+1 < len(s) {
					i++
					b.WriteByte(s[i])
				}
			case '"':
				return b.String()
			default:
				b.WriteByte(c)
			}
		}
		return b.String() // an unterminated quote runs to the end
	}
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, ")") {
		if open := strings.LastIndexByte(s, '('); open >= 0 {
			s = strings.TrimSpace(s[:open])
		}
	}
	return s
}

// splitUnquoted splits s on sep wherever sep is outside a double-quoted string
// (a backslash inside quotes escapes the next byte).
func splitUnquoted(s string, sep byte) []string {
	var out []string
	start, quoted := 0, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case quoted && c == '\\':
			i++
		case c == '"':
			quoted = !quoted
		case c == sep && !quoted:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// pctDecode decodes %XX escapes and leaves anything else, including a bad escape,
// as it is.
func pctDecode(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			out = append(out, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
			continue
		}
		out = append(out, s[i])
	}
	return out
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}
