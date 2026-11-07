// Package tbrestore is the engine of Restore to Thunderbird
// (docs/design-restore-thunderbird.md): it writes an archive's preserved originals,
// as mboxrd files, into one new folder under a closed Thunderbird profile's Local
// Folders, and never changes, renames, replaces or removes anything it did not make
// (invariant R40).
//
// This package holds the parts that need nothing of the archive: the Thunderbird side
// (the profile list, prefs.js and user.js, Local Folders, the store kind, the version,
// whether Thunderbird runs, Thunderbird's own profile lock and MailArchive's restore
// lock; slice R3), the writer (names, budgets, the marker and made-list, the write
// sequence, the post-write check, the one cleanup, restores.log; slice R4) and what a
// backup needs to leave a restore's folder out (slice R5). It imports nothing of
// internal/app, so a backup can use it; the plan and the run that read an archive
// (slice R6) feed it the archive's folders and original bytes.
package tbrestore
