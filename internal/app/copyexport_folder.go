package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/util"
)

// Create folder (design §4.8, W1 of §5.1): exactly one new, sanitized directory under
// an existing destination, made through the handle on the parent that the checks
// opened, so a parent swapped for a symlink, junction or bind mount after the check
// cannot move the new directory into the archive.

// refusalQ is R-q (design §7.6).
func refusalQ(name, why string) *CopyRefusal {
	if why == "exists" {
		return refuse("R-q", fmt.Sprintf("A folder named '%s' already exists here — choose it with Browse, or use another name.", name))
	}
	return refuse("R-q", fmt.Sprintf("'%s' can't be a folder name (%s) — try '%s'.", name, why, suggestFolderName(name)))
}

// suggestFolderName is a name that is safe as it stands: the illegal characters
// become spaces, the whitespace is tidied and a leading or trailing dot is dropped.
func suggestFolderName(name string) string {
	var b strings.Builder
	for _, r := range util.StripControl(name) {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	s := strings.Trim(strings.Join(strings.Fields(b.String()), " "), " .")
	if s == "" {
		return "New folder"
	}
	if safe := util.SanitizeSegment(s); safe != s {
		return strings.SplitN(safe, "~", 2)[0]
	}
	return s
}

// folderNameWhy says why name is not a folder name this computer can use, or "" when it is.
func folderNameWhy(name string) string {
	switch {
	case name == "":
		return "it is empty"
	case name == "." || name == "..":
		return "it names a folder that already exists"
	case strings.ContainsAny(name, `/\`):
		return "it contains '" + string(name[strings.IndexAny(name, `/\`)]) + "'"
	case name != util.SanitizeSegment(name):
		return "this computer can't use it as a folder name"
	}
	return ""
}

// CreateFolder makes the folder name under the existing folder parent and returns its
// path. parent must be a folder outside every archive by path and identity (R-f, R-f2),
// because its mkdir is a write too (RR2-C13). name must be a single sanitized segment
// (R-q otherwise, with the sanitized suggestion); an existing name is R-q. The handle
// on parent is opened first, the checks then read that same folder, and the directory
// is made through the handle at 0700, with no path walked again between the check and
// the mkdir. Nothing is created on a refusal.
func (e *CopyExportEnv) CreateFolder(parent, name string, archives []string) (string, error) {
	return e.CreateFolderOther(parent, name, archives, "")
}

// CreateFolderOther is CreateFolder where one of the archives is another archive folder
// the workspace has open as its source [ruling W1]: a parent inside it, or an alias of it, is
// refused too.
func (e *CopyExportEnv) CreateFolderOther(parent, name string, archives []string, other string) (string, error) {
	if err := checkDestPath(parent); err != nil {
		return "", err
	}
	if why := folderNameWhy(name); why != "" {
		return "", refusalQ(printable(name), why)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return "", destMissing(parent)
	}
	defer root.Close()
	if _, err := e.CheckDestinationOther(parent, archives, other); err != nil {
		return "", err
	}
	// The checks read the folder by path: it must be the folder the handle holds.
	hf, herr := root.Open(".")
	if herr != nil {
		return "", destMissing(parent)
	}
	hid, _ := readIdentity(hf)
	hf.Close()
	if ids, cerr := e.destChain(parent); cerr != nil || len(ids) == 0 || dirKeyOf(ids[0]) != dirKeyOf(hid) {
		return "", refuse("R-e", fmt.Sprintf("The folder %s changed while MailArchive was checking it — nothing was created. Choose it again.", printable(parent)))
	}
	e.step("W1check")
	if err := root.Mkdir(name, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", refusalQ(name, "exists")
		}
		return "", refusalW(parent, leaveName(err))
	}
	e.step("W1")
	return filepath.Join(parent, name), nil
}
