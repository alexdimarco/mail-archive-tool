package zenity

import (
	"context"

	"github.com/ncruces/zenity/internal/win"
)

func message(kind messageKind, text string, opts options) error {
	var flags uint32

	switch {
	case kind == questionKind && opts.extraButton != nil:
		flags |= win.MB_YESNOCANCEL
	case kind == questionKind:
		flags |= win.MB_OKCANCEL
	case opts.extraButton != nil:
		flags |= win.MB_YESNO
	default:
		opts.cancelLabel = opts.okLabel
	}

	switch opts.icon {
	case ErrorIcon:
		flags |= win.MB_ICONERROR
	case QuestionIcon:
		flags |= win.MB_ICONQUESTION
	case WarningIcon:
		flags |= win.MB_ICONWARNING
	case InfoIcon:
		flags |= win.MB_ICONINFORMATION
	case NoIcon:
		//
	default:
		switch kind {
		case errorKind:
			flags |= win.MB_ICONERROR
		case questionKind:
			flags |= win.MB_ICONQUESTION
		case warningKind:
			flags |= win.MB_ICONWARNING
		case infoKind:
			flags |= win.MB_ICONINFORMATION
		}
	}

	if kind == questionKind && opts.defaultCancel {
		if opts.extraButton == nil {
			flags |= win.MB_DEFBUTTON2
		} else {
			flags |= win.MB_DEFBUTTON3
		}
	}

	owner, _ := opts.attach.(win.HWND)
	defer setup(owner)()
	unhook, err := hookMessageDialog(opts)
	if err != nil {
		return err
	}
	defer unhook()

	var title *uint16
	if opts.title != nil {
		title = strptr(*opts.title)
	}

	s, err := win.MessageBox(owner, strptr(text), title, flags)

	if opts.ctx != nil && opts.ctx.Err() != nil {
		return opts.ctx.Err()
	}
	switch s {
	case win.IDOK, win.IDYES:
		return nil
	case win.IDCANCEL:
		return ErrCanceled
	case win.IDNO:
		return ErrExtraButton
	default:
		return err
	}
}

func hookMessageDialog(opts options) (context.CancelFunc, error) {
	var init func(wnd win.HWND)
	var icon icon
	if opts.okLabel != nil || opts.cancelLabel != nil || opts.extraButton != nil || opts.icon != nil {
		init = func(wnd win.HWND) {
			if opts.okLabel != nil {
				win.SetDlgItemText(wnd, win.IDOK, strptr(quoteAccelerators(*opts.okLabel)))
				win.SetDlgItemText(wnd, win.IDYES, strptr(quoteAccelerators(*opts.okLabel)))
			}
			if opts.cancelLabel != nil {
				win.SetDlgItemText(wnd, win.IDCANCEL, strptr(quoteAccelerators(*opts.cancelLabel)))
			}
			if opts.extraButton != nil {
				win.SetDlgItemText(wnd, win.IDNO, strptr(quoteAccelerators(*opts.extraButton)))
			}

			if icon.handle != 0 {
				ctl, _ := win.GetDlgItem(wnd, win.IDC_STATIC_OK)
				win.SendMessage(ctl, win.STM_SETICON, uintptr(icon.handle), 0)
			}

			// MailArchive's patch: renamed buttons as wide as their labels (fit.go).
			if opts.okLabel != nil || opts.cancelLabel != nil || opts.extraButton != nil {
				fitMessageBox(wnd)
			}
		}
	}
	icon, err := getIcon(opts.icon)
	if err != nil {
		return nil, err
	}
	unhook, err := hookDialog(opts.ctx, opts.windowIcon, nil, init)
	if err != nil {
		icon.delete()
		return nil, err
	}
	return func() {
		icon.delete()
		unhook()
	}, nil
}

// fitMessageBox widens a message box's renamed buttons to their labels (MailArchive's
// patch, fit.go; design §4.3, Message boxes). It reads every button of the box's row,
// renamed or not, with the font and label each draws, and changes nothing unless every
// read succeeds and a label needs more than its button. Then every button becomes as
// wide as the widest need, the row keeps Windows' gap and right margin, and the box grows
// to the right by what the row lacks, re-centred about its old centre in its monitor's
// work area, never past the cap. The box's own text is Windows' layout, untouched. The
// dialog hook runs it again after the box handles WM_DPICHANGED.
func fitMessageBox(wnd win.HWND) {
	// Inside MessageBox's WM_INITDIALOG, the lab read every rectangle at 2/3 of its size
	// at 150 % (113x40 buttons as 75x27) while the label measured true, and placed buttons
	// 1.5 times too wide: read and place in physical pixels here, as the labels are measured.
	if old, _ := win.SetThreadDpiAwarenessContext(win.DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2); old != 0 {
		defer win.SetThreadDpiAwarenessContext(old)
	}
	type button struct {
		hwnd win.HWND
		r    fitRect
		w    int
	}
	var row []button
	for _, id := range []int{win.IDOK, win.IDYES, win.IDNO, win.IDCANCEL} {
		btn, err := win.GetDlgItem(wnd, id)
		if err != nil || btn == 0 {
			continue
		}
		var r win.RECT
		if win.GetWindowRect(btn, &r) != nil {
			return
		}
		pts := [2]win.POINT{{X: r.Left, Y: r.Top}, {X: r.Right, Y: r.Bottom}}
		win.MapWindowPoints(0, wnd, &pts[0], 2)
		font := win.Handle(win.SendMessage(btn, win.WM_GETFONT, 0, 0))
		w, _, ok := fitText{wnd: wnd, font: font}.label(win.GetWindowText(btn))
		if !ok {
			return
		}
		row = append(row, button{hwnd: btn, w: w,
			r: fitRect{X: int(pts[0].X), Y: int(pts[0].Y), W: int(pts[1].X - pts[0].X), H: int(pts[1].Y - pts[0].Y)}})
	}
	for i := 1; i < len(row); i++ { // left to right
		for j := i; j > 0 && row[j].r.X < row[j-1].r.X; j-- {
			row[j], row[j-1] = row[j-1], row[j]
		}
	}
	var client win.RECT
	work := fitWorkArea(wnd)
	if len(row) == 0 || win.GetClientRect(wnd, &client) != nil || work.W <= 0 {
		return
	}
	box := fitWindowRect(wnd)
	clientW := int(client.Right - client.Left)
	rects := make([]fitRect, len(row))
	labels := make([]int, len(row))
	for i, b := range row {
		rects[i], labels[i] = b.r, b.w
	}
	out, grow, ok := fitMessageRow(rects, labels, clientW, work.W*8/10-(box.W-clientW), int(getDPI(wnd)))
	if !ok {
		return
	}
	if grow > 0 {
		fitPlace(wnd, fitAbout(box, fitSize{W: box.W + grow, H: box.H}, work))
	}
	for i, b := range row {
		fitPlace(b.hwnd, out[i])
	}
}
