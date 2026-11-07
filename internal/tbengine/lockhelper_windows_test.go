//go:build windows

package tbengine

import "golang.org/x/sys/windows"

// takeThunderbirdLock opens parent.lock as Thunderbird opens it (read/write, no
// sharing; OPEN_ALWAYS so the helper never truncates), and lets it go.
func takeThunderbirdLock(path string) string {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "error"
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return "refused"
	}
	windows.CloseHandle(h)
	return "took"
}
