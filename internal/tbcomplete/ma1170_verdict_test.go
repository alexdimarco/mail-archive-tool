package tbcomplete

import (
	"errors"
	"path/filepath"
	"testing"
)

// covers: MA-1170, MA-1171
// What the reader's facts mean for the verdict (C1b's amendment of MA-1170 and MA-1171; §4.1 step
// 2's M2 and L2 folds, §4.4's fold table, the C1a pass's notes for C1b): a cache that could not be
// read, a store with no keys or keys it cannot tell, are unknown (cache_unreadable, the "could not
// read Thunderbird's list of folders" words); a key twice or two keys for one folder duplicate_key;
// a refused key invalid_entry (L2's cautious reading, kept); a folder with only an old-path key
// still in the store old_path_only, or behind when the other folders are behind; a copied profile
// with old and new keys for every folder is checked; Panorama, by its file or its setting, or a
// signal that cannot be read, is unknown (panorama), decided before the cache. Positive twin first.
func TestTheReadersFactsGiveTheVerdict(t *testing.T) {
	s := vstore(t, at("INBOX", folder(FlagInbox, 10, 10)))
	disk := map[string]int64{"INBOX": 10}
	want(t, "the positive twin", Assess(healthy(s, disk)), CaughtUp, NoReason)

	t.Run("the cache refused or unread", func(t *testing.T) {
		for _, k := range []CacheErrorKind{CacheMissing, CacheNotRegular, CacheUnreadable, CacheMalformed} {
			in := healthy(s, disk) // a store that would read caught up: the error alone decides
			in.CacheErr = &CacheError{Kind: k, Path: "folderCache.json", Err: errors.New("x")}
			want(t, "cache error", Assess(in), Unknown, ReasonCacheUnreadable)
		}
		want(t, "no keys", Assess(healthy(Store{Mode: NoKeys}, disk)), Unknown, ReasonCacheUnreadable)
		want(t, "keys it cannot tell", Assess(healthy(Store{Mode: CantTellKeys, CantTellWhy: "two places"}, disk)), Unknown, ReasonCacheUnreadable)
		want(t, "a mode nobody set", Assess(healthy(Store{Mode: Mode(99), Folders: s.Folders}, disk)), Unknown, ReasonCacheUnreadable)
	})

	t.Run("duplicates and refusals", func(t *testing.T) {
		dupKey := parseStore(t, cacheJSON(t, pair{vStore, root()}, at("INBOX", folder(FlagInbox, 10, 10)), at("INBOX", folder(FlagInbox, 10, 10))), vStore, "linux")
		want(t, "a key twice", Assess(healthy(dupKey, disk)), Unknown, ReasonDuplicateKey)
		oldTwice := parseStore(t, cacheJSON(t, pair{vStore, root()}, at("INBOX", folder(FlagInbox, 10, 10)),
			pair{"/old/ImapMail/h/INBOX.msf", folder(FlagInbox, 1, 1)}, pair{"/old/ImapMail/h/INBOX.msf", folder(FlagInbox, 1, 1)}), vStore, "linux")
		if len(oldTwice.DuplicateKeys) != 1 || len(oldTwice.Duplicates) != 0 {
			t.Fatalf("the old key twice fixture: %+v", oldTwice)
		}
		want(t, "an old-path key twice beside an exact key", Assess(healthy(oldTwice, disk)), Unknown, ReasonDuplicateKey)
		twin := parseStore(t, cacheJSON(t, pair{"/old/ImapMail/h/Café.msf", folder(0, 1, 1)}, pair{"/old/ImapMail/h/Café.msf", folder(0, 1, 1)}), vStore, "linux")
		if twin.Mode != Tail || len(twin.Duplicates) != 1 {
			t.Fatalf("the tail twins fixture: %+v", twin)
		}
		want(t, "two tail keys for one folder", Assess(healthy(twin, nil)), Unknown, ReasonDuplicateKey)
		refused := parseStore(t, cacheJSON(t, pair{vStore, root()}, at("INBOX", folder(FlagInbox, 10, 10)), pair{vStore + "/x.sbd/../A.msf", folder(0, 1, 1)}), vStore, "linux")
		if len(refused.Refused) != 1 {
			t.Fatalf("the refused fixture: %+v", refused)
		}
		want(t, "a refused key", Assess(healthy(refused, disk)), Unknown, ReasonInvalidEntry)
	})

	t.Run("Panorama, before the cache", func(t *testing.T) {
		for name, p := range map[string]Profile{
			"the file":                   {PanoramaFile: Yes, PanoramaPref: No},
			"the setting":                {PanoramaFile: No, PanoramaPref: Yes},
			"the file cannot be told":    {PanoramaFile: CantTell, PanoramaPref: No},
			"the setting cannot be told": {PanoramaFile: No, PanoramaPref: CantTell},
		} {
			in := healthy(s, disk)
			in.Profile = p
			want(t, name, Assess(in), Unknown, ReasonPanorama)
			in.CacheErr = &CacheError{Kind: CacheMissing}
			want(t, name+", no cache", Assess(in), Unknown, ReasonPanorama)
		}
	})

	t.Run("M2: old-path keys", func(t *testing.T) {
		p, store := newProfile(t, "imap.example.com")
		write(t, filepath.Join(store, "Sent"), "From x\n")
		old := "/old/Profiles/x/ImapMail/imap.example.com"
		cache := func(inboxListed int) []pair {
			return []pair{{store, root()}, {filepath.Join(store, "INBOX.msf"), folder(FlagInbox, inboxListed, inboxListed)},
				{old + "/Sent.msf", folder(FlagSentMail, 5, 5)}}
		}
		writeCache(t, p, cache(10)...)
		st := readStore(t, p, store)
		if len(st.OldPathOnly) != 1 {
			t.Fatalf("the fixture: %+v", st)
		}
		r := Assess(healthy(st, map[string]int64{"INBOX": 10, "Sent": 1}))
		want(t, "one folder with an old key only, the rest caught up", r, Unknown, ReasonOldPathOnly)
		if r.CantTellFolders != 1 || r.Listed != 10 {
			t.Fatalf("the can't-tell folder is counted and left out of Y: %+v", r)
		}
		writeCache(t, p, cache(100)...)
		want(t, "the same, the rest behind", Assess(healthy(readStore(t, p, store), map[string]int64{"INBOX": 10})), Behind, NoReason)
		writeCache(t, p, pair{store, root()}, pair{filepath.Join(store, "INBOX.msf"), folder(FlagInbox, 10, 10)},
			pair{old, root()}, pair{old + "/INBOX.msf", folder(FlagInbox, 3, 3)}, pair{old + "/Gone.msf", folder(0, 9, 9)})
		r = Assess(healthy(readStore(t, p, store), disk))
		want(t, "a copied profile with old and new keys", r, CaughtUp, NoReason)
		if r.IgnoredOldKeys != 3 {
			t.Fatalf("old keys ignored and counted: %d", r.IgnoredOldKeys)
		}
	})
}
