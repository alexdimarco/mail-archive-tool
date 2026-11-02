package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1041, R36
// uninstall's plan (-dry-run, or without -yes) says under the deletion log, when it holds any line,
// how to go on once it is copied — "Once it is copied, run mailarchive uninstall -yes -log-copied."
// — so -log-copied is not first learnt from -yes's refusal (the 1.5.3 re-walk's N10). Positive twin
// first: a log holding no line needs no -log-copied, and the plan does not say it.
func TestUninstallsPlanSaysHowToGoOnOnceTheLogIsCopied(t *testing.T) {
	const next = "Once it is copied, run mailarchive uninstall -yes -log-copied."
	env, cfg := uninstallHome(t)
	writeOwnerOnly(t, filepath.Join(cfg, "desktop-settings.json"), "{}")
	writeOwnerOnly(t, filepath.Join(cfg, "deletions.log"), "")
	code, out, errs := runUninstallIn(t, env, "-dry-run")
	if code != 0 || strings.Contains(out, "-log-copied") {
		t.Fatalf("the twin: exit %d\n%s%s", code, out, errs)
	}

	env, cfg = uninstallHome(t)
	writeOwnerOnly(t, filepath.Join(cfg, "desktop-settings.json"), "{}")
	writeOwnerOnly(t, filepath.Join(cfg, "deletions.log"), `{"at":"2026-11-01T10:00:00Z","event":"started","run":"r1","folder":"/gone/Old","row":"/gone/Old","kind":"plain"}`+"\n"+
		`{"at":"2026-11-01T10:00:02Z","event":"finished","run":"r1","folder":"/gone/Old","outcome":"complete"}`+"\n")
	for _, args := range [][]string{{"-dry-run"}, nil} {
		_, out, errs = runUninstallIn(t, env, args...)
		if !strings.Contains(out, "      Copy deletions.log first, somewhere you keep records (your Documents folder, for example), if you need it to check saved deletion records. "+next+"\n") {
			t.Fatalf("uninstall %v: the plan does not say how to go on:\n%s%s", args, out, errs)
		}
	}

	// A delete not finished: the plan gives no next step that would be refused (the settle pass's
	// LOW-4); it says the log note alone, then that uninstall will refuse until the delete is done.
	env, cfg = uninstallHome(t)
	writeOwnerOnly(t, filepath.Join(cfg, "desktop-settings.json"), "{}")
	writeOwnerOnly(t, filepath.Join(cfg, "deletions.log"), `{"at":"2026-11-01T10:00:00Z","event":"started","run":"r1","folder":"/gone/Old","row":"/gone/Old","kind":"plain"}`+"\n")
	for _, args := range [][]string{{"-dry-run"}, nil} {
		_, out, errs = runUninstallIn(t, env, args...)
		if strings.Contains(out+errs, next) || strings.Contains(out+errs, "-log-copied") ||
			!strings.Contains(out, "      Copy deletions.log first, somewhere you keep records (your Documents folder, for example), if you need it to check saved deletion records.\n") ||
			!strings.Contains(errs, "Uninstall will refuse until the delete of /gone/Old is finished") {
			t.Fatalf("uninstall %v with a delete not finished:\n%s%s", args, out, errs)
		}
	}
}
