package archdelete

import (
	"regexp"
	"sort"
	"strings"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
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
}

// outlookPSTDir is -outlook's export folder: Delete removes its .pst files only.
const outlookPSTDir = "_outlook-pst"

// stateFilePrefix begins every MailArchive state file, descriptor and run leftover
// at an archive's root; the lock file (lockfile.Name) is not one of them.
const stateFilePrefix = ".mailarchive-"

// encRootFiles are the files an encrypted archive keeps at its root (the tree
// grammar of internal/assure/leak and encmark's reserved names, §4.2), other than
// the lock (removed after unlocking, step 7) and the vault files (removed last).
var encRootFiles = map[string]bool{
	state.LastRunName: true, state.RunHistoryName: true, state.SourceStreakName: true,
	state.LastVerifyName: true, state.AttentionName: true, state.IntegrityAttentionName: true,
	seal.Name: true, "README.txt": true,
	container.EnvelopeName: true, container.PointerName: true, container.MirrorName: true, container.InventoryName: true,
	".mailarchive-convert.json": true,
}

var (
	indexPageRe = regexp.MustCompile(`^index(-[0-9]+)?\.html$`)
	shardRe     = regexp.MustCompile(`^[0-9a-f]{2}$`)
	unitRe      = regexp.MustCompile(`^[0-9a-f]{32}$`)
	invShardRe  = regexp.MustCompile(`^[0-9a-f]{2,4}\.[1-9][0-9]*$`)
	encRootTmp  = regexp.MustCompile(`^\.mailarchive-[a-z-]*[0-9]+\.tmp$`)
	unitTmp     = regexp.MustCompile(`^\.mailarchive-[0-9]+\.tmp$`)
	programTmp  = regexp.MustCompile(`^\.mailarchive-program-[0-9]{1,20}\.tmp$`)
	// stemShapeRe is the exporter's stem (export.StemName): a date, an optional
	// subject slug, an 8-to-40 hex digest.
	stemShapeRe = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{4})_(?:.*_)?([0-9a-f]{8,40})$`)
)

// otherArchiveMarkers are the names whose presence in a store tree is another
// archive's marker: the whole delete is refused (§4.1).
var otherArchiveMarkers = []string{manifestName, vault.FileName, vault.BackupName, container.PointerName, ".mailarchive-convert.json"}

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

// fold is a name as the platform's file system compares it: case is ignored on
// Windows and macOS (their default volumes), kept on Linux.
func fold(goos, s string) string {
	if goos == "windows" || goos == "darwin" {
		return strings.ToLower(s)
	}
	return s
}

func eqName(goos, a, b string) bool { return fold(goos, a) == fold(goos, b) }

// storeNumber is token's 1-based number in tokens (already in byte order), or 0.
func storeNumber(tokens []string, token string) int {
	i := sort.SearchStrings(tokens, token)
	if i < len(tokens) && tokens[i] == token {
		return i + 1
	}
	return 0
}
