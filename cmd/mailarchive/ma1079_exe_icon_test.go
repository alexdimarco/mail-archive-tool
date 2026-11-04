package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// Windows resource types (winuser.h RT_*).
const (
	rtIcon      = 3
	rtGroupIcon = 14
)

// peResources returns a PE file's resources as type -> name id -> data (the first
// language of each), read from its .rsrc section.
func peResources(t *testing.T, path string) map[int]map[int][]byte {
	t.Helper()
	f, err := pe.Open(path)
	if err != nil {
		t.Fatalf("pe.Open %s: %v", path, err)
	}
	defer f.Close()
	sec := f.Section(".rsrc")
	if sec == nil {
		t.Errorf("%s has no .rsrc section (no resources)", filepath.Base(path))
		return nil
	}
	b, err := sec.Data()
	if err != nil {
		t.Fatal(err)
	}
	u16 := func(i int) int { return int(binary.LittleEndian.Uint16(b[i:])) }
	u32 := func(i int) uint32 { return binary.LittleEndian.Uint32(b[i:]) }
	// entries lists a directory's id entries: id -> offset (with the subdirectory bit).
	entries := func(dir int) [][2]uint32 {
		named, ids := u16(dir+12), u16(dir+14)
		var out [][2]uint32
		for k := 0; k < named+ids; k++ {
			e := dir + 16 + 8*k
			out = append(out, [2]uint32{u32(e), u32(e + 4)})
		}
		return out
	}
	const sub = 0x80000000
	res := map[int]map[int][]byte{}
	for _, ty := range entries(0) {
		if ty[1]&sub == 0 {
			continue
		}
		res[int(ty[0])] = map[int][]byte{}
		for _, nm := range entries(int(ty[1] &^ sub)) {
			if nm[1]&sub == 0 {
				continue
			}
			langs := entries(int(nm[1] &^ sub))
			if len(langs) == 0 || langs[0][1]&sub != 0 {
				continue
			}
			de := int(langs[0][1])
			rva, size := u32(de), u32(de+4)
			off := int(rva - sec.VirtualAddress)
			res[int(ty[0])][int(nm[0])] = b[off : off+int(size)]
		}
	}
	return res
}

// icoImages returns each image's bytes from a .ico file, in directory order.
func icoImages(t *testing.T, ico []byte) [][]byte {
	t.Helper()
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		t.Fatal("not a .ico file")
	}
	var out [][]byte
	for k := 0; k < int(binary.LittleEndian.Uint16(ico[4:])); k++ {
		e := 6 + 16*k
		n, off := binary.LittleEndian.Uint32(ico[e+8:]), binary.LittleEndian.Uint32(ico[e+12:])
		out = append(out, ico[off:off+n])
	}
	return out
}

// covers: MA-1079, R33
// The .exe files carry MailArchive's icon: cmd/mailarchive/rsrc_windows_amd64.syso is
// committed, and the two Windows builds of the one program, made by build-release.sh's
// own two GOOS=windows lines (which still differ only in -o and -H=windowsgui, MA-628),
// each hold one icon group (RT_GROUP_ICON) whose icons (RT_ICON), in order, are byte
// for byte the images of packaging/mailarchive-desktop/branding/mailarchive.ico, the
// file the installer's shortcuts and Add/Remove Programs use, so Explorer, the taskbar
// and the installer show one icon.
//
// Prove-fail (recorded in the commit): without the .syso both builds have no .rsrc
// section; a .syso made from the old .ico holds images that differ.
func TestMA1079_TheExeFilesCarryTheIcon(t *testing.T) {
	root := moduleRoot(t)
	if _, err := os.Stat(filepath.Join(root, "cmd", "mailarchive", "rsrc_windows_amd64.syso")); err != nil {
		t.Errorf("the icon resource is not committed: %v", err)
	}
	ico, err := os.ReadFile(filepath.Join(root, "packaging", "mailarchive-desktop", "branding", "mailarchive.ico"))
	if err != nil {
		t.Fatal(err)
	}
	want := assure.Reached(t, icoImages(t, ico), "the .ico's images")

	br, err := os.ReadFile(filepath.Join(root, "packaging", "build-release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	lines := regexp.MustCompile(`(?m)^\s*GOOS=windows GOARCH=amd64 go build (.*)\$ONE.*$`).FindAllStringSubmatch(string(br), -1)
	if len(lines) != 2 {
		t.Fatalf("want build-release.sh's 2 GOOS=windows build lines, got %d", len(lines))
	}
	tmp := t.TempDir()
	for _, m := range lines {
		on := regexp.MustCompile(`-o "\$DIST/([^"]+)"`).FindStringSubmatch(m[1])
		if on == nil {
			t.Fatalf("no -o in %q", m[0])
		}
		args := []string{"build"}
		if strings.Contains(m[1], "-ldflags -H=windowsgui") {
			args = append(args, "-ldflags", "-H=windowsgui")
		}
		out := filepath.Join(tmp, on[1])
		cmd := exec.Command("go", append(args, "-o", out, "./cmd/mailarchive")...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("building %s: %v\n%s", on[1], err, b)
		}

		res := peResources(t, out)
		groups := res[rtGroupIcon]
		if len(groups) != 1 {
			t.Errorf("%s holds %d icon groups, want 1", on[1], len(groups))
			continue
		}
		for _, g := range groups {
			// GRPICONDIR: reserved, type 1, count; then 14-byte entries ending in the RT_ICON id.
			n := int(binary.LittleEndian.Uint16(g[4:]))
			if n != len(want) {
				t.Errorf("%s's icon group has %d images, the .ico %d", on[1], n, len(want))
				continue
			}
			for k := 0; k < n; k++ {
				id := int(binary.LittleEndian.Uint16(g[6+14*k+12:]))
				if !bytes.Equal(res[rtIcon][id], want[k]) {
					t.Errorf("%s's icon %d (resource %d) is not the .ico's image %d", on[1], k, id, k)
				}
			}
		}
	}
}
