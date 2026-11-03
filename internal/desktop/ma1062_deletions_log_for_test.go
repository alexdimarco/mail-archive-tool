package desktop

import (
	"runtime"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1062, R36
// desktop.DeletionsLogFor names the deletion log beside a -config FILE, and it is the one a
// MailArchive Desktop started with -config FILE writes (Config.deletionsLogPath) and the one
// `delete-archive -config FILE` writes (NewCommandLineDeleteAt), so the terminal's pause, resume and
// schedule given the same -config read the log those deletes wrote (G3, design-archive-delete
// §16.2, G3-1, G3-3, G3-6). "" is "" (MailArchive's own log, as archdelete reads an empty path).
// Each computer's own spellings: on Windows a drive path with spaces, a network path and a drive
// root; on macOS and Linux a path with spaces and a dot folder. Positive twin first: a Desktop with
// no config path writes MailArchive's own log.
func TestMA1062_DeletionsLogForIsTheDesktopsAndTheDeleteVerbsLog(t *testing.T) {
	own, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	if got := (Config{}).deletionsLogPath(); got != assure.Reached(t, own, "MailArchive's own deletion log") {
		t.Fatalf("positive twin: a Desktop with no config path writes %q, want %q", got, own)
	}
	if got := DeletionsLogFor(""); got != "" {
		t.Fatalf(`DeletionsLogFor("") = %q, want "" (MailArchive's own)`, got)
	}

	cases := map[string]string{
		"/Users/a/My Config/desktop.json": "/Users/a/My Config/deletions.log",
		"/home/a/.mailarchive-two/x.json": "/home/a/.mailarchive-two/deletions.log",
		" /home/a/Spaced/desktop.json ":   "/home/a/Spaced/deletions.log",
	}
	if runtime.GOOS == "windows" {
		cases = map[string]string{
			`C:\Users\A\My Config\desktop.json`:      `C:\Users\A\My Config\deletions.log`,
			`\\server\share\mailarchive\config.json`: `\\server\share\mailarchive\deletions.log`,
			`E:\config.json`:                         `E:\deletions.log`,
			` C:\Users\A\Spaced\desktop.json `:       `C:\Users\A\Spaced\deletions.log`,
		}
	}
	for cfg, want := range cases {
		if got := DeletionsLogFor(cfg); got != want {
			t.Errorf("DeletionsLogFor(%q) = %q, want %q", cfg, got, want)
		}
		if got := (Config{ConfigPath: cfg}).deletionsLogPath(); got != want {
			t.Errorf("the Desktop started with -config %q writes %q, want %q", cfg, got, want)
		}
		if got := NewCommandLineDeleteAt(cfg, nil, nil, nil).LogPath(); got != want {
			t.Errorf("delete-archive -config %q writes %q, want %q", cfg, got, want)
		}
	}
}
