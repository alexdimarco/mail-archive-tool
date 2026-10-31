package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/graphconfig"
)

// overviewLede is the Overview's top line: the text of its first lede paragraph.
func overviewLede(t *testing.T, body string) string {
	t.Helper()
	const open = `<p class="lede">`
	i := strings.Index(body, open)
	if i < 0 {
		t.Fatal("the Overview has no top line")
	}
	rest := body[i+len(open):]
	j := strings.Index(rest, "</p>")
	if j < 0 {
		t.Fatal("the Overview's top line is not closed")
	}
	return rest[:j]
}

// covers: MA-937, S41
// The Overview's top line names Microsoft 365 only when a Microsoft 365 archive is
// actually set up (owner report 2026-10-31, seen in the Windows Desktop: with no
// archive the line said "Your personal Microsoft 365 archive, running locally on
// this PC."). The positive twin first: an archive made from a Microsoft 365 mailbox
// is called one, with or without a Microsoft 365 connection saved here. Then: no
// archive chosen (with and without a saved connection) gets neutral words that say
// how to begin; an archive of local mail files, and a mixed one, are "your mail
// archive" even where a Microsoft 365 connection is saved. No case says "this PC".
func TestOverviewTopLineNamesMicrosoft365OnlyForAMicrosoft365Archive(t *testing.T) {
	const (
		m365Line    = "Your Microsoft 365 mail archive, kept on this computer."
		plainLine   = "Your mail archive, kept on this computer."
		neutralLine = "MailArchive keeps a copy of your mail on this computer. Open or make an archive below to begin."
	)
	dir := t.TempDir()
	tenant := filepath.Join(dir, "graph-config.json")
	if err := graphconfig.Save(tenant, &graphconfig.Config{Tenant: "contoso", ClientID: "APPID", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	none := filepath.Join(dir, "none.json")
	m365 := archiveAt(t, dir, "M365", map[string]string{"mailbox:u@contoso.com": "b"})
	local := archiveAt(t, dir, "Local", map[string]string{"path:/mail/Local Folders": "b"})
	mixed := archiveAt(t, dir, "Mixed", map[string]string{"mailbox:u@contoso.com": "b", "path:/mail/Local Folders": "c"})

	for _, c := range []struct {
		what, config, out, want string
	}{
		// Positive twin first: a Microsoft 365 archive is called one.
		{"a Microsoft 365 archive with its connection saved", tenant, m365, m365Line},
		{"a Microsoft 365 archive with no connection saved here", none, m365, m365Line},
		{"no archive and no connection", none, "", neutralLine},
		{"no archive with a connection saved", tenant, "", neutralLine},
		{"a local-files archive with a connection saved", tenant, local, plainLine},
		{"a local-files archive with no connection", none, local, plainLine},
		{"a mixed archive", tenant, mixed, plainLine},
	} {
		body := getOverview(t, Config{ConfigPath: c.config, SettingsPath: filepath.Join(t.TempDir(), "s.json"),
			TokenCachePath: filepath.Join(t.TempDir(), "tok.json"), Out: c.out})
		got := overviewLede(t, body)
		if got != c.want {
			t.Errorf("%s: the top line is %q, want %q", c.what, got, c.want)
		}
		if strings.Contains(got, "this PC") {
			t.Errorf("%s: the top line says \"this PC\": %q", c.what, got)
		}
	}
}
