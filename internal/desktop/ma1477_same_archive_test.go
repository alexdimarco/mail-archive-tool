package desktop

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-1475, MA-1477, R36
// The "changed" reply says whether it is still the same folder the plan was made for (the 1.7.5
// re-verify's RV-03): sameArchive is true when the archive's own manifest changed in the same folder
// (as a backup in between does), and never true when the folder was replaced by another of the same
// name during the check, so the page re-arms (Q9) only in the first case. Positive twin first.
func TestTheChangedReplySaysWhetherItIsStillTheSameFolder(t *testing.T) {
	e := newDeleteEnv(t)
	x := e.plainArchive("Xsame")
	p := e.plan(x)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(x, ".mailarchive-manifest.json"), later, later); err != nil {
		t.Fatal(err)
	}
	if r := e.deleteWith(p["token"].(string), "Xsame", false); r["changed"] != true || r["sameArchive"] != true {
		t.Fatalf("positive twin: the manifest changed in the same folder: changed %v, sameArchive %v (%v)", r["changed"], r["sameArchive"], replyErr(r))
	}

	p = e.plan(x)
	if err := os.Rename(x, x+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(x, 0o755); err != nil {
		t.Fatal(err)
	}
	buildArchiveAt(t, x) // another archive, same name, same place
	r := e.deleteWith(p["token"].(string), "Xsame", false)
	if _, err := os.Stat(filepath.Join(x, ".mailarchive-manifest.json")); r["ok"] == true || err != nil || r["sameArchive"] == true {
		t.Errorf("a folder replaced during the check: ok %v, sameArchive %v, the new folder kept %v (%v)", r["ok"], r["sameArchive"], err == nil, replyErr(r))
	}
}
