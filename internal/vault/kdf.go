package vault

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

// Params is an Argon2id cost, recorded per wrap entry (A3).
type Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

func (p Params) String() string {
	return fmt.Sprintf("memory %d MiB, time %d, threads %d", p.MemoryKiB/1024, p.Time, p.Threads)
}

// floor is the minimum cost (A3): every new wrap uses it and an unwrap refuses
// anything below it, so a weak host, an old writer or a tampered vault can never
// lower what an offline guesser pays. ceiling bounds what a hostile vault can
// make an unlock spend: this build only ever writes the floor, so the ceiling is
// headroom for a future raise of time (A3), not a cost anyone writes today (A3,
// re-review). A var only so this package's tests can run cheaply;
// TestShippedKDFFloor pins the shipped values.
var (
	floor   = Params{MemoryKiB: 256 * 1024, Time: 3, Threads: 1}
	ceiling = Params{MemoryKiB: 1024 * 1024, Time: 16, Threads: 4}
)

// argon2Key is Argon2id itself, a var so a test can count derivations and
// prove a refused cost never reaches one.
var argon2Key = argon2.IDKey

const saltSize = 16

// MinPasswordRunes is the password floor (C12): the conditional promise of R-E2
// (a copied archive is unreadable) holds only for a password a guesser cannot
// enumerate, so a shorter one is refused when it is set.
const MinPasswordRunes = 12

// ErrParams refuses a wrap entry whose cost is outside [floor, ceiling].
var ErrParams = errors.New("vault: key-derivation cost outside the allowed range")

// ErrWeakPassword refuses a password below the floor when it is set.
var ErrWeakPassword = errors.New("vault: password too short")

func (p Params) check() error {
	if p.MemoryKiB < floor.MemoryKiB || p.Time < floor.Time || p.Threads < floor.Threads {
		return fmt.Errorf("%w: %s is below the minimum %s — this vault was weakened or damaged", ErrParams, p, floor)
	}
	if p.MemoryKiB > ceiling.MemoryKiB || p.Time > ceiling.Time || p.Threads > ceiling.Threads {
		return fmt.Errorf("%w: %s is above the maximum %s — this vault was altered or damaged", ErrParams, p, ceiling)
	}
	return nil
}

// deriveKEK stretches secret into a key-encryption key with Argon2id. It checks
// the cost before spending it, so a hostile vault can neither lower it nor make
// one derivation cost more than the ceiling; openVault also tries the cheapest
// copy first, so a planted expensive copy is paid for only if the real one fails.
func deriveKEK(secret []byte, salt []byte, p Params) ([]byte, error) {
	if err := p.check(); err != nil {
		return nil, err
	}
	if len(salt) != saltSize {
		return nil, fmt.Errorf("vault: salt is %d bytes, want %d", len(salt), saltSize)
	}
	return argon2Key(secret, salt, p.Time, p.MemoryKiB, p.Threads, keySize), nil
}

// passwordBytes is the password as Argon2id sees it: Unicode NFC, so the same
// password typed on systems that compose accents differently unlocks the same
// archive.
func passwordBytes(pw string) []byte { return []byte(norm.NFC.String(pw)) }

// checkNewPassword enforces the floor on a password being set (create, change,
// reset). Unlocking never re-checks it.
func checkNewPassword(pw string) error {
	if !utf8.ValidString(pw) {
		return fmt.Errorf("%w: it is not valid text", ErrWeakPassword)
	}
	if n := utf8.RuneCountInString(norm.NFC.String(pw)); n < MinPasswordRunes {
		return fmt.Errorf("%w: it has %d characters; use at least %d (a passphrase of several words is easiest)", ErrWeakPassword, n, MinPasswordRunes)
	}
	return nil
}
