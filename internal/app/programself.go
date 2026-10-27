package app

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// A self-copy is this computer's own program (program design §1, R14): on a Mac the
// running .app bundle zipped — Unix modes recorded, links stored as links — when the
// program runs from one, else the bare program; on Windows the console
// mailarchive.exe beside the running program, checked to be the same build. It is
// written only while the file on disk is still the one this process started from.

// selfAtStart is the running program's file as the process started (R14).
var selfAtStart = func() (s struct {
	path string
	fi   os.FileInfo
}) {
	if exe, err := os.Executable(); err == nil {
		s.path = util.ResolveExisting(exe)
		fi, _ := os.Stat(s.path)
		s.fi = util.PinIdentity(fi) // the file as the process started, compared later (B1)
	}
	return
}()

// selfExecutable is the running program's path and its identity at start; a var so a
// test can stand a file in for it.
var selfExecutable = func() (string, os.FileInfo) { return selfAtStart.path, selfAtStart.fi }

// UseTestProgram stands the file at path in for this program's own file in a test
// binary's whole run (another package's TestMain), so a test's self-copy is a small
// file rather than the test binary. Never called outside tests.
func UseTestProgram(path string) {
	fi, _ := os.Stat(path)
	fi = util.PinIdentity(fi)
	selfExecutable = func() (string, os.FileInfo) { return path, fi }
}

// appBundle is the .app bundle exe runs from, or "".
func appBundle(exe string) string {
	for d := filepath.Dir(exe); d != filepath.Dir(d); d = filepath.Dir(d) {
		if strings.HasSuffix(strings.ToLower(d), ".app") && strings.EqualFold(filepath.Base(filepath.Dir(exe)), "MacOS") {
			return d
		}
	}
	return ""
}

// selfPlatformAndName is this computer's <os>-<arch> and the name its self-copy takes.
func selfPlatformAndName() (string, string) {
	plat := runtime.GOOS + "-" + runtime.GOARCH
	name := "mailarchive-self-" + plat
	exe, _ := selfExecutable()
	switch {
	case runtime.GOOS == "windows":
		name += ".exe"
	case runtime.GOOS == "darwin" && appBundle(exe) != "":
		name += ".zip"
	}
	return plat, name
}

// selfSource is the file a self-copy is made from, checked to be the one this process
// started from (R14): on Windows the console sibling of the same build.
func selfSource() (string, error) {
	exe, start := selfExecutable()
	if exe == "" || start == nil {
		return "", errors.New("this program's own file could not be found")
	}
	now, err := os.Stat(exe)
	if err != nil || !os.SameFile(now, start) {
		return "", errors.New("this program's file was replaced since it started")
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Base(exe), "mailarchive.exe") {
		sib := filepath.Join(filepath.Dir(exe), "mailarchive.exe")
		if ok, _ := schedule.SiblingQualifies(exe, sib); !ok {
			return "", errors.New("mailarchive.exe beside this program is missing or is not the same build")
		}
		return sib, nil
	}
	return exe, nil
}

// writeSelfCopy writes this computer's self-copy to w: the bundle zipped, or the bare
// program.
func writeSelfCopy(w io.Writer, name string) error {
	src, err := selfSource()
	if err != nil {
		return err
	}
	if strings.HasSuffix(name, ".zip") {
		b := appBundle(src)
		if b == "" {
			return errors.New("this program does not run from an app bundle")
		}
		return zipBundle(w, b)
	}
	f, err := util.OpenRegularFile(src)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// zipBundle writes the bundle at dir to w as a zip whose entries keep their Unix modes
// (0755 for executables) and whose links are stored as links (R14).
func zipBundle(w io.Writer, dir string) error {
	z := zip.NewWriter(w)
	base := filepath.Dir(dir)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			h.Name += "/"
			_, err = z.CreateHeader(h)
			return err
		case fi.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			h.Method = zip.Store
			hw, err := z.CreateHeader(h)
			if err != nil {
				return err
			}
			_, err = io.WriteString(hw, target)
			return err
		case !fi.Mode().IsRegular():
			return fmt.Errorf("%s is not a regular file", rel)
		}
		h.Method = zip.Deflate
		hw, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		f, err := util.OpenRegularFile(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(hw, f)
		return err
	})
	if err != nil {
		return err
	}
	return z.Close()
}
