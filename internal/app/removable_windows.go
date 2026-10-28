//go:build windows

package app

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// removableDrive says abs is on a drive that can leave this computer (K5): Windows
// calls its volume removable (a USB stick or card) or optical, or calls it fixed but
// its medium is marked removable or it hangs off a USB or FireWire bus — an external
// disk, which Windows reports as fixed (the P3-8 Windows lab, B31). A fixed disk on
// the SD or MMC bus is not counted: that is the soldered-in eMMC many small laptops
// and tablets start from, while an SD card in a reader is reported removable (the
// P3-8 fixes' pass, WINACL-2). The volume is the one mounted where abs is, so a
// removable volume mounted inside a folder on C: is judged as itself, not as C:. A
// disk on another external bus (Thunderbolt NVMe, eSATA) cannot be told from an
// internal one here and is not caught.
func removableDrive(abs string) bool { return volumes.leaves(abs) }

// volumeOS is what removableDrive asks Windows; a var so a test can stand in drives
// the lab cannot attach (a folder-mounted volume, an external USB disk).
type volumeOS struct {
	// mountOf is the root of the volume holding abs: `C:\`, or the folder it is
	// mounted on, such as `C:\mnt\usb\`.
	mountOf func(abs string) (string, error)
	// driveType is GetDriveType of a volume root.
	driveType func(root string) uint32
	// bus is the storage bus the volume at root hangs off, and whether its medium is
	// removable.
	bus func(root string) (busType uint32, removableMedia bool, err error)
}

var volumes = volumeOS{mountOf: volumeMountOf, driveType: driveTypeOf, bus: storageBus}

// leaves says the volume holding abs can leave this computer. A path whose volume
// cannot be read is not called removable; the copy's other checks refuse it.
func (v volumeOS) leaves(abs string) bool {
	root, err := v.mountOf(abs)
	if err != nil || root == "" {
		return false
	}
	switch v.driveType(root) {
	case windows.DRIVE_REMOVABLE, windows.DRIVE_CDROM:
		return true
	case windows.DRIVE_FIXED:
		bt, removableMedia, err := v.bus(root)
		return err == nil && (removableMedia || busLeaves(bt))
	}
	return false
}

// The storage buses (STORAGE_BUS_TYPE) named here: the two a fixed disk that leaves
// with its owner hangs off, and the SD and MMC buses, which a fixed disk sits on when
// it is the computer's own soldered-in eMMC.
const (
	busType1394 = 0x4
	busTypeUsb  = 0x7
	busTypeSd   = 0xC
	busTypeMmc  = 0xD
)

// busLeaves says a fixed disk on bus bt is external: USB or FireWire. Not SD or MMC,
// whose fixed disks are internal eMMC (WINACL-2).
func busLeaves(bt uint32) bool {
	switch bt {
	case busTypeUsb, busType1394:
		return true
	}
	return false
}

// volumeMountOf is the root of the volume holding abs (GetVolumePathName), asked of
// its nearest folder that exists, since the copy's folder may not be made yet.
func volumeMountOf(abs string) (string, error) {
	p := filepath.Clean(abs)
	for {
		if _, err := os.Lstat(p); err == nil {
			break
		}
		up := filepath.Dir(p)
		if up == p {
			break
		}
		p = up
	}
	in, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	if err := windows.GetVolumePathName(in, &buf[0], uint32(len(buf))); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf), nil
}

// driveTypeOf is GetDriveType of the volume root.
func driveTypeOf(root string) uint32 {
	rp, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return windows.DRIVE_UNKNOWN
	}
	return windows.GetDriveType(rp)
}

// ioctlStorageQueryProperty is IOCTL_STORAGE_QUERY_PROPERTY.
const ioctlStorageQueryProperty = 0x2D1400

// storagePropertyQuery is STORAGE_PROPERTY_QUERY asking for the device descriptor
// (StorageDeviceProperty, PropertyStandardQuery: both zero).
type storagePropertyQuery struct {
	PropertyID           uint32
	QueryType            uint32
	AdditionalParameters [1]byte
}

// storageBus asks the disk under the volume at root for its STORAGE_DEVICE_DESCRIPTOR:
// RemovableMedia at byte 10, BusType at byte 28. The volume is opened for no access
// at all, which needs no administrator rights.
func storageBus(root string) (uint32, bool, error) {
	rp, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, false, err
	}
	name := make([]uint16, 64)
	if err := windows.GetVolumeNameForVolumeMountPoint(rp, &name[0], uint32(len(name))); err != nil {
		return 0, false, err
	}
	vol, err := windows.UTF16PtrFromString(strings.TrimSuffix(windows.UTF16ToString(name), `\`))
	if err != nil {
		return 0, false, err
	}
	h, err := windows.CreateFile(vol, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return 0, false, err
	}
	defer windows.CloseHandle(h)
	var q storagePropertyQuery
	out := make([]byte, 1024)
	var n uint32
	if err := windows.DeviceIoControl(h, ioctlStorageQueryProperty, (*byte)(unsafe.Pointer(&q)), uint32(unsafe.Sizeof(q)), &out[0], uint32(len(out)), &n, nil); err != nil {
		return 0, false, err
	}
	if n < 32 {
		return 0, false, windows.ERROR_INSUFFICIENT_BUFFER
	}
	return binary.LittleEndian.Uint32(out[28:32]), out[10] != 0, nil
}
