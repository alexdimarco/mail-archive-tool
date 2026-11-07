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

// Slice R3 replaced the encoders of MA-1084 (covers: MA-1084, R40), MA-1085 (MA-1085,
// R40, S73), MA-1086 (MA-1086, R40) and MA-1087 (MA-1087, R40, S66, S67) with the real
// covering tests, each carrying the same ids: internal/tbrestore/ma1084_profiles_test.go,
// ma1085_prefs_test.go, ma1086_storekind_test.go and ma1087_running_test.go.

// Slice R4 replaced the encoders of MA-1088 (ids MA-1088, R40, S69), MA-1089 (MA-1089,
// R40), MA-1119 (MA-1119, R40), MA-1120 (MA-1120, R40), MA-1122 (MA-1122, R40, S68,
// S74), MA-1123 (MA-1123, R40, S68), MA-1126 (MA-1126, R40), MA-1139 (MA-1139, R40,
// S74) and MA-1142 (MA-1142, R40, S68) with the real covering tests, each carrying the
// same ids: internal/tbrestore/ma1088_names_test.go, ma1119_layout_test.go,
// ma1120_collisions_test.go, ma1122_sequence_test.go, ma1123_settle_test.go,
// ma1126_pathbudget_test.go, ma1139_check_test.go, ma1142_log_test.go, and
// internal/interchange/ma1089_restore_mbox_test.go.

// covers: MA-1124, R40, S70
func TestR0EncoderPlan(t *testing.T) {
	t.Skip("MA-1124 pending: the plan's words, free space (W-SPACE), W-PST, W-NORAW, W-RECAPTURE, W-FAT32 and the All Mail note land in slice R6 (the read side is proven since slice R1 by internal/app/ma1124_restore_read_test.go)")
}

// covers: MA-1125, R40, S66, S67
func TestR0EncoderRecheckAtStart(t *testing.T) {
	t.Skip("MA-1125 pending: the re-check at Start lands in slice R6")
}

// covers: MA-1127, R40, S67
func TestR0EncoderLocks(t *testing.T) {
	t.Skip("MA-1127 pending: the archive lock held for the run (a backup refused meanwhile, no verb for an encrypted archive) and each lock released on success, error, Cancel, Shut down and a recovered panic land in slice R6 (Thunderbird's profile lock is proven since slice R3 by internal/tbrestore/ma1127_profilelock_test.go)")
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

// covers: MA-1140, R40, R31, S71
func TestR0EncoderRestoreLock(t *testing.T) {
	t.Skip("MA-1140 pending: uninstall's targets and a -config Desktop's restore seen by a scheduled backup's skip land in slice R5 (the restore lock, presync honouring it and ProfileKey are proven since slice R3 by internal/presync/ma1140_restore_lock_test.go and internal/thunderbird/ma1140_profilekey_test.go)")
}

// covers: MA-1141, R41, S68
func TestR0EncoderUnfinishedShown(t *testing.T) {
	t.Skip("MA-1141 pending: W-UNFINISHED at Desktop start, in status and in uninstall's plan lands in slice R5")
}

// covers: MA-1143, R40
func TestR0EncoderWindowsLegs(t *testing.T) {
	t.Skip("MA-1143 pending: W-STORE first in the plan and repeated in the Done words lands in slice R6 (the detection of a Store package, an identity-less volume and a remote drive is proven since slice R3 by internal/tbrestore/ma1143_winlegs_test.go and, on the Windows VM, ma1143_winlegs_windows_test.go)")
}
