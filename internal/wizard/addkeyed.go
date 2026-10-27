package wizard

import (
	"context"
	"errors"
	"log"
	"runtime"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// The wizard's part of making an encrypted archive from MailArchive Desktop
// (design-encrypted-archives-phase3 §5.1, §5.2; the P3-3 plan, 3f). The wizard asks
// its usual questions with its native dialogs — never a password — and hands the
// answers back; the Desktop makes the key in its page, and the first backup then
// runs here with the key it is given. The wizard never imports internal/keyring
// (§3.1): the key arrives only through apply.

// PendingAdd is an archive the wizard's questions chose, not yet made: its folder
// and what to back up into it.
type PendingAdd struct{ c wizardChoice }

// Out is the new archive's folder.
func (p *PendingAdd) Out() string { return p.c.out }

// Outlook reports that it would back up through the Outlook app, which an
// encrypted archive made here does not do yet.
func (p *PendingAdd) Outlook() bool { return p.c.outlookCOM }

// addEncryptedFlow is the native-dialog flow, a seam a test replaces.
var addEncryptedFlow = func(check func(string) error) (*PendingAdd, error) {
	if refusal, ok := dialogPreflight(runtime.GOOS, dialogToolOnPath(), displayIsSet()); !ok {
		return nil, errors.New(refusal)
	}
	var pending *PendingAdd
	err := localArchiveFlowWith(nil, check, func(c wizardChoice) { pending = &PendingAdd{c: c} })
	if errors.Is(err, zenity.ErrCanceled) {
		return nil, nil
	}
	return pending, err
}

// AddEncryptedArchive asks the add-an-archive questions for an encrypted archive:
// check judges the folder as soon as it is chosen (empty, outside every archive and
// the settings), before anything else is asked. nil with no error: the person backed
// out.
func AddEncryptedArchive(check func(out string) error) (*PendingAdd, error) {
	return addEncryptedFlow(check)
}

// PendingAddFor is a pending add of inputs into out, for a test of the Desktop's
// create that runs no dialog.
func PendingAddFor(out string, inputs []string) *PendingAdd {
	return &PendingAdd{c: wizardChoice{out: out, inputs: inputs, keepRaw: true}}
}

// RunFirstBackup writes the run job Run now repeats for the new archive — in the
// settings folder, with no schedule (O5) — and runs its first backup attended with
// the key apply hands over, as Run now does (RunJobInProcessKeyed).
func (p *PendingAdd) RunFirstBackup(ctx context.Context, logger *log.Logger, apply func(*app.Options) (func(), error)) (string, error) {
	name := schedule.DefaultNameFor(p.c.out)
	path, err := job.PathFor(name)
	if err != nil {
		return "", err
	}
	if err := job.Write(path, jobFor(name, p.c)); err != nil {
		return "", err
	}
	return RunJobInProcessKeyed(ctx, path, logger, apply)
}
