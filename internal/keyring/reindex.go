package keyring

import (
	"context"
	"log"

	"mail-archive-tool/internal/app"
)

// Reindex rebuilds the search index of the encrypted archive u opened for a write
// (design-encrypted-archives-phase3 §3.1, E7): every record's sealed units read
// into a new index, committed by the record's save. It never removes a message: a
// sealed unit that is missing is damage, not a deletion (the owner's ruling,
// 2026-10-26). A paused archive is refused; stopped before the save, nothing is
// saved.
func Reindex(ctx context.Context, u *Unlocked, logger *log.Logger) (app.IndexCatchUp, error) {
	opts := app.Options{Out: u.Out()}
	done, err := Apply(&opts, u)
	if err != nil {
		return app.IndexCatchUp{}, err
	}
	defer done()
	return app.RebuildEncrypted(ctx, opts, logger)
}
