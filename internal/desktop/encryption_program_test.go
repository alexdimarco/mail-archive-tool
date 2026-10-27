package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-847, R37
// The Encryption card's switch for keeping copies of MailArchive (program design §6,
// C16): refused while the archive is locked, and while another run holds the archive,
// nothing sealed either time; on, it is sealed at once and the card says the copies
// come at the next backup, which writes them; off, it is sealed at once, the copies stay
// until the next backup — the card says they are removed then — and that backup removes
// them.
func TestTheEncryptionCardSwitchesProgramCopies(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	keep := func() *bool {
		t.Helper()
		u, err := keyring.Open(out, keyring.Password(keysPassword), keyring.OpenOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer u.Destroy()
		ps, _, err := keyring.ProgramCopies(u)
		if err != nil {
			t.Fatal(err)
		}
		return ps.Keep
	}
	backUp := func() {
		t.Helper()
		u, err := keyring.Open(out, keyring.Password(keysPassword), keyring.OpenOptions{Write: true})
		if err != nil {
			t.Fatal(err)
		}
		defer u.Destroy()
		backUpInto(t, out, u)
	}
	program := func() int {
		entries, _ := os.ReadDir(filepath.Join(out, container.ProgramDir))
		return len(entries)
	}

	code, m := e.post(t, "api/encryption/program-copies", map[string]any{"on": true})
	assure.Refused(t, code, toText(m), assure.Code(http.StatusOK), assure.Names("locked", "Encryption card"),
		assure.NoSideEffect(func() bool { return keep() == nil }))

	if _, m := e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword}); m["ok"] != true {
		t.Fatalf("unlock: %v", m)
	}
	if page := sessionPage(t, e.h); !strings.Contains(page, `id="encProgramCopies">`) || !strings.Contains(page, "No copies of MailArchive are kept") {
		t.Fatal("an archive with no choice sealed: the switch is offered, off")
	}

	lock, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "a test's run")
	if err != nil {
		t.Fatal(err)
	}
	code, m = e.post(t, "api/encryption/program-copies", map[string]any{"on": true})
	lock.Release()
	assure.Refused(t, code, toText(m), assure.Code(http.StatusOK), assure.Names("Nothing was changed"),
		assure.NoSideEffect(func() bool { return keep() == nil }))

	if _, m := e.post(t, "api/encryption/program-copies", map[string]any{"on": true}); m["ok"] != true {
		t.Fatalf("on: %v", m)
	}
	if k := keep(); k == nil || !*k || program() != 0 {
		t.Fatalf("on: sealed %v, program/ holds %d (written by the next backup, not now)", k, program())
	}
	if page := sessionPage(t, e.h); !strings.Contains(page, `id="encProgramCopies" checked>`) || !strings.Contains(page, "written into this archive at its next backup") {
		t.Fatal("on: the card says the copies come at the next backup")
	}
	backUp()
	if program() == 0 || !strings.Contains(sessionPage(t, e.h), "This archive keeps copies of MailArchive") {
		t.Fatalf("the backup after on writes the copies: program/ holds %d", program())
	}

	if _, m := e.post(t, "api/encryption/program-copies", map[string]any{"on": false}); m["ok"] != true {
		t.Fatalf("off: %v", m)
	}
	if k := keep(); k == nil || *k || program() == 0 {
		t.Fatalf("off: sealed %v, program/ holds %d (removed by the next backup, not now)", k, program())
	}
	if !strings.Contains(sessionPage(t, e.h), "removed at its next backup") {
		t.Fatal("off: the card says the copies go at the next backup")
	}
	backUp()
	assure.Reached(t, program() == 0 && strings.Contains(sessionPage(t, e.h), "No copies of MailArchive are kept"), "the backup after off removes the copies")
}
