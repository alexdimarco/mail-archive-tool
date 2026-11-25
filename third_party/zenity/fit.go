package zenity

// This file is MailArchive's addition to zenity v0.10.12 (UPSTREAM.md;
// docs/design-windows-dialog-fit.md §4.3 in MailArchive's repository): the arithmetic
// that sizes the Windows list, entry and progress dialogs to their words, and re-flows a
// message box's renamed buttons. It is pure and total: it imports nothing, calls nothing
// but the measurer it is given, never panics, and works in device pixels at the window's
// DPI. The _windows.go files measure the text and apply the result.

// fitRect is a rectangle in device pixels.
type fitRect struct{ X, Y, W, H int }

// fitSize is a width and a height in device pixels.
type fitSize struct{ W, H int }

// fitMeasurer measures text in the dialog's own font at the window's DPI.
type fitMeasurer interface {
	// line measures text drawn on one line.
	line(text string) (w, h int, ok bool)
	// wrapped measures text wrapped at width, as a prompt label draws it.
	wrapped(text string, width int) (w, h int, ok bool)
}

// fitEnv is what a layout reads from Windows besides the text, in device pixels at the
// window's DPI.
type fitEnv struct {
	DPI     int     // the window's DPI; 0 or less is read as 96
	Work    fitRect // the work area of the window's monitor
	Frame   fitSize // the window's non-client width and height
	VScroll int     // a vertical scroll bar's width
	HScroll int     // a horizontal scroll bar's height
	Edge    int     // a client edge's width
	AvgChar int     // one average character's width in the dialog's font
	LineH   int     // one line's height in the dialog's font
}

// fitDialog is a laid-out list, entry or progress dialog. Upstream means the dialog
// keeps v0.10.12's layout and label style (§4.3, Fallbacks), and nothing else is set.
type fitDialog struct {
	Upstream bool
	DPI      int       // the DPI it was laid out at (96 for a DPI of 0 or less)
	Window   fitSize   // the outer size
	Client   fitSize   // the client area
	Prompt   fitRect   // the prompt label, or the scrolling prompt
	Scroll   bool      // the prompt scrolls: a list's prompt too tall for the room
	PromptH  int       // the prompt's height before the room cut it (a progress dialog's floor)
	Control  fitRect   // the list, the edit box or the progress bar
	Extent   int       // the list's sideways extent
	HBar     bool      // the list's sideways bar shows
	Buttons  []fitRect // left to right, in the order given
}

// Sizes in logical pixels: upstream's, and this patch's (§4.3).
const (
	fitMargin    = 12  // upstream's left, right and bottom margin
	fitTop       = 10  // upstream's top margin
	fitGap       = 4   // between a prompt and the control below it
	fitRowGap    = 12  // between the content and the button row
	fitMinW      = 241 // upstream's content width: the floor
	fitStepW     = 400 // a list's or an entry's width when its words need less, the cap permitting: every wizard step one width (the friction review)
	fitWrapW     = 400 // a prompt line wider than this wraps rather than widening the dialog
	fitProgressW = 400 // the progress dialog's content width
	fitListH     = 164 // upstream's list height
	fitEditH     = 24  // upstream's edit box height
	fitBarH      = 16  // upstream's progress bar height
	fitButtonH   = 24  // upstream's button height
	fitButtonW   = 75  // upstream's button width: the floor
	fitButtonPad = 12  // beside a button's label, on each side
	fitButtonGap = 8   // between two buttons
	fitItemPad   = 4   // a list item's text margins, both sides together
	fitEditPad   = 8   // an edit box's text margins, both sides together

	// upstream's window sizes, which the cap must leave room for
	fitUpstreamW         = 281
	fitUpstreamListH     = 281
	fitUpstreamEntryH    = 141
	fitUpstreamProgressH = 133
	fitUpstreamBareH     = 97 // a progress dialog without buttons
)

func fitMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fitMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// fitDPI reads a DPI of 0 or less as 96.
func fitDPI(dpi int) int {
	if dpi <= 0 {
		return 96
	}
	return dpi
}

// fitUp scales a logical size to the DPI, rounding up, so a control is never smaller
// than what it holds (§4.3, Units).
func fitUp(v, dpi int) int {
	return (v*fitDPI(dpi) + 95) / 96
}

// fitDown is upstream's scaling, which rounds down.
func fitDown(v, dpi int) int {
	return v * fitDPI(dpi) / 96
}

// fitCap is the largest outer size a dialog may have: 80 % of its monitor's work area's
// width and 90 % of its height.
func fitCap(work fitRect) fitSize {
	return fitSize{W: work.W * 8 / 10, H: work.H * 9 / 10}
}

// fitUsable reports whether the environment allows the patched layout: a readable work
// area whose cap leaves room for upstream's window (w by h, logical) at this DPI, and a
// measured line height. Otherwise the dialog keeps upstream's layout (§4.3, Fallbacks).
func fitUsable(e fitEnv, w, h int) bool {
	c := fitCap(e.Work)
	return e.Work.W > 0 && e.Work.H > 0 && e.LineH > 0 &&
		c.W >= fitDown(w, e.DPI) && c.H >= fitDown(h, e.DPI)
}

// fitSlack is a quarter line, so a pixel lost between measuring and drawing never drops
// a prompt's last line (§4.3, Prompt labels).
func fitSlack(lineH int) int {
	return (lineH + 3) / 4
}

// fitLines splits text at its line breaks.
func fitLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	return append(lines, text[start:])
}

// fitWidestLine is the width of the prompt's widest line, each measured on one line.
// Empty text is never measured (§4.3, Measuring).
func fitWidestLine(m fitMeasurer, text string) (int, bool) {
	widest := 0
	for _, l := range fitLines(text) {
		if l == "" || l == "\r" {
			continue
		}
		w, _, ok := m.line(l)
		if !ok || w <= 0 {
			return 0, false
		}
		widest = fitMax(widest, w)
	}
	return widest, true
}

// fitLabels measures each button label on one line.
func fitLabels(m fitMeasurer, labels []string) ([]int, bool) {
	out := make([]int, len(labels))
	for i, l := range labels {
		if l == "" {
			continue
		}
		w, _, ok := m.line(l)
		if !ok || w <= 0 {
			return nil, false
		}
		out[i] = w
	}
	return out, true
}

// fitButtonWidth is every button's width in a row: the widest label plus the padding on
// each side, at least upstream's 75 (§4.3, Buttons).
func fitButtonWidth(labels []int, dpi int) int {
	w := fitUp(fitButtonW, dpi)
	for _, l := range labels {
		w = fitMax(w, l+2*fitUp(fitButtonPad, dpi))
	}
	return w
}

// fitRowWidth is the width of a row of n buttons of width w.
func fitRowWidth(n, w, dpi int) int {
	if n <= 0 {
		return 0
	}
	return n*w + (n-1)*fitUp(fitButtonGap, dpi)
}

// fitShare holds a row of n buttons of width w inside the content width: a label wider
// than its share of the widest dialog is cut (P1's limit; MailArchive's labels are short
// fixed words).
func fitShare(w, n, content, dpi int) int {
	if n <= 0 || fitRowWidth(n, w, dpi) <= content {
		return w
	}
	return fitMax((content-(n-1)*fitUp(fitButtonGap, dpi))/n, 1)
}

// fitRow places n buttons of width w left to right, the row ending at right, its top at y.
func fitRow(n, w, right, y, dpi int) []fitRect {
	if n <= 0 {
		return nil
	}
	gap := fitUp(fitButtonGap, dpi)
	x := right - fitRowWidth(n, w, dpi)
	out := make([]fitRect, n)
	for i := range out {
		out[i] = fitRect{X: x + i*(w+gap), Y: y, W: w, H: fitUp(fitButtonH, dpi)}
	}
	return out
}

// fitContent is the content width: the widest of what it must hold, capped, and never
// under upstream's 241 (the floors win over the cap). A list's and an entry's step width,
// 400, is one of what it must hold, so it gives way to the cap.
func fitContent(dpi, capContent int, widths ...int) int {
	c := 0
	for _, w := range widths {
		c = fitMax(c, w)
	}
	return fitMax(fitMin(c, capContent), fitUp(fitMinW, dpi))
}

// fitPrompt measures the prompt at the content width. A measured width wider than the
// label (a word that cannot break at a space) widens the content, up to the cap, and is
// measured again (§4.3, Prompt labels). The label's height is the wrapped height plus a
// quarter line of slack, and at least one line; empty text takes one line.
func fitPrompt(m fitMeasurer, e fitEnv, text string, content, capContent int) (h, width int, ok bool) {
	lineH := e.LineH
	if text == "" {
		return lineH + fitSlack(lineH), content, true
	}
	w, wh, ok := m.wrapped(text, content)
	if !ok || w <= 0 || wh <= 0 {
		return 0, 0, false
	}
	if w > content && content < capContent {
		content = fitMin(w, capContent)
		w, wh, ok = m.wrapped(text, content)
		if !ok || w <= 0 || wh <= 0 {
			return 0, 0, false
		}
	}
	return fitMax(wh, lineH) + fitSlack(lineH), content, true
}

// fitList lays out a list dialog: a prompt, a list of rows, a button row (§4.3, List
// dialog).
func fitList(e fitEnv, m fitMeasurer, prompt string, rows, buttons []string) fitDialog {
	if !fitUsable(e, fitUpstreamW, fitUpstreamListH) {
		return fitDialog{Upstream: true}
	}
	dpi := e.DPI
	capS := fitCap(e.Work)
	capContent := capS.W - e.Frame.W - 2*fitUp(fitMargin, dpi)

	widest := 0
	for _, r := range rows {
		if r == "" {
			continue
		}
		w, _, ok := m.line(r)
		if !ok || w <= 0 {
			return fitDialog{Upstream: true}
		}
		widest = fitMax(widest, w)
	}
	textArea := widest + fitUp(fitItemPad, dpi) + fitMax(e.AvgChar, 0)
	listW := textArea + 2*fitMax(e.Edge, 0) + fitMax(e.VScroll, 0)

	labels, ok := fitLabels(m, buttons)
	if !ok {
		return fitDialog{Upstream: true}
	}
	btnW := fitButtonWidth(labels, dpi)
	natural, ok := fitWidestLine(m, prompt)
	if !ok {
		return fitDialog{Upstream: true}
	}
	content := fitContent(dpi, capContent, fitUp(fitStepW, dpi), listW, fitMin(natural, fitUp(fitWrapW, dpi)), fitRowWidth(len(buttons), btnW, dpi))
	promptH, content, ok := fitPrompt(m, e, prompt, content, capContent)
	if !ok {
		return fitDialog{Upstream: true}
	}

	d := fitDialog{DPI: fitDPI(dpi), PromptH: promptH, Extent: textArea, HBar: listW > content}
	listH := fitUp(fitListH, dpi)
	if d.HBar {
		listH += fitMax(e.HScroll, 0)
	}
	rest := fitUp(fitTop, dpi) + fitUp(fitGap, dpi) + listH + fitUp(fitRowGap, dpi) + fitUp(fitButtonH, dpi) + fitUp(fitMargin, dpi)
	if room := capS.H - e.Frame.H - rest; promptH > room {
		// A prompt too tall for the room: the list first gives up its empty rows, down to
		// the rows it holds, and the prompt takes that room; it scrolls only if it still
		// does not fit (the friction review's G1: the health view's three choices sat
		// under nine rows' height while its reasons were cut).
		if short := fitRowsH(len(rows), e, d.HBar); short < listH {
			room += listH - short
			listH = short
		}
		if promptH > room {
			d.Scroll = true
			promptH = fitMax(room, e.LineH)
		}
	}

	x := fitUp(fitMargin, dpi)
	y := fitUp(fitTop, dpi)
	d.Prompt = fitRect{X: x, Y: y, W: content, H: promptH}
	y += promptH + fitUp(fitGap, dpi)
	d.Control = fitRect{X: x, Y: y, W: content, H: listH}
	y += listH + fitUp(fitRowGap, dpi)
	d.Buttons = fitRow(len(buttons), fitShare(btnW, len(buttons), content, dpi), x+content, y, dpi)
	d.Client = fitSize{W: content + 2*x, H: y + fitUp(fitButtonH, dpi) + fitUp(fitMargin, dpi)}
	d.Window = fitSize{W: d.Client.W + e.Frame.W, H: d.Client.H + e.Frame.H}
	return d
}

// fitRowsH is the height of a list that shows n rows (at least one) without scrolling:
// a line each, half a line spare so the list box's rounding to whole rows keeps the last
// one, both client edges, and the sideways bar when it shows.
func fitRowsH(n int, e fitEnv, hbar bool) int {
	h := fitMax(n, 1)*e.LineH + e.LineH/2 + 2*fitMax(e.Edge, 0)
	if hbar {
		h += fitMax(e.HScroll, 0)
	}
	return h
}

// fitEntry lays out an entry dialog: a prompt, an edit box holding start, a button row
// (§4.3, Entry dialog). A prompt taller than the room is cut at the bottom (P1's limit).
func fitEntry(e fitEnv, m fitMeasurer, prompt, start string, buttons []string) fitDialog {
	if !fitUsable(e, fitUpstreamW, fitUpstreamEntryH) {
		return fitDialog{Upstream: true}
	}
	dpi := e.DPI
	capS := fitCap(e.Work)
	capContent := capS.W - e.Frame.W - 2*fitUp(fitMargin, dpi)

	startW := 0
	if start != "" {
		w, _, ok := m.line(start)
		if !ok || w <= 0 {
			return fitDialog{Upstream: true}
		}
		startW = w + fitUp(fitEditPad, dpi) + 2*fitMax(e.Edge, 0)
	}
	labels, ok := fitLabels(m, buttons)
	if !ok {
		return fitDialog{Upstream: true}
	}
	btnW := fitButtonWidth(labels, dpi)
	natural, ok := fitWidestLine(m, prompt)
	if !ok {
		return fitDialog{Upstream: true}
	}
	content := fitContent(dpi, capContent, fitUp(fitStepW, dpi), startW, fitMin(natural, fitUp(fitWrapW, dpi)), fitRowWidth(len(buttons), btnW, dpi))
	promptH, content, ok := fitPrompt(m, e, prompt, content, capContent)
	if !ok {
		return fitDialog{Upstream: true}
	}

	d := fitDialog{DPI: fitDPI(dpi), PromptH: promptH}
	editH := fitUp(fitEditH, dpi)
	rest := fitUp(fitTop, dpi) + fitUp(fitGap, dpi) + editH + fitUp(fitRowGap, dpi) + fitUp(fitButtonH, dpi) + fitUp(fitMargin, dpi)
	if room := capS.H - e.Frame.H - rest; promptH > room {
		promptH = fitMax(room, e.LineH)
	}

	x := fitUp(fitMargin, dpi)
	y := fitUp(fitTop, dpi)
	d.Prompt = fitRect{X: x, Y: y, W: content, H: promptH}
	y += promptH + fitUp(fitGap, dpi)
	d.Control = fitRect{X: x, Y: y, W: content, H: editH}
	y += editH + fitUp(fitRowGap, dpi)
	d.Buttons = fitRow(len(buttons), fitShare(btnW, len(buttons), content, dpi), x+content, y, dpi)
	d.Client = fitSize{W: content + 2*x, H: y + fitUp(fitButtonH, dpi) + fitUp(fitMargin, dpi)}
	d.Window = fitSize{W: d.Client.W + e.Frame.W, H: d.Client.H + e.Frame.H}
	return d
}

// fitProgress lays out a progress dialog: its text, the bar, and a button row when it
// has buttons (§4.3, Progress dialog). The content is 400 wide; the content is never
// narrower than floor.W nor the prompt lower than floor.H, the width and height they
// reached before (the cap permitting), so the window grows and never shrinks while it is
// open; past the room the prompt is cut at the bottom (P1's limit).
func fitProgress(e fitEnv, m fitMeasurer, text string, floor fitSize, buttons []string) fitDialog {
	h := fitUpstreamProgressH
	if len(buttons) == 0 {
		h = fitUpstreamBareH
	}
	if !fitUsable(e, fitUpstreamW, h) {
		return fitDialog{Upstream: true}
	}
	dpi := e.DPI
	capS := fitCap(e.Work)
	capContent := capS.W - e.Frame.W - 2*fitUp(fitMargin, dpi)

	labels, ok := fitLabels(m, buttons)
	if !ok {
		return fitDialog{Upstream: true}
	}
	btnW := fitButtonWidth(labels, dpi)
	content := fitContent(dpi, capContent, fitUp(fitProgressW, dpi), fitRowWidth(len(buttons), btnW, dpi), floor.W)
	promptH, content, ok := fitPrompt(m, e, text, content, capContent)
	if !ok {
		return fitDialog{Upstream: true}
	}
	promptH = fitMax(promptH, floor.H)

	d := fitDialog{DPI: fitDPI(dpi), PromptH: promptH}
	barH := fitUp(fitBarH, dpi)
	rest := fitUp(fitTop, dpi) + fitUp(fitGap, dpi) + barH + fitUp(fitMargin, dpi)
	if len(buttons) > 0 {
		rest += fitUp(fitRowGap, dpi) + fitUp(fitButtonH, dpi)
	}
	if room := capS.H - e.Frame.H - rest; promptH > room {
		promptH = fitMax(room, e.LineH)
	}

	x := fitUp(fitMargin, dpi)
	y := fitUp(fitTop, dpi)
	d.Prompt = fitRect{X: x, Y: y, W: content, H: promptH}
	y += promptH + fitUp(fitGap, dpi)
	d.Control = fitRect{X: x, Y: y, W: content, H: barH}
	y += barH
	if len(buttons) > 0 {
		y += fitUp(fitRowGap, dpi)
		d.Buttons = fitRow(len(buttons), fitShare(btnW, len(buttons), content, dpi), x+content, y, dpi)
		y += fitUp(fitButtonH, dpi)
	}
	d.Client = fitSize{W: content + 2*x, H: y + fitUp(fitMargin, dpi)}
	d.Window = fitSize{W: d.Client.W + e.Frame.W, H: d.Client.H + e.Frame.H}
	return d
}

// fitRebase carries a size kept across layouts (a progress dialog's floor) from one DPI
// to another, rounding up, so a round trip between two DPIs never shrinks it.
func fitRebase(v, from, to int) int {
	from, to = fitDPI(from), fitDPI(to)
	return (v*to + from - 1) / from
}

// fitInside moves r, never resizing it, as little as it must to lie inside work; a
// window larger than the work area keeps its top-left corner at the work area's.
func fitInside(r, work fitRect) fitRect {
	if r.X+r.W > work.X+work.W {
		r.X = work.X + work.W - r.W
	}
	if r.Y+r.H > work.Y+work.H {
		r.Y = work.Y + work.H - r.H
	}
	if r.X < work.X {
		r.X = work.X
	}
	if r.Y < work.Y {
		r.Y = work.Y
	}
	return r
}

// fitCenter places a window of size s in the middle of the work area (§4.3).
func fitCenter(s fitSize, work fitRect) fitRect {
	return fitInside(fitRect{X: work.X + (work.W-s.W)/2, Y: work.Y + (work.H-s.H)/2, W: s.W, H: s.H}, work)
}

// fitGrow is where a window goes when it changes size: its top-left corner stays, and it
// moves only as far as it must to stay inside the work area, so a progress dialog grows
// downward (§4.3, Progress dialog).
func fitGrow(old fitRect, s fitSize, work fitRect) fitRect {
	return fitInside(fitRect{X: old.X, Y: old.Y, W: s.W, H: s.H}, work)
}

// fitAbout re-centres a window that changed size about its old centre, inside the work
// area (a message box, §4.3).
func fitAbout(old fitRect, s fitSize, work fitRect) fitRect {
	cx, cy := old.X+old.W/2, old.Y+old.H/2
	return fitInside(fitRect{X: cx - s.W/2, Y: cy - s.H/2, W: s.W, H: s.H}, work)
}

// fitMessageRow re-flows a message box's button row (§4.3, Message boxes). btns are the
// row's buttons, left to right, in the box's client coordinates, and labels their labels'
// measured widths. When a label needs more than its button (its width plus 12 on each
// side, scaled), every button becomes as wide as the widest need, the row keeps Windows'
// gap and right margin, and the client area grows to the right by what the row lacks,
// with a left margin as wide as the right one. ok is false, and nothing is to change,
// when every label fits, when a measurement is missing, or when the box would grow past
// maxClientW.
func fitMessageRow(btns []fitRect, labels []int, clientW, maxClientW, dpi int) (out []fitRect, grow int, ok bool) {
	n := len(btns)
	if n == 0 || len(labels) != n || clientW <= 0 {
		return nil, 0, false
	}
	pad := fitUp(fitButtonPad, dpi)
	need := false
	w := 0
	for i, b := range btns {
		if labels[i] <= 0 || b.W <= 0 {
			return nil, 0, false
		}
		want := labels[i] + 2*pad
		if want > b.W {
			need = true
		}
		w = fitMax(w, fitMax(want, b.W))
	}
	if !need {
		return nil, 0, false
	}
	gap := 0
	if n > 1 {
		gap = btns[1].X - (btns[0].X + btns[0].W)
		if gap < 0 {
			gap = fitUp(fitButtonGap, dpi)
		}
	}
	margin := fitMax(clientW-(btns[n-1].X+btns[n-1].W), 0)
	row := n*w + (n-1)*gap
	newW := fitMax(clientW, row+2*margin)
	if newW > maxClientW {
		return nil, 0, false
	}
	x := newW - margin - row
	out = make([]fitRect, n)
	for i, b := range btns {
		out[i] = fitRect{X: x + i*(w+gap), Y: b.Y, W: w, H: b.H}
	}
	return out, newW - clientW, true
}
