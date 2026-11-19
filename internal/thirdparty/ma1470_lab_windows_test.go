//go:build windows

package thirdparty

// MA-1470: the Windows lab test of the patched dialogs in the copy of zenity
// (docs/design-windows-dialog-fit.md §5). It opens real dialogs, so it runs only with
// MAILARCHIVE_DESKTOP_LAB=1 in a signed-in, unlocked Windows session (the office test
// PC). It measures each dialog from outside with Win32, with the font each control
// draws with: every row, prompt line and button label must fit, every control must lie
// inside its dialog without overlapping another, the dialog must lie inside its work
// area within the cap, the ink of each label's last line must be on screen, and the
// answers must be the ones v0.10.12 gives. When every MA-1470 test passes in a program
// built with the copy's lab-copy hash, the run writes its receipt (UPSTREAM.md, Running
// MA-1470); with MAILARCHIVE_LAB_SHOTS set it also saves screenshots for the friction
// review.

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/ncruces/zenity"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
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
	procSetWindowPos           = labUser32.NewProc("SetWindowPos")
	procGetDpiForWindow        = labUser32.NewProc("GetDpiForWindow")
	procGetGuiResources        = labUser32.NewProc("GetGuiResources")
	procSetThreadDpiAwareness  = labUser32.NewProc("SetThreadDpiAwarenessContext")
	procGetSystemMetrics       = labUser32.NewProc("GetSystemMetrics")
	procGetDpiForSystem        = labUser32.NewProc("GetDpiForSystem")
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
	bmClick       = 0x00f5
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
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
	smCXScreen      = 0
	smCYScreen      = 1

	labShotsEnv = "MAILARCHIVE_LAB_SHOTS"
)

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) w() int32 { return r.Right - r.Left }
func (r rect) h() int32 { return r.Bottom - r.Top }

// call, send and post take pointers converted to uintptr in their argument lists, as
// Windows calls do: //go:uintptrescapes keeps what each points to on the heap and alive
// until the call returns, as LazyProc.Call itself does (adversarial finding OUT-07).
//
//go:uintptrescapes
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
	r, _ := readWindowRect(h)
	return r
}

// readWindowRect reads h's rectangle on the screen, once more after 50 ms if the first
// read fails (one lab run read a button's as all zeros once, and not again).
func readWindowRect(h uintptr) (rect, bool) {
	var r rect
	for try := 0; try < 2; try++ {
		if call(procGetWindowRect, h, uintptr(unsafe.Pointer(&r))) != 0 {
			return r, true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return rect{}, false
}

func clientRect(h uintptr) rect {
	var r rect
	call(procGetClientRect, h, uintptr(unsafe.Pointer(&r)))
	return r
}

// rectIn is a child's rectangle in its parent's client coordinates; ok is false when
// it cannot be read.
func rectIn(parent, h uintptr) (rect, bool) {
	r, ok := readWindowRect(h)
	if !ok {
		return rect{}, false
	}
	call(procMapWindowPoints, 0, parent, uintptr(unsafe.Pointer(&r)), 2)
	return r, true
}

func visible(h uintptr) bool { return call(procIsWindowVisible, h) != 0 }
func enabled(h uintptr) bool { return call(procIsWindowEnabled, h) != 0 }

//go:uintptrescapes
func send(h, msg, w, l uintptr) uintptr { return call(procSendMessageW, h, msg, w, l) }

//go:uintptrescapes
func post(h, msg, w, l uintptr) { call(procPostMessageW, h, msg, w, l) }

// child is one control of a dialog.
type child struct {
	hwnd  uintptr
	class string
	text  string
	r     rect // in the dialog's client coordinates
	read  bool // the rectangle could be read
}

func children(dlg uintptr) []child {
	var out []child
	for h := call(procGetWindow, dlg, gwChild); h != 0; h = call(procGetWindow, h, gwHwndNext) {
		r, ok := rectIn(dlg, h)
		out = append(out, child{hwnd: h, class: className(h), text: windowText(h), r: r, read: ok})
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
	return printWindow(dlg, cr.w(), cr.h(), pwClientOnly|pwRenderFull)
}

// printWindow is window h as it is drawn (flags say which part, w by h pixels), 32-bit
// pixels, top row first.
func printWindow(dlg uintptr, w, h int32, flags uintptr) ([]uint32, int32, int32) {
	screen := call(procGetDC, 0)
	defer call(procReleaseDC, 0, screen)
	mem := call(procCreateCompatibleDC, screen)
	defer call(procDeleteDC, mem)
	bmp := call(procCreateCompatibleBitmap, screen, uintptr(w), uintptr(h))
	defer call(procDeleteObject, bmp)
	old := call(procSelectObject, mem, bmp)
	call(procPrintWindow, dlg, mem, flags)
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
		if !c.read {
			t.Errorf("%s: cannot read the rectangle of %s %q (twice, 50 ms apart)", name, c.class, short(c.text))
			continue
		}
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
	// The window exists from CreateWindowEx on, before its layout: wait until it is shown
	// with a client area (the first dialog of a run was once read too early), then let it
	// draw.
	for !(visible(o.hwnd) && clientRect(o.hwnd).w() > 0) && time.Now().Before(end) {
		time.Sleep(20 * time.Millisecond)
	}
	if !visible(o.hwnd) || clientRect(o.hwnd).w() <= 0 {
		cancel()
		t.Fatalf("%s: the dialog %q was not shown with a client area within %v", kind, title, labDialogWaitLimit)
	}
	time.Sleep(150 * time.Millisecond)
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

// labCopyHash is the lab-copy hash of the copy this program was built from, set by the lab
// build with -ldflags -X (UPSTREAM.md, Running MA-1470); empty in any other build.
var labCopyHash string

// labTests are MA-1470's tests. When every one passes in a run of a program built with
// labCopyHash, the run writes its receipt, which the pins' recorder requires
// (adversarial finding INS-03).
var labTests = []string{"TestMA1470ListsFitTheirWords", "TestMA1470EntriesAndProgressFitTheirWords",
	"TestMA1470TheAnswersAreTheSame", "TestMA1470MeasuringLosesNoGDIObject", "TestMA1470MessageBoxesFitTheirLabels"}

var (
	labMu     sync.Mutex
	labPassed = map[string]bool{}
)

// labDone records, when t ends, whether it ran to the end and passed.
func labDone(t *testing.T) {
	t.Cleanup(func() {
		if !t.Failed() && !t.Skipped() {
			labMu.Lock()
			labPassed[t.Name()] = true
			labMu.Unlock()
		}
	})
}

func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 && labOn() {
		fmt.Println(writeLabReceipt())
	}
	os.Exit(code)
}

// writeLabReceipt writes ma1470-receipt.txt beside the test program when every MA-1470
// test passed in this run and the program carries its lab-copy hash, and says what it did.
func writeLabReceipt() string {
	var missing []string
	for _, name := range labTests {
		if !labPassed[name] {
			missing = append(missing, name)
		}
	}
	switch {
	case len(missing) > 0:
		return "MA-1470: no receipt: these lab tests did not run to a pass: " + strings.Join(missing, ", ")
	case labCopyHash == "":
		return "MA-1470: every lab test passed, but this program carries no lab-copy hash: build it as UPSTREAM.md says (Running MA-1470) for a receipt"
	}
	exe, err := os.Executable()
	if err != nil {
		return "MA-1470: no receipt: " + err.Error()
	}
	run := fmt.Sprintf("MA-1470 %s %s, %s pass", time.Now().UTC().Format("2006-01-02"), labWindows(), labScreen())
	body := "# MA-1470's receipt, written by the lab test after every MA-1470 test passed (UPSTREAM.md)\n" +
		"run: " + run + "\ncopy: " + labCopyHash + "\n"
	out := filepath.Join(filepath.Dir(exe), "ma1470-receipt.txt")
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		return "MA-1470: no receipt: " + err.Error()
	}
	return "MA-1470: receipt written to " + out + ": " + run
}

// labWindows names the Windows the run used, as its registry records it.
func labWindows() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		v := windows.RtlGetVersion()
		return fmt.Sprintf("Windows %d.%d build %d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	}
	defer k.Close()
	edition, _, _ := k.GetStringValue("EditionID")
	display, _, _ := k.GetStringValue("DisplayVersion")
	build, _, _ := k.GetStringValue("CurrentBuild")
	ubr, _, _ := k.GetIntegerValue("UBR")
	product := "Windows 10"
	if n, _ := strconv.Atoi(build); n >= 22000 {
		product = "Windows 11"
	}
	return fmt.Sprintf("%s %s %s build %s.%d", product, edition, display, build, ubr)
}

// labScreen is the primary screen's size in pixels and its scaling, read on a thread that
// is per-monitor DPI aware.
func labScreen() string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if old := call(procSetThreadDpiAwareness, dpiPerMonitorV2); old != 0 {
		defer call(procSetThreadDpiAwareness, old)
	}
	w, h := call(procGetSystemMetrics, smCXScreen), call(procGetSystemMetrics, smCYScreen)
	return fmt.Sprintf("%dx%d at %d %%", w, h, call(procGetDpiForSystem)*100/96)
}

// saveShot writes window h, as it is drawn, frame included, to name.png in the folder
// MAILARCHIVE_LAB_SHOTS names.
func saveShot(t *testing.T, name string, h uintptr) {
	t.Helper()
	wr := windowRect(h)
	px, w, ht := printWindow(h, wr.w(), wr.h(), pwRenderFull)
	if len(px) == 0 {
		t.Fatalf("%s: nothing drawn to save", name)
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(ht)))
	for i, p := range px {
		img.Pix[4*i], img.Pix[4*i+1], img.Pix[4*i+2], img.Pix[4*i+3] = byte(p>>16), byte(p>>8), byte(p), 0xff
	}
	f, err := os.Create(filepath.Join(os.Getenv(labShotsEnv), name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
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
	// typicalHealthView is a typical warning health view, as internal/health's Summary
	// wrote it on 2026-11-19 for a daily backup whose scheduled task is gone and last ran
	// three days before (the friction review's F2).
	typicalHealthView = "Backup health — WARN\n\nArchive:    C:\\Users\\Sam\\Documents\\MailArchive\\Thunderbird mail\nMessages:   48213 in manifest · 48213 indexed\nIncomplete: none\nFixity coverage: 0 of 48213 records recorded (run `mailarchive verify -out \"C:\\Users\\Sam\\Documents\\MailArchive\\Thunderbird mail\"` to check the bytes) · run `mailarchive verify -record -out \"C:\\Users\\Sam\\Documents\\MailArchive\\Thunderbird mail\"` to baseline the rest\nHistory:    none recorded — no go-back timeline (a live capture writes it; a one-shot local import has none)\nLast run:   2026-11-16 01:00 EST → ok · exported 0 · filled 0 · took 1h0m0s\nSchedule:   \"mailarchive-1234abcd\" not installed · daily at 02:00 · runs C:\\Program Files\\MailArchive\\mailarchivew.exe · installed 2026-10-02 (UTC) on SAM-LAPTOP\nProgram:    this program\nscheduled:  export\nPosture:    WARN\n  WARN: the last run was 3 days ago, more than twice the daily schedule period — is the machine on and logged in at 02:00?\n  WARN: the schedule \"mailarchive-1234abcd\" is recorded but not present in this host's scheduler — re-run `mailarchive schedule -out \"C:\\Users\\Sam\\Documents\\MailArchive\\Thunderbird mail\" -install`"
	// healthChoices are the health view's choices in the wizard (internal/wizard/wizard.go).
	healthChoices = []string{"Continue to the wizard", "Repair the scheduled backup", "Remove the scheduled backup"}
)

// covers: MA-1470, S86
// Lists: the walk's rows, a hostile name, a row past the cap and the health view at its
// longest each fit (checkFits); then the same dialogs at another DPI (a WM_DPICHANGED
// sent to each) fit again.
func TestMA1470ListsFitTheirWords(t *testing.T) {
	if !labOn() {
		t.Skip(labSkip)
	}
	labDone(t)
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
		{"a typical warning health view", typicalHealthView, healthChoices},
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
			suggested := rect{Left: r.Left + 24, Top: r.Top + 16, Right: r.Right + 24, Bottom: r.Bottom + 16}
			send(o.hwnd, wmDPIChanged, 192<<16|192, uintptr(unsafe.Pointer(&suggested)))
			time.Sleep(150 * time.Millisecond)
			checkFits(t, c.name+" at 192 DPI", o.hwnd)
			// It goes to the corner Windows suggests, unless that would leave the work area
			// (checkFits holds it inside); which monitor's work area cannot be told apart on
			// a one-screen PC.
			got, work := windowRect(o.hwnd), workArea(o.hwnd)
			if suggested.Left+got.w() <= work.Right && suggested.Top+got.h() <= work.Bottom &&
				(got.Left != suggested.Left || got.Top != suggested.Top) {
				t.Errorf("%s: after a DPI change the dialog is at %+v; want the corner Windows suggested, %d,%d", c.name, got, suggested.Left, suggested.Top)
			}
			o.close(t, c.name)
		})
	}
}

// covers: MA-1470, S86
// Entries and progress dialogs fit their words: Desktop's link entry shows its whole
// link; a progress dialog grows as its text grows (Text from another goroutine), stays
// fitted, keeps its top-left corner unless the work area moves it, shrinks neither for a
// word wider than 400 (which Windows breaks inside the label) nor for a shorter text, and
// never takes the keyboard from another window while it grows (R39).
func TestMA1470EntriesAndProgressFitTheirWords(t *testing.T) {
	if !labOn() {
		t.Skip(labSkip)
	}
	labDone(t)
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
	// A word wider than 400: Windows breaks it inside the label, as an edit control does
	// (DT_EDITCONTROL and SS_EDITCONTROL; the 2026-11-19 lab run showed the dialog keeps
	// its width), so the label must hold it whole and the dialog must not shrink.
	if err := dlg.Text("Copying " + strings.Repeat("w", 70)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	checkFits(t, "progress, a word wider than 400", ph)
	wide := windowRect(ph)
	if wide.w() < after.w() || wide.h() < after.h() {
		t.Errorf("a word wider than 400 shrank the progress dialog from %+v to %+v", after, wide)
	}
	if err := dlg.Text("Done."); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if again := windowRect(ph); again.h() < wide.h() || again.w() < wide.w() {
		t.Errorf("a shorter text shrank the progress dialog from %+v to %+v", wide, again)
	}
	if now := call(procGetForegroundWindow); now != fg {
		t.Errorf("the growing progress dialog took the foreground from %x (now %x)", fg, now)
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
	labDone(t)
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
	labDone(t)
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

// checkMessageBox measures an open message box from outside: every button's label inside
// its button and no button wider than the row needs (the widest label plus 12 on each
// side, or Windows' own 75, scaled: the first walk found them 1.5 times too wide), the
// buttons inside the client area and none over another, the box inside its work area
// within the cap. The box's own text is Windows' layout, outside P1.
func checkMessageBox(t *testing.T, name string, box uintptr) {
	t.Helper()
	cr, wr, work := clientRect(box), windowRect(box), workArea(box)
	if wr.Left < work.Left || wr.Top < work.Top || wr.Right > work.Right || wr.Bottom > work.Bottom {
		t.Errorf("%s: box %+v is not inside the work area %+v", name, wr, work)
	}
	if wr.w() > work.w()*8/10 {
		t.Errorf("%s: box %d wide is past the cap of a %d-wide work area", name, wr.w(), work.w())
	}
	var buttons []child
	for _, c := range children(box) {
		if strings.EqualFold(c.class, "BUTTON") && visible(c.hwnd) {
			buttons = append(buttons, c)
		}
	}
	if len(buttons) == 0 {
		t.Errorf("%s: no button found", name)
	}
	dpi := int32(call(procGetDpiForWindow, box))
	up := func(v int32) int32 { return (v*dpi + 95) / 96 }
	widest := up(75)
	for _, b := range buttons {
		if w, _ := measure(b.hwnd, b.text, 0, dtSingleLine); w+2*up(12) > widest {
			widest = w + 2*up(12)
		}
	}
	for _, b := range buttons {
		if bw := clientRect(b.hwnd).w(); bw > widest+2 {
			t.Errorf("%s: button %q is %d wide; the row needs %d at %d DPI", name, b.text, bw, widest, dpi)
		}
	}
	for i, b := range buttons {
		w, _ := measure(b.hwnd, b.text, 0, dtSingleLine)
		if bw := clientRect(b.hwnd).w(); w+2*4 > bw {
			t.Errorf("%s: button label %q needs %d, the button is %d wide", name, b.text, w+8, bw)
		}
		if b.r.Left < 0 || b.r.Top < 0 || b.r.Right > cr.Right || b.r.Bottom > cr.Bottom {
			t.Errorf("%s: button %q at %+v is not inside the client area %+v", name, b.text, b.r, cr)
		}
		for _, o := range buttons[i+1:] {
			if b.r.Left < o.r.Right && o.r.Left < b.r.Right && b.r.Top < o.r.Bottom && o.r.Top < b.r.Bottom {
				t.Errorf("%s: buttons %q %+v and %q %+v overlap", name, b.text, b.r, o.text, o.r)
			}
		}
	}
}

func question(t *testing.T, kind, text string, opts ...zenity.Option) opened {
	t.Helper()
	return open(t, kind, func(ctx context.Context, title string) (string, error) {
		all := append(append([]zenity.Option(nil), opts...), zenity.Title(title), zenity.Context(ctx))
		return "", zenity.Question(text, all...)
	})
}

func buttonOf(t *testing.T, box uintptr, label string) uintptr {
	t.Helper()
	for _, c := range children(box) {
		if strings.EqualFold(c.class, "BUTTON") && strings.ReplaceAll(c.text, "&&", "&") == label {
			return c.hwnd
		}
	}
	t.Fatalf("no button %q", label)
	return 0
}

// covers: MA-1470, S86
// Message boxes: the wizard's renamed buttons fit their labels (the backup summary's,
// the IMAP question's, the Outlook question's, and three buttons with an extra one); for
// a one-word text the box grew for them; each button still gives its own answer when
// clicked; after a DPI change (a WM_DPICHANGED sent to the box) buttons narrowed by the
// test are widened again. It records, without asserting, whether Windows cuts the text
// of an end-of-backup summary for several accounts (P1 leaves a message box's text to
// Windows).
func TestMA1470MessageBoxesFitTheirLabels(t *testing.T) {
	if !labOn() {
		t.Skip(labSkip)
	}
	labDone(t)
	perMonitorAware(t)

	plain := question(t, "plain", "Done.")
	plainW := windowRect(plain.hwnd).w()
	plain.close(t, "plain")
	long := question(t, "a one-word text", "Done.", zenity.OKLabel("Open the archive"), zenity.CancelLabel("Close"),
		zenity.ExtraButton("Export what's changed"))
	checkMessageBox(t, "a one-word text", long.hwnd)
	if w := windowRect(long.hwnd).w(); w <= plainW {
		t.Errorf("a one-word text with long labels: the box is %d wide, not wider than with OK and Cancel (%d)", w, plainW)
	}
	long.close(t, "a one-word text")

	for _, c := range []struct{ name, text, ok, cancel string }{
		{"the backup summary", "Backup finished: 1,204 messages saved in D:\\Mail archive.", "Open the archive", "Close"},
		{"the IMAP question", "Thunderbird keeps only part of this account's mail on this computer. Prepare a full offline download first?", "Yes, prepare", "No, export what's cached"},
		{"the Outlook question", "Outlook is installed. Export through the Outlook app, or read the files directly?", "Use the Outlook app", "Read the files directly"},
	} {
		o := question(t, c.name, c.text, zenity.OKLabel(c.ok), zenity.CancelLabel(c.cancel))
		checkMessageBox(t, c.name, o.hwnd)
		o.close(t, c.name)
	}

	for _, a := range []struct {
		label string
		want  error
	}{{"Open the archive", nil}, {"Export what's changed", zenity.ErrExtraButton}, {"Close", zenity.ErrCanceled}} {
		o := question(t, "click "+a.label, "Done.", zenity.OKLabel("Open the archive"), zenity.CancelLabel("Close"),
			zenity.ExtraButton("Export what's changed"))
		call(procSetForegroundWindow, o.hwnd)
		time.Sleep(100 * time.Millisecond)
		post(buttonOf(t, o.hwnd, a.label), bmClick, 0, 0)
		if _, err := o.result(t, "click "+a.label); !errors.Is(err, a.want) && !(err == nil && a.want == nil) {
			t.Errorf("a click on %q answered %v; want %v", a.label, err, a.want)
		}
	}

	o := question(t, "DPI", "Done.", zenity.OKLabel("Open the archive"), zenity.CancelLabel("Close"))
	for _, c := range children(o.hwnd) {
		if strings.EqualFold(c.class, "BUTTON") {
			call(procSetWindowPos, c.hwnd, 0, 0, 0, 30, uintptr(c.r.h()), swpNoMove|swpNoZOrder|swpNoActivate)
		}
	}
	r := windowRect(o.hwnd)
	send(o.hwnd, wmDPIChanged, 192<<16|192, uintptr(unsafe.Pointer(&r)))
	time.Sleep(200 * time.Millisecond)
	checkMessageBox(t, "after a DPI change", o.hwnd)
	o.close(t, "DPI")

	var summary strings.Builder
	summary.WriteString("Backup finished: 3,912 messages saved in D:\\Mail archive.\n\n")
	for _, acct := range []string{"qzsam.walker@gmail.com", "qzsam.office@qzcompany.example", "firstname.lastname@mail.qzuniversity.example"} {
		fmt.Fprintf(&summary, "The Thunderbird account %s: MailArchive cannot tell whether Thunderbird has downloaded all of its mail. "+
			"Open Thunderbird, let it download the account's mail, then run the backup again. Messages Thunderbird has not downloaded are not in the archive yet.\n\n", acct)
	}
	o = question(t, "a summary for three accounts", summary.String(), zenity.OKLabel("Open the archive"), zenity.CancelLabel("Close"))
	checkMessageBox(t, "a summary for three accounts", o.hwnd)
	var text child
	for _, c := range children(o.hwnd) {
		if strings.EqualFold(c.class, "STATIC") && len(c.text) > len(text.text) {
			text = c
		}
	}
	if text.hwnd != 0 {
		cr := clientRect(text.hwnd)
		_, need := measure(text.hwnd, strings.TrimRight(text.text, "\r\n"), cr.w(), dtWordBreak|dtNoPrefix|dtExpandTabs)
		verdict := "shown whole"
		if need > cr.h() {
			verdict = "CUT by Windows"
		}
		t.Logf("record: the summary box's text needs %d pixels and its control is %d high: %s (box %+v, work area %+v)",
			need, cr.h(), verdict, windowRect(o.hwnd), workArea(o.hwnd))
	}
	o.close(t, "a summary for three accounts")
}

// covers: MA-1470, S86
// Screenshots for the friction review (design §5, C22), saved only in the lab with
// MAILARCHIVE_LAB_SHOTS naming a folder: the wizard's first menu, its kind list, the date
// window, the walk's mailbox list, and the health view, typical and longest, each with the
// wizard's own words, and shots.txt naming the copy that drew them (its lab-copy hash).
func TestMA1470Screenshots(t *testing.T) {
	if !labOn() || os.Getenv(labShotsEnv) == "" {
		t.Skip("saves screenshots only in the lab, with " + labShotsEnv + " naming a folder")
	}
	perMonitorAware(t)
	lists := []struct {
		name, prompt string
		rows         []string
		def          string // the row the wizard chooses for the person (zenity.DefaultItems)
	}{
		{"31-start", "What would you like to do?", []string{
			"Archive a mail file on this computer (Outlook, Thunderbird, mbox)",
			"Microsoft 365, folder export, browsing & tools (open MailArchive Desktop)",
			"Open an archive folder I already have, encrypted or not (in MailArchive Desktop)",
			"Make a new encrypted archive (in MailArchive Desktop)"}, "Archive a mail file on this computer (Outlook, Thunderbird, mbox)"},
		{"32-kind", "What are you exporting?", []string{"Auto-detect my mailboxes", "Outlook data file (.pst / .ost)",
			"Outlook account (via Outlook app — for Exchange / .ost)", "Thunderbird / mbox mail folder", "Single mbox file"}, "Auto-detect my mailboxes"},
		{"34-picker", "Which mailbox do you want to export?", walkRows, walkRows[0]},
		{"35-health-typical", typicalHealthView, healthChoices, healthChoices[0]},
		{"36-health-longest", healthView, healthChoices, healthChoices[0]},
	}
	saved := 0
	for _, l := range lists {
		o := open(t, "list", func(ctx context.Context, title string) (string, error) {
			return zenity.List(l.prompt, l.rows, zenity.DefaultItems(l.def), zenity.Title(title), zenity.Context(ctx))
		})
		saveShot(t, l.name, o.hwnd)
		o.close(t, l.name)
		saved++
	}
	o := open(t, "entry", func(ctx context.Context, title string) (string, error) {
		return zenity.Entry("Only export items newer than\n(e.g. 30d, 4w, 720h, or a date like 2026-07-01).\n\nLeave blank to export everything.",
			zenity.EntryText(""), zenity.Title(title), zenity.Context(ctx))
	})
	saveShot(t, "33-date", o.hwnd)
	o.close(t, "the date window")
	saved++
	note := fmt.Sprintf("MA-1470 screenshots, %s, %s, %s; drawn by the copy whose lab-copy hash is %q\n",
		time.Now().UTC().Format(time.RFC3339), labWindows(), labScreen(), labCopyHash)
	if err := os.WriteFile(filepath.Join(os.Getenv(labShotsEnv), "shots.txt"), []byte(note), 0o644); err != nil {
		t.Fatal(err)
	}
	if saved != len(lists)+1 {
		t.Errorf("saved %d screenshots; want %d", saved, len(lists)+1)
	}
}
