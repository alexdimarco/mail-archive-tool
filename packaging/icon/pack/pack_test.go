package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"mail-archive-tool/internal/assure"
)

// repoRoot is the repository root, two folders above this package's.
func repoRoot() string { return filepath.Join("..", "..", "..") }

func readRoot(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(), rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return assure.Reached(t, b, rel)
}

// parseICNS returns each entry's type and data, refusing a file whose lengths do not
// add up.
func parseICNS(t *testing.T, b []byte) (types []string, data map[string][]byte) {
	t.Helper()
	if len(b) < 8 || string(b[:4]) != "icns" || int(binary.BigEndian.Uint32(b[4:8])) != len(b) {
		t.Fatalf("not an icns file of its stated length (%d bytes)", len(b))
	}
	data = map[string][]byte{}
	for i := 8; i < len(b); {
		if i+8 > len(b) {
			t.Fatalf("icns: truncated entry header at %d", i)
		}
		typ, n := string(b[i:i+4]), int(binary.BigEndian.Uint32(b[i+4:i+8]))
		if n < 8 || i+n > len(b) {
			t.Fatalf("icns: entry %s at %d has length %d past the end", typ, i, n)
		}
		types = append(types, typ)
		data[typ] = b[i+8 : i+n]
		i += n
	}
	return types, data
}

// icoImage is one image of a .ico as its directory and data say.
type icoImage struct {
	Size  int
	PNG   bool
	Bits  int
	Bytes []byte
}

// parseICO reads a .ico's directory and checks each image against it: a PNG's own
// size, or a bitmap's header (size, height twice the width for the mask, 32 bits).
func parseICO(t *testing.T, b []byte) []icoImage {
	t.Helper()
	le16 := func(i int) int { return int(binary.LittleEndian.Uint16(b[i:])) }
	le32 := func(i int) int { return int(binary.LittleEndian.Uint32(b[i:])) }
	if len(b) < 6 || le16(0) != 0 || le16(2) != 1 {
		t.Fatal("not a .ico (reserved 0, type 1)")
	}
	var out []icoImage
	for k := 0; k < le16(4); k++ {
		e := 6 + 16*k
		size := int(b[e])
		if size == 0 {
			size = 256
		}
		if int(b[e+1]) != int(b[e]) || le16(e+4) != 1 || le16(e+6) != 32 {
			t.Errorf("ico entry %d: not square, 1 plane, 32 bits", k)
		}
		n, off := le32(e+8), le32(e+12)
		if off+n > len(b) {
			t.Fatalf("ico entry %d runs past the end", k)
		}
		d := b[off : off+n]
		img := icoImage{Size: size, Bytes: d}
		if bytes.HasPrefix(d, []byte("\x89PNG")) {
			img.PNG = true
			cfg, err := png.DecodeConfig(bytes.NewReader(d))
			if err != nil || cfg.Width != size || cfg.Height != size {
				t.Errorf("ico entry %d: PNG is not %d px (%v)", k, size, err)
			}
		} else {
			w, h := int(int32(binary.LittleEndian.Uint32(d[4:]))), int(int32(binary.LittleEndian.Uint32(d[8:])))
			img.Bits = int(binary.LittleEndian.Uint16(d[14:]))
			mask := ((size + 31) / 32) * 4
			if le32(off) != 40 || w != size || h != 2*size || img.Bits != 32 || n != 40+size*size*4+mask*size {
				t.Errorf("ico entry %d: bitmap header w=%d h=%d bits=%d len=%d does not fit %d px", k, w, h, img.Bits, n, size)
			}
		}
		out = append(out, img)
	}
	return out
}

// covers: MA-1077
// The icon's files are made from its PNGs and agree with them: every master PNG is
// the size its name says, transparent in the corners and opaque in the middle, and
// holds nothing but its pixels; and pack, run again on the masters, makes byte for
// byte the committed derived PNGs, mailarchive.icns, the Windows mailarchive.ico and
// the Desktop's favicon. The .icns holds icp4 16, ic11 32, icp5 32, ic12 64, ic07 128,
// ic13 256, ic08 256, ic14 512, ic09 512 and ic10 1024, each a PNG of that size; the
// .ico holds 16, 24, 32, 48, 64 and 128 as 32-bit bitmaps and 256 as a PNG.
//
// Prove-fail (recorded in the commit): a changed master pixel, a .ico left from before
// (the old art), or an icns entry dropped from icnsSet, each red.
func TestMA1077_IconFilesAreMadeFromTheirPNGs(t *testing.T) {
	for _, m := range masters {
		b := readRoot(t, pngDir+"/"+m+".png")
		img, err := decode(b)
		if err != nil {
			t.Fatalf("%s.png: %v", m, err)
		}
		want := map[string]int{"mailarchive-16": 16, "mailarchive-24": 24, "mailarchive-32": 32, "mailarchive-48": 48, "macos-1024": 1024, "square-1024": 1024}[m]
		if img.Rect.Dx() != want || img.Rect.Dy() != want {
			t.Errorf("%s.png is %dx%d, want %d px square", m, img.Rect.Dx(), img.Rect.Dy(), want)
		}
		w := img.Rect.Dx()
		if a := img.NRGBAAt(0, 0).A; a != 0 && m != "mailarchive-16" {
			t.Errorf("%s.png: the top-left corner is not transparent (alpha %d)", m, a)
		}
		if a := img.NRGBAAt(w/2, w/2).A; a != 255 {
			t.Errorf("%s.png: the middle is not opaque (alpha %d)", m, a)
		}
	}

	files, err := build(func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(repoRoot(), pngDir, name+".png"))
	})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rel := range files {
		names = append(names, rel)
	}
	sort.Strings(names)
	for _, rel := range names {
		got, err := os.ReadFile(filepath.Join(repoRoot(), rel))
		if err != nil || !bytes.Equal(got, files[rel]) {
			t.Errorf("%s is not what pack makes from the masters (run go run ./packaging/icon/pack): %v", rel, err)
		}
	}
	for _, want := range []string{icnsPath, icoPath, faviconPath, pngDir + "/macos-64.png", pngDir + "/square-256.png"} {
		if files[want] == nil {
			t.Errorf("pack does not make %s", want)
		}
	}

	types, data := parseICNS(t, readRoot(t, icnsPath))
	wantTypes := []string{"icp4", "ic11", "icp5", "ic12", "ic07", "ic13", "ic08", "ic14", "ic09", "ic10"}
	wantSize := map[string]int{"icp4": 16, "ic11": 32, "icp5": 32, "ic12": 64, "ic07": 128, "ic13": 256, "ic08": 256, "ic14": 512, "ic09": 512, "ic10": 1024}
	if len(types) != len(wantTypes) {
		t.Errorf("mailarchive.icns holds %v, want %v", types, wantTypes)
	}
	for i, typ := range wantTypes {
		if i < len(types) && types[i] != typ {
			t.Errorf("mailarchive.icns entry %d is %s, want %s", i, types[i], typ)
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(data[typ]))
		if err != nil || cfg.Width != wantSize[typ] || cfg.Height != wantSize[typ] {
			t.Errorf("mailarchive.icns %s is not a %d px PNG (%v)", typ, wantSize[typ], err)
		}
	}

	imgs := parseICO(t, readRoot(t, icoPath))
	var sizes []int
	for _, im := range imgs {
		sizes = append(sizes, im.Size)
		if im.PNG != (im.Size == 256) {
			t.Errorf("mailarchive.ico %d px: PNG=%v, want a PNG only at 256", im.Size, im.PNG)
		}
	}
	if !equalInts(sizes, []int{16, 24, 32, 48, 64, 128, 256}) {
		t.Errorf("mailarchive.ico holds sizes %v, want 16 24 32 48 64 128 256", sizes)
	}
}

// covers: MA-1077
// The packer's own pieces, on made-up pixels: halving averages by alpha (a
// transparent neighbour does not darken an edge) and a fully transparent block stays
// transparent; a .ico bitmap's mask marks exactly the transparent pixels, bottom row
// first; the .icns lengths add up. Positive twin of the committed-file check above.
func TestMA1077_PackPiecesOnMadeUpPixels(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	// Left 2x2: one opaque red pixel and three transparent ones.
	src.Pix[0], src.Pix[3] = 200, 255
	// Right 2x2: all transparent.
	h := halve(src)
	if p := h.NRGBAAt(0, 0); p.R != 200 || p.A != 64 {
		t.Errorf("halve: an opaque red beside transparent pixels gave %v, want R 200 A 64", p)
	}
	if p := h.NRGBAAt(1, 0); p.A != 0 || p.R != 0 {
		t.Errorf("halve: a transparent block gave %v", p)
	}

	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	img.Pix[img.PixOffset(1, 0)+3] = 0 // top-right transparent
	d := dib(img)
	mask := d[40+2*2*4:]
	// Bottom-up: the first mask row is the bottom row (all opaque), the second the top.
	if mask[0] != 0 || mask[4] != 0x40 {
		t.Errorf("dib mask rows %x %x, want 00 40", mask[0], mask[4])
	}
	ico, err := makeICO([]*image.NRGBA{img}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseICO(t, ico); len(got) != 1 || got[0].Size != 2 || got[0].PNG {
		t.Errorf("makeICO of one 2 px image parsed as %+v", got)
	}
	if _, err := makeICO([]*image.NRGBA{image.NewNRGBA(image.Rect(0, 0, 256, 256))}, nil); err == nil {
		t.Error("makeICO made a 256 px entry without its PNG")
	}

	b := icns([]icnsEntry{{"icp4", "x", 16}}, map[string][]byte{"x": []byte("abc")})
	if types, data := parseICNS(t, b); len(types) != 1 || string(data["icp4"]) != "abc" {
		t.Errorf("icns of one entry parsed as %v %q", types, data["icp4"])
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
