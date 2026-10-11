package app

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"mail-archive-tool/internal/util"
)

// The destination check (design §4.7 step 3, RR2-C13, RR-2b re-verification): an
// export is written into an existing folder that is outside the archive, by path
// and by identity. A path compare misses a folder reached by another name (a mapped
// drive beside its UNC path, a `subst` letter, a junction or mount folder, a bind
// mount of the archive or of a folder inside it), so R-f also compares file
// identities (dev+inode, or volume serial + file id) of the destination and each of
// its ancestors against the identity of every directory of the archive. Where
// identity cannot decide (two views of one computer's storage that do not keep it)
// the answer is R-f2, "couldn't confirm", and nothing is written.

// ceViewKind is how a path is reached.
type ceViewKind int

const (
	viewLocal    ceViewKind = iota // this computer's own local filesystem
	viewShare                      // an SMB or NFS share (the OS's own client)
	viewStacking                   // a FUSE, overlay or other stacking filesystem, or one the classifier does not know
)

// ceView is the classification of one path: its kind, and for a share the server and
// the share's name.
type ceView struct {
	Kind   ceViewKind
	Server string // a share's server; "" when it cannot be named
	Mount  string // names one share ("//server/share", "server:/export")
	NoID   bool   // a share whose file ids are its own (a GNOME gvfs path): never decides by identity
	Type   string // the filesystem's type, for a refusal's wording
}

// ceDirKey is a directory's identity as a map key: dev+inode, or volume serial + file id.
type ceDirKey struct {
	win  bool
	a, b uint64
}

func dirKeyOf(id ceIdentity) ceDirKey {
	if id.isWindowsID() {
		return ceDirKey{win: true, a: uint64(id.Vol), b: id.FileID}
	}
	return ceDirKey{a: id.Dev, b: id.Ino}
}

func volOf(id ceIdentity) uint64 {
	if id.isWindowsID() {
		return uint64(id.Vol)
	}
	return id.Dev
}

// The walk of an archive's directories is bounded (design §4.7): at 100,000
// directories and 10 s.
const (
	copyWalkMaxDirs = 100000
	copyWalkMaxTime = 10 * time.Second
)

// ceArchiveSet is the identity of every directory of one archive, collected by a
// no-follow walk of directories only, and how the archive itself is reached.
type ceArchiveSet struct {
	Root     string
	Keys     map[ceDirKey]bool
	Vols     map[uint64]bool // the devices or volumes the archive's directories sit on
	Complete bool            // false when the walk passed its bound or met a directory it could not read
	Why      string          // what stopped it: "too large", "unreadable"
	View     ceView
}

// CopyOutside is what the preview learned about the destination's place against the
// archives it must stay outside of: the sets Start compares the destination's identity
// against again (design §4.7: "the destination walked again, against the set the
// preview collected").
type CopyOutside struct {
	archives []string
	sets     []*ceArchiveSet
}

func (e *CopyExportEnv) walkLimits() (int, time.Duration) {
	max, limit := copyWalkMaxDirs, copyWalkMaxTime
	if e.walkDirs > 0 {
		max = e.walkDirs
	}
	if e.walkTime > 0 {
		limit = e.walkTime
	}
	return max, limit
}

// walkArchive collects the identity of every directory under root. It never follows
// a link, opens directories only, and is bounded: a walk that passes its bound or
// meets a directory it cannot read is marked incomplete, and the caller decides
// whether the set had to decide.
func (e *CopyExportEnv) walkArchive(root string) *ceArchiveSet {
	real := util.ResolveExisting(root)
	set := &ceArchiveSet{Root: root, Keys: map[ceDirKey]bool{}, Vols: map[uint64]bool{}, Complete: true, View: e.viewFor(real)}
	maxDirs, limit := e.walkLimits()
	deadline := time.Now().Add(limit)
	stop := func(why string) error {
		set.Complete, set.Why = false, why
		return filepath.SkipAll
	}
	_ = filepath.WalkDir(real, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d == nil { // the root itself could not be read
				set.Complete, set.Why = false, "unreadable"
				return err
			}
			if d.IsDir() { // a directory whose entries could not be listed
				set.Complete, set.Why = false, "unreadable"
			}
			return nil
		}
		if !d.IsDir() { // files, and links, which are never followed
			return nil
		}
		if len(set.Keys) >= maxDirs || time.Now().After(deadline) {
			return stop("too large")
		}
		f, oerr := os.Open(p)
		if oerr != nil {
			set.Complete, set.Why = false, "unreadable"
			return filepath.SkipDir
		}
		id, ierr := readIdentity(f)
		f.Close()
		if ierr != nil {
			set.Complete, set.Why = false, "unreadable"
			return nil
		}
		set.Keys[dirKeyOf(id)] = true
		set.Vols[volOf(id)] = true
		return nil
	})
	return set
}

// destChain reads the identity of dest and of each ancestor up to its volume or share
// root. Each is opened by its real (symlink-resolved) path, so an alias that keeps the
// identity of the folder it shows (a bind mount, a mount folder, a `subst` letter, a
// mapped drive) is seen through whichever folder of the archive it shows.
func (e *CopyExportEnv) destChain(dest string) ([]ceIdentity, error) {
	if e.destIDs != nil {
		return e.destIDs(dest)
	}
	cur := util.ResolveExisting(dest)
	var out []ceIdentity
	for {
		// Without blocking: a folder of the path that is a named pipe must not hang the
		// plan (EW-NF1).
		f, err := os.OpenFile(cur, os.O_RDONLY|openNoBlock, 0)
		if err != nil {
			return out, err
		}
		id, ierr := readIdentity(f)
		f.Close()
		if ierr != nil {
			return out, ierr
		}
		out = append(out, id)
		parent := filepath.Dir(cur)
		if parent == cur {
			return out, nil
		}
		cur = parent
	}
}

// viewFor classifies the way path is reached (a seam in tests).
func (e *CopyExportEnv) viewFor(path string) ceView {
	if e.view != nil {
		return e.view(path)
	}
	return classifyView(util.ResolveExisting(path))
}

// servedHere reports whether a share's server is this computer: localhost, a loopback
// address, this computer's own names or one of its interface addresses (design §4.7).
// It looks nothing up on the network.
func (e *CopyExportEnv) servedHere(server string) bool {
	if e.served != nil {
		return e.served(server)
	}
	s := strings.TrimSuffix(strings.ToLower(strings.Trim(server, "[]")), ".")
	if s == "" {
		return false
	}
	if s == "localhost" || strings.HasPrefix(s, "localhost.") {
		return true
	}
	if ip := parseServerIP(s); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return true
		}
		addrs, _ := net.InterfaceAddrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(ip) {
				return true
			}
		}
		return false
	}
	if h, err := os.Hostname(); err == nil {
		h = strings.ToLower(h)
		short, _, _ := strings.Cut(h, ".")
		// The host's bare name, its own dotted name, and its name qualified by a
		// domain: Windows reports only the host label, so a share by the fully
		// qualified name would otherwise look like another computer. A different
		// computer that shares the label is refused as unconfirmable, never trusted.
		if s == h || s == short || (short != "" && strings.HasPrefix(s, short+".")) {
			return true
		}
	}
	return false
}

// parseServerIP reads the spellings of an address a share path can carry: a literal Go
// parses, the classic inet_aton forms (127.1, a decimal or hexadecimal number, an octal
// part), and Windows' IPv6-literal UNC name ("--1.ipv6-literal.net", with an "s" before
// a zone index). It returns nil for a name.
func parseServerIP(s string) net.IP {
	if ip := net.ParseIP(s); ip != nil {
		return ip
	}
	if body, ok := strings.CutSuffix(s, ".ipv6-literal.net"); ok {
		body, _, _ = strings.Cut(body, "s")
		return net.ParseIP(strings.ReplaceAll(body, "-", ":"))
	}
	parts := strings.Split(s, ".")
	if len(parts) > 4 {
		return nil
	}
	var v uint64
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 0, 64)
		if err != nil {
			return nil
		}
		if i < len(parts)-1 {
			if n > 255 {
				return nil
			}
			v = v<<8 | n
			continue
		}
		room := uint(8 * (5 - len(parts))) // the bits the last part fills
		if n >= uint64(1)<<room {
			return nil
		}
		v = v<<room | n
	}
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// viewDecision is what two views allow: identity decides, they cannot reach the same
// storage (outside), or the answer is R-f2 with its cause.
type viewDecision struct {
	byIdentity bool
	unconfirm  string // "" or "fuse" | "here" | "shares", with archiveSide
	archive    bool   // the cause is on the archive's side
}

// decideViews applies design §4.7's table. Identity decides only between two views
// that keep it: both on this computer's local filesystems, or both through one network
// share whose client reports the server's file ids. Where the two views cannot reach
// the same storage, no identity is needed: a destination on another computer's share
// beside an archive on this computer's local filesystems is outside, and so is the
// reverse. Every other pairing is R-f2.
func (e *CopyExportEnv) decideViews(av, dv ceView) viewDecision {
	switch {
	case av.Kind == viewStacking:
		return viewDecision{unconfirm: "fuse", archive: true}
	case av.Kind == viewShare && e.servedHere(av.Server):
		return viewDecision{unconfirm: "here", archive: true}
	case dv.Kind == viewStacking:
		return viewDecision{unconfirm: "fuse"}
	case dv.Kind == viewShare && e.servedHere(dv.Server):
		return viewDecision{unconfirm: "here"}
	case av.Kind == viewLocal && dv.Kind == viewLocal:
		return viewDecision{byIdentity: true}
	case av.Kind == viewShare && dv.Kind == viewShare:
		if av.Mount != "" && av.Mount == dv.Mount && !av.NoID && !dv.NoID {
			return viewDecision{byIdentity: true}
		}
		return viewDecision{unconfirm: "shares"}
	}
	return viewDecision{} // one local, one on another computer's share: outside
}

// CheckDestination validates dest as a place an export may be written (design §4.7
// step 3): an absolute, clean path to an existing folder (else R-e), not equal to or
// inside any of the archives (else R-f), by path and by identity, and not a view whose
// identity cannot be compared with an archive's (else R-f2). It writes nothing. It
// returns what Start compares again, and a *CopyRefusal on a refusal.
func (e *CopyExportEnv) CheckDestination(dest string, archives []string) (*CopyOutside, error) {
	if err := checkDestPath(dest); err != nil {
		return nil, err
	}
	out := &CopyOutside{}
	for _, a := range archives {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if fi, err := os.Stat(a); err != nil || !fi.IsDir() {
			continue // no archive there yet: nothing to be inside of
		}
		if r := refusePathInside(dest, a); r != nil {
			return nil, r
		}
		set := e.walkArchive(a)
		out.archives = append(out.archives, a)
		out.sets = append(out.sets, set)
		if r := e.refuseByIdentity(dest, a, set); r != nil {
			return nil, r
		}
	}
	return out, nil
}

// Recheck is Start's repetition of the destination check (MA-444): the path checks
// again, and the destination's identity and view again against the sets the preview
// collected, without walking the archives a second time.
func (o *CopyOutside) Recheck(e *CopyExportEnv, dest string) error {
	if o == nil {
		return nil
	}
	if err := checkDestPath(dest); err != nil {
		return err
	}
	for i, a := range o.archives {
		if r := refusePathInside(dest, a); r != nil {
			return r
		}
		if r := e.refuseByIdentity(dest, a, o.sets[i]); r != nil {
			return r
		}
	}
	return nil
}

// DestExampleFor is an example of a full folder path in the shape of the given system's, for
// the words that tell an owner what to type (R-e) and the destination box's placeholder.
func DestExampleFor(goos string) string {
	switch goos {
	case "windows":
		return `C:\Users\you\Downloads`
	case "darwin":
		return "/Users/you/Downloads"
	}
	return "/home/you/Downloads"
}

// checkDestPath is R-e: the destination is absolute, clean, has no NUL, exists and is a folder.
func checkDestPath(dest string) *CopyRefusal {
	example := DestExampleFor(runtime.GOOS)
	switch {
	case strings.TrimSpace(dest) == "":
		return &CopyRefusal{ID: "R-e", Message: "No destination folder is chosen — type the full path of an existing folder, or click Browse to choose one, or Create folder. An export is written into an existing folder."}
	case !filepath.IsAbs(dest) && (strings.HasPrefix(dest, "~") || strings.Contains(dest, "%") || strings.HasPrefix(dest, "$")):
		return &CopyRefusal{ID: "R-e", Message: fmt.Sprintf("The folder %s isn't a full path — MailArchive doesn't expand ~, %%NAME%% or $NAME in a path. Type the full path of an existing folder (for example %s), or click Browse to choose one, or Create folder.", printable(dest), example)}
	case strings.ContainsRune(dest, 0) || !filepath.IsAbs(dest) || dest != filepath.Clean(dest):
		return &CopyRefusal{ID: "R-e", Message: fmt.Sprintf("The folder %s isn't a full path to an existing folder — an export is written into an existing folder. Type the full path (for example %s), or click Browse to choose one, or Create folder.", printable(dest), example)}
	}
	fi, err := os.Stat(dest)
	if err == nil && !fi.IsDir() {
		return &CopyRefusal{ID: "R-e", Message: fmt.Sprintf("The folder %s doesn't exist — that is a file, not a folder. An export is written into an existing folder. Click Browse to choose one, or Create folder.", dest)}
	}
	if err != nil {
		return destMissing(dest)
	}
	return nil
}

// printable is a path as a refusal names it, without a NUL or a control character.
func printable(s string) string { return util.StripControl(strings.ReplaceAll(s, "\x00", "")) }

// refusePathInside is R-f by path: dest equal to, or inside, the archive, symlinks
// resolved (util.Overlap's "same" and "inside" cases; a destination that contains the
// archive does not write into it).
func refusePathInside(dest, archive string) *CopyRefusal {
	if overlap, how := util.Overlap(dest, archive); overlap && how != "contains" {
		return refusalF(archive)
	}
	return nil
}

func refusalF(archive string) *CopyRefusal {
	return refuse("R-f", fmt.Sprintf("That folder is inside your archive (%s) — an export written there would mix into the archive. Choose a folder outside %s.", archive, archive))
}

// refuseByIdentity applies the view table and the identity set: R-f when the
// destination or one of its ancestors is a directory of the archive, R-f2 where
// identity cannot decide.
func (e *CopyExportEnv) refuseByIdentity(dest, archive string, set *ceArchiveSet) *CopyRefusal {
	dv := e.viewFor(dest)
	d := e.decideViews(set.View, dv)
	if d.unconfirm != "" {
		return refusalF2(dest, archive, d.unconfirm, d.archive)
	}
	if !d.byIdentity {
		return nil
	}
	ids, err := e.destChain(dest)
	if err != nil && len(ids) == 0 {
		return refusalF2(dest, archive, "walk", false)
	}
	onArchiveVolume := false
	for _, id := range ids {
		if set.Keys[dirKeyOf(id)] {
			return refusalF(archive)
		}
		if set.Vols[volOf(id)] {
			onArchiveVolume = true
		}
	}
	if !set.Complete && onArchiveVolume {
		return refusalF2(dest, archive, "walk", false)
	}
	return nil
}

// refusalF2 is R-f2 (design §7.6): the destination's place cannot be confirmed to be
// outside the archive. why names the cause; archiveSide is true when it is the
// archive's own view, whose remedy is on the archive's side.
func refusalF2(dest, archive, why string, archiveSide bool) *CopyRefusal {
	var msg string
	switch {
	case archiveSide && why == "here":
		msg = fmt.Sprintf("Your archive at %s is reached through a share on this computer, so MailArchive can't confirm any folder is outside it — set Archive location to its local path, then click Preview copy export again.", archive)
	case archiveSide:
		msg = "Your archive is on an sshfs or other FUSE mount, where MailArchive can't compare folder identities — set Archive location to the archive's own path, or mount its share with your system's SMB or NFS client."
	case why == "here":
		msg = fmt.Sprintf("MailArchive couldn't confirm that %s is outside your archive (%s) — it is reached through a share on this computer, which can show one folder under two names, so nothing was written. Use the folder's local path instead.", dest, archive)
	case why == "fuse":
		msg = fmt.Sprintf("MailArchive couldn't confirm that %s is outside your archive (%s) — it is on an sshfs, FUSE or overlay mount, where folder identities can't be compared, so nothing was written. Choose a folder on a local drive, or one mounted with your system's SMB or NFS client.", dest, archive)
	case why == "walk":
		msg = fmt.Sprintf("MailArchive couldn't confirm that %s is outside your archive (%s) — the archive is too large or partly unreadable to check, so nothing was written. Choose a folder on another drive.", dest, archive)
	default:
		msg = fmt.Sprintf("MailArchive couldn't confirm that %s is outside your archive (%s) — the two are reached through different network shares, which can show one folder under two names, so nothing was written. Choose a folder on a local drive or a USB drive, or one on the archive's own share outside the archive.", dest, archive)
	}
	return refuse("R-f2", msg)
}

// ---- classification of a view ------------------------------------------------

// classifyFSName classifies a view from a filesystem type and its mount source, the
// inputs macOS's statfs gives (f_fstypename, f_mntfromname) and the ones a Linux mount
// line gives (its type and its source). A type or source it does not know is stacking.
func classifyFSName(fstype, from string) ceView {
	t := strings.ToLower(fstype)
	v := ceView{Type: t}
	switch {
	case t == "cifs" || t == "smb3" || t == "smbfs" || t == "smb2":
		v.Kind, v.Server, v.Mount = viewShare, serverOfUNC(from), strings.ToLower(strings.TrimLeft(strings.ReplaceAll(from, `\`, "/"), "/"))
	case t == "nfs" || t == "nfs4" || t == "nfs3":
		v.Kind, v.Server, v.Mount = viewShare, serverOfNFS(from), strings.ToLower(from)
	case t == "afpfs" || t == "webdav":
		v.Kind, v.Mount = viewShare, strings.ToLower(from) // its server cannot be named from the source
	case t == "fuseblk": // a FUSE block filesystem on a device (ntfs-3g) is a local filesystem
		v.Kind = viewLocal
	case strings.HasPrefix(t, "fuse") || strings.Contains(t, "fuse") || t == "overlay" || t == "overlayfs" || t == "aufs" || t == "ecryptfs" || t == "unionfs":
		v.Kind = viewStacking
	case localFSTypes[t]:
		v.Kind = viewLocal
	default:
		v.Kind = viewStacking
	}
	return v
}

// localFSTypes are the filesystems of one local device.
var localFSTypes = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true, "f2fs": true, "jfs": true, "reiserfs": true,
	"vfat": true, "msdos": true, "msdosfs": true, "exfat": true, "ntfs": true, "ntfs3": true, "tmpfs": true, "ramfs": true,
	"iso9660": true, "cd9660": true, "udf": true, "hfs": true, "hfsplus": true, "apfs": true, "ufs": true, "squashfs": true,
	"bcachefs": true, "erofs": true, "devtmpfs": true, "ReFS": true, "refs": true,
}

// serverOfUNC is the server of "//server/share" or `\\server\share`, or of "//user@server/share".
func serverOfUNC(s string) string {
	s = strings.TrimLeft(strings.ReplaceAll(s, `\`, "/"), "/")
	host, _, _ := strings.Cut(s, "/")
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	return strings.ToLower(host)
}

// serverOfNFS is the server of "server:/export" or "[::1]:/export".
func serverOfNFS(s string) string {
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "]"); i > 0 {
			return strings.ToLower(s[1:i])
		}
	}
	host, _, _ := strings.Cut(s, ":")
	return strings.ToLower(host)
}

// gvfsShare names the server and share of a GNOME gvfs path
// (".../gvfs/smb-share:server=nas,share=mail/..."), whose ids are the mount's own.
func gvfsShare(abs string) (ceView, bool) {
	const key = "smb-share:"
	i := strings.Index(abs, key)
	if i < 0 || !strings.Contains(abs[:i], "gvfs") {
		return ceView{}, false
	}
	seg := abs[i+len(key):]
	if j := strings.IndexAny(seg, `/\`); j >= 0 {
		seg = seg[:j]
	}
	var server, share string
	for _, kv := range strings.Split(seg, ",") {
		k, v, _ := strings.Cut(kv, "=")
		switch k {
		case "server":
			server = strings.ToLower(v)
		case "share":
			share = strings.ToLower(v)
		}
	}
	if server == "" {
		return ceView{}, false
	}
	return ceView{Kind: viewShare, Server: server, Mount: "//" + server + "/" + share, NoID: true, Type: "gvfs"}, true
}

// classifyMountinfo classifies abs from the lines of /proc/self/mountinfo: the mount
// that holds the path is the one whose mount point is its longest prefix, and its
// filesystem type and mount source decide (design §4.7, RR-2b re-verification).
func classifyMountinfo(lines []string, abs string) ceView {
	if v, ok := gvfsShare(abs); ok {
		return v
	}
	best, bestLen := ceView{Kind: viewStacking}, -1
	found := false
	for _, ln := range lines {
		pre, post, ok := strings.Cut(ln, " - ")
		if !ok {
			continue
		}
		f := strings.Fields(pre)
		pf := strings.Fields(post)
		if len(f) < 5 || len(pf) < 2 {
			continue
		}
		mp := unescapeMountinfo(f[4])
		if !pathUnder(abs, mp) || len(mp) < bestLen {
			continue
		}
		best, bestLen, found = classifyFSName(pf[0], unescapeMountinfo(pf[1])), len(mp), true
	}
	if !found {
		return ceView{Kind: viewStacking}
	}
	return best
}

// pathUnder reports whether abs is mp or below it, component-wise.
func pathUnder(abs, mp string) bool {
	if mp == "/" {
		return true
	}
	return abs == mp || strings.HasPrefix(abs, mp+"/")
}

// unescapeMountinfo reads the octal escapes (\040 for a space) a mountinfo field carries.
func unescapeMountinfo(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, ok := octal3(s[i+1 : i+4]); ok {
				b.WriteByte(n)
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func octal3(s string) (byte, bool) {
	if len(s) != 3 {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, false
		}
		n = n*8 + int(c-'0')
	}
	if n > 255 {
		return 0, false
	}
	return byte(n), true
}
