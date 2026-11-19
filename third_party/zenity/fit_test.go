package zenity

// MailArchive's tests of fit.go (MA-1469 in its docs/scenario-catalog.md). They run with
// this module's own `go test ./...`, which MailArchive's internal/thirdparty test starts
// from the main module. They use a stand-in measurer: fit.go's arithmetic is what they
// prove; the real measuring is proved on Windows (MA-1470). Written to Go 1.20's rules
// (this module's go.mod): no min or max built-ins, and each loop variable is copied
// before a closure uses it.

import (
	"strings"
	"testing"
)

// stand is a stand-in measurer: every character is charW wide and every line lineH
// high. wrapped breaks lines at spaces and, as an edit control does, inside a word wider
// than the width, unless noBreak is set. A text in fail fails to measure, and one in
// zeroWide measures 0 wide and one line high; zero makes every measurement 0 and neg
// every measurement negative.
type stand struct {
	charW, lineH int
	noBreak      bool
	fail         map[string]bool
	zeroWide     map[string]bool
	zero, neg    bool
}

func (s stand) result(w, h int, text string) (int, int, bool) {
	switch {
	case s.fail[text]:
		return 0, 0, false
	case s.zeroWide[text]:
		return 0, s.lineH, true
	case s.zero:
		return 0, 0, true
	case s.neg:
		return -w, -h, true
	}
	return w, h, true
}

func (s stand) line(text string) (int, int, bool) {
	return s.result(len([]rune(text))*s.charW, s.lineH, text)
}

func (s stand) wrapped(text string, width int) (int, int, bool) {
	perLine := width / s.charW
	if perLine < 1 {
		perLine = 1
	}
	lines, widest := 0, 0
	for _, para := range strings.Split(text, "\n") {
		cur := 0
		lines++
		for _, word := range strings.Fields(para) {
			n := len([]rune(word))
			if cur > 0 && cur+1+n <= perLine {
				cur += 1 + n
			} else {
				if cur > 0 {
					lines++
				}
				cur = n
			}
			for !s.noBreak && cur > perLine {
				widest = fitMax(widest, perLine)
				lines++
				cur -= perLine
			}
			widest = fitMax(widest, cur)
		}
	}
	return s.result(widest*s.charW, lines*s.lineH, text)
}

// envAt is a 1920x1040 work area (at 96 DPI) with upstream's frame (a 281-pixel window
// holds a 265 by 242 client area) and Windows' usual metrics, scaled to dpi.
func envAt(dpi int) fitEnv {
	return fitEnv{
		DPI:     dpi,
		Work:    fitRect{X: 0, Y: 0, W: 1920 * dpi / 96, H: 1040 * dpi / 96},
		Frame:   fitSize{W: 16 * dpi / 96, H: 39 * dpi / 96},
		VScroll: 17 * dpi / 96,
		HScroll: 17 * dpi / 96,
		Edge:    2 * dpi / 96,
		AvgChar: 6 * dpi / 96,
		LineH:   16 * dpi / 96,
	}
}

func standAt(dpi int) stand { return stand{charW: 6 * dpi / 96, lineH: 16 * dpi / 96} }

var okCancel = []string{"OK", "Cancel"}

// checkGeometry checks every rectangle of d lies inside its client area, none overlaps
// another, and the window is the client plus the frame, within the cap.
func checkGeometry(t *testing.T, name string, e fitEnv, d fitDialog) {
	t.Helper()
	if d.Upstream {
		t.Errorf("%s: kept upstream's layout", name)
		return
	}
	rects := append([]fitRect{d.Prompt, d.Control}, d.Buttons...)
	for i, r := range rects {
		if r.X < 0 || r.Y < 0 || r.W <= 0 || r.H <= 0 || r.X+r.W > d.Client.W || r.Y+r.H > d.Client.H {
			t.Errorf("%s: rectangle %d %+v is not inside the client area %+v", name, i, r, d.Client)
		}
		for j := i + 1; j < len(rects); j++ {
			o := rects[j]
			if r.X < o.X+o.W && o.X < r.X+r.W && r.Y < o.Y+o.H && o.Y < r.Y+r.H {
				t.Errorf("%s: rectangles %d %+v and %d %+v overlap", name, i, r, j, o)
			}
		}
	}
	if c := fitCap(e.Work); d.Window.W > c.W || d.Window.H > c.H {
		t.Errorf("%s: window %+v is past the cap %+v", name, d.Window, c)
	}
	if d.Window.W != d.Client.W+e.Frame.W || d.Window.H != d.Client.H+e.Frame.H {
		t.Errorf("%s: window %+v is not the client %+v plus the frame %+v", name, d.Window, d.Client, e.Frame)
	}
}

// covers: MA-1469, S86
// Positive twins first: for upstream's short words at 96 DPI the list and entry dialogs
// come out at upstream's heights and control heights, 400 wide (the step width: every
// wizard step one width, the friction review) where upstream's were 241, with OK at 254
// and Cancel at 337 ending the row at the right margin, and every prompt label a quarter
// line taller than upstream's 16 (the slack, §4.3), which moves what is below it down by
// that much; the progress dialog comes out 400 wide with upstream's heights and the same
// slack.
func TestFitPositiveTwins(t *testing.T) {
	e, m := envAt(96), standAt(96)
	slack := fitSlack(16)
	if slack != 4 {
		t.Fatalf("a quarter of a 16-pixel line is %d", slack)
	}

	l := fitList(e, m, "Pick one:", []string{"First", "Second"}, okCancel)
	checkGeometry(t, "list", e, l)
	if l.Window.W != 440 || l.Client.W != 424 || l.Window.H != 281+slack {
		t.Errorf("list window %+v, client %+v; want 440 wide (424 inside) and 281+%d high", l.Window, l.Client, slack)
	}
	if l.Prompt != (fitRect{X: 12, Y: 10, W: 400, H: 16 + slack}) {
		t.Errorf("list prompt %+v", l.Prompt)
	}
	if l.Control != (fitRect{X: 12, Y: 30 + slack, W: 400, H: 164}) || l.HBar || l.Scroll {
		t.Errorf("list %+v (sideways bar %v, scrolling prompt %v)", l.Control, l.HBar, l.Scroll)
	}
	want := []fitRect{{X: 254, Y: 206 + slack, W: 75, H: 24}, {X: 337, Y: 206 + slack, W: 75, H: 24}}
	if len(l.Buttons) != 2 || l.Buttons[0] != want[0] || l.Buttons[1] != want[1] {
		t.Errorf("list buttons %+v; want %+v", l.Buttons, want)
	}

	en := fitEntry(e, m, "Your name:", "", okCancel)
	checkGeometry(t, "entry", e, en)
	if en.Window.W != 440 || en.Window.H != 141+slack || en.Control != (fitRect{X: 12, Y: 30 + slack, W: 400, H: 24}) {
		t.Errorf("entry window %+v, edit box %+v", en.Window, en.Control)
	}
	if len(en.Buttons) != 2 || en.Buttons[0].X != 254 || en.Buttons[1].X != 337 || en.Buttons[0].Y != 66+slack {
		t.Errorf("entry buttons %+v", en.Buttons)
	}

	p := fitProgress(e, m, "", fitSize{}, []string{"Cancel"})
	checkGeometry(t, "progress", e, p)
	if p.Client.W != 400+24 || p.Prompt.H != 16+slack || p.Control != (fitRect{X: 12, Y: 30 + slack, W: 400, H: 16}) || p.Client.H != 94+slack {
		t.Errorf("progress client %+v, prompt %+v, bar %+v", p.Client, p.Prompt, p.Control)
	}
	bare := fitProgress(e, m, "", fitSize{}, nil)
	if bare.Client.H != 58+slack || len(bare.Buttons) != 0 {
		t.Errorf("progress without buttons: client %+v, buttons %+v", bare.Client, bare.Buttons)
	}
}

// covers: MA-1469, S86
// The list is as wide as its widest row: rows that fit set the sideways extent to the
// text area and show no sideways bar, ten or more of them too; rows past the cap hold the
// list at the cap with the extent at the widest row, and the list grows by the sideways
// bar so as many rows show (§4.3, List dialog).
func TestFitListRows(t *testing.T) {
	e, m := envAt(96), standAt(96)
	row := strings.Repeat("x", 90) // 540 pixels
	rows := []string{row}
	for i := 0; i < 11; i++ {
		rows = append(rows, "short")
	}
	d := fitList(e, m, "Which mailbox?", rows, okCancel)
	checkGeometry(t, "rows that fit", e, d)
	textArea := 540 + 4 + 6
	if d.Extent != textArea || d.HBar || d.Control.W != textArea+2*2+17 || d.Control.H != 164 {
		t.Errorf("rows that fit: extent %d, sideways bar %v, list %+v; want extent %d, no bar, list %d wide",
			d.Extent, d.HBar, d.Control, textArea, textArea+21)
	}

	long := strings.Repeat("y", 400) // 2400 pixels, past the cap
	d = fitList(e, m, "Which mailbox?", []string{long, "short"}, okCancel)
	checkGeometry(t, "a row past the cap", e, d)
	if c := fitCap(e.Work); c != (fitSize{W: 1536, H: 936}) {
		t.Errorf("the cap of a 1920x1040 work area is %+v; want 80 %% of its width and 90 %% of its height, 1536x936", c)
	}
	capContent := 1536 - 16 - 24
	if !d.HBar || d.Control.W != capContent || d.Extent != 2400+4+6 || d.Control.H != 164+17 {
		t.Errorf("a row past the cap: list %+v, extent %d, sideways bar %v; want %d wide, extent %d, %d high",
			d.Control, d.Extent, d.HBar, capContent, 2410, 181)
	}
}

// covers: MA-1469, S86
// The floors: a list or an entry whose words need less is the step width, 400 (every
// wizard step one width, the friction review), which gives way to the cap on a small work
// area but never goes under upstream's 241, which wins over the cap (by a pixel at 144
// DPI); a button is never under its 75, whatever the words (§4.3).
func TestFitFloors(t *testing.T) {
	m := standAt(96)
	m.charW = 1
	at := func(dpi, w, h int) fitEnv {
		e := envAt(dpi)
		e.Work = fitRect{W: w, H: h}
		return e
	}
	for _, c := range []struct {
		name string
		e    fitEnv
		want int
	}{
		{"a full screen", envAt(96), 400},
		{"a small work area (a cap of 320 leaves 280)", at(96, 400, 400), 280},
		{"a cap of upstream's window", at(96, 352, 400), 241},
		{"a cap of upstream's window at 144 DPI (361 left)", at(144, 527, 600), 362},
	} {
		for _, d := range []fitDialog{
			fitList(c.e, m, "a", []string{"b"}, []string{"c", "d"}),
			fitEntry(c.e, m, "a", "b", []string{"c", "d"}),
		} {
			if d.Upstream || d.Prompt.W != c.want || d.Control.W != c.want {
				t.Errorf("%s, tiny words: prompt %+v, control %+v; want %d wide", c.name, d.Prompt, d.Control, c.want)
			}
			for _, b := range d.Buttons {
				if b.W != fitUp(75, c.e.DPI) {
					t.Errorf("%s, tiny label: button %+v; want %d wide", c.name, b, fitUp(75, c.e.DPI))
				}
			}
		}
	}
}

// covers: MA-1469, S86
// A prompt line within the step width stays one line at 400; a longer one wraps at 400
// and the label is as tall as the wrapped text plus the slack. A word that cannot break widens the content
// up to the cap and is measured again (§4.3, Prompt labels).
func TestFitPromptWraps(t *testing.T) {
	e, m := envAt(96), standAt(96)
	slack := fitSlack(16)

	d := fitList(e, m, strings.Repeat("a", 50), []string{"x"}, okCancel) // 300 pixels
	if d.Prompt.W != 400 || d.Prompt.H != 16+slack {
		t.Errorf("a 300-pixel line: prompt %+v; want the step width, 400, one line", d.Prompt)
	}

	words := strings.TrimSpace(strings.Repeat("word ", 40)) // 199 characters, 1194 pixels
	d = fitList(e, m, words, []string{"x"}, okCancel)
	_, h, _ := m.wrapped(words, 400)
	if d.Prompt.W != 400 || d.Prompt.H != h+slack || h <= 16 {
		t.Errorf("a long line: prompt %+v; want 400 wide and %d high (%d wrapped + %d)", d.Prompt, h+slack, h, slack)
	}

	nb := m
	nb.noBreak = true
	word := strings.Repeat("p", 120) // 720 pixels, no space
	d = fitList(e, nb, word, []string{"x"}, okCancel)
	if d.Prompt.W != 720 {
		t.Errorf("a 720-pixel word: prompt %+v; want the content widened to 720", d.Prompt)
	}
	huge := strings.Repeat("q", 600) // 3600 pixels, past the cap
	d = fitList(e, nb, huge, []string{"x"}, okCancel)
	checkGeometry(t, "a word past the cap", e, d)
	if capContent := fitCap(e.Work).W - e.Frame.W - 24; d.Prompt.W != capContent {
		t.Errorf("a word past the cap: prompt %+v; want the content at the cap, %d", d.Prompt, capContent)
	}
}

// covers: MA-1469, S86
// A list prompt too tall for the room: the list first gives up its empty rows, down to
// the rows it holds (two rows at 144 DPI: 2 x 24 + 12 + 2 x 3 = 66), and the prompt takes
// that room (the friction review's G1); a prompt that still does not fit scrolls, the
// dialog at the cap and the prompt taking exactly the room left; one that fits once the
// list is short is a label at its wrapped height, under the cap; and its twin, a prompt
// that fits from the start, leaves the list at upstream's height (§4.3, A prompt too tall
// for the room).
func TestFitOverTallPromptScrolls(t *testing.T) {
	e, m := envAt(144), standAt(144)
	lines := make([]string, 80)
	for i := range lines {
		lines[i] = "Posture: WARN, a reason that names its remedy"
	}
	two := []string{"Continue", "Remove"}
	d := fitList(e, m, strings.Join(lines, "\n"), two, okCancel)
	checkGeometry(t, "an over-tall prompt", e, d)
	c := fitCap(e.Work)
	if !d.Scroll || d.Window.H != c.H {
		t.Errorf("an over-tall prompt: scroll %v, window %+v; want a scrolling prompt and the window at the cap %+v", d.Scroll, d.Window, c)
	}
	if d.PromptH <= d.Prompt.H {
		t.Errorf("the scrolling prompt %+v should be shorter than its text (%d)", d.Prompt, d.PromptH)
	}
	if d.Control.H != 66 {
		t.Errorf("an over-tall prompt: the list %+v kept its empty rows; want it 66 high, two rows", d.Control)
	}

	// 45 lines: too tall beside upstream's list (room 1007), short enough beside two rows (1187).
	mid := strings.Join(lines[:45], "\n")
	d = fitList(e, m, mid, two, okCancel)
	checkGeometry(t, "a prompt that fits once the list is short", e, d)
	_, h, _ := m.wrapped(mid, d.Prompt.W)
	if d.Scroll || d.Prompt.H != h+fitSlack(m.lineH) || d.Control.H != 66 || d.Window.H >= c.H {
		t.Errorf("a prompt that fits once the list is short: scroll %v, prompt %+v, list %+v, window %+v; want a label %d high above a two-row list, under the cap",
			d.Scroll, d.Prompt, d.Control, d.Window, h+fitSlack(m.lineH))
	}

	short := strings.Join(lines[:5], "\n")
	d = fitList(e, m, short, []string{"Continue"}, okCancel)
	_, h, _ = m.wrapped(short, d.Prompt.W)
	if d.Scroll || d.Prompt.H != h+fitSlack(m.lineH) || d.Control.H != fitUp(164, 144) {
		t.Errorf("a prompt that fits: scroll %v, prompt %+v, list %+v; want a label %d high above upstream's list", d.Scroll, d.Prompt, d.Control, h+fitSlack(m.lineH))
	}
}

// covers: MA-1469, S86
// Buttons fit their labels: every button of a row as wide as its widest label plus 12 on
// each side, all equal, 8 apart, the row ending at the content's right edge, in the
// order given (§4.3, Buttons).
func TestFitButtonsFitTheirLabels(t *testing.T) {
	e, m := envAt(120), standAt(120)
	labels := []string{"Open the archive", "Export what's changed", "Close"}
	d := fitList(e, m, "Backup finished", []string{"x"}, labels)
	checkGeometry(t, "long labels", e, d)
	widest := len([]rune("Export what's changed")) * m.charW
	w := widest + 2*fitUp(12, 120)
	gap := fitUp(8, 120)
	for i, b := range d.Buttons {
		if b.W != w {
			t.Errorf("button %d %+v; want %d wide", i, b, w)
		}
		if i > 0 && b.X != d.Buttons[i-1].X+w+gap {
			t.Errorf("button %d %+v is not %d after the one before", i, b, gap)
		}
	}
	if last := d.Buttons[len(d.Buttons)-1]; last.X+last.W != d.Prompt.X+d.Prompt.W {
		t.Errorf("the row ends at %d, not at the content's right edge %d", last.X+last.W, d.Prompt.X+d.Prompt.W)
	}

	// A label wider than its share of the widest dialog is cut, never pushed outside it
	// (P1's limit; MailArchive's labels are short fixed words).
	huge := []string{strings.Repeat("H", 400), "Cancel"}
	d = fitList(e, m, "x", []string{"x"}, huge)
	checkGeometry(t, "a label past the cap", e, d)
	if first, last := d.Buttons[0], d.Buttons[1]; first.X < d.Prompt.X || last.X+last.W != d.Prompt.X+d.Prompt.W || first.W != last.W {
		t.Errorf("a label past the cap: buttons %+v do not share the content %+v", d.Buttons, d.Prompt)
	}
}

// covers: MA-1469, S86
// An entry's starting text widens the content, up to the cap, so a link shows whole
// (§4.3, Entry dialog).
func TestFitEntryStartingText(t *testing.T) {
	e, m := envAt(144), standAt(144)
	link := "http://127.0.0.1:53817/_open?t=" + strings.Repeat("Z", 43) // 74 characters
	d := fitEntry(e, m, "Your browser did not open. Copy this link:", link, okCancel)
	checkGeometry(t, "the link", e, d)
	need := len([]rune(link))*m.charW + fitUp(8, 144) + 2*e.Edge
	if d.Control.W < need {
		t.Errorf("the edit box %+v is narrower than the link needs (%d)", d.Control, need)
	}
	d = fitEntry(e, m, "Paste:", strings.Repeat("w", 900), okCancel)
	checkGeometry(t, "a link past the cap", e, d)
	if capContent := fitCap(e.Work).W - e.Frame.W - 2*fitUp(12, 144); d.Control.W != capContent {
		t.Errorf("a starting text past the cap: edit box %+v; want %d wide", d.Control, capContent)
	}
}

// covers: MA-1469, S86
// At every DPI, and with odd measured sizes, every rectangle holds at least what it was
// measured to need: sizes go up, never down (§4.3, Units).
func TestFitDPIsRoundUp(t *testing.T) {
	for _, dpi := range []int{96, 120, 144, 168, 192} {
		e, m := envAt(dpi), standAt(dpi)
		for _, odd := range []int{0, 1, 3} {
			m := m
			m.lineH += odd // an odd measured line height
			prompt := "Line one\nLine two\nLine three"
			d := fitList(e, m, prompt, []string{strings.Repeat("r", 33)}, okCancel)
			checkGeometry(t, "list", e, d)
			if d.DPI != dpi {
				t.Errorf("DPI %d: the layout records DPI %d", dpi, d.DPI)
			}
			_, h, _ := m.wrapped(prompt, d.Prompt.W)
			if d.Prompt.H < h || d.Prompt.H < m.lineH {
				t.Errorf("DPI %d, line %d: prompt %+v shorter than its text (%d)", dpi, m.lineH, d.Prompt, h)
			}
			if rw := 33*m.charW + e.AvgChar; d.Control.W-2*e.Edge-e.VScroll < rw && !d.HBar {
				t.Errorf("DPI %d: the list %+v cannot hold its row (%d) and shows no sideways bar", dpi, d.Control, rw)
			}
			for _, b := range d.Buttons {
				if b.W < fitDown(75, dpi) || b.H < fitDown(24, dpi) {
					t.Errorf("DPI %d: button %+v smaller than upstream's", dpi, b)
				}
			}
			if d.Prompt.X != fitUp(12, dpi) || d.Prompt.Y != fitUp(10, dpi) {
				t.Errorf("DPI %d: prompt at %+v, not at the scaled margins", dpi, d.Prompt)
			}
		}
	}
	if got := fitUp(70, 144); got != 105 {
		t.Errorf("70 logical pixels at 144 DPI scale up to %d; want 105", got)
	}
	if got := fitUp(241, 120); got != 302 { // 301.25 rounded up
		t.Errorf("241 at 120 DPI scales up to %d; want 302", got)
	}
}

// covers: MA-1469, S86
// The progress dialog opens one line high at 400; its text grows the label, which never
// shrinks while the dialog is open, in height or, after a word wider than 400, in width
// (its buttons stay put); the floor carried to another DPI rounds up, so a round trip
// never shrinks it; the window grows downward from where it was and stays inside the work
// area (§4.3, Progress dialog).
func TestFitProgressGrowsAndNeverShrinks(t *testing.T) {
	e, m := envAt(144), standAt(144)
	slack := fitSlack(m.lineH)
	open := fitProgress(e, m, "", fitSize{}, []string{"Cancel"})
	if open.Prompt.H != m.lineH+slack || open.Control.W != fitUp(400, 144) {
		t.Errorf("opening: prompt %+v, bar %+v", open.Prompt, open.Control)
	}
	four := "Downloaded 12 MB so far (+3 MB).\nWaiting for the sync to finish…\n\nIn Thunderbird: right-click the account → Download / Sync Now."
	grown := fitProgress(e, m, four, fitSize{W: open.Prompt.W, H: open.PromptH}, []string{"Cancel"})
	if grown.PromptH <= open.PromptH || grown.Window.H <= open.Window.H {
		t.Errorf("four lines did not grow it: %+v after %+v", grown.Prompt, open.Prompt)
	}
	back := fitProgress(e, m, "Done.", fitSize{W: grown.Prompt.W, H: grown.PromptH}, []string{"Cancel"})
	if back.Prompt.H != grown.Prompt.H || back.Window != grown.Window {
		t.Errorf("a shorter text shrank it: %+v after %+v", back.Window, grown.Window)
	}
	nb := m
	nb.noBreak = true
	wide := fitProgress(e, nb, strings.Repeat("w", 100), fitSize{W: back.Prompt.W, H: back.PromptH}, []string{"Cancel"})
	after := fitProgress(e, nb, "Done.", fitSize{W: wide.Prompt.W, H: wide.PromptH}, []string{"Cancel"})
	if wide.Window.W <= back.Window.W || after.Window != wide.Window || after.Buttons[0] != wide.Buttons[0] {
		t.Errorf("a 900-pixel word, then a short text: %+v then %+v (button %+v then %+v); want it widened and kept",
			wide.Window, after.Window, wide.Buttons[0], after.Buttons[0])
	}
	if got := fitRebase(106, 144, 96); got != 71 {
		t.Errorf("a floor of 106 at 144 DPI is %d at 96; want 71 (70.7 rounded up)", got)
	}
	for _, v := range []int{1, 105, 106, 641} {
		if trip := fitRebase(fitRebase(v, 144, 96), 96, 144); trip < v {
			t.Errorf("a floor of %d came back from 96 DPI as %d", v, trip)
		}
	}

	at := fitCenter(open.Window, e.Work)
	moved := fitGrow(at, grown.Window, e.Work)
	if moved.X != at.X || moved.Y != at.Y {
		t.Errorf("growing moved the window from %+v to %+v", at, moved)
	}
	low := fitRect{X: at.X, Y: e.Work.H - open.Window.H, W: open.Window.W, H: open.Window.H}
	moved = fitGrow(low, grown.Window, e.Work)
	if moved.Y+moved.H != e.Work.Y+e.Work.H || moved.X != low.X {
		t.Errorf("growing at the bottom of the work area: %+v; want it moved up just enough", moved)
	}
}

// covers: MA-1469, S86
// Nothing exceeds the cap: across work areas, DPIs and texts from tiny to huge, every
// dialog lies within 80 % of its work area's width and 90 % of its height, with every
// control inside it and none overlapping another.
func TestFitNothingPastTheCap(t *testing.T) {
	texts := []string{"", "Short", strings.Repeat("word ", 300), strings.Repeat("unbroken", 200), strings.Repeat("line\n", 200)}
	works := []fitRect{{W: 1280, H: 680}, {W: 1920, H: 1040}, {X: -1920, Y: 0, W: 1920, H: 1080}, {W: 3840, H: 2100}}
	dpis := []int{96, 144, 192}
	checked := 0
	for _, dpi := range dpis {
		for _, w := range works {
			e := envAt(dpi)
			e.Work = w
			m := standAt(dpi)
			for _, text := range texts {
				checkGeometry(t, "list", e, fitList(e, m, text, []string{text, "row"}, []string{text, "Cancel"}))
				checkGeometry(t, "entry", e, fitEntry(e, m, text, text, []string{text, "Cancel"}))
				checkGeometry(t, "progress", e, fitProgress(e, m, text, fitSize{}, []string{"Cancel"}))
				nb := m
				nb.noBreak = true
				checkGeometry(t, "list, no break", e, fitList(e, nb, text, []string{"row"}, okCancel))
				checked += 4
			}
		}
	}
	if want := 4 * len(texts) * len(works) * len(dpis); checked != want {
		t.Errorf("the sweep checked %d dialogs; want %d", checked, want)
	}
}

// covers: MA-1469, S86
// fit.go is total (§4.3): empty text, zero rows, DPI 0, zero and negative measurements,
// and a 0x0 or tiny work area never panic. Empty text and zero rows give the dialog's own
// widths; a failed or empty measurement (one line of the prompt measuring 0 wide
// included), or a work area that cannot be read or is smaller than upstream's window,
// gives upstream's layout (§4.3, Fallbacks).
func TestFitIsTotal(t *testing.T) {
	e, m := envAt(96), standAt(96)

	d := fitList(e, m, "", nil, nil)
	if d.Upstream || d.Prompt.H != 16+fitSlack(16) || d.Control.W != 400 || len(d.Buttons) != 0 {
		t.Errorf("empty text and zero rows: %+v; want one line at the step width", d)
	}
	z := e
	z.DPI = 0
	if d := fitList(z, m, "Pick", []string{"a"}, okCancel); d.Upstream || d.Prompt.X != 12 || d.DPI != 96 {
		t.Errorf("DPI 0 is read as 96: %+v", d)
	}

	upstream := func(name string, d fitDialog) {
		t.Helper()
		if !d.Upstream {
			t.Errorf("%s: want upstream's layout, got %+v", name, d)
		}
	}
	for _, mm := range []stand{
		{charW: 6, lineH: 16, zero: true},
		{charW: 6, lineH: 16, neg: true},
		{charW: 6, lineH: 16, fail: map[string]bool{"Pick": true, "a": true, "OK": true, "start": true, "Copying": true}},
	} {
		upstream("list, a bad measurement", fitList(e, mm, "Pick", []string{"a"}, okCancel))
		upstream("entry, a bad measurement", fitEntry(e, mm, "Pick", "start", okCancel))
		upstream("progress, a bad measurement", fitProgress(e, mm, "Copying", fitSize{}, okCancel))
	}
	zw := standAt(96)
	zw.zeroWide = map[string]bool{"\u200b": true}
	upstream("list, a prompt line measuring 0 wide", fitList(e, zw, "Pick\n\u200b", []string{"a"}, okCancel))
	upstream("list, a row measuring 0 wide", fitList(e, zw, "Pick", []string{"a", "\u200b"}, okCancel))
	if d := fitList(e, zw, "Pick", []string{"a"}, okCancel); d.Upstream {
		t.Errorf("its twin, with every text visible, kept upstream's layout")
	}
	for _, w := range []fitRect{{}, {W: 100, H: 100}, {W: -5, H: 900}} {
		bad := e
		bad.Work = w
		upstream("list, a bad work area", fitList(bad, m, "Pick", []string{"a"}, okCancel))
		upstream("entry, a bad work area", fitEntry(bad, m, "Pick", "", okCancel))
		upstream("progress, a bad work area", fitProgress(bad, m, "", fitSize{}, nil))
	}
	nl := e
	nl.LineH = 0
	upstream("no line height", fitList(nl, m, "Pick", []string{"a"}, okCancel))

	if _, _, ok := fitMessageRow(nil, nil, 0, 0, 0); ok {
		t.Error("an empty message-box row should change nothing")
	}
	if r := fitInside(fitRect{X: 5, Y: 5, W: 900, H: 900}, fitRect{}); r.X != 0 || r.Y != 0 {
		t.Errorf("a window larger than an empty work area: %+v", r)
	}
}

// covers: MA-1469, S86
// A message box's renamed buttons (§4.3, Message boxes): when every label fits, nothing
// changes; when one needs more, every button becomes as wide as the widest need, Windows'
// gap and right margin are kept, the box grows to the right by what the row lacks with a
// left margin as wide as the right one, and nothing changes when it would grow past its
// limit or a measurement is missing.
func TestFitMessageRow(t *testing.T) {
	dpi := 144
	pad := fitUp(12, dpi)
	btns := []fitRect{{X: 200, Y: 150, W: 113, H: 35}, {X: 323, Y: 150, W: 113, H: 35}}
	clientW := 450

	if _, _, ok := fitMessageRow(btns, []int{30, 60}, clientW, 2000, dpi); ok {
		t.Error("labels that fit their buttons should change nothing")
	}

	labels := []int{180, 150} // "Open the archive", "Close", at 144 DPI
	out, grow, ok := fitMessageRow(btns, labels, clientW, 2000, dpi)
	if !ok {
		t.Fatal("a clipped label should re-flow the row")
	}
	w := 180 + 2*pad
	gap := btns[1].X - (btns[0].X + btns[0].W)
	margin := clientW - (btns[1].X + btns[1].W)
	newW := clientW + grow
	for i, b := range out {
		if b.W != w || b.Y != btns[i].Y || b.H != btns[i].H {
			t.Errorf("button %d %+v; want %d wide at the same height", i, b, w)
		}
	}
	if out[1].X-(out[0].X+out[0].W) != gap || newW-(out[1].X+out[1].W) != margin || out[0].X < margin {
		t.Errorf("row %+v in a client %d wide: gap %d and right margin %d not kept, or left margin under %d", out, newW, gap, margin, margin)
	}
	if grow != 2*w+gap+2*margin-clientW {
		t.Errorf("the box grew by %d; want %d", grow, 2*w+gap+2*margin-clientW)
	}

	if _, _, ok := fitMessageRow(btns, labels, clientW, clientW+10, dpi); ok {
		t.Error("a box that would grow past its limit should change nothing")
	}
	if _, _, ok := fitMessageRow(btns, []int{180, 0}, clientW, 2000, dpi); ok {
		t.Error("a missing measurement should change nothing")
	}
}

// covers: MA-1469, S86
// Placement: a dialog is centred in its monitor's work area, even on a monitor left of
// the primary; a window that changed size is re-centred about its old centre inside the
// work area (a message box), and one that would leave the work area moves only as far as
// it must (§4.3).
func TestFitPlacement(t *testing.T) {
	work := fitRect{X: -1920, Y: 0, W: 1920, H: 1040}
	c := fitCenter(fitSize{W: 600, H: 400}, work)
	if c != (fitRect{X: -1920 + 660, Y: 320, W: 600, H: 400}) {
		t.Errorf("centred at %+v", c)
	}
	a := fitAbout(c, fitSize{W: 700, H: 400}, work)
	if a.X+a.W/2 != c.X+c.W/2 || a.Y != c.Y {
		t.Errorf("re-centred at %+v; want the same centre as %+v", a, c)
	}
	edge := fitRect{X: -300, Y: 900, W: 600, H: 400}
	if in := fitInside(edge, work); in != (fitRect{X: -600, Y: 640, W: 600, H: 400}) {
		t.Errorf("moved inside to %+v", in)
	}
}
