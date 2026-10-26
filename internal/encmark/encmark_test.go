package encmark

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// archive makes a folder holding an encrypted archive's key files, recorded by this
// computer, and returns it with its id.
func archive(t *testing.T) (string, string) {
	t.Helper()
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "Archive")
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := vault.PrepareCreate(out, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	id := p.ArchiveID().String()
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	os.WriteFile(filepath.Join(out, container.PointerName), []byte("sealed"), 0o600)
	os.MkdirAll(filepath.Join(out, "units", "ab"), 0o700)
	if _, err := encrecord.Update("test", func(r *encrecord.Record) error { return r.Add(id, out, false, time.Now()) }); err != nil {
		t.Fatal(err)
	}
	return out, id
}

// strip removes everything an encrypted archive wrote.
func strip(t *testing.T, out string) {
	t.Helper()
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(out, e.Name()))
	}
}

// covers: MA-770, R37
// A folder this computer recorded as encrypted stays encrypted whatever it holds.
// The positive twin: whole, it reads as its own archive (case None). Stripped of its
// key files, units and record and replanted as a plaintext archive, it still reads
// encrypted, case 2 — and so with a planted foreign key file added. Each other case
// in its order: a link, an unproven path, key files of another archive, key files
// missing beside sealed data (operating-system litter ignored), and nothing left.
func TestARecordedArchiveStaysEncryptedWhateverItHolds(t *testing.T) {
	out, id := archive(t)
	r, err := At(out)
	if err != nil || !r.Encrypted || !r.Recorded || r.ID != id || r.Case != None {
		t.Fatalf("the whole archive: %+v %v", r, err)
	}

	cases := []struct {
		name  string
		plant func(t *testing.T)
		want  Case
		says  string
	}{
		{"replanted as plaintext", func(t *testing.T) {
			strip(t, out)
			os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":6,"entries":{}}`), 0o600)
			os.Mkdir(filepath.Join(out, "Inbox"), 0o700)
		}, Other, "may have been tampered with"},
		{"replanted with a foreign key file", func(t *testing.T) {
			strip(t, out)
			os.WriteFile(filepath.Join(out, "index.html"), []byte("<html>"), 0o600)
			os.WriteFile(filepath.Join(out, vault.FileName), []byte(`{"archive_id":"fedcba9876543210fedcba9876543210"}`), 0o600)
		}, Other, "may have been tampered with"},
		{"another archive's key file", func(t *testing.T) {
			strip(t, out)
			os.WriteFile(filepath.Join(out, vault.FileName), []byte(`{"archive_id":"fedcba9876543210fedcba9876543210"}`), 0o600)
		}, Foreign, "different encrypted archive"},
		{"key files missing beside sealed data and litter", func(t *testing.T) {
			os.Remove(filepath.Join(out, vault.FileName))
			os.Remove(filepath.Join(out, vault.BackupName))
			for _, n := range []string{".DS_Store", "._units", "Thumbs.db", "System Volume Information"} {
				os.WriteFile(filepath.Join(out, n), nil, 0o600)
			}
		}, KeysMissing, "key files are missing or damaged"},
		{"only litter and the note", func(t *testing.T) {
			strip(t, out)
			os.WriteFile(filepath.Join(out, ".DS_Store"), nil, 0o600)
			os.WriteFile(filepath.Join(out, "README.txt"), nil, 0o600)
		}, Gone, "used to hold an encrypted archive"},
		{"gone", func(t *testing.T) { os.RemoveAll(out) }, Gone, "used to hold an encrypted archive"},
		{"a link to an empty sibling", func(t *testing.T) {
			os.RemoveAll(out)
			sib := out + "-empty"
			os.Mkdir(sib, 0o700)
			if err := os.Symlink(sib, out); err != nil {
				t.Skip("no symlinks here:", err)
			}
		}, Link, "is now a link"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _ = archive(t)
			tc.plant(t)
			r, err := At(out)
			if err != nil || !r.Encrypted || !r.Recorded || r.Case != tc.want || !strings.Contains(r.Why, tc.says) {
				t.Fatalf("want encrypted, case %d saying %q; got %+v %v", tc.want, tc.says, r, err)
			}
			if dir, why, _ := AtOrAbove(filepath.Join(out, "store")); dir == "" || why == "" {
				t.Fatal("a folder inside the recorded archive reads as plaintext")
			}
		})
	}
	t.Run("unproven", func(t *testing.T) {
		t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
		folder := t.TempDir()
		encrecord.Update("test", func(r *encrecord.Record) error {
			return r.Add("0123456789abcdef0123456789abcdef", folder, true, time.Now())
		})
		r, _ := At(folder)
		if r.Case != Unproven || !r.Encrypted || !strings.Contains(r.Why, "record was repaired") {
			t.Fatalf("an unproven path: %+v", r)
		}
	})
}

// covers: MA-770, R37
// While this computer's record cannot be used, a folder holding MailArchive's own
// files is refused, naming the record's problem; a new empty folder is not.
func TestAnUnusableRecordRefusesFoldersHoldingArchiveFiles(t *testing.T) {
	was := load
	load = func() (*encrecord.Record, error) {
		return nil, &encrecord.UnusableError{Case: encrecord.CaseDamaged, Path: "/x/" + encrecord.FileName}
	}
	defer func() { load = was }()
	plain := t.TempDir()
	os.WriteFile(filepath.Join(plain, state.LastRunName), []byte("{}"), 0o600)
	r, _ := At(plain)
	if !r.Encrypted || !r.ByRecordErr || r.RecordErr == nil || !strings.Contains(r.Why, "needs repair") {
		t.Fatalf("a folder holding archive files: %+v", r)
	}
	// Refused, but not labelled encrypted, while what is written beside it stays
	// minimised (CON-01, REG-02, CRIT-03).
	if enc, _, _ := Here(plain); enc {
		t.Fatal("a plaintext archive is labelled encrypted while the record cannot be used")
	}
	if !MustMinimise(plain) {
		t.Fatal("a refused run's record beside a folder that may be encrypted is not minimised")
	}
	empty := t.TempDir()
	r, err := At(empty)
	if r.Encrypted || r.RecordErr == nil || err != nil || MustMinimise(empty) {
		t.Fatalf("a new empty folder: %+v %v", r, err)
	}
}

// covers: MA-775, R37
// The cases' edges found by the P3-0 adversarial pass: an archive reached through
// the link it was created with is its own (FID-01); key files of this archive from
// a newer MailArchive are newer, not damaged (FID-02); a plaintext manifest under
// the reserved name is other files (OUT-06); operating systems' drive-root litter
// changes nothing (CRIT-04); and an unproven path stays unproven whatever it holds
// (FID-09).
func TestTheCasesHoldAtTheirEdges(t *testing.T) {
	t.Run("created through a link", func(t *testing.T) {
		out, id := archive(t)
		link := filepath.Join(t.TempDir(), "via")
		if err := os.Symlink(out, link); err != nil {
			t.Skip("no symlinks here:", err)
		}
		encrecord.Update("test", func(r *encrecord.Record) error { return r.Add(id, link, false, time.Now()) })
		for _, q := range []string{link, out} {
			if r, _ := At(q); r.Case != None || !r.Encrypted {
				t.Fatalf("%s: %+v", q, r)
			}
		}
	})
	t.Run("newer key files", func(t *testing.T) {
		out, id := archive(t)
		for _, n := range []string{vault.FileName, vault.BackupName} {
			os.WriteFile(filepath.Join(out, n), []byte(`{"format":99,"archive_id":"`+id+`"}`), 0o600)
		}
		if r, _ := At(out); r.Case != None {
			t.Fatalf("newer key files of this archive: %+v", r)
		}
	})
	t.Run("a plaintext manifest", func(t *testing.T) {
		out, _ := archive(t)
		strip(t, out)
		os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":6,"entries":{}}`), 0o600)
		if r, _ := At(out); r.Case != Other {
			t.Fatalf("a plaintext manifest: %+v", r)
		}
	})
	t.Run("drive-root litter", func(t *testing.T) {
		out, _ := archive(t)
		for _, n := range []string{".Trash-1000", ".DocumentRevisions-V100", ".apdisk", ".VolumeIcon.icns", "Icon\r", ".localized", "found.000", ".PKInstallSandboxManager-SystemSoftware"} {
			os.WriteFile(filepath.Join(out, n), nil, 0o600)
		}
		if r, _ := At(out); r.Case != None {
			t.Fatalf("a healthy archive with drive-root litter: %+v", r)
		}
	})
	t.Run("unproven holding other files", func(t *testing.T) {
		t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
		folder := t.TempDir()
		os.WriteFile(filepath.Join(folder, "index.html"), nil, 0o600)
		encrecord.Update("test", func(r *encrecord.Record) error {
			return r.Add("0123456789abcdef0123456789abcdef", folder, true, time.Now())
		})
		if r, _ := At(folder); r.Case != Unproven {
			t.Fatalf("an unproven path holding other files: %+v", r)
		}
	})
}

// covers: MA-770, R37
// The reserved names are exactly the names an encrypted archive writes at its root
// (the tree grammar's constants), so none of them reads as "other files".
func TestTheReservedNamesAreTheArchivesOwn(t *testing.T) {
	for _, n := range []string{vault.FileName, vault.BackupName, lockfile.Name, state.LastRunName, state.RunHistoryName,
		state.SourceStreakName, state.LastVerifyName, state.AttentionName, state.IntegrityAttentionName, seal.Name,
		"README.txt", container.EnvelopeName, container.PointerName, container.MirrorName, container.InventoryName,
		container.InventoryDir, container.ChunksDir, export.UnitsDir, ".mailarchive-manifest-123.tmp"} {
		if !isReserved(n) {
			t.Errorf("%s is an encrypted archive's own name but is not reserved", n)
		}
	}
	for _, n := range []string{"index.html", "Inbox", "report.tsv", "search.db"} {
		if isReserved(n) || isLitter(n) {
			t.Errorf("%s is neither reserved nor litter", n)
		}
	}
}

// covers: MA-770, R37
// Every production caller asks encmark: no Go file outside internal/encmark and
// internal/vault calls the vault's markers itself.
func TestEveryCallerAsksEncmark(t *testing.T) {
	root, _ := filepath.Abs("../..")
	hits, scanned := markerUses(t, root, []string{"internal", "cmd"})
	if scanned < 100 {
		t.Fatalf("the scan read only %d Go files: wrong root", scanned)
	}
	if len(hits) > 0 {
		t.Fatalf("these use the vault's markers instead of encmark: %v", hits)
	}
	// The scan's twin: a renamed import, a call and a function value are each found
	// (the P3-0 adversarial pass, FID-07).
	plant := t.TempDir()
	os.MkdirAll(filepath.Join(plant, "internal", "x"), 0o700)
	os.WriteFile(filepath.Join(plant, "internal", "x", "x.go"), []byte(`package x

import vlt "mail-archive-tool/internal/vault"

var f = vlt.EncryptedAtOrAbove

func g() { vlt.EncryptedMarker("") }
`), 0o600)
	if hits, _ := markerUses(t, plant, []string{"internal"}); len(hits) != 2 {
		t.Fatalf("the scan missed a planted use: %v", hits)
	}
}

// markerUses parses every non-test Go file under root's tops and reports each use
// of vault.EncryptedMarker or vault.EncryptedAtOrAbove — through whatever name the
// file imports the vault under — outside internal/encmark and internal/vault.
func markerUses(t *testing.T, root string, tops []string) (hits []string, scanned int) {
	t.Helper()
	const vaultPath = `"mail-archive-tool/internal/vault"`
	for _, top := range tops {
		err := filepath.WalkDir(filepath.Join(root, top), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "internal/encmark/") || strings.HasPrefix(rel, "internal/vault/") {
				return nil
			}
			f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
			if perr != nil {
				return perr
			}
			scanned++
			name := ""
			for _, im := range f.Imports {
				if im.Path.Value == vaultPath {
					name = "vault"
					if im.Name != nil {
						name = im.Name.Name
					}
				}
			}
			if name == "" {
				return nil
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == name && (sel.Sel.Name == "EncryptedMarker" || sel.Sel.Name == "EncryptedAtOrAbove") {
						hits = append(hits, rel+": "+sel.Sel.Name)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", top, err)
		}
	}
	return hits, scanned
}
