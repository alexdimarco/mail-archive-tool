package thunderbird

import "testing"

// Restore to Thunderbird, slice R0: catalog-first pending encoders.
//
// Each test below encodes one U row of the restore design
// (docs/design-restore-thunderbird.md §12, docs/scenario-catalog.md MA-1084..MA-1099
// and MA-1119..MA-1143, invariants R40 and R41, scenarios S66..S74) with a
// `// covers:` marker and a t.Skip naming the slice that builds it. They keep
// `go test ./...` green and the covers-map meta-test satisfied while the rows exist
// but the behaviour is unbuilt. When a slice builds a row, it REPLACES that row's
// encoder here with the real covering test (or narrows the skip message to the part
// a later slice still owes), so the skip becomes a permanent guard (CLAUDE.md
// testing discipline). The lab rows (MA-1090..MA-1099, MA-1121, MA-1133..MA-1137)
// are tier L and pending, and carry no encoder.

// covers: MA-1084, R40
func TestR0EncoderProfilesINIRestoreUse(t *testing.T) {
	t.Skip("MA-1084 pending: the shared parser lands in slice R2; the restore's use of it (W-PROFINI over 256 KiB, an absolute [Install] Default=, bidi and controls stripped from Name=, Linux ownership and mode checks, the list id binding) lands in slice R3")
}

// covers: MA-1085, R40, S73
func TestR0EncoderPrefsJSLocalFolders(t *testing.T) {
	t.Skip("MA-1085 pending: prefs.js and user.js decoding and the Local Folders lookup (W-PREFS, W-NOLOCAL) land in slice R3")
}

// covers: MA-1086, R40
func TestR0EncoderStoreKind(t *testing.T) {
	t.Skip("MA-1086 pending: the store-kind check (W-MAILDIR) lands in slice R3")
}

// covers: MA-1087, R40, S66, S67
func TestR0EncoderThunderbirdRunning(t *testing.T) {
	t.Skip("MA-1087 pending: running detection (lock pid, parent.lock, the Linux /proc probe, the macOS raw count) lands in slice R3")
}

// covers: MA-1088, R40, S69
func TestR0EncoderSafeNames(t *testing.T) {
	t.Skip("MA-1088 pending: SafeName and the name budgets land in slice R4; the restore's own folder caps (W-TOOMANY) land in slice R1")
}

// covers: MA-1089, R40
func TestR0EncoderMboxBytes(t *testing.T) {
	t.Skip("MA-1089 pending: the mbox writer's matching line endings and the read-rule port land in slice R4")
}

// covers: MA-1119, R40
func TestR0EncoderLayout(t *testing.T) {
	t.Skip("MA-1119 pending: the layout (NAME + NAME.sbd, the marker, the made-list) lands in slice R4")
}

// covers: MA-1120, R40
func TestR0EncoderCollisions(t *testing.T) {
	t.Skip("MA-1120 pending: the free name and the exclusive creates (W-NAMES) land in slice R4")
}

// covers: MA-1122, R40, S68, S74
func TestR0EncoderWriteSequenceAndCleanup(t *testing.T) {
	t.Skip("MA-1122 pending: the write sequence, fault seams and the one cleanup in decision order land in slice R4")
}

// covers: MA-1123, R40, S68
func TestR0EncoderCrashStates(t *testing.T) {
	t.Skip("MA-1123 pending: settling open restores at Start (cases 0, 1, 2, 3a, 3b, the taken line) lands in slice R4")
}

// covers: MA-1124, R40, S70
func TestR0EncoderPlan(t *testing.T) {
	t.Skip("MA-1124 pending: the read side lands in slice R1; the plan's words, free space and refusals land in slice R6")
}

// covers: MA-1125, R40, S66, S67
func TestR0EncoderRecheckAtStart(t *testing.T) {
	t.Skip("MA-1125 pending: the re-check at Start lands in slice R6")
}

// covers: MA-1126, R40
func TestR0EncoderPathBudget(t *testing.T) {
	t.Skip("MA-1126 pending: the path budget (W-LONGPATH) lands in slice R4")
}

// covers: MA-1127, R40, S67
func TestR0EncoderLocks(t *testing.T) {
	t.Skip("MA-1127 pending: Thunderbird's profile lock lands in slice R3 (its Unix code after lab row MA-1134); the archive lock for the run lands in slice R6")
}

// covers: MA-1128, R37, R40, S72
func TestR0EncoderEncrypted(t *testing.T) {
	t.Skip("MA-1128 pending: the encrypted path, the tick and ask-twice land in slice R6")
}

// covers: MA-1129, R40, R33
func TestR0EncoderCommandLine(t *testing.T) {
	t.Skip("MA-1129 pending: the restore-thunderbird verb lands in slice R6")
}

// covers: MA-1130, R23, R40
func TestR0EncoderDesktopRoutes(t *testing.T) {
	t.Skip("MA-1130 pending: the Desktop routes and the restore plan's own key land in slice R7")
}

// covers: MA-1131, R40
func TestR0EncoderDesktopFlow(t *testing.T) {
	t.Skip("MA-1131 pending: the Desktop flow and its words land in slice R7")
}

// covers: MA-1132, R40
func TestR0EncoderElevatedRefused(t *testing.T) {
	t.Skip("MA-1132 pending: the elevated or root refusal (W-ELEVATED) lands in slice R6")
}

// covers: MA-1138, R41, S70
func TestR0EncoderBackupSkip(t *testing.T) {
	t.Skip("MA-1138 pending: the backup-side skip lands in slice R5 (the finished-folder part waits for the owner's O1 answer)")
}

// covers: MA-1139, R40, S74
func TestR0EncoderPostWriteCheck(t *testing.T) {
	t.Skip("MA-1139 pending: the post-write check (W-CHECKFAIL) lands in slice R4")
}

// covers: MA-1140, R40, R31, S71
func TestR0EncoderRestoreLock(t *testing.T) {
	t.Skip("MA-1140 pending: the MailArchive restore lock and presync honouring it land in slice R3; uninstall's targets land in slice R5")
}

// covers: MA-1141, R41, S68
func TestR0EncoderUnfinishedShown(t *testing.T) {
	t.Skip("MA-1141 pending: W-UNFINISHED at Desktop start, in status and in uninstall's plan lands in slice R5")
}

// covers: MA-1142, R40, S68
func TestR0EncoderRestoresLog(t *testing.T) {
	t.Skip("MA-1142 pending: restores.log on the delete log's writer lands in slice R4")
}

// covers: MA-1143, R40
func TestR0EncoderWindowsLegs(t *testing.T) {
	t.Skip("MA-1143 pending: W-STORE, W-NOIDENT and W-NETWORK land in slice R3 (run on the Windows VM)")
}
