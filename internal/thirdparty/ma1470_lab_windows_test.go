//go:build windows

package thirdparty

// MA-1470: the Windows lab test of the patched dialogs in the copy of zenity
// (docs/design-windows-dialog-fit.md §5). It opens real dialogs, so it runs only with
// MAILARCHIVE_DESKTOP_LAB=1 in a signed-in, unlocked Windows session (the office test
// PC). It measures each dialog from outside with Win32, with the font each control
// draws with: every row, prompt line and button label must fit, every control must lie
// inside its dialog without overlapping another, the dialog must lie inside its work
// area within the cap, the ink of each label's last line must be on screen, and the
// answers must be the ones v0.10.12 gives.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/ncruces/zenity"
	"golang.org/x/sys/windows"
)

var (
	labUser32 = windows.NewLazySystemDLL("user32.dll")
	labGdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procFindWindowW            = labUser32.NewProc("FindWindowW")
	procGetWindow              = labUser32.NewProc("GetWindow")
	procGetClassNameW          = labUser32.NewProc("GetClassNameW")
	procGetWindowTextW         = labUser32.NewProc("GetWindowTextW")
	procGetWindowRect          = labUser32.NewProc("GetWindowRect")
	procGetClientRect          = labUser32.NewProc("GetClientRect")
	procMapWindowPoints        = labUser32.NewProc("MapWindowPoints")
	procIsWindowVisible        = labUser32.NewProc("IsWindowVisible")
	procIsWindowEnabled        = labUser32.NewProc("IsWindowEnabled")
	procSendMessageW           = labUser32.NewProc("SendMessageW")
	procPostMessageW           = labUser32.NewProc("PostMessageW")
	procGetDC                  = labUser32.NewProc("GetDC")
	procReleaseDC              = labUser32.NewProc("ReleaseDC")
	procDrawTextW              = labUser32.NewProc("DrawTextW")
	procMonitorFromWindow      = labUser32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW        = labUser32.NewProc("GetMonitorInfoW")
	procGetScrollBarInfo       = labUser32.NewProc("GetScrollBarInfo")
	procPrintWindow            = labUser32.NewProc("PrintWindow")
	procGetForegroundWindow    = labUser32.NewProc("GetForegroundWindow")
	procGetGUIThreadInfo       = labUser32.NewProc("GetGUIThreadInfo")
	procGetWindowThreadProcess = labUser32.NewProc("GetWindowThreadProcessId")
	procAllowSetForeground     = labUser32.NewProc("AllowSetForegroundWindow")
	procSetForegroundWindow    = labUser32.NewProc("SetForegroundWindow")
	procGetGuiResources        = labUser32.NewProc("GetGuiResources")
	procSetThreadDpiAwareness  = labUser32.NewProc("SetThreadDpiAwarenessContext")
	procSelectObject           = labGdi32.NewProc("SelectObject")
	procCreateCompatibleDC     = labGdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = labGdi32.NewProc("CreateCompatibleBitmap")
	procGetDIBits              = labGdi32.NewProc("GetDIBits")
	procDeleteObject           = labGdi32.NewProc("DeleteObject")
	procDeleteDC               = labGdi32.NewProc("DeleteDC")
)

const (
	labEnv = "MAILARCHIVE_DESKTOP_LAB"

	gwHwndNext = 2
	gwChild    = 5

	wmClose       = 0x0010
	wmGetFont     = 0x0031
	wmKeyDown     = 0x0100
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmVScroll     = 0x0115
	wmDPIChanged  = 0x02e0
	vkReturn      = 0x0d
	vkDown        = 0x28
	sbBottom      = 7
	mkLButton     = 0x0001

	emGetLineCount        = 0x00ba
	emGetFirstVisibleLine = 0x00ce
	emPosFromChar         = 0x00d6
	lbGetCount            = 0x018b
	lbGetText             = 0x0189
	lbGetTextLen          = 0x018a
	lbGetCurSel           = 0x0188
	lbGetHorizontalExtent = 0x0193

	dtWordBreak   = 0x0010
	dtSingleLine  = 0x0020
	dtExpandTabs  = 0x0040
	dtCalcRect    = 0x0400
	dtNoPrefix    = 0x0800
	dtEditControl = 0x2000

	objidHScroll       = ^uintptr(5) // OBJID_HSCROLL (-6)
	stateInvisible     = 0x8000      // STATE_SYSTEM_INVISIBLE
	monitorNearest     = 2
	pwClientOnly       = 1
	pwRenderFull       = 2
	grGDIObjects       = 0
	asfwAny            = ^uintptr(0) // ASFW_ANY
	dibRGBColors       = 0
	biRGB              = 0
	labDialogWaitLimit = 10 * time.Second

	dpiPerMonitorV2 = ^uintptr(4) + 1 // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
)

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) w() int32 { return r.Right - r.Left }
func (r rect) h() int32 { return r.Bottom - r.Top }

func call(p *windows.LazyProc, args ...uintptr) uintptr {
	r, _, _ := p.Call(args...)
	return r
}

func ptrTo(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		panic(err)
	}
	return p
}

func className(h uintptr) string {
	buf := make([]uint16, 256)
	call(procGetClassNameW, h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return windows.UTF16ToString(buf)
}

func windowText(h uintptr) string {
	buf := make([]uint16, 1<<16)
	call(procGetWindowTextW, h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}

func windowRect(h uintptr) rect {
	var r rect
	call(procGetWindowRect, h, uintptr(unsafe.Pointer(&r)))
	return r
}

func clientRect(h uintptr) rect {
	var r rect
	call(procGetClientRect, h, uintptr(unsafe.Pointer(&r)))
	return r
}

// rectIn is a child's rectangle in its parent's client coordinates.
func rectIn(parent, h uintptr) rect {
	r := windowRect(h)
	call(procMapWindowPoints, 0, parent, uintptr(unsafe.Pointer(&r)), 2)
	return r
}

func visible(h uintptr) bool { return call(procIsWindowVisible, h) != 0 }
func enabled(h uintptr) bool { return call(procIsWindowEnabled, h) != 0 }

func send(h, msg, w, l uintptr) uintptr { return call(procSendMessageW, h, msg, w, l) }
func post(h, msg, w, l uintptr)         { call(procPostMessageW, h, msg, w, l) }

// child is one control of a dialog.
type child struct {
	hwnd  uintptr
	class string
	text  string
	r     rect // in the dialog's client coordinates
}

func children(dlg uintptr) []child {
	var out []child
	for h := call(procGetWindow, dlg, gwChild); h != 0; h = call(procGetWindow, h, gwHwndNext) {
		out = append(out, child{hwnd: h, class: className(h), text: windowText(h), r: rectIn(dlg, h)})
	}
	return out
}

func firstOf(cs []child, class string, mustBeVisible bool) (child, bool) {
	for _, c := range cs {
		if strings.EqualFold(c.class, class) && (!mustBeVisible || visible(c.hwnd)) {
			return c, true
		}
	}
	return child{}, false
}

// measure is text's size in the font control h draws with, wrapped at width when the
// flags say so; this is the test's own measurement, outside the patch.
func measure(h uintptr, text string, width int32, flags uintptr) (int32, int32) {
	if text == "" {
		return 0, 0
	}
	dc := call(procGetDC, h)
	defer call(procReleaseDC, h, dc)
	old := call(procSelectObject, dc, send(h, wmGetFont, 0, 0))
	defer call(procSelectObject, dc, old)
	r := rect{Right: width}
	call(procDrawTextW, dc, uintptr(unsafe.Pointer(ptrTo(text))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags|dtCalcRect)
	return r.w(), r.h()
}

type monitorInfo struct {
	size          uint32
	monitor, work rect
	flags         uint32
}

func workArea(h uintptr) rect {
	mi := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	call(procGetMonitorInfoW, call(procMonitorFromWindow, h, monitorNearest), uintptr(unsafe.Pointer(&mi)))
	return mi.work
}

type scrollBarInfo struct {
	size                                        uint32
	bar                                         rect
	lineButton, thumbTop, thumbBottom, reserved int32
	state                                       [6]uint32
}

func hbarShown(h uintptr) bool {
	sbi := scrollBarInfo{size: uint32(unsafe.Sizeof(scrollBarInfo{}))}
	if call(procGetScrollBarInfo, h, objidHScroll, uintptr(unsafe.Pointer(&sbi))) == 0 {
		return false
	}
	return sbi.state[0]&stateInvisible == 0
}

type guiThreadInfo struct {
	size, flags                                        uint32
	active, focus, capture, menuOwner, moveSize, caret uintptr
	caretRect                                          rect
}

// focusOf is the window that has the keyboard focus on dlg's thread.
func focusOf(dlg uintptr) uintptr {
	tid := call(procGetWindowThreadProcess, dlg, 0)
	gti := guiThreadInfo{size: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	call(procGetGUIThreadInfo, tid, uintptr(unsafe.Pointer(&gti)))
	return gti.focus
}

func gdiObjects() uintptr {
	return call(procGetGuiResources, uintptr(windows.CurrentProcess()), grGDIObjects)
}

// steadyGDIObjects is the process's GDI object count once Windows' short-lived drawing
// objects are gone: the least of five samples 100 ms apart (the second lab run read 18,
// then 20, then 18 again with no leak).
func steadyGDIObjects() uintptr {
	least := gdiObjects()
	for i := 0; i < 4; i++ {
		time.Sleep(100 * time.Millisecond)
		if n := gdiObjects(); n < least {
			least = n
		}
	}
	return least
}

// capture is the dialog's client area as it is drawn, 32-bit pixels, top row first.
func capture(dlg uintptr) ([]uint32, int32, int32) {
	cr := clientRect(dlg)
	w, h := cr.w(), cr.h()
	screen := call(procGetDC, 0)
	defer call(procReleaseDC, 0, screen)
	mem := call(procCreateCompatibleDC, screen)
	defer call(procDeleteDC, mem)
	bmp := call(procCreateCompatibleBitmap, screen, uintptr(w), uintptr(h))
	defer call(procDeleteObject, bmp)
	old := call(procSelectObject, mem, bmp)
	call(procPrintWindow, dlg, mem, pwClientOnly|pwRenderFull)
	call(procSelectObject, mem, old)
	var bi struct {
		size                   uint32
		width, height          int32
		planes, bitCount       uint16
		compression, sizeImage uint32
		xPels, yPels           int32
		clrUsed, clrImportant  uint32
	}
	bi.size = uint32(unsafe.Sizeof(bi))
	bi.width, bi.height, bi.planes, bi.bitCount, bi.compression = w, -h, 1, 32, biRGB
	px := make([]uint32, int(w)*int(h))
	if len(px) == 0 {
		return nil, 0, 0
	}
	call(procGetDIBits, mem, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&px[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	return px, w, h
}

// inkRows reports, for each row of r in the captured pixels, whether it holds a pixel
// unlike r's background (the colour of its top-left corner), and whether r's rightmost
// column holds one.
func inkRows(px []uint32, pw int32, r rect) (rows []bool, rightEdge bool) {
	if len(px) == 0 || r.w() <= 0 || r.h() <= 0 {
		return nil, false
	}
	bg := px[int(r.Top)*int(pw)+int(r.Left)] & 0xffffff
	rows = make([]bool, r.h())
	for y := r.Top; y < r.Bottom; y++ {
		for x := r.Left; x < r.Right; x++ {
			if px[int(y)*int(pw)+int(x)]&0xffffff != bg {
				rows[y-r.Top] = true
				if x == r.Right-1 {
					rightEdge = true
				}
			}
		}
	}
	return rows, rightEdge
}

// checkFits measures an open dialog from outside: every visible control inside the
// client area, none overlapping another; the window inside its work area within the cap;
// every label's wrapped text inside it with ink on its last line and none at its right
// edge; every list row inside the list or covered by its sideways extent, with the bar
// shown exactly then; every button label inside its button; a scrolling prompt reaching
// its last line; an edit box showing its whole text when it fits.
func checkFits(t *testing.T, name string, dlg uintptr) {
	t.Helper()
	cr := clientRect(dlg)
	wr := windowRect(dlg)
	work := workArea(dlg)
	if wr.Left < work.Left || wr.Top < work.Top || wr.Right > work.Right || wr.Bottom > work.Bottom {
		t.Errorf("%s: window %+v is not inside the work area %+v", name, wr, work)
	}
	if wr.w() > work.w()*8/10 || wr.h() > work.h()*9/10 {
		t.Errorf("%s: window %dx%d is past the cap of a %dx%d work area", name, wr.w(), wr.h(), work.w(), work.h())
	}
	cs := children(dlg)
	var shown []child
	for _, c := range cs {
		if visible(c.hwnd) {
			shown = append(shown, c)
		}
	}
	for i, c := range shown {
		if c.r.Left < 0 || c.r.Top < 0 || c.r.Right > cr.Right || c.r.Bottom > cr.Bottom {
			t.Errorf("%s: %s %q at %+v is not inside the client area %+v", name, c.class, short(c.text), c.r, cr)
		}
		for _, o := range shown[i+1:] {
			if c.r.Left < o.r.Right && o.r.Left < c.r.Right && c.r.Top < o.r.Bottom && o.r.Top < c.r.Bottom {
				t.Errorf("%s: %s %+v and %s %+v overlap", name, c.class, c.r, o.class, o.r)
			}
		}
	}
	px, pw, _ := capture(dlg)
	for _, c := range shown {
		ccr := clientRect(c.hwnd)
		switch strings.ToUpper(c.class) {
		case "STATIC":
			if c.text == "" {
				continue
			}
			w, h := measure(c.hwnd, c.text, ccr.w(), dtWordBreak|dtEditControl|dtNoPrefix|dtExpandTabs)
			if w > ccr.w() || h > ccr.h() {
				t.Errorf("%s: label %q needs %dx%d, has %dx%d", name, short(c.text), w, h, ccr.w(), ccr.h())
			}
			_, lineH := measure(c.hwnd, "Ag", 0, dtSingleLine)
			rows, right := inkRows(px, pw, c.r)
			if h > 0 && lineH > 0 && len(rows) >= int(h) {
				band := false
				for y := h - lineH; y < h; y++ {
					band = band || rows[y]
				}
				if !band {
					t.Errorf("%s: label %q has no ink on its last line (rows %d to %d of %d)", name, short(c.text), h-lineH, h, ccr.h())
				}
			}
			// A line as wide as the label may touch its last column legitimately; a label
			// whose text measures narrower must not.
			if right && w < ccr.w()-2 {
				t.Errorf("%s: label %q has ink at its right edge although its text measures %d of %d: it runs out of it", name, short(c.text), w, ccr.w())
			}
		case "BUTTON":
			w, _ := measure(c.hwnd, c.text, 0, dtSingleLine)
			if w+2*4 > ccr.w() {
				t.Errorf("%s: button label %q needs %d, the button is %d wide", name, c.text, w+8, ccr.w())
			}
		case "LISTBOX":
			extent := int32(send(c.hwnd, lbGetHorizontalExtent, 0, 0))
			n := int(send(c.hwnd, lbGetCount, 0, 0))
			wider := false
			for i := 0; i < n; i++ {
				l := send(c.hwnd, lbGetTextLen, uintptr(i), 0)
				buf := make([]uint16, l+1)
				send(c.hwnd, lbGetText, uintptr(i), uintptr(unsafe.Pointer(&buf[0])))
				row := windows.UTF16ToString(buf)
				w, _ := measure(c.hwnd, row, 0, dtSingleLine|dtNoPrefix)
				if w+4 > ccr.w() {
					wider = true
					if extent < w {
						t.Errorf("%s: row %q is %d wide; the list is %d and its extent %d", name, short(row), w, ccr.w(), extent)
					}
				}
			}
			if wider != hbarShown(c.hwnd) {
				t.Errorf("%s: a row wider than the list: %v; the sideways bar shown: %v", name, wider, hbarShown(c.hwnd))
			}
		case "EDIT":
			if lines := send(c.hwnd, emGetLineCount, 0, 0); lines > 1 {
				send(c.hwnd, wmVScroll, sbBottom, 0)
				first := send(c.hwnd, emGetFirstVisibleLine, 0, 0)
				_, lineH := measure(c.hwnd, "Ag", 0, dtSingleLine)
				if lineH > 0 && int32(first)+ccr.h()/lineH < int32(lines) {
					t.Errorf("%s: the scrolling prompt shows lines %d to %d of %d after a scroll to its end", name, first, int32(first)+ccr.h()/lineH, lines)
				}
			} else if c.text != "" {
				n := len(utf16.Encode([]rune(c.text)))
				for _, i := range []int{0, n - 1} {
					p := send(c.hwnd, emPosFromChar, uintptr(i), 0)
					if x := int16(p & 0xffff); x < 0 || int32(x) >= ccr.w() {
						t.Errorf("%s: the edit box's character %d is at x=%d, outside its %d pixels", name, i, x, ccr.w())
					}
				}
			}
		}
	}
}

func short(s string) string {
	if r := []rune(s); len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}

// opened is a dialog opened on its own goroutine, found by its unique title.
type opened struct {
	hwnd   uintptr
	done   chan string
	err    chan error
	cancel context.CancelFunc
}

func open(t *testing.T, kind string, run func(ctx context.Context, title string) (string, error)) opened {
	t.Helper()
	title := fmt.Sprintf("MA-1470 %s %d", kind, time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	o := opened{done: make(chan string, 1), err: make(chan error, 1), cancel: cancel}
	go func() {
		out, err := run(ctx, title)
		o.done <- out
		o.err <- err
	}()
	end := time.Now().Add(labDialogWaitLimit)
	for o.hwnd == 0 && time.Now().Before(end) {
		o.hwnd = call(procFindWindowW, 0, uintptr(unsafe.Pointer(ptrTo(title))))
		time.Sleep(20 * time.Millisecond)
	}
	if o.hwnd == 0 {
		cancel()
		t.Fatalf("%s: the dialog %q did not appear within %v", kind, title, labDialogWaitLimit)
	}
	time.Sleep(150 * time.Millisecond) // let it draw
	return o
}

// result waits for the dialog's answer.
func (o opened) result(t *testing.T, kind string) (string, error) {
	t.Helper()
	defer o.cancel()
	select {
	case out := <-o.done:
		return out, <-o.err
	case <-time.After(15 * time.Second):
		t.Fatalf("%s: no answer within 15 s (a hang)", kind)
		return "", nil
	}
}

func (o opened) close(t *testing.T, kind string) {
	t.Helper()
	post(o.hwnd, wmClose, 0, 0)
	if _, err := o.result(t, kind); err != nil && !errors.Is(err, zenity.ErrCanceled) {
		t.Errorf("%s: closing returned %v, want a cancel", kind, err)
	}
}

// labSkip is why MA-1470 skips outside the lab.
const labSkip = "desktop lab: set " + labEnv + "=1 in a signed-in, unlocked Windows session (the office test PC); it opens real dialogs"

func labOn() bool { return os.Getenv(labEnv) == "1" }

// perMonitorAware keeps the test's goroutine on one thread that is per-monitor DPI aware,
// as the dialogs' own threads are, so it reads every size in physical pixels: a thread
// that is not aware gets them scaled down (the first lab run read a 362-pixel label as
// 241 at 150 %). Each subtest runs on its own goroutine and calls it again.
func perMonitorAware(t *testing.T) {
	t.Helper()
	runtime.LockOSThread()
	old := call(procSetThreadDpiAwareness, dpiPerMonitorV2)
	if old == 0 {
		runtime.UnlockOSThread()
		t.Fatal("cannot make the measuring thread per-monitor DPI aware")
	}
	t.Cleanup(func() {
		call(procSetThreadDpiAwareness, old)
		runtime.UnlockOSThread()
	})
}

// The texts the lab dialogs carry: the walk's rows (MA-1467), and the hostile and long
// ones the design names (§5).
var (
	walkRows = []string{
		"All of them ➤ 4 mailboxes, 31 GB",
		"qzsam.walker@gmail.com (13 GB, last changed 2026-11-02)",
		"qzsam.office@qzcompany.example (2.1 GB, last changed 2026-11-14)",
		"firstname.lastname@mail.qzuniversity.example (640 MB, last changed 2026-10-30)",
		"Older copy: qzsam.walker@gmail.com (1.2 GB, last changed 2025-03-02, profile qz98zy76.default)",
		"Not in Thunderbird now: imap.gmail-3.com (12 MB, last changed 2025-03-02)",
		"Local Folders (3.4 MB, last changed 2026-11-01)",
		"qz.second.walker@gmail.com (4.8 GB, last changed 2026-11-17)",
		"qzsam@qzfamily.example (900 MB, last changed 2026-09-12)",
		"Outlook: qzsam.walker@outlook.example (7.7 GB, last changed 2026-11-10)",
	}
	hostileName = strings.Repeat("Ω", 20) + strings.Repeat("w", 40) + "‮שלום‬\u200b" + strings.Repeat("m", 15)
	longRow     = strings.Repeat("A long row that goes past the cap ", 12)
	longPath    = `C:\Users\qzsam\AppData\Roaming\Thunderbird\Profiles\qz12ab34.default-release\ImapMail\imap.gmail-1.com\INBOX.sbd\Archive`
	longWord    = strings.Repeat("unbreakable", 28)
	healthView  = func() string {
		var b strings.Builder
		b.WriteString("Backup health — WARN\n\nPosture: WARN\n")
		for i := 0; i < 6; i++ {
			fmt.Fprintf(&b, "Reason %d: the scheduled backup of %s did not finish; open MailArchive Desktop to see why and run it again.\n", i+1, longPath)
		}
		for i := 0; i < 20; i++ {
			fmt.Fprintf(&b, "Incomplete: folder %d of %s holds messages Thunderbird has not downloaded yet — see %s\\report.html\n", i+1, longPath, longPath)
		}
		return b.String()
	}()
	link = "http://127.0.0.1:53817/_open?t=" + strings.Repeat("Q", 43)
)

// covers: MA-1470, S86
// Lists: the walk's rows, a hostile name, a row past the cap and the health view at its
// longest each fit (checkFits); then the same dialogs at another DPI (a WM_DPICHANGED
// sent to each) fit again.
func TestMA1470ListsFitTheirWords(t *testing.T) {
	if !labOn() {
		t.Skip(labSkip)
	}
	perMonitorAware(t)
	cases := []struct {
		name, prompt string
		rows         []string
	}{
		{"the walk's rows", "Which mailbox do you want to export?", walkRows},
		{"a hostile name", "Which mailbox do you want to export?", append([]string{hostileName}, walkRows[:3]...)},
		{"a row past the cap", "Pick one:", []string{longRow, "short"}},
		{"ten rows that fit", "Pick one:", strings.Split("one two three four five six seven eight nine ten eleven", " ")},
		{"a long path in the prompt", "The archive is kept in " + longPath + ". Continue?", []string{"Continue", "Stop"}},
		{"a word past the cap", "Copy this: " + longWord, []string{"Done"}},
		{"the health view at its longest", healthView, []string{"Continue", "Rebuild the index", "Remove the schedule"}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			perMonitorAware(t)
			o := open(t, "list", func(ctx context.Context, title string) (string, error) {
				return zenity.List(c.prompt, c.rows, zenity.Title(title), zenity.Context(ctx))
			})
			checkFits(t, c.name, o.hwnd)
			r := windowRect(o.hwnd)
			send(o.hwnd, wmDPIChanged, 192<<16|192, uintptr(unsafe.Pointer(&r)))
			time.Sleep(150 * time.Millisecond)
			checkFits(t, c.name+" at 192 DPI", o.hwnd)
			o.close(t, c.name)
		})
	}
}

// covers: MA-1470, S86
// Entries and progress dialogs fit their words: Desktop's link entry shows its whole
// link; a progress dialog grows as its text grows (Text from another goroutine), stays
// fitted, keeps its top-left corner unless the work area moves it, and never takes the
// keyboard from another window while it grows (R39).
func TestMA1470EntriesAndProgressFitTheirWords(t *testing.T) {
	if !labOn() {
		t.Skip(labSkip)
	}
	perMonitorAware(t)
	o := open(t, "entry", func(ctx context.Context, title string) (string, error) {
		return zenity.Entry("Your browser did not open. Copy this link into it:", zenity.EntryText(link), zenity.Title(title), zenity.Context(ctx))
	})
	checkFits(t, "the link entry", o.hwnd)
	o.close(t, "the link entry")

	title := fmt.Sprintf("MA-1470 progress %d", time.Now().UnixNano())
	dlg, err := zenity.Progress(zenity.Title(title), zenity.Pulsate())
	if err != nil {
		t.Fatal(err)
	}
	defer dlg.Close()
	ph := call(procFindWindowW, 0, uintptr(unsafe.Pointer(ptrTo(title))))
	if ph == 0 {
		t.Fatal("the progress dialog did not appear")
	}
	time.Sleep(150 * time.Millisecond)
	checkFits(t, "progress, opening", ph)
	before := windowRect(ph)

	// another window of this process takes the keyboard; any process may take the
	// foreground from now on, so a stray activation would succeed
	other := open(t, "entry", func(ctx context.Context, title string) (string, error) {
		return zenity.Entry("Another window", zenity.Title(title), zenity.Context(ctx))
	})
	call(procAllowSetForeground, asfwAny)
	call(procSetForegroundWindow, other.hwnd)
	time.Sleep(150 * time.Millisecond)
	fg := call(procGetForegroundWindow)
	if fg != other.hwnd {
		t.Errorf("precondition: the other dialog %x could not take the keyboard (the foreground is %x, %s %q), so the check below proves nothing: start the test from a process allowed to set the foreground",
			other.hwnd, fg, className(fg), short(windowText(fg)))
	}

	four := "Downloaded 12 MB so far (+3 MB).\nWaiting for the sync to finish…\n\nIn Thunderbird: right-click the account → Download / Sync Now."
	done := make(chan error, 1)
	go func() { done <- dlg.Text(four) }() // Text from another goroutine
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	checkFits(t, "progress, four lines", ph)
	after := windowRect(ph)
	if after.h() <= before.h() || after.Left != before.Left || after.Top != before.Top {
		t.Errorf("progress grew from %+v to %+v; want it taller with the same top-left corner", before, after)
	}
	if now := call(procGetForegroundWindow); now != fg {
		t.Errorf("the growing progress dialog took the foreground from %x (now %x)", fg, now)
	}
	if err := dlg.Text("Done."); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if again := windowRect(ph); again.h() < after.h() {
		t.Errorf("a shorter text shrank the progress dialog from %+v to %+v", after, again)
	}
	other.close(t, "the other window")
}

// covers: MA-1470, S86
// The same answers as v0.10.12 (P2): Enter on the default row returns it; Down then
// Enter returns the next; OK is greyed under DisallowEmpty with nothing chosen; the
// arrows still move the selection while the sideways bar shows; after a click in the
// scrolling prompt the list has the keyboard and Enter returns the default row; the
// close box cancels.
func TestMA1470TheAnswersAreTheSame(t *testing.T) {
	if !labOn() {
		t.Skip(labSkip)
	}
	perMonitorAware(t)
	rows := []string{"first", "second", "third"}
	listOf := func(t *testing.T, name, prompt string, rows []string, opts ...zenity.Option) opened {
		return open(t, name, func(ctx context.Context, title string) (string, error) {
			return zenity.List(prompt, rows, append(opts, zenity.Title(title), zenity.Context(ctx))...)
		})
	}
	listCtl := func(t *testing.T, o opened) uintptr {
		l, ok := firstOf(children(o.hwnd), "LISTBOX", true)
		if !ok {
			t.Fatal("no list in the dialog")
		}
		return l.hwnd
	}

	o := listOf(t, "enter", "Pick:", rows, zenity.DefaultItems("second"))
	post(listCtl(t, o), wmKeyDown, vkReturn, 0)
	if out, err := o.result(t, "enter"); out != "second" || err != nil {
		t.Errorf("Enter on the default row: %q, %v; want second", out, err)
	}

	o = listOf(t, "down", "Pick:", rows, zenity.DefaultItems("second"))
	l := listCtl(t, o)
	post(l, wmKeyDown, vkDown, 0)
	post(l, wmKeyDown, vkReturn, 0)
	if out, err := o.result(t, "down"); out != "third" || err != nil {
		t.Errorf("Down then Enter: %q, %v; want third", out, err)
	}

	o = listOf(t, "empty", "Pick:", rows, zenity.DisallowEmpty())
	for _, c := range children(o.hwnd) {
		if strings.EqualFold(c.class, "BUTTON") && c.text == "OK" && enabled(c.hwnd) {
			t.Error("DisallowEmpty with nothing chosen: OK is not greyed")
		}
	}
	o.close(t, "empty")

	o = listOf(t, "sideways", "Pick:", []string{longRow, "short", "other"}, zenity.DefaultItems(longRow))
	l = listCtl(t, o)
	if !hbarShown(l) {
		t.Error("a row past the cap shows no sideways bar")
	}
	post(l, wmKeyDown, vkDown, 0)
	time.Sleep(100 * time.Millisecond)
	if sel := send(l, lbGetCurSel, 0, 0); sel != 1 {
		t.Errorf("Down with the sideways bar shown selected row %d; want 1", sel)
	}
	o.close(t, "sideways")

	o = listOf(t, "click in the prompt", healthView, []string{"Continue", "Remove"}, zenity.DefaultItems("Continue"))
	cs := children(o.hwnd)
	prompt, ok := firstOf(cs, "EDIT", true)
	if !ok {
		t.Fatal("the health view's prompt does not scroll")
	}
	post(prompt.hwnd, wmLButtonDown, mkLButton, 5<<16|5)
	post(prompt.hwnd, wmLButtonUp, 0, 5<<16|5)
	time.Sleep(150 * time.Millisecond)
	l = listCtl(t, o)
	if f := focusOf(o.hwnd); f != l {
		t.Errorf("after a click in the scrolling prompt the keyboard is on %x (%s), not the list", f, className(f))
	}
	post(l, wmKeyDown, vkReturn, 0)
	if out, err := o.result(t, "click in the prompt"); out != "Continue" || err != nil {
		t.Errorf("Enter after a click in the prompt: %q, %v; want Continue", out, err)
	}
}

// covers: MA-1470, S86
// No GDI object is lost (the measuring releases every DC and font): after a warm-up, a
// thousand Text calls and ten openings and closings of each dialog kind leave the
// process's GDI object count where it was.
func TestMA1470MeasuringLosesNoGDIObject(t *testing.T) {
	if !labOn() {
		t.Skip(labSkip)
	}
	perMonitorAware(t)
	openClose := func() {
		o := open(t, "list", func(ctx context.Context, title string) (string, error) {
			return zenity.List("Which mailbox?", walkRows, zenity.Title(title), zenity.Context(ctx))
		})
		o.close(t, "list")
		o = open(t, "entry", func(ctx context.Context, title string) (string, error) {
			return zenity.Entry("Copy this link:", zenity.EntryText(link), zenity.Title(title), zenity.Context(ctx))
		})
		o.close(t, "entry")
	}
	openClose() // warm-up
	start := steadyGDIObjects()
	for i := 0; i < 10; i++ {
		openClose()
	}
	if after := steadyGDIObjects(); after > start {
		t.Errorf("ten openings of each dialog kind: GDI objects %d, then %d", start, after)
	}

	dlg, err := zenity.Progress(zenity.Title("MA-1470 GDI"), zenity.Pulsate())
	if err != nil {
		t.Fatal(err)
	}
	defer dlg.Close()
	// the warm-up grows the dialog to the loop's text, so the loop measures without growing
	for i := 0; i < 10; i++ {
		dlg.Text(fmt.Sprintf("Downloaded %d MB so far.\nWaiting for the sync to finish…", 1000+i))
	}
	start = steadyGDIObjects()
	for i := 0; i < 1000; i++ {
		dlg.Text(fmt.Sprintf("Downloaded %d MB so far.\nWaiting for the sync to finish…", i))
	}
	mid := steadyGDIObjects()
	for i := 0; i < 1000; i++ {
		dlg.Text(fmt.Sprintf("Downloaded %d MB so far.\nWaiting for the sync to finish…", 2000+i))
	}
	if after := steadyGDIObjects(); after > start || mid > start {
		t.Errorf("Text calls: GDI objects %d, then %d after 1,000 and %d after 2,000", start, mid, after)
	}
}
