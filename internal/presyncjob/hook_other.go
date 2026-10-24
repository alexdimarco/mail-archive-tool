//go:build !darwin && !windows

package presyncjob

import (
	"io"

	"mail-archive-tool/internal/app"
)

// SetHook is a no-op where there is no pre-sync adapter (Linux, TP7): the run
// archives what is on disk and opts.BeforeRead stays nil, exactly as an older
// build that did not know the field does (design §4.2.5). A scheduled job that
// somehow carries the setting on such a platform is therefore inert, not an
// error — the archive still runs (R32).
func SetHook(_ *app.Options, _ string, _ io.Writer) error { return nil }
