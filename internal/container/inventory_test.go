package container

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

func digest(b byte, size int64) Digest {
	d := Digest{Size: size}
	d.SHA256[0] = b
	return d
}

func sampleTop() Top {
	m := digest(2, 300)
	s := ShardRef{Prefix: "ab", Name: ShardName("ab", 4), Entries: 2}
	s.SHA256[0] = 9
	u := ShardRef{Prefix: "cd", Name: ShardName("cd", 5), Entries: 1}
	return Top{Gen: 5, Pointer: digest(1, 300), Mirror: &m, Fanout: 2, Shards: []ShardRef{s, u}}
}

func sampleShard() Shard {
	return Shard{Prefix: "ab", Files: []File{
		{Path: "chunks/ab/ab000000000000000000000000000001", Digest: digest(3, 4096), State: Live},
		{Path: "units/ab/ab0123456789abcdef0123456789abcd", Digest: digest(4, 812), State: Retained},
	}}
}

// covers: MA-750, R37
// The inventory's text is frozen: the top file's version, gen, pointer and mirror
// digests (or "mirror none"), fan-out and one line per shard; a shard's sorted
// "<path> <sha256> <size> <live|retained>" lines. Both parse back to exactly what was
// written, and only from their canonical bytes.
func TestTheInventoryIsTheFrozenText(t *testing.T) {
	z := strings.Repeat("0", 62)
	top, err := sampleTop().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	want := "mailarchive-inventory 1\ngen 5\npointer 01" + z + " 300\nmirror 02" + z + " 300\nfanout 2\n" +
		"ab inventory/ab.4 09" + z + " 2\ncd inventory/cd.5 00" + z + " 1\n"
	if string(top) != want {
		t.Fatalf("top file\n got  %q\n want %q", top, want)
	}
	got, err := ParseTop(top)
	if err != nil || !reflect.DeepEqual(got, sampleTop()) {
		t.Fatalf("parsed top %+v, %v", got, err)
	}
	noMirror := sampleTop()
	noMirror.Mirror = nil
	b, _ := noMirror.Marshal()
	if !strings.Contains(string(b), "\nmirror none\n") {
		t.Fatalf("a failed mirror write is recorded as %q", b)
	}
	if got, err := ParseTop(b); err != nil || got.Mirror != nil {
		t.Fatalf("mirror none parses to %+v, %v", got.Mirror, err)
	}

	shard, err := sampleShard().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	want = "chunks/ab/ab000000000000000000000000000001 03" + z + " 4096 live\n" +
		"units/ab/ab0123456789abcdef0123456789abcd 04" + z + " 812 retained\n"
	if string(shard) != want {
		t.Fatalf("shard\n got  %q\n want %q", shard, want)
	}
	if got, err := ParseShard("ab", shard); err != nil || !reflect.DeepEqual(got, sampleShard()) {
		t.Fatalf("parsed shard %+v, %v", got, err)
	}
	if _, err := ParseTop([]byte(strings.Replace(string(top), "gen 5", "gen  5", 1))); !errors.Is(err, ErrInventory) {
		t.Fatalf("a non-canonical top file: want ErrInventory, got %v", err)
	}
}

// covers: MA-750, R37
// A newer inventory version is ErrNewerFormat — "upgrade", never damaged — read
// before anything else (P2b-C11).
func TestANewerInventoryAsksForAnUpgrade(t *testing.T) {
	_, err := ParseTop([]byte("mailarchive-inventory 2\nwhatever comes next\n"))
	if !errors.Is(err, ErrNewerFormat) {
		t.Fatalf("want ErrNewerFormat, got %v", err)
	}
	rc, msg := refusal(err)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("upgrade", "inventory version 2"))
}

// covers: MA-750, R37
// A top file that breaks the rules is never written and never read: generation 0, a
// fan-out outside 2..4, a shard prefix of the wrong width, shards out of order, a
// shard name that is not its prefix's or is newer than the inventory, a shard with no
// entries, a pointer size outside the bound, a missing line, a carriage return, an
// empty line, or no final newline. Each is the typed ErrInventory.
func TestATopFileThatBreaksTheRulesIsRefused(t *testing.T) {
	// The generation and fan-out cases drop the shards, so no shard rule can refuse
	// them in their place.
	cases := map[string]func(*Top){
		"generation 0":        func(t *Top) { t.Gen = 0; t.Shards = nil },
		"fan-out 1":           func(t *Top) { t.Fanout = 1; t.Shards = nil },
		"fan-out 5":           func(t *Top) { t.Fanout = 5; t.Shards = nil },
		"prefix width":        func(t *Top) { t.Shards[0].Prefix = "abc"; t.Shards[0].Name = ShardName("abc", 4) },
		"out of order":        func(t *Top) { t.Shards[0], t.Shards[1] = t.Shards[1], t.Shards[0] },
		"another prefix name": func(t *Top) { t.Shards[0].Name = ShardName("ac", 4) },
		"newer shard":         func(t *Top) { t.Shards[1].Name = ShardName("cd", 6) },
		"no entries":          func(t *Top) { t.Shards[1].Entries = 0 },
		"pointer size 0":      func(t *Top) { t.Pointer.Size = 0 },
		"mirror over bound":   func(t *Top) { t.Mirror.Size = MaxPointerSize + 1 },
	}
	for label, mutate := range cases {
		t.Run(label, func(t *testing.T) {
			top := sampleTop()
			mutate(&top)
			out, err := top.Marshal()
			if !errors.Is(err, ErrInventory) || out != nil {
				t.Fatalf("Marshal: want ErrInventory and no bytes, got %d bytes, %v", len(out), err)
			}
			rc, msg := refusal(err)
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names("inventory"))
		})
	}
	bare := sampleTop()
	bare.Shards = nil
	if out, err := bare.Marshal(); err != nil || out == nil {
		t.Fatalf("positive twin: a top file with no shards (the empty first commit) is written: %v", err)
	}
	good, _ := sampleTop().Marshal()
	s := string(good)
	for label, raw := range map[string]string{
		"missing fanout line": strings.Replace(s, "fanout 2\n", "", 1),
		"carriage return":     strings.Replace(s, "gen 5\n", "gen 5\r\n", 1),
		"empty line":          strings.Replace(s, "gen 5\n", "gen 5\n\n", 1),
		"no final newline":    strings.TrimSuffix(s, "\n"),
		"upper-case digest":   strings.Replace(s, "pointer 01", "pointer 0A", 1),
		"newer shard":         strings.Replace(s, "inventory/cd.5", "inventory/cd.6", 1),
		"not an inventory":    "mailarchive-manifest 1\n",
	} {
		if _, err := ParseTop([]byte(raw)); !errors.Is(err, ErrInventory) {
			t.Errorf("%s: want ErrInventory, got %v", label, err)
		}
	}
}

// covers: MA-750, R37
// A shard that breaks the rules is never written and never read: a path that is not a
// unit or chunk path, or whose shard folder is not its name's, a file in another
// prefix's shard, files out of order or repeated, a size of 0, a state outside
// live/retained, or no files. Each is the typed ErrInventory.
func TestAShardThatBreaksTheRulesIsRefused(t *testing.T) {
	cases := map[string]func(*Shard){
		"not a unit path":    func(s *Shard) { s.Files[0].Path = "pages/ab/ab000000000000000000000000000001" },
		"wrong shard folder": func(s *Shard) { s.Files[0].Path = "chunks/cd/ab000000000000000000000000000001" },
		"another prefix":     func(s *Shard) { s.Files[1].Path = "units/ac/ac0123456789abcdef0123456789abcd" },
		"out of order":       func(s *Shard) { s.Files[0], s.Files[1] = s.Files[1], s.Files[0] },
		"repeated":           func(s *Shard) { s.Files[1] = s.Files[0] },
		"size 0":             func(s *Shard) { s.Files[0].Size = 0 },
		"no state":           func(s *Shard) { s.Files[0].State = 0 },
		"no files":           func(s *Shard) { s.Files = nil },
	}
	for label, mutate := range cases {
		t.Run(label, func(t *testing.T) {
			s := sampleShard()
			mutate(&s)
			out, err := s.Marshal()
			if !errors.Is(err, ErrInventory) || out != nil {
				t.Fatalf("Marshal: want ErrInventory and no bytes, got %d bytes, %v", len(out), err)
			}
			rc, msg := refusal(err)
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names("inventory"))
		})
	}
	good, _ := sampleShard().Marshal()
	for label, raw := range map[string]string{
		"unknown state":      strings.Replace(string(good), " live\n", " alive\n", 1),
		"read as another":    string(good),
		"three fields":       strings.Replace(string(good), " 4096 live", " 4096", 1),
		"non-canonical size": strings.Replace(string(good), " 4096 ", " 04096 ", 1),
	} {
		prefix := "ab"
		if label == "read as another" {
			prefix = "cd"
		}
		if _, err := ParseShard(prefix, []byte(raw)); !errors.Is(err, ErrInventory) {
			t.Errorf("%s: want ErrInventory, got %v", label, err)
		}
	}
}

// covers: MA-750, R37
// The line bound holds: more lines than a file may hold is ErrInventory (exercised
// at a small bound; the parsers pass their own).
func TestAnInventoryFileOverItsLineBoundIsRefused(t *testing.T) {
	if lines, err := splitLines([]byte("a\nb\n"), 2); err != nil || len(lines) != 2 {
		t.Fatalf("positive twin: two lines at a bound of two: %v, %v", lines, err)
	}
	if _, err := splitLines([]byte("a\nb\nc\n"), 2); !errors.Is(err, ErrInventory) {
		t.Fatalf("three lines at a bound of two: want ErrInventory, got %v", err)
	}
}

// covers: MA-750, R37
// A file's shard is the first d hex digits of its name, at fan-out 2 to 4; a path that
// is not a unit or chunk path, or a fan-out outside the format, is refused.
func TestAFileBelongsToTheShardOfItsNamePrefix(t *testing.T) {
	const p = "units/ab/abcdef0123456789abcdef0123456789"
	for d, want := range map[int]string{2: "ab", 3: "abc", 4: "abcd"} {
		if got, err := PrefixOf(p, d); err != nil || got != want {
			t.Errorf("PrefixOf(%d) = %q, %v; want %q", d, got, err, want)
		}
	}
	for _, bad := range []struct {
		path string
		d    int
	}{{p, 1}, {p, 5}, {"units/cd/abcdef0123456789abcdef0123456789", 2}, {"units/ab/abcdef", 2}, {"chunks/ab/ABCDEF0123456789abcdef0123456789", 2}} {
		if _, err := PrefixOf(bad.path, bad.d); !errors.Is(err, ErrInventory) {
			t.Errorf("PrefixOf(%q, %d): want ErrInventory, got %v", bad.path, bad.d, err)
		}
	}
}
