// Command pack builds MailArchive's icon files from the PNGs in packaging/icon/png:
// the macOS mailarchive.icns, the Windows mailarchive.ico (the installer's and the
// .exe files'), the Desktop page's favicon, and the .exe files' icon resource. It uses
// the standard library only, and its output depends only on the master PNGs, so the
// files it writes are committed and CI only copies them.
//
// Run it from the repository root after render.sh has drawn new masters:
//
//	go run ./packaging/icon/pack
//
// It reads the masters (png/mailarchive-16, -24, -32, -48, png/macos-1024 and
// png/square-1024), writes each back with nothing but its pixels, makes the smaller
// macOS and square sizes from the 1024 masters by averaging (halving each time), and
// writes the files named in outputs. Then it runs github.com/akavel/rsrc (already in
// go.mod) to turn the .ico into cmd/mailarchive/rsrc_windows_amd64.syso, which the Go
// linker adds to every windows/amd64 build of ./cmd/mailarchive.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// Paths, relative to the repository root.
const (
	pngDir      = "packaging/icon/png"
	icnsPath    = "packaging/icon/mailarchive.icns"
	icoPath     = "packaging/mailarchive-desktop/branding/mailarchive.ico"
	faviconPath = "internal/desktop/favicon.png"
	sysoPath    = "cmd/mailarchive/rsrc_windows_amd64.syso"
)

// masters are the PNGs render.sh draws; every other PNG is made from them.
var masters = []string{"mailarchive-16", "mailarchive-24", "mailarchive-32", "mailarchive-48", "macos-1024", "square-1024"}

// derived are the sizes made from a 1024 master by halving.
var derived = map[string][]int{"macos": {512, 256, 128, 64}, "square": {256, 128, 64}}

// icnsEntry is one image of the .icns: its type code and the PNG it holds.
type icnsEntry struct {
	Type string
	PNG  string // name in png/, without .png
	Size int
}

// icnsSet is what the .icns holds, in the order iconutil writes it: the small
// drawings at 16 and 32 points (1x and 2x), the macOS drawing from 32 points @2x up.
var icnsSet = []icnsEntry{
	{"icp4", "mailarchive-16", 16},
	{"ic11", "mailarchive-32", 32}, // 16 pt @2x
	{"icp5", "mailarchive-32", 32},
	{"ic12", "macos-64", 64}, // 32 pt @2x
	{"ic07", "macos-128", 128},
	{"ic13", "macos-256", 256}, // 128 pt @2x
	{"ic08", "macos-256", 256},
	{"ic14", "macos-512", 512}, // 256 pt @2x
	{"ic09", "macos-512", 512},
	{"ic10", "macos-1024", 1024}, // 512 pt @2x
}

// icoSet is what the .ico holds: the small drawings up to 48 px, the square drawing
// above. Sizes under 256 are stored as 32-bit bitmaps, 256 as PNG, as Windows' own are.
var icoSet = []struct {
	PNG  string
	Size int
}{
	{"mailarchive-16", 16}, {"mailarchive-24", 24}, {"mailarchive-32", 32}, {"mailarchive-48", 48},
	{"square-64", 64}, {"square-128", 128}, {"square-256", 256},
}

// faviconPNG is the PNG the Desktop page uses as its favicon.
const faviconPNG = "mailarchive-32"

func main() {
	root := flag.String("root", ".", "the repository root")
	noRsrc := flag.Bool("no-rsrc", false, "do not rebuild the .syso")
	flag.Parse()
	if err := run(*root, !*noRsrc); err != nil {
		fmt.Fprintln(os.Stderr, "pack:", err)
		os.Exit(1)
	}
}

func run(root string, rsrc bool) error {
	files, err := build(func(name string) ([]byte, error) {
		return os.ReadFile(filepath.Join(root, pngDir, name+".png"))
	})
	if err != nil {
		return err
	}
	var rels []string
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		if err := os.WriteFile(filepath.Join(root, rel), files[rel], 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%d bytes)\n", rel, len(files[rel]))
	}
	if !rsrc {
		return nil
	}
	cmd := exec.Command("go", "run", "github.com/akavel/rsrc", "-arch", "amd64", "-ico", icoPath, "-o", sysoPath)
	cmd.Dir, cmd.Stdout, cmd.Stderr = root, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsrc: %w", err)
	}
	fmt.Printf("wrote %s\n", sysoPath)
	return nil
}

// build returns every file pack writes, by path relative to the repository root,
// from the masters that read returns: the masters rewritten with nothing but their
// pixels, the derived PNGs, the .icns, the .ico and the favicon.
func build(read func(name string) ([]byte, error)) (map[string][]byte, error) {
	out := map[string][]byte{}
	pngs := map[string][]byte{}
	imgs := map[string]*image.NRGBA{}
	for _, m := range masters {
		raw, err := read(m)
		if err != nil {
			return nil, err
		}
		img, err := decode(raw)
		if err != nil {
			return nil, fmt.Errorf("%s.png: %w", m, err)
		}
		imgs[m] = img
	}
	for prefix, sizes := range derived {
		img := imgs[prefix+"-1024"]
		for _, s := range sizes {
			for img.Bounds().Dx() > s {
				img = halve(img)
			}
			imgs[fmt.Sprintf("%s-%d", prefix, s)] = img
		}
	}
	for name, img := range imgs {
		b, err := encode(img)
		if err != nil {
			return nil, err
		}
		pngs[name] = b
		out[pngDir+"/"+name+".png"] = b
	}

	var entries []icnsEntry
	for _, e := range icnsSet {
		if imgs[e.PNG] == nil || imgs[e.PNG].Bounds().Dx() != e.Size || imgs[e.PNG].Bounds().Dy() != e.Size {
			return nil, fmt.Errorf("icns %s: %s is not %d px square", e.Type, e.PNG, e.Size)
		}
		entries = append(entries, e)
	}
	out[icnsPath] = icns(entries, pngs)

	var icoImgs []*image.NRGBA
	for _, e := range icoSet {
		img := imgs[e.PNG]
		if img == nil || img.Bounds().Dx() != e.Size || img.Bounds().Dy() != e.Size {
			return nil, fmt.Errorf("ico: %s is not %d px square", e.PNG, e.Size)
		}
		icoImgs = append(icoImgs, img)
	}
	ico, err := makeICO(icoImgs, pngs["square-256"])
	if err != nil {
		return nil, err
	}
	out[icoPath] = ico
	out[faviconPath] = pngs[faviconPNG]
	return out, nil
}

// decode reads a PNG into non-premultiplied RGBA.
func decode(b []byte) (*image.NRGBA, error) {
	src, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	if n, ok := src.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) {
		return n, nil
	}
	n := image.NewNRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(n, n.Rect, src, src.Bounds().Min, draw.Src)
	return n, nil
}

// encode writes img as a PNG with nothing but its pixels (no colour profile, gamma or
// date), always the same bytes for the same pixels.
func encode(img *image.NRGBA) ([]byte, error) {
	var b bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// halve makes an image half the size by averaging each 2x2 block, weighting colour by
// alpha so transparent pixels do not darken the edges.
func halve(src *image.NRGBA) *image.NRGBA {
	w, h := src.Rect.Dx()/2, src.Rect.Dy()/2
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sa, sr, sg, sb int
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					i := src.PixOffset(2*x+dx, 2*y+dy)
					p := src.Pix[i : i+4]
					a := int(p[3])
					sa += a
					sr += int(p[0]) * a
					sg += int(p[1]) * a
					sb += int(p[2]) * a
				}
			}
			o := dst.PixOffset(x, y)
			if sa == 0 {
				continue // fully transparent: stays 0,0,0,0
			}
			dst.Pix[o+0] = uint8((sr + sa/2) / sa)
			dst.Pix[o+1] = uint8((sg + sa/2) / sa)
			dst.Pix[o+2] = uint8((sb + sa/2) / sa)
			dst.Pix[o+3] = uint8((sa + 2) / 4)
		}
	}
	return dst
}

// icns writes Apple's icon file: "icns", the file's length, then each entry as its
// type, its length (8 + data) and its PNG, all lengths big-endian.
func icns(entries []icnsEntry, pngs map[string][]byte) []byte {
	var body bytes.Buffer
	for _, e := range entries {
		data := pngs[e.PNG]
		body.WriteString(e.Type)
		binary.Write(&body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

// makeICO writes a Windows icon file: the ICONDIR header, one 16-byte entry per
// image, then the images. A 256 px image is stored as the PNG given; a smaller one as
// a 32-bit bottom-up bitmap (BITMAPINFOHEADER, BGRA pixels with straight alpha, then
// the 1-bit AND mask that marks fully transparent pixels), all little-endian.
func makeICO(imgs []*image.NRGBA, png256 []byte) ([]byte, error) {
	var datas [][]byte
	for _, img := range imgs {
		if img.Rect.Dx() == 256 {
			if png256 == nil {
				return nil, fmt.Errorf("ico: no PNG for 256 px")
			}
			datas = append(datas, png256)
			continue
		}
		datas = append(datas, dib(img))
	}
	var out bytes.Buffer
	le := func(v any) { binary.Write(&out, binary.LittleEndian, v) }
	le(uint16(0))         // reserved
	le(uint16(1))         // type: icon
	le(uint16(len(imgs))) // count
	offset := 6 + 16*len(imgs)
	for i, img := range imgs {
		s := img.Rect.Dx()
		dim := uint8(s)
		if s >= 256 {
			dim = 0 // 0 means 256
		}
		out.WriteByte(dim) // width
		out.WriteByte(dim) // height
		out.WriteByte(0)   // colours in palette: none
		out.WriteByte(0)   // reserved
		le(uint16(1))      // colour planes
		le(uint16(32))     // bits per pixel
		le(uint32(len(datas[i])))
		le(uint32(offset))
		offset += len(datas[i])
	}
	for _, d := range datas {
		out.Write(d)
	}
	return out.Bytes(), nil
}

// dib is one image of a .ico in bitmap form.
func dib(img *image.NRGBA) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	maskRow := ((w + 31) / 32) * 4
	var b bytes.Buffer
	le := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	le(uint32(40))                // header size
	le(int32(w))                  // width
	le(int32(2 * h))              // height: the colour rows and the mask rows
	le(uint16(1))                 // planes
	le(uint16(32))                // bits per pixel
	le(uint32(0))                 // no compression
	le(uint32(w*h*4 + maskRow*h)) // image size
	le(int32(0))                  // horizontal resolution
	le(int32(0))                  // vertical resolution
	le(uint32(0))                 // colours used
	le(uint32(0))                 // important colours
	for y := h - 1; y >= 0; y-- { // bottom row first
		for x := 0; x < w; x++ {
			p := img.Pix[img.PixOffset(x, y):]
			b.Write([]byte{p[2], p[1], p[0], p[3]})
		}
	}
	for y := h - 1; y >= 0; y-- {
		row := make([]byte, maskRow)
		for x := 0; x < w; x++ {
			if img.Pix[img.PixOffset(x, y)+3] == 0 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
		b.Write(row)
	}
	return b.Bytes()
}
