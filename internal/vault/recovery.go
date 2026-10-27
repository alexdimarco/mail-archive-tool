package vault

import (
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// RecoveryKey is the second way into an archive (G4, C9): 128 bits from the
// random source (A3), shown once as 26 base32 characters plus a check character.
type RecoveryKey [16]byte

const (
	recoveryAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	recoveryBodyLen  = 26 // ceil(128/5)
	recoveryLen      = recoveryBodyLen + 1
	recoveryGroup    = 4
)

var recoveryEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// ErrRecoveryFormat refuses text that is not a recovery key; ErrRecoveryCheck
// refuses one with a typing mistake (its check character does not match).
var (
	ErrRecoveryFormat = errors.New("that is not a recovery key")
	ErrRecoveryCheck  = errors.New("that recovery key has a typing mistake")
)

func newRecoveryKey() (RecoveryKey, error) {
	var k RecoveryKey
	if _, err := io.ReadFull(randReader, k[:]); err != nil {
		return k, fmt.Errorf("this computer could not supply random numbers for the recovery key: %w", err)
	}
	return k, nil
}

// String is the key as shown to the person: dash-separated groups of four,
// ending in the check character, e.g. ABCD-EFGH-…-XYZ.
func (k RecoveryKey) String() string {
	body := recoveryEncoding.EncodeToString(k[:])
	full := body + string(recoveryAlphabet[luhn32Check(body)])
	var b strings.Builder
	for i := 0; i < len(full); i += recoveryGroup {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(full[i:min(i+recoveryGroup, len(full))])
	}
	return b.String()
}

// ParseRecoveryKey reads a recovery key as typed or pasted: case, and any
// spacing or dash a document or password manager may have put in (Unicode
// spaces, dash punctuation, soft hyphens, zero-width characters), are ignored,
// and compatibility forms (full-width letters) read as plain ones; what is left
// must be the 27 characters exactly, the body must be canonical, and the check
// character must match.
func ParseRecoveryKey(s string) (RecoveryKey, error) {
	var k RecoveryKey
	var kept []rune
	for _, r := range strings.ToUpper(norm.NFKC.String(s)) {
		if unicode.IsSpace(r) || unicode.Is(unicode.Pd, r) || unicode.Is(unicode.Cf, r) || r == '\u2212' {
			continue
		}
		kept = append(kept, r)
	}
	if len(kept) != recoveryLen {
		return k, fmt.Errorf("%w: it has %d characters (ignoring dashes and spaces), want %d", ErrRecoveryFormat, len(kept), recoveryLen)
	}
	for i, r := range kept {
		if r > unicode.MaxASCII || strings.IndexByte(recoveryAlphabet, byte(r)) < 0 {
			return k, fmt.Errorf("%w: character %d, %q, is not a recovery-key character (A–Z, 2–7)", ErrRecoveryFormat, i+1, r)
		}
	}
	t := string(kept)
	if !luhn32Valid(t) {
		return k, fmt.Errorf("%w: check it character by character against your saved copy", ErrRecoveryCheck)
	}
	body := t[:recoveryBodyLen]
	raw, err := recoveryEncoding.DecodeString(body)
	if err != nil || len(raw) != len(k) || recoveryEncoding.EncodeToString(raw) != body {
		return k, fmt.Errorf("%w: its last data character is not one a recovery key can end in", ErrRecoveryFormat)
	}
	copy(k[:], raw)
	return k, nil
}

// luhn32Check is the Luhn mod 32 check character index for s. Luhn mod N detects
// every single-character substitution and most adjacent transpositions.
func luhn32Check(s string) int {
	const n = len(recoveryAlphabet)
	factor, sum := 2, 0
	for i := len(s) - 1; i >= 0; i-- {
		addend := factor * strings.IndexByte(recoveryAlphabet, s[i])
		factor = 3 - factor
		sum += addend/n + addend%n
	}
	return (n - sum%n) % n
}

// luhn32Valid reports whether s ends in its correct Luhn mod 32 check character.
func luhn32Valid(s string) bool {
	const n = len(recoveryAlphabet)
	factor, sum := 1, 0
	for i := len(s) - 1; i >= 0; i-- {
		addend := factor * strings.IndexByte(recoveryAlphabet, s[i])
		factor = 3 - factor
		sum += addend/n + addend%n
	}
	return sum%n == 0
}
