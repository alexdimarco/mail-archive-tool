//go:build windows

package app

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// classifyView classifies how abs is reached from the canonical path of its opened
// handle (GetFinalPathNameByHandleW) and its volume's drive type (design §4.7,
// RR-2b re-verification). A filesystem type cannot tell a share from a local disk
// (GetVolumeInformationW reports a share's own type, NTFS from a Windows server and
// NTFS by default from Samba), so the type is never what decides: a
// `\\?\UNC\<server>\<share>\…` path is a share on <server>, and so is a volume whose
// root GetDriveTypeW reports as DRIVE_REMOTE. A remote view is never classified local.
func classifyView(abs string) ceView {
	f, err := os.Open(abs)
	if err != nil {
		return ceView{Kind: viewStacking}
	}
	defer f.Close()
	var final string
	if rerr := rawControl(f, func(h windows.Handle) {
		buf := make([]uint16, 1024)
		n, ferr := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if ferr == nil && int(n) <= len(buf) {
			final = windows.UTF16ToString(buf[:n])
		}
	}); rerr != nil || final == "" {
		return ceView{Kind: viewStacking}
	}
	if rest, ok := strings.CutPrefix(final, `\\?\UNC\`); ok {
		server, tail, _ := strings.Cut(rest, `\`)
		share, _, _ := strings.Cut(tail, `\`)
		server = strings.ToLower(server)
		return ceView{Kind: viewShare, Server: server, Mount: `\\` + server + `\` + strings.ToLower(share), Type: "unc"}
	}
	root := strings.TrimPrefix(final, `\\?\`)
	if len(root) >= 2 && root[1] == ':' {
		rp, perr := windows.UTF16PtrFromString(root[:2] + `\`)
		if perr == nil && windows.GetDriveType(rp) == windows.DRIVE_REMOTE {
			return ceView{Kind: viewShare, Type: "remote"} // a remote volume whose server cannot be named
		}
		return ceView{Kind: viewLocal, Type: "local"}
	}
	return ceView{Kind: viewStacking}
}
