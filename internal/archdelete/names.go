package archdelete

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// plainFixedNames are §4.1's fixed root names [C9]: the search index and its
// rebuild with their SQLite companions, the notices and reports, the copy markers
// and the run logs of a run-job and its scheduler.
var plainFixedNames = []string{
	"search.db", "search.db-wal", "search.db-shm", "search.db-journal",
	"search.db.rebuild", "search.db.rebuild-wal", "search.db.rebuild-shm",
	"README.txt", "attachments-report.tsv", "attachments-report-legacy.tsv",
	state.AttentionName, state.IntegrityAttentionName, "_COPY-PROBLEMS.txt", state.UnfinishedCopyName,
	"mailarchive.log", "mailarchive.stderr.log",
	// runlog.Open keeps one rotated generation of a log it opens (<log>.1):
	// mailarchive.log is opened through it, the stderr log (the scheduler's) is not.
	"mailarchive.log" + rotatedSuffix,
}

// rotatedSuffix is the rotated generation runlog.Open keeps of a run log.
const rotatedSuffix = ".1"

// folderOwnNames are files at the archive folder's root that belong to the folder
// itself, not to the archive: Windows' desktop.ini (the folder's display name and
// icon — a known folder such as Documents carries its LocalizedResourceName there)
// and macOS's custom-icon file "Icon\r". A plain archive can be made in such a
// folder (§4), so at the root these are the person's, kept, never litter
// (del-s1a-A1-04, for the operator: §4.4 lists them as litter everywhere).
var folderOwnNames = []string{"desktop.ini", "Icon\r"}

// outlookPSTDir is -outlook's export folder: Delete removes its .pst files only.
const outlookPSTDir = "_outlook-pst"

// stateFilePrefix begins every MailArchive state file, descriptor and run leftover
// at an archive's root; the lock file (lockfile.Name) is not one of them.
const stateFilePrefix = ".mailarchive-"

// encRootFiles are the files an encrypted archive keeps at its root, taken from
// encmark's reserved names (the tree grammar, §4.2) so the two cannot drift apart
// (conformance gap 6): every reserved name but the lock (removed after unlocking,
// step 7), the vault files (removed last), the delete marker (the delete's own note,
// written and removed by its own two steps, never in the plan: §15.2) and the
// folders of the validated layouts.
var encRootFiles = func() map[string]bool {
	notFiles := map[string]bool{
		lockName: true, vault.FileName: true, vault.BackupName: true, delmark.Name: true,
		export.UnitsDir: true, container.ChunksDir: true, container.InventoryDir: true, container.ProgramDir: true,
	}
	out := map[string]bool{}
	for _, n := range encmark.ReservedNames() {
		if !notFiles[n] {
			out[n] = true
		}
	}
	return out
}()

// encRootTmp is an encrypted archive's root temp file (encmark's shape).
func encRootTmp(name string) bool { return encmark.IsReservedTemp(name) }

var (
	indexPageRe = regexp.MustCompile(`^index(-[0-9]+)?\.html$`)
	shardRe     = regexp.MustCompile(`^[0-9a-f]{2}$`)
	unitRe      = regexp.MustCompile(`^[0-9a-f]{32}$`)
	invShardRe  = regexp.MustCompile(`^[0-9a-f]{2,4}\.[1-9][0-9]*$`)
	unitTmp     = regexp.MustCompile(`^\.mailarchive-[0-9]+\.tmp$`)
	programTmp  = regexp.MustCompile(`^\.mailarchive-program-[0-9]{1,20}\.tmp$`)
	// stemShapeRe is the exporter's stem (export.StemName): a date, an optional
	// subject slug, an 8-to-40 hex digest.
	stemShapeRe = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{4})_(?:.*_)?([0-9a-f]{8,40})$`)
)

// otherArchiveMarkers are the file names whose presence in a store tree is another
// archive's marker: the whole delete is refused (§4.1). They are vault.EncryptedMarker's
// file signals and the manifest; its folder signals (units/, chunks/, inventory/ in
// their layouts) are asked of vault.EncryptedMarker for each folder walked
// (del-s1a-A1-08). Another archive's delete marker is one too (§15.7, MA-1053).
var otherArchiveMarkers = []string{manifestName, vault.FileName, vault.BackupName, container.PointerName,
	container.MirrorName, container.InventoryName, ".mailarchive-convert.json", delmark.Name}

// isDeleteMarker reports the delete marker's name at an archive's root, as the
// platform's file system compares names (§15.1).
func isDeleteMarker(goos, name string) bool { return eqName(goos, name, delmark.Name) }

// lateStateFiles are the root state files removed after every other name and just
// before the anchor, in this order (del-s1a-A2-05): a delete stopped part way (a
// file Windows holds open) leaves the archive with its history, its schedule
// records and — last — its pause seal, so a paused archive stays paused and a
// schedule this delete could not stop still finds the archive as it was.
var lateStateFiles = []string{state.HistoryName, schedule.VerifyDescriptorName, schedule.DescriptorName, seal.Name}

// lockName is the archive lock, which step 7 removes after unlocking [C1].
const lockName = lockfile.Name

// unitsDirs are an encrypted archive's sharded folders.
var unitsDirs = []string{export.UnitsDir, container.ChunksDir}

// isLeftover is §4.1's run-leftover rule (export.SweepTemps's names, no age cut-off).
func isLeftover(name string) bool {
	return (strings.HasPrefix(name, stateFilePrefix) && strings.HasSuffix(name, ".tmp")) || strings.HasPrefix(name, export.SnapshotPrefix)
}

// mailShaped is §4.3's three kinds of file.
func mailShaped(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".html") || strings.HasSuffix(l, ".eml") || strings.HasSuffix(l, "-attachments.zip")
}

// fold is a name as the platform's file system compares it: on Windows and macOS
// (their default volumes) util.FoldKey — NFC, then Unicode case folding — so a
// name the exporter wrote composed (NFC) and a listing returns decomposed (an HFS+
// drive) are one name (del-s1a-A2-02, A3-02); kept exact on Linux.
func fold(goos, s string) string {
	if goos == "windows" || goos == "darwin" {
		return util.FoldKey(s)
	}
	return s
}

// rootCannotName reports a name an os.Root on this computer cannot reach: on
// Windows a reserved device name (nul, con, COM1, and on Windows 10 nul.txt), which
// os.Root refuses as escaping the root (filepath.IsLocal's rule, the one os.Root
// uses). Such an entry, listed by a folder read, is never the archive's (the
// exporter's names avoid them): it is kept, never Lstat'ed (del-s1a-A1-01).
func rootCannotName(name string) bool { return !filepath.IsLocal(name) }

func eqName(goos, a, b string) bool { return fold(goos, a) == fold(goos, b) }

// storeNumber is token's 1-based number in tokens (already in byte order), or 0.
func storeNumber(tokens []string, token string) int {
	i := sort.SearchStrings(tokens, token)
	if i < len(tokens) && tokens[i] == token {
		return i + 1
	}
	return 0
}
