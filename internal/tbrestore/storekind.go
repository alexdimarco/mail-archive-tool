package tbrestore

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The store kinds' contract ids (design §3.3; the mbox one seen in the Windows lab).
const (
	storeMbox    = "@mozilla.org/msgstore/berkeleystore;1"
	storeMaildir = "@mozilla.org/msgstore/maildirstore;1"
)

// CheckStoreKind refuses, W-MAILDIR, a Local Folders Thunderbird keeps as maildir
// (design §3.3, Q5's recommended answer): by the account's storeContractID, else the
// profile-wide mail.serverDefaultStoreContractID, else mbox; and, as a second signal,
// any folder directly in Local Folders that holds cur, new or tmp, or an Inbox or Trash
// that is a folder (RT-20). Any maildir signal refuses, and so does a contract id that
// is neither kind (MailArchive writes only what it knows Thunderbird reads). Nothing is
// written before this check.
func CheckStoreKind(p *Prefs, lf LocalFolders) error {
	kind, _, err := p.Get("mail.server." + lf.ServerKey + ".storeContractID")
	if err == nil && kind == "" {
		kind, _, err = p.Get("mail.serverDefaultStoreContractID")
	}
	if err != nil {
		return prefsRefusal(err)
	}
	if kind != "" && kind != storeMbox {
		return refuse("W-MAILDIR", wMaildir, nil)
	}
	entries, err := os.ReadDir(lf.Dir)
	if err != nil {
		return refuse("W-LOCALGONE", fill(wLocalGone, "PATH", lf.Dir), err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.EqualFold(name, "Inbox") || strings.EqualFold(name, "Trash") {
			return refuse("W-MAILDIR", wMaildir, nil)
		}
		if strings.HasSuffix(name, ".sbd") {
			continue // a folder's subfolders, as mbox keeps them
		}
		for _, sub := range []string{"cur", "new", "tmp"} {
			if fi, err := os.Lstat(filepath.Join(lf.Dir, name, sub)); err == nil && fi.IsDir() {
				return refuse("W-MAILDIR", wMaildir, nil)
			}
		}
	}
	return nil
}

// LastVersion is the Thunderbird version that last opened the profile, from
// compatibility.ini's LastVersion= ("153.6.0_20251212000000/…"), and its major number;
// ok is false when it cannot be read (design §4.1).
func LastVersion(profileDir string) (version string, major int, ok bool) {
	data, err := readBounded(filepath.Join(profileDir, "compatibility.ini"), 64<<10)
	if err != nil {
		return "", 0, false
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		k, v, found := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !found || strings.TrimSpace(k) != "LastVersion" {
			continue
		}
		v = shownName(strings.TrimSpace(v))
		ver := v
		if i := strings.IndexAny(ver, "_/"); i >= 0 {
			ver = ver[:i]
		}
		head := ver
		if i := strings.IndexByte(head, '.'); i >= 0 {
			head = head[:i]
		}
		n, err := strconv.Atoi(head)
		if err != nil {
			return "", 0, false
		}
		return ver, n, true
	}
	return "", 0, false
}

// OldThunderbirdWarning is W-OLDTB when the profile was last opened by a Thunderbird
// older than 122, whose reader does not undo mboxrd's quoting (§4.1); "" otherwise.
func OldThunderbirdWarning(profileDir string) string {
	ver, major, ok := LastVersion(profileDir)
	if !ok || major >= 122 {
		return ""
	}
	return fill(wOldTB, "VERSION", ver)
}
