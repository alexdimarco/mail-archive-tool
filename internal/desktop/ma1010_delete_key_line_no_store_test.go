package desktop

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/encrecord"
)

// covers: MA-1010, R36, R37
// A Desktop whose secret store could not be opened (launch keeps going with none: a Linux session
// with no Secret Service) says on the plan panel what step 8 then does with the last recorded
// copy's key: the record, any remembered key and the row are kept, even when the folder is
// removed (the core-plan-record adversarial pass, finding 1). Before, the panel said the key would
// be forgotten, and the result then said this computer still remembers it. The delete itself
// agrees: the folder is gone, the result says the key is still remembered, the record still names
// the folder and the row stays.
func TestWithNoSecretStoreThePlanPanelSaysTheKeyIsKept(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = nil; w.cfg.Keys = newArchiveKeys(nil, readerLockedNote) })
	e.list(out)

	p := e.plan(out)
	const noStore = "This computer's secret store could not be opened, so this computer will keep its record of this archive, any key it remembers for it and its row, even if the folder is removed. Use Forget this encrypted archive here later."
	if p["ok"] != true || p["keyLine"] != noStore {
		t.Fatalf("the plan with no secret store says %q (%v); want %q", p["keyLine"], p["error"], noStore)
	}

	r := e.deleteWith(p["token"].(string), p["name"].(string), false)
	lines := fmt.Sprint(r["lines"])
	if !strings.Contains(lines, "this computer still remembers its key") {
		t.Fatalf("the result does not say the key is kept: %v", r)
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatalf("the folder is still there: %v (%v)", err, r)
	}
	rec, err := encrecord.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, still := rec.Lookup(out); !still || !e.listed(out) {
		t.Fatalf("the record (%v) and the row (%v) are kept, as the plan said", still, e.listed(out))
	}
}
