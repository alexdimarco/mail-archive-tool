package schedule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-894, R37
// The package's tests reach only the throwaway home: the settings folder a job file is
// written to (on every OS, %APPDATA% included), the cache folder and the home folder
// are all under the home TestMain made, never the developer's.
func TestScheduleTestsSeeOnlyTheThrowawayHome(t *testing.T) {
	under := func(p string) bool {
		rel, err := filepath.Rel(testHome, p)
		return err == nil && !strings.HasPrefix(rel, "..")
	}
	for name, get := range map[string]func() (string, error){
		"settings": os.UserConfigDir, "cache": os.UserCacheDir, "home": os.UserHomeDir,
	} {
		if p, err := get(); err != nil || !under(p) {
			t.Errorf("the %s folder %q (%v) is not under the throwaway home %s", name, p, err, testHome)
		}
	}
}
