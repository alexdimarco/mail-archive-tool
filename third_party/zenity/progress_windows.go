package zenity

import (
	"context"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ncruces/zenity/internal/win"
)

func progress(opts options) (ProgressDialog, error) {
	if opts.title == nil {
		opts.title = ptr("")
	}
	if opts.okLabel == nil {
		opts.okLabel = ptr("OK")
	}
	if opts.cancelLabel == nil {
		opts.cancelLabel = ptr("Cancel")
	}
	if opts.maxValue == 0 {
		opts.maxValue = 100
	}
	if opts.ctx == nil {
		opts.ctx = context.Background()
	} else if cerr := opts.ctx.Err(); cerr != nil {
		return nil, cerr
	}

	dlg := &progressDialog{
		done:  make(chan struct{}),
		max:   opts.maxValue,
		close: opts.autoClose,
	}
	dlg.init.Add(1)

	go func() {
		dlg.err = dlg.setup(opts)
		close(dlg.done)
	}()

	dlg.init.Wait()
	return dlg, nil
}

type progressDialog struct {
	init  sync.WaitGroup
	done  chan struct{}
	err   error
	max   int
	close bool

	wnd       win.HWND
	textCtl   win.HWND
	progCtl   win.HWND
	okBtn     win.HWND
	cancelBtn win.HWND
	extraBtn  win.HWND
	font      font

	// MailArchive's patch: the dialog fits its words (fit.go). Only the dialog's own
	// thread reads or writes these.
	text   string    // the text last laid out
	floor  int       // the prompt's height so far: it never shrinks while the dialog is open
	dpi    dpi       // the DPI of the last layout
	labels []string  // the buttons' labels, left to right
	fit    fitDialog // the layout last applied; Upstream keeps upstream's layout
}

func (d *progressDialog) Text(text string) error {
	select {
	default:
		win.SetWindowText(d.textCtl, strptr(text))
		// MailArchive's patch: the dialog lays itself out again, on its own thread.
		win.SendMessage(d.wnd, fitTextChanged, 0, 0)
		return nil
	case <-d.done:
		return d.err
	}
}

func (d *progressDialog) Value(value int) error {
	if value >= d.max && d.close {
		return d.Close()
	}
	select {
	default:
		win.SendMessage(d.progCtl, win.PBM_SETPOS, uintptr(value), 0)
		if value >= d.max {
			win.EnableWindow(d.okBtn, true)
		}
		return nil
	case <-d.done:
		return d.err
	}
}

func (d *progressDialog) MaxValue() int {
	return d.max
}

func (d *progressDialog) Done() <-chan struct{} {
	return d.done
}

func (d *progressDialog) Complete() error {
	select {
	default:
		win.SetWindowLong(d.progCtl, win.GWL_STYLE, win.WS_CHILD|win.WS_VISIBLE|win.PBS_SMOOTH)
		win.SendMessage(d.progCtl, win.PBM_SETRANGE32, 0, 1)
		win.SendMessage(d.progCtl, win.PBM_SETPOS, 1, 0)
		win.EnableWindow(d.okBtn, true)
		win.EnableWindow(d.cancelBtn, false)
		return nil
	case <-d.done:
		return d.err
	}
}

func (d *progressDialog) Close() error {
	win.SendMessage(d.wnd, win.WM_SYSCOMMAND, win.SC_CLOSE, 0)
	<-d.done
	if d.err == ErrCanceled {
		return nil
	}
	return d.err
}

func (dlg *progressDialog) setup(opts options) error {
	var once sync.Once
	defer once.Do(dlg.init.Done)

	owner, _ := opts.attach.(win.HWND)
	defer setup(owner)()
	dlg.font = getFont()
	defer dlg.font.delete()
	icon, _ := getIcon(opts.windowIcon)
	defer icon.delete()

	if opts.ctx != nil && opts.ctx.Err() != nil {
		return opts.ctx.Err()
	}

	instance, err := win.GetModuleHandle(nil)
	if err != nil {
		return err
	}

	cls, err := registerClass(instance, icon.handle, syscall.NewCallback(progressProc))
	if err != nil {
		return err
	}
	defer win.UnregisterClass(cls, instance)

	dlg.wnd, _ = win.CreateWindowEx(_WS_EX_ZEN_DIALOG,
		cls, strptr(*opts.title), _WS_ZEN_DIALOG,
		win.CW_USEDEFAULT, win.CW_USEDEFAULT,
		281, 133, owner, 0, instance, unsafe.Pointer(dlg))

	// MailArchive's patch: decide, before the controls exist, whether this dialog can be
	// laid out to fit its words; if not, it keeps upstream's layout and label style.
	var okLabel, cancelLabel *string
	if !opts.noCancel || !opts.autoClose {
		okLabel = opts.okLabel
	}
	if !opts.noCancel {
		cancelLabel = opts.cancelLabel
	}
	dlg.labels = fitButtonLabels(okLabel, opts.extraButton, cancelLabel)
	dlg.fit = dlg.measure(getDPI(dlg.wnd))
	var labelStyle uint32 = _WS_ZEN_LABEL
	if !dlg.fit.Upstream {
		labelStyle = _WS_ZEN_FIT_LABEL
	}

	dlg.textCtl, _ = win.CreateWindowEx(0,
		strptr("STATIC"), nil, labelStyle,
		12, 10, 241, 16, dlg.wnd, 0, instance, nil)

	var flags uint32 = win.WS_CHILD | win.WS_VISIBLE | win.PBS_SMOOTH
	if opts.maxValue < 0 {
		flags |= win.PBS_MARQUEE
	}
	dlg.progCtl, _ = win.CreateWindowEx(0,
		strptr(win.PROGRESS_CLASS),
		nil, flags,
		12, 30, 241, 16, dlg.wnd, 0, instance, nil)

	if !opts.noCancel || !opts.autoClose {
		dlg.okBtn, _ = win.CreateWindowEx(0,
			strptr("BUTTON"), strptr(quoteAccelerators(*opts.okLabel)),
			_WS_ZEN_BUTTON|win.BS_DEFPUSHBUTTON|win.WS_DISABLED,
			12, 58, 75, 24, dlg.wnd, win.IDOK, instance, nil)
	}
	if !opts.noCancel {
		dlg.cancelBtn, _ = win.CreateWindowEx(0,
			strptr("BUTTON"), strptr(quoteAccelerators(*opts.cancelLabel)),
			_WS_ZEN_BUTTON,
			12, 58, 75, 24, dlg.wnd, win.IDCANCEL, instance, nil)
	}
	if opts.extraButton != nil {
		dlg.extraBtn, _ = win.CreateWindowEx(0,
			strptr("BUTTON"), strptr(quoteAccelerators(*opts.extraButton)),
			_WS_ZEN_BUTTON,
			12, 58, 75, 24, dlg.wnd, win.IDNO, instance, nil)
	}

	dlg.layout(getDPI(dlg.wnd))
	if dlg.fit.Upstream {
		centerWindow(dlg.wnd)
	} else {
		fitCenterWindow(dlg.wnd)
	}
	win.ShowWindow(dlg.wnd, win.SW_NORMAL)
	if opts.maxValue < 0 {
		win.SendMessage(dlg.progCtl, win.PBM_SETMARQUEE, 1, 0)
	} else {
		win.SendMessage(dlg.progCtl, win.PBM_SETRANGE32, 0, uintptr(opts.maxValue))
	}
	once.Do(dlg.init.Done)

	if opts.ctx != nil && opts.ctx.Done() != nil {
		wait := make(chan struct{})
		defer close(wait)
		go func() {
			select {
			case <-opts.ctx.Done():
				win.SendMessage(dlg.wnd, win.WM_SYSCOMMAND, win.SC_CLOSE, 0)
			case <-wait:
			}
		}()
	}

	if err := win.MessageLoop(win.HWND(dlg.wnd)); err != nil {
		return err
	}
	if opts.ctx != nil && opts.ctx.Err() != nil {
		return opts.ctx.Err()
	}
	return dlg.err
}

func (d *progressDialog) layout(dpi dpi) {
	font := d.font.forDPI(dpi)
	win.SendMessage(d.textCtl, win.WM_SETFONT, font, 1)
	win.SendMessage(d.okBtn, win.WM_SETFONT, font, 1)
	win.SendMessage(d.cancelBtn, win.WM_SETFONT, font, 1)
	win.SendMessage(d.extraBtn, win.WM_SETFONT, font, 1)
	if !d.fit.Upstream {
		// MailArchive's patch: lay out at this DPI, the prompt's height so far re-based
		// to it; a layout that cannot be measured keeps the last one.
		if d.dpi != 0 && d.dpi != dpi {
			d.floor = d.floor * int(dpi) / int(d.dpi)
		}
		d.dpi = dpi
		if f := d.measure(dpi); !f.Upstream {
			d.fit = f
			d.floor = f.PromptH
			fitResize(d.wnd, f.Window)
			d.apply(f)
		}
		return
	}
	win.SetWindowPos(d.wnd, 0, 0, 0, dpi.scale(281), dpi.scale(133), win.SWP_NOMOVE|win.SWP_NOZORDER)
	win.SetWindowPos(d.textCtl, 0, dpi.scale(12), dpi.scale(10), dpi.scale(241), dpi.scale(16), win.SWP_NOZORDER)
	win.SetWindowPos(d.progCtl, 0, dpi.scale(12), dpi.scale(30), dpi.scale(241), dpi.scale(16), win.SWP_NOZORDER)

	pos := 178
	if d.cancelBtn != 0 {
		win.SetWindowPos(d.cancelBtn, 0, dpi.scale(pos), dpi.scale(58), dpi.scale(75), dpi.scale(24), win.SWP_NOZORDER)
		pos -= 83
	}
	if d.extraBtn != 0 {
		win.SetWindowPos(d.extraBtn, 0, dpi.scale(pos), dpi.scale(58), dpi.scale(75), dpi.scale(24), win.SWP_NOZORDER)
		pos -= 83
	}
	if d.okBtn != 0 {
		win.SetWindowPos(d.okBtn, 0, dpi.scale(pos), dpi.scale(58), dpi.scale(75), dpi.scale(24), win.SWP_NOZORDER)
		pos -= 83
	}

	if pos == 178 {
		win.SetWindowPos(d.wnd, 0, 0, 0, dpi.scale(281), dpi.scale(97), win.SWP_NOMOVE|win.SWP_NOZORDER)
	}
}

// measure lays the dialog out to fit its words at dpi (MailArchive's patch, fit.go).
func (d *progressDialog) measure(dpi dpi) fitDialog {
	m := fitText{wnd: d.wnd, font: win.Handle(d.font.forDPI(dpi))}
	return fitProgress(fitEnvironment(d.wnd, dpi, m), m, d.text, d.floor, d.labels)
}

// apply places the controls as f says, never activating any (MailArchive's patch).
func (d *progressDialog) apply(f fitDialog) {
	fitPlace(d.textCtl, f.Prompt)
	fitPlace(d.progCtl, f.Control)
	var buttons []win.HWND
	for _, b := range []win.HWND{d.okBtn, d.extraBtn, d.cancelBtn} {
		if b != 0 {
			buttons = append(buttons, b)
		}
	}
	for i, b := range buttons {
		if i < len(f.Buttons) {
			fitPlace(b, f.Buttons[i])
		}
	}
}

// relayout fits the dialog to the text Text just set, on the dialog's own thread
// (MailArchive's patch). The window keeps its top-left corner and grows downward,
// moving up only as far as it must to stay inside its work area, and never shrinks.
func (d *progressDialog) relayout() {
	if d.fit.Upstream {
		return
	}
	text := win.GetWindowText(d.textCtl)
	if text == d.text {
		return
	}
	d.text = text
	f := d.measure(d.dpi)
	if f.Upstream {
		return
	}
	d.fit = f
	d.floor = f.PromptH
	fitPlace(d.wnd, fitGrow(fitWindowRect(d.wnd), f.Window, fitWorkArea(d.wnd)))
	d.apply(f)
}

func progressProc(wnd win.HWND, msg uint32, wparam uintptr, lparam *unsafe.Pointer) uintptr {
	var dlg *progressDialog
	switch msg {
	case win.WM_NCCREATE:
		saveBackRef(uintptr(wnd), *lparam)
		dlg = (*progressDialog)(*lparam)
	case win.WM_NCDESTROY:
		deleteBackRef(uintptr(wnd))
	default:
		dlg = (*progressDialog)(loadBackRef(uintptr(wnd)))
	}

	switch msg {
	case win.WM_DESTROY:
		win.PostQuitMessage(0)

	case win.WM_CLOSE:
		dlg.err = ErrCanceled
		win.DestroyWindow(wnd)

	case win.WM_COMMAND:
		switch wparam {
		default:
			return 1
		case win.IDOK, win.IDYES:
			//
		case win.IDCANCEL:
			dlg.err = ErrCanceled
		case win.IDNO:
			dlg.err = ErrExtraButton
		}
		win.DestroyWindow(wnd)

	case win.WM_DPICHANGED:
		dlg.layout(dpi(uint32(wparam) >> 16))
		if !dlg.fit.Upstream {
			fitSuggested(wnd, (*win.RECT)(unsafe.Pointer(lparam)))
		}

	case fitTextChanged:
		dlg.relayout()

	default:
		return win.DefWindowProc(wnd, msg, wparam, unsafe.Pointer(lparam))
	}

	return 0
}
