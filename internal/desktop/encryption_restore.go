package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// Put back the key file (design-encrypted-archives-phase3 §9.4, §9.5, K15; the P3-3
// plan, 3e): an archive this computer recorded whose key files are missing or damaged
// beside its sealed messages cannot be unlocked, so its card offers to put them back —
// from its recovery file and recovery key, or from the key this computer remembers —
// with a new password and a new recovery key, shown and typed back (K2) through the
// same steps as a new recovery key before anything is written.

// pickFile is the system's file dialog, a seam a test replaces.
var pickFile = func(ctx context.Context, title string) (string, error) {
	return zenity.SelectFile(zenity.Title(title), zenity.Context(ctx))
}

// maxRecoveryFileRead bounds the recovery file read: a real one is under 4 KiB.
const maxRecoveryFileRead = 4 << 10

// keysMissingHere reports that out is a folder this computer recorded as an
// encrypted archive whose key files are missing or damaged beside its sealed data.
func keysMissingHere(out string) bool {
	res, err := encmark.At(out)
	return err == nil && res.Recorded && res.Case == encmark.KeysMissing
}

// restoreRequest is what the card sends to put back the key file.
type restoreRequest struct {
	From        string        `json:"from"` // "file" or "remembered"
	RecoveryKey string        `json:"recoveryKey"`
	NewPassword string        `json:"newPassword"`
	Again       string        `json:"again"`
	ReRecord    bool          `json:"reRecord"`
	Cost        *vault.Params `json:"cost,omitempty"`
}

// restoreAction prepares the key file to put back and shows its new recovery key; the
// confirm step writes it.
func (d *dashboard) restoreAction(w http.ResponseWriter, r *http.Request) {
	var in restoreRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSecretBody)).Decode(&in); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "That request was not understood — reload the page and try again."})
		return
	}
	defer func() { in.RecoveryKey, in.NewPassword, in.Again = "", "", "" }()
	out := d.cfg.effectiveOut()
	if !keysMissingHere(out) {
		writeJSON(w, map[string]any{"ok": false, "error": "The key file is put back only for an archive this computer recorded whose key files are missing or damaged."})
		return
	}
	if p := keyring.NewPasswordProblem(in.NewPassword); p != "" {
		writeJSON(w, map[string]any{"ok": false, "error": p})
		return
	}
	if in.NewPassword != in.Again {
		writeJSON(w, map[string]any{"ok": false, "error": "The two new passwords do not match."})
		return
	}
	var askedCost *vault.Params
	askedReRecord := false
	o := keyring.RestoreOptions{
		ConfirmCost: func(p vault.Params) (bool, error) {
			if c := in.Cost; c != nil && p.MemoryKiB <= c.MemoryKiB && p.Time <= c.Time && p.Threads <= c.Threads {
				return true, nil
			}
			askedCost = &p
			return false, nil
		},
		ConfirmReRecord: func(string) (bool, error) {
			askedReRecord = !in.ReRecord
			return in.ReRecord, nil
		}}
	var p *keyring.PendingRestore
	var err error
	switch in.From {
	case "remembered":
		p, err = keyring.RestoreFromRemembered(out, d.cfg.Store, in.NewPassword, o)
	case "file":
		// A typing mistake is told before the file dialog opens, in words that quote
		// none of what was typed (HTTP-01).
		if perr := keyring.RecoveryKeyProblem(in.RecoveryKey); perr != nil {
			writeJSON(w, map[string]any{"ok": false, "error": "That is not a recovery key (" + perr.Error() + "). Type it again from where you saved it."})
			return
		}
		ctx, cancel := context.WithTimeout(d.life.Context(), pickerBudget)
		defer cancel()
		stop := context.AfterFunc(r.Context(), cancel)
		defer stop()
		path, perr := pickFile(ctx, "Choose this archive's recovery file")
		if perr != nil || strings.TrimSpace(path) == "" {
			writeJSON(w, map[string]any{"ok": true, "canceled": true})
			return
		}
		data, rerr := readRecoveryFile(path)
		if rerr != nil {
			writeJSON(w, map[string]any{"ok": false, "error": rerr.Error()})
			return
		}
		p, err = keyring.RestoreFromFile(out, data, keyring.RecoveryKey(in.RecoveryKey), in.NewPassword, o)
	default:
		writeJSON(w, map[string]any{"ok": false, "error": "Choose where the key comes from: the recovery file, or the key this computer remembers."})
		return
	}
	switch {
	case err == nil:
	case askedCost != nil && errors.Is(err, keyring.ErrCostRefused):
		writeJSON(w, map[string]any{"ok": false, "confirm": "cost", "cost": askedCost,
			"error": fmt.Sprintf("Opening this recovery file would use %s (MailArchive's own key files use %s). Go ahead?", askedCost, vault.CostFloor())})
		return
	case askedReRecord && errors.Is(err, keyring.ErrReRecordRefused):
		writeJSON(w, map[string]any{"ok": false, "confirm": "rerecord",
			"error": "You once told MailArchive to forget this encrypted archive on this computer. Putting back its key file records it here again. Go ahead?"})
		return
	case errors.Is(err, vault.ErrWrongSecret):
		writeJSON(w, map[string]any{"ok": false, "error": "That recovery key does not open this recovery file."})
		return
	default:
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	abs, key := keyOf(out)
	token, ok := d.keep(w, &pendingItem{key: key, out: abs, res: p})
	if !ok {
		return
	}
	writeJSON(w, map[string]any{"ok": true, "token": token, "recoveryKey": p.RecoveryKey(), "warning": keyring.RecoveryWarning})
}

// readRecoveryFile reads a recovery file the person picked, bounded and never through
// a link: the open neither follows a link nor waits on a pipe swapped in after the
// check, and the file opened is checked again (the P3-3 pass, OUT-B).
func readRecoveryFile(path string) ([]byte, error) {
	if fi, err := os.Lstat(path); err == nil && fi.Mode().IsRegular() && fi.Size() > maxRecoveryFileRead {
		return nil, tooLargeForRecoveryFile(path)
	}
	data, err := util.ReadRegularFile(path, maxRecoveryFileRead)
	if nr := (*util.NotRegularError)(nil); errors.As(err, &nr) {
		return nil, fmt.Errorf("%s is not a file", path)
	}
	return data, err
}

func tooLargeForRecoveryFile(path string) error {
	return fmt.Errorf("%s is larger than a recovery file is; choose the recovery file saved with the archive's recovery key", path)
}

// commitRestore writes the key file a typed-back restore prepared (§9.4): once the
// first copy is written it is in effect, whatever follows, and is said so.
func (d *dashboard) commitRestore(w http.ResponseWriter, it *pendingItem) {
	// Only for the current archive: one switched away from is not written to unasked
	// (the P3-3 pass, KEY-05).
	if _, key := keyOf(d.cfg.effectiveOut()); key != it.key {
		it.discard()
		writeJSON(w, map[string]any{"ok": false, "error": "That key file is no longer waiting — the current archive changed. Put it back again from its card."})
		return
	}
	err := it.res.Commit()
	if !keyring.InEffect(err) {
		it.res.Discard() // the reply says to start again: this one's key goes now (KEY-04)
		if it.file != "" {
			_ = keyring.VoidFile(it.file)
		}
		writeJSON(w, map[string]any{"ok": false, "error": "The key file was not put back: " + err.Error()})
		return
	}
	lines := []string{"The key file was put back: unlock the archive with the new password."}
	if err != nil {
		lines = append(lines, err.Error())
	}
	if it.file != "" {
		lines = append(lines, keyring.EarlierKeyWarning, keyring.RecoveryFileNote)
	} else {
		lines = append(lines, keyring.NoRecoveryFileWarning("New recovery key on this card once it is unlocked, saving its recovery file"))
	}
	writeJSON(w, map[string]any{"ok": true, "lines": lines})
}
