package tbrestore

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// PrefsMaxBytes bounds prefs.js and user.js (design §3.2, foldcheck N11): a larger
// file is refused with W-PREFS naming it.
const PrefsMaxBytes = 16 << 20

// Prefs are a profile's settings as Thunderbird applies them: prefs.js, then user.js
// over it (Thunderbird applies user.js last, design §3.2 note; lab row MA-1095 checks
// the order). A string value is decoded as a JavaScript string literal; any other
// value is kept as written ("true", "1").
type Prefs struct {
	vals map[string]string
	// bad are the "mail." settings whose value could not be decoded (an escape the
	// decoder does not take): reading one refuses, W-PREFS, never a guess.
	bad map[string]badPref
}

// badPref is a setting that could not be read: the file that set it last, and why.
type badPref struct{ file, why string }

func (b badPref) Error() string { return b.why }

// prefsRefusal is W-PREFS for a setting that could not be read, naming the file that
// set it (prefs.js or user.js, foldcheck N11).
func prefsRefusal(err error) error {
	var b badPref
	file := "prefs.js"
	if errors.As(err, &b) {
		file = b.file
	}
	return refuse("W-PREFS", fill(wPrefs, "FILE", file, "WHY", err.Error()), err)
}

// Get is a setting's value; ok is false when it is not set. A "mail." setting that
// could not be decoded is an error (a badPref, naming its file).
func (p *Prefs) Get(key string) (string, bool, error) {
	if b, isBad := p.bad[key]; isBad {
		return "", false, b
	}
	v, ok := p.vals[key]
	return v, ok, nil
}

// Keys are every readable setting's name.
func (p *Prefs) Keys() []string {
	out := make([]string, 0, len(p.vals))
	for k := range p.vals {
		out = append(out, k)
	}
	return out
}

// ReadPrefs reads the profile's prefs.js and then user.js. Neither is ever written.
// A file over PrefsMaxBytes, one that is not an ordinary file, or a "mail." line that
// cannot be read is refused with W-PREFS naming the file. A missing user.js is
// normal; a missing prefs.js is W-PREFS.
func ReadPrefs(profileDir string) (*Prefs, error) {
	p := &Prefs{vals: map[string]string{}, bad: map[string]badPref{}}
	for _, name := range []string{"prefs.js", "user.js"} {
		data, err := readBounded(filepath.Join(profileDir, name), PrefsMaxBytes)
		if errors.Is(err, fs.ErrNotExist) && name == "user.js" {
			continue
		}
		if err != nil {
			return nil, refuse("W-PREFS", fill(wPrefs, "FILE", name, "WHY", err.Error()), err)
		}
		if err := p.parse(name, data); err != nil {
			return nil, refuse("W-PREFS", fill(wPrefs, "FILE", name, "WHY", err.Error()), err)
		}
	}
	return p, nil
}

// errTooLarge is a settings file over the cap.
var errTooLarge = errors.New("it is larger than MailArchive reads")

// readBounded reads an ordinary file of at most max bytes, never through a link.
func readBounded(path string, max int64) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("it is not an ordinary file")
	}
	if fi.Size() > max {
		return nil, errTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errTooLarge
	}
	return data, nil
}

// parse reads every `user_pref("key", value);` line (pref( and sticky_pref( too, as
// user.js may use). A line that is not one is passed over; a "mail." line whose value
// cannot be decoded is kept as bad; a "mail." line that cannot be read at all fails
// the file.
func (p *Prefs) parse(file string, data []byte) error {
	for n, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		var rest string
		ok := false
		for _, fn := range []string{"user_pref(", "pref(", "sticky_pref("} {
			if r, found := strings.CutPrefix(line, fn); found {
				rest, ok = r, true
				break
			}
		}
		if !ok {
			continue
		}
		key, rest, err := readJSString(strings.TrimLeft(rest, " \t"))
		if err != nil {
			if strings.Contains(line, `"mail.`) || strings.Contains(line, `'mail.`) {
				return fmt.Errorf("line %d: %v", n+1, err)
			}
			continue
		}
		rest = strings.TrimLeft(rest, " \t")
		rest, found := strings.CutPrefix(rest, ",")
		if !found {
			if strings.HasPrefix(key, "mail.") {
				return fmt.Errorf("line %d: no value", n+1)
			}
			continue
		}
		rest = strings.TrimLeft(rest, " \t")
		var val string
		if rest != "" && (rest[0] == '"' || rest[0] == '\'') {
			v, after, err := readJSString(rest)
			if err != nil {
				if strings.HasPrefix(key, "mail.") {
					p.bad[key] = badPref{file, err.Error()}
					delete(p.vals, key)
				}
				continue
			}
			val, rest = v, after
			if !strings.HasPrefix(strings.TrimLeft(rest, " \t"), ")") {
				if strings.HasPrefix(key, "mail.") {
					p.bad[key] = badPref{file, "the value does not end where it should"}
					delete(p.vals, key)
				}
				continue
			}
		} else {
			i := strings.IndexByte(rest, ')')
			if i < 0 {
				if strings.HasPrefix(key, "mail.") {
					p.bad[key] = badPref{file, "the value does not end where it should"}
					delete(p.vals, key)
				}
				continue
			}
			val = strings.TrimSpace(rest[:i])
		}
		p.vals[key] = val
		delete(p.bad, key) // a later readable line wins, as Thunderbird applies the last
	}
	return nil
}

// readJSString reads one JavaScript string literal at the start of s (quoted with "
// or ') and returns its value and what follows it. It decodes \\, \", \', \n, \r, \t
// and \uXXXX (a surrogate pair joined, a lone surrogate refused) and refuses any
// other escape rather than guessing (design §3.2 note).
func readJSString(s string) (string, string, error) {
	if s == "" || (s[0] != '"' && s[0] != '\'') {
		return "", s, errors.New("a setting is not a quoted string")
	}
	q := s[0]
	var b strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		switch {
		case c == q:
			return b.String(), s[i+1:], nil
		case c == '\\':
			if i+1 >= len(s) {
				return "", s, errors.New("a string ends in the middle of an escape")
			}
			e := s[i+1]
			switch e {
			case '\\', '"', '\'':
				b.WriteByte(e)
				i += 2
			case 'n':
				b.WriteByte('\n')
				i += 2
			case 'r':
				b.WriteByte('\r')
				i += 2
			case 't':
				b.WriteByte('\t')
				i += 2
			case 'u':
				r, n, err := readU(s[i:])
				if err != nil {
					return "", s, err
				}
				if utf16.IsSurrogate(r) {
					if r >= 0xdc00 {
						return "", s, errors.New("a string holds a lone surrogate")
					}
					lo, n2, err := readU(s[i+n:])
					if err != nil || lo < 0xdc00 || lo > 0xdfff {
						return "", s, errors.New("a string holds a lone surrogate")
					}
					r = utf16.DecodeRune(r, lo)
					n += n2
				}
				b.WriteRune(r)
				i += n
			default:
				return "", s, fmt.Errorf("a string holds the escape \\%c, which MailArchive does not read", e)
			}
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && size == 1 {
				return "", s, errors.New("a string is not valid UTF-8")
			}
			b.WriteString(s[i : i+size])
			i += size
		}
	}
	return "", s, errors.New("a string is not closed")
}

// readU reads "\uXXXX" at the start of s.
func readU(s string) (rune, int, error) {
	if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
		return 0, 0, errors.New("a \\u escape is cut short")
	}
	v, err := strconv.ParseUint(s[2:6], 16, 32)
	if err != nil {
		return 0, 0, errors.New("a \\u escape is not four hex digits")
	}
	return rune(v), 6, nil
}
