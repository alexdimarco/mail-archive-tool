//go:build !windows

package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/keyring"
)

// covers: MA-830, R37
// Ctrl-C stops a copy and decrypt exits 130 (FID-07, FAIL-03): the signal reaches the
// copy, the folder keeps its unfinished mark, and the exit is the signal's — never 0.
func TestDecryptStoppedByCtrlCExits130(t *testing.T) {
	out, _ := backedUpArchive(t)
	parent := t.TempDir()
	orig := decryptCopy
	decryptCopy = func(ctx context.Context, u *keyring.Unlocked, e *app.CopyExportEnv, p *app.DecryptPlan, archives, settings []string, logger *log.Logger, onProgress func(app.DecryptReport)) (app.DecryptReport, error) {
		syscall.Kill(os.Getpid(), syscall.SIGINT)
		<-ctx.Done()
		return orig(ctx, u, e, p, archives, settings, logger, onProgress)
	}
	defer func() { decryptCopy = orig }()
	var stdout, stderr bytes.Buffer
	a := &scripted{isAttended: true, answers: []string{"y", flowPassword}}
	err := decryptWith(a, out, parent, "", noKeyFlags(), decryptDay, &stdout, &stderr)
	var ex *exitError
	dest := filepath.Join(parent, filepath.Base(out)+"-unencrypted-2026-10-27")
	_, marker := os.Stat(filepath.Join(dest, app.CopyMarkerName))
	assure.Reached(t, errors.As(err, &ex) && ex.code == 130 && marker == nil, "stopped: "+stderr.String())
}
