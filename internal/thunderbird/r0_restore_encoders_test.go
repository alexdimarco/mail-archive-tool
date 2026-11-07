package thunderbird

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

// Slice R5 replaced the encoders of MA-1138 (ids MA-1138, R41, S70), MA-1140 (MA-1140,
// R40, R31, S71; its R3 part was proven already) and MA-1141 (MA-1141, R41, S68) with
// the real covering tests, each carrying the same ids:
// internal/source/ma1138_restore_skip_test.go, internal/app/ma1138_backup_skip_test.go,
// internal/tbrestore/ma1138_skipset_test.go, internal/uninstall/ma1140_restore_targets_test.go,
// internal/tbrestore/ma1141_leftovers_test.go, internal/desktop/ma1141_restore_notice_test.go
// and cmd/mailarchive/ma1141_restore_status_test.go.

// Slice R6 replaced the encoders of MA-1124 (ids MA-1124, R40, S70), MA-1125 (MA-1125,
// R40, S66, S67), MA-1127 (MA-1127, R40, S67; its R3 part was proven already), MA-1129
// (MA-1129, R40, R33), MA-1132 (MA-1132, R40) and MA-1143 (MA-1143, R40; its R3 part was
// proven already) with the real covering tests, each carrying the same ids:
// internal/tbengine/ma1124_plan_test.go, ma1125_recheck_test.go, ma1127_locks_test.go,
// integration_test.go, ma1122_crash_test.go, ma1132_ma1143_test.go and
// cmd/mailarchive/ma1129_restore_cli_test.go. MA-1128 (MA-1128, R37, R40, S72) was built
// except the Lock reply, which is Desktop's (slice R7): internal/tbengine/
// ma1128_encrypted_test.go proves the rest.

// Slice R7 replaced the encoders of MA-1128's Lock reply (ids MA-1128, R37, R40, S72),
// MA-1130 (MA-1130, R23, R40) and MA-1131 (MA-1131, R40) with the real covering tests,
// each carrying the same ids: internal/desktop/ma1128_restore_lock_reply_test.go,
// ma1130_restore_routes_test.go, ma1131_restore_flow_test.go,
// ma1131_restore_unfinished_test.go and ma1131_restore_page_test.go. No R0 encoder is
// left in this file.
