package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/vault"
)

// CommandLineDelete is the Desktop's delete as `mailarchive delete-archive` and `mailarchive
// unlist-archive` reach it (docs/design-archive-delete.md §3.3, slice 4): the very seams the
// Desktop's own delete uses (MA-973) — discovery with the scheduler asked, the settings file and
// the list of archives, the pointers, the keyring adapter, the scratch folders, deletions.log —
// read from where a Desktop started without -config keeps them. The command line refuses while a
// Desktop runs, so here nothing of this process runs on the folder and no archive is open in a
// page.
type CommandLineDelete struct {
	cfg   Config
	store vault.Store
}

// NewCommandLineDelete reads the Desktop's default configuration. store is the secret store a
// remembered key is forgotten from (nil keeps it); sched and installed are this computer's
// scheduler and its installed entries (nil: the real ones; tests give their own).
func NewCommandLineDelete(store vault.Store, sched archdelete.Scheduler, installed func() []schedule.InstalledEntry) (*CommandLineDelete, error) {
	cfgPath, err := graphconfig.DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	cfg := Config{ConfigPath: cfgPath, SettingsPath: settingsPathFor(cfgPath), IndexPath: indexPathFor(cfgPath),
		Version: buildVersion(), DeleteScheduler: sched, DeleteInstalled: installed}
	return &CommandLineDelete{cfg: cfg, store: store}, nil
}

// PlannedText is the saved plan's text (§8.1, Status "planned, nothing deleted"), headed as the
// Desktop's Save this plan is: the time, this computer, the account and MailArchive's version.
func (c *CommandLineDelete) PlannedText(p *archdelete.DeletePlan) []byte {
	host, _ := os.Hostname()
	return archdelete.RecordText(archdelete.RecordInput{Status: archdelete.StatusPlanned, When: time.Now(),
		Computer: host, Account: c.cfg.account(), Version: c.cfg.Version, Plan: p})
}

// settingsPathFor is the Desktop's settings file beside its config file.
func settingsPathFor(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), "desktop-settings.json")
}

// indexPathFor is the Desktop's list of archives beside its settings (SD-6).
func indexPathFor(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), archindex.Name)
}

// Row is §3.1's row rule: the row of Your archives that path is, by its own spelling first,
// then by file identity while the folder can be read; ok is false when it is no row. err says
// the list itself could not be read, so whether path is a row cannot be told. The rows are
// those the Desktop draws: its list, and the archives its tidy-up adds to the list when it
// draws the page (reconcileArchives: the current archive, the folder a scheduled backup is
// for, the archive the wizard made last), added here in memory only, no file written (del-s4
// residual 3).
func (c *CommandLineDelete) Row(path string) (row string, ok bool, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false, nil
	}
	idx, err := archindex.ReadFile(c.cfg.IndexPath)
	if err != nil {
		return "", false, err
	}
	if row, ok := rowIn(idx, path); ok {
		return row, true, nil
	}
	row, ok = rowIn(c.drawn(idx), path)
	return row, ok, nil
}

// drawn is idx as the Desktop draws its page: the archives its tidy-up adds added, in memory.
func (c *CommandLineDelete) drawn(idx archindex.Index) archindex.Index {
	cfg := c.cfg
	cfg.LastArchive = OfferedArchive // as the Desktop's launch sets it
	return archindex.Reconcile(idx, cfg.archivePointers(), time.Now())
}

// drawnArchives are the archives the Desktop's tidy-up adds when it draws its page, listed or
// not: known archives as its list is (§7's other known archive, §8.1's saved file), since on the
// Desktop they are in its list by then (the residual fixes' adversarial pass, finding 4).
func (c *CommandLineDelete) drawnArchives() []string {
	var paths []string
	for _, e := range c.drawn(archindex.Index{}).Entries {
		paths = append(paths, e.Path)
	}
	return paths
}

// ListRow writes row into the Desktop's list when it is a row only as the Desktop draws it,
// as the Desktop's tidy-up would on its next page load; a listed row writes nothing. A delete
// and Remove from list call it right before they change anything: a delete's step 4 clears the
// settings that drew the row, and a delete stopped after that must leave a row, "Delete not
// finished", to run again (the residual fixes' adversarial pass, finding 1). Step 9 then removes
// it as any row.
func (c *CommandLineDelete) ListRow(row string) error {
	idx, err := archindex.ReadFile(c.cfg.IndexPath)
	if err != nil {
		return err
	}
	n := len(idx.Entries)
	idx = archindex.Reconcile(idx, []string{row}, time.Now())
	if len(idx.Entries) == n {
		return nil
	}
	return archindex.WriteFile(c.cfg.IndexPath, idx)
}

// ListPath is the Desktop's list of archives (known-archives.json) these verbs read.
func (c *CommandLineDelete) ListPath() string { return c.cfg.IndexPath }

// KnownArchives are the archive folders the Desktop knows: its list, the archives it adds to
// the list when it draws its page, and this computer's record of encrypted archives (§8.1: a
// saved file is never put inside one). err says part of them could not be read.
func (c *CommandLineDelete) KnownArchives() ([]string, error) {
	paths, err := c.cfg.knownArchivesErr()
	return append(paths, c.drawnArchives()...), err
}

// PlanDeps are the facts and seams a plan reads (§3.1, §7). The busy row is another program's
// run holding the archive lock (a scheduled backup, serve's rebuild): nothing of this process
// runs. The other known archives §7 protects include those the Desktop draws.
func (c *CommandLineDelete) PlanDeps() archdelete.Deps {
	deps := c.cfg.deleteDeps(func(folder string) bool { return lockfile.Held(filepath.Join(folder, lockfile.Name)) }, nil)
	deps.KeyPlan = desktopKeyring{store: c.store}.PlanForget // the plan's key line (§6, §14)
	deps.ScratchDir = app.ArchiveScratchFor                  // a reader held elsewhere refuses at the plan (§3.1)
	deps.Protected.Archives = append(deps.Protected.Archives, c.drawnArchives()...)
	return deps
}

// ExecDeps are Execute's seams for a delete planned from row, with before the command line's
// re-check before steps 4, 8 and 9 (§3.3). There is no page (NoPage): no archive is open here to
// lock in one, and a reader another program holds is refused at the plan and again at step 1 (c). Under the delete's
// own lock nothing of this process can be busy on the folder, and another program's run was
// refused by that lock, so the busy row asks nothing more.
func (c *CommandLineDelete) ExecDeps(row string, before func(step int) error) archdelete.ExecDeps {
	host, _ := os.Hostname()
	x := c.cfg.deleteExecDeps(row, func(string) bool { return false }, c.store, time.Now, host)
	x.NoPage, x.BeforeStep = true, before
	x.Deps.Protected.Archives = append(x.Deps.Protected.Archives, c.drawnArchives()...) // as PlanDeps: Execute plans again
	return x
}

// UnlistDeps are Remove from list's seams, stopping only the names its question showed.
func (c *CommandLineDelete) UnlistDeps(named []string) archdelete.UnlistDeps {
	return c.cfg.deleteUnlistDeps(named)
}
