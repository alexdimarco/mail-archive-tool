package vault

import (
	"errors"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-706, R37
func TestRecoveryKeyIs128RandomBitsShownAs27Characters(t *testing.T) {
	rec := recordDraws(t)
	rk, err := newRecoveryKey()
	if err != nil {
		t.Fatal(err)
	}
	draws := assure.Reached(t, rec.draws, "recovery key draws")
	if len(draws) != 1 || len(draws[0]) != 16 || string(draws[0]) != string(rk[:]) {
		t.Fatalf("the recovery key must be exactly one 16-byte draw from the random source")
	}

	s := rk.String()
	groups := strings.Split(s, "-")
	if len(groups) != 7 {
		t.Fatalf("%q: want 7 dash-separated groups, got %d", s, len(groups))
	}
	for i, g := range groups {
		want := recoveryGroup
		if i == len(groups)-1 {
			want = recoveryLen - recoveryGroup*(len(groups)-1)
		}
		if len(g) != want {
			t.Fatalf("%q: group %d has %d characters, want %d", s, i, len(g), want)
		}
		for _, r := range g {
			if !strings.ContainsRune(recoveryAlphabet, r) {
				t.Fatalf("%q: character %q is outside A–Z/2–7", s, r)
			}
		}
	}

	for _, typed := range []string{s, strings.ToLower(s), strings.ReplaceAll(s, "-", ""), strings.ReplaceAll(s, "-", " "), "  " + s + "\n"} {
		got, err := ParseRecoveryKey(typed)
		if err != nil {
			t.Fatalf("%q must parse: %v", typed, err)
		}
		if got != rk {
			t.Fatalf("%q parsed to a different key", typed)
		}
	}
}

// covers: MA-706, R37
func TestEverySingleCharacterTypoInARecoveryKeyIsRefused(t *testing.T) {
	for trial := 0; trial < 8; trial++ {
		rk, err := newRecoveryKey()
		if err != nil {
			t.Fatal(err)
		}
		flat := strings.ReplaceAll(rk.String(), "-", "")
		tried := 0
		for pos := 0; pos < len(flat); pos++ {
			for _, c := range recoveryAlphabet {
				if byte(c) == flat[pos] {
					continue
				}
				typo := flat[:pos] + string(c) + flat[pos+1:]
				got, err := ParseRecoveryKey(typo)
				tried++
				if err == nil {
					t.Fatalf("typo %q (position %d) was accepted as key %x", typo, pos, got)
				}
				if !errors.Is(err, ErrRecoveryCheck) && !errors.Is(err, ErrRecoveryFormat) {
					t.Fatalf("typo at %d: want a typed recovery-key refusal, got %v", pos, err)
				}
			}
		}
		if tried != recoveryLen*(len(recoveryAlphabet)-1) {
			t.Fatalf("tried %d typos, want every one", tried)
		}
	}
}

// covers: MA-706, R37
func TestANonCanonicalLastCharacterIsRefused(t *testing.T) {
	rk, err := newRecoveryKey()
	if err != nil {
		t.Fatal(err)
	}
	flat := strings.ReplaceAll(rk.String(), "-", "")
	body := []byte(flat[:recoveryBodyLen])
	// 128 bits fill 25 characters and 3 bits of the 26th; its low 2 bits are
	// padding, zero in the canonical spelling. Set one, and give the result a
	// correct check character so only the canonical check can catch it.
	last := strings.IndexByte(recoveryAlphabet, body[recoveryBodyLen-1])
	body[recoveryBodyLen-1] = recoveryAlphabet[last|1]
	if last|1 == last {
		t.Fatal("fixture: the canonical last character must have zero padding bits")
	}
	alias := string(body) + string(recoveryAlphabet[luhn32Check(string(body))])
	if !luhn32Valid(alias) {
		t.Fatal("fixture: the alias must pass the check character")
	}
	_, err = ParseRecoveryKey(alias)
	if !errors.Is(err, ErrRecoveryFormat) {
		t.Fatalf("a second spelling of the same key must be refused: want ErrRecoveryFormat, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("last data character"))
}

// covers: MA-706, R37
func TestARecoveryKeyPastedFromADocumentStillParses(t *testing.T) {
	rk, err := newRecoveryKey()
	if err != nil {
		t.Fatal(err)
	}
	s := rk.String()
	fullwidth := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				r += 0xFEE0 // the full-width form
			}
			b.WriteRune(r)
		}
		return b.String()
	}
	for label, pasted := range map[string]string{
		"no-break spaces":    strings.ReplaceAll(s, "-", "\u00a0"),
		"en dashes":          strings.ReplaceAll(s, "-", "\u2013"),
		"zero-width spaces":  strings.ReplaceAll(s, "-", "\u200b"),
		"soft hyphens":       strings.ReplaceAll(s, "-", "\u00ad"),
		"minus signs":        strings.ReplaceAll(s, "-", "\u2212"),
		"full-width letters": fullwidth(s),
		"a byte-order mark":  "\ufeff" + s,
		"line separators":    strings.ReplaceAll(s, "-", "\u2028"),
		"next-line marks":    strings.ReplaceAll(s, "-", "\u0085"),
	} {
		got, err := ParseRecoveryKey(pasted)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if got != rk {
			t.Fatalf("%s: parsed to a different key", label)
		}
	}

	_, err = ParseRecoveryKey(strings.ReplaceAll(s, "-", "\u00a0") + "ÉÉ")
	if !errors.Is(err, ErrRecoveryFormat) {
		t.Fatalf("want ErrRecoveryFormat, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("29 characters"))
}

// covers: MA-706, R37
func TestMalformedRecoveryKeysAreRefused(t *testing.T) {
	rk, _ := newRecoveryKey()
	flat := strings.ReplaceAll(rk.String(), "-", "")
	for label, c := range map[string]struct {
		in   string
		name string
	}{
		"one short":   {flat[:recoveryLen-1], "27"},
		"one long":    {flat + "A", "27"},
		"a digit 1":   {"1" + flat[1:], "not a recovery-key character"},
		"empty":       {"", "27"},
		"a password":  {testPassword, "27"},
		"punctuation": {flat[:10] + "!" + flat[11:], "not a recovery-key character"},
	} {
		_, err := ParseRecoveryKey(c.in)
		if !errors.Is(err, ErrRecoveryFormat) {
			t.Fatalf("%s: want ErrRecoveryFormat, got %v", label, err)
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(c.name))
	}
}
