package encmark

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-784
// Cases L, 1, 2 and 4 offer forgetting the folder by the verb that does it, naming
// the folder (§4.6); an unproven path and missing key files do not (0: not until it
// is proven; 3: restore the folder instead).
func TestTheCasesThatOfferForgettingNameTheVerb(t *testing.T) {
	const folder = "/Volumes/Backup/Mail Archive"
	offers := 0
	for c, want := range map[Case]bool{Link: true, Foreign: true, Other: true, Gone: true, Unproven: false, KeysMissing: false} {
		w := Wording(c, folder)
		got := strings.Contains(w, "mailarchive forget-archive -out \""+folder+"\"")
		if got != want {
			t.Errorf("case %v offers forget-archive = %v, want %v: %s", c, got, want, w)
		}
		if got {
			offers++
		}
		if strings.Contains(w, "cannot write here") {
			t.Errorf("case %v still has the interim wording: %s", c, w)
		}
	}
	assure.Reached(t, offers == 4, "four cases offering forget-archive")
}
