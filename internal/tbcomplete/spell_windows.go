//go:build windows

package tbcomplete

import (
	"errors"
	"io/fs"
	"syscall"
)

// osSpellings gives the store's long and 8.3 short spellings (C1a pass C1A-08): Thunderbird may
// hold the profile's path in either (Windows itself hands out a short user folder in places), and
// a key written through one never equals the other part for part. Only the store's own path is
// looked up, never a key's. A path that is not there has no spellings to find (nil, nil); any
// other failure is an error, so the store reads as CantTellKeys.
func osSpellings(abs string) ([]string, error) {
	long, err := winPath(abs, syscall.GetLongPathName)
	if err == nil {
		var short string
		if short, err = winPath(abs, syscall.GetShortPathName); err == nil {
			return []string{long, short}, nil
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return nil, err
}

// winPath calls a GetLongPathName-shaped function, growing its buffer as it asks.
func winPath(p string, call func(in, out *uint16, size uint32) (uint32, error)) (string, error) {
	in, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, 300)
	for range 4 {
		n, err := call(in, &buf[0], uint32(len(buf)))
		switch {
		case err != nil:
			return "", err
		case n == 0:
			return "", errors.New("an empty spelling")
		case int(n) < len(buf):
			return syscall.UTF16ToString(buf[:n]), nil
		}
		buf = make([]uint16, n+1) // n is the size needed, its NUL included
	}
	return "", errors.New("the spelling kept growing")
}
