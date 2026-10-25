package container

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

func refusal(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	return 1, err.Error()
}

func name(b byte) Name {
	var n Name
	for i := range n {
		n[i] = b
	}
	return n
}

func entry(i uint32, b byte, gen uint64) Entry {
	e := Entry{I: i, Name: name(b), Gen: gen, Size: 1000 + uint32(b)}
	e.SHA256[0] = b
	return e
}

// samplePointer is a generation-3 pointer with chunks in every section,
// manifest chunks mirrored (two entries per position).
func samplePointer() Pointer {
	return Pointer{
		Head:     Head{Format: vault.FormatVersion, Gen: 3, RunStart: 1761350400, Inv: 2, Buckets: 16, IndexMeta: 4, IndexImage: 524288, IndexChunk: 262144},
		Manifest: []Entry{entry(0, 1, 3), entry(0, 2, 3), entry(1, 3, 1), entry(1, 4, 1)},
		History:  []Entry{entry(0, 5, 2)},
		Index:    []Entry{entry(0, 6, 1), entry(1, 7, 3)},
	}
}

// covers: MA-750, R37
// A sealed root pointer opens to exactly the pointer that was sealed, under its own
// archive's key; its file starts "MAPF" ‖ format u16 ‖ gen u64; and Peek reads that
// format and generation without the key.
func TestAPointerRoundTripsAndPeeksWithoutTheKey(t *testing.T) {
	p := samplePointer()
	raw, err := Seal(keys[0], p)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(raw[:fileHeadSize]); got != "4d415046"+"0001"+"0000000000000003" {
		t.Fatalf("pointer file head %s", got)
	}
	format, gen, err := Peek(raw)
	if err != nil || format != vault.FormatVersion || gen != 3 {
		t.Fatalf("Peek = %d, %d, %v", format, gen, err)
	}
	got, err := Open(keys[0], raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("opened\n %+v\nwant\n %+v", got, p)
	}
	empty := Pointer{Head: Head{Format: vault.FormatVersion, Gen: 1}}
	raw, err = Seal(keys[0], empty)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Open(keys[0], raw); err != nil || !reflect.DeepEqual(got, empty) {
		t.Fatalf("the empty first commit's pointer: %+v %v", got, err)
	}
}

// covers: MA-750, R37
// The plaintext layout is frozen: u32 head length ‖ head JSON ‖ for each of manifest,
// history and index, u32 count ‖ 64-byte entries (i u32 ‖ name 16 ‖ gen u64 ‖ sha256 32
// ‖ size u32), all big-endian.
func TestThePointerPlaintextIsTheFrozenLayout(t *testing.T) {
	p := Pointer{Head: Head{Format: 1, Gen: 2, Buckets: 16}, Manifest: []Entry{entry(7, 0xab, 2)}}
	plain, err := p.encode()
	if err != nil {
		t.Fatal(err)
	}
	head := `{"format":1,"gen":2,"run_start":0,"inv":0,"buckets":16,"index_meta":0,"index_image":0,"index_chunk":0}`
	want := hex.EncodeToString(binary.BigEndian.AppendUint32(nil, uint32(len(head)))) + hex.EncodeToString([]byte(head)) +
		"00000001" + "00000007" + strings.Repeat("ab", 16) + "0000000000000002" + "ab" + strings.Repeat("00", 31) + "00000493" +
		"00000000" + "00000000"
	if got := hex.EncodeToString(plain); got != want {
		t.Fatalf("plaintext\n got  %s\n want %s", got, want)
	}
}

// covers: MA-750, R37
// A pointer opens only as what it is: under another archive's key, with a byte flipped
// anywhere — its cleartext generation included — or truncated, it is vault.ErrAuth;
// bytes that are not a pointer file are ErrNotPointer; a newer format is
// ErrNewerFormat before any decryption (P2b-C11), for Peek and Open alike.
func TestAPointerRefusesWhatItIsNot(t *testing.T) {
	raw, err := Seal(keys[0], samplePointer())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(keys[1], raw); !errors.Is(err, vault.ErrAuth) {
		t.Errorf("another archive's key: want ErrAuth, got %v", err)
	}
	for _, at := range []int{13, fileHeadSize, fileHeadSize + 30, len(raw) - 1} {
		bad := append([]byte(nil), raw...)
		bad[at] ^= 1
		if _, err := Open(keys[0], bad); !errors.Is(err, vault.ErrAuth) {
			t.Errorf("byte %d flipped: want ErrAuth, got %v", at, err)
		}
	}
	if _, err := Open(keys[0], raw[:len(raw)-1]); !errors.Is(err, vault.ErrAuth) {
		t.Errorf("truncated: want ErrAuth, got %v", err)
	}
	for label, b := range map[string][]byte{"empty": nil, "short": raw[:fileHeadSize-1], "other magic": append([]byte("MAPX"), raw[4:]...)} {
		if _, _, err := Peek(b); !errors.Is(err, ErrNotPointer) {
			t.Errorf("%s: want ErrNotPointer, got %v", label, err)
		}
	}
	newer := append([]byte(nil), raw...)
	binary.BigEndian.PutUint16(newer[4:6], vault.FormatVersion+1)
	for label, try := range map[string]func() error{
		"Peek": func() error { _, _, e := Peek(newer); return e },
		"Open": func() error { _, e := Open(keys[0], newer); return e },
	} {
		err := try()
		if !errors.Is(err, ErrNewerFormat) {
			t.Errorf("%s of a newer format: want ErrNewerFormat, got %v", label, err)
			continue
		}
		rc, msg := refusal(err)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names("upgrade"))
	}
	zero := append([]byte(nil), raw...)
	binary.BigEndian.PutUint64(zero[6:14], 0)
	if _, _, err := Peek(zero); !errors.Is(err, ErrPointer) {
		t.Errorf("generation 0: want ErrPointer, got %v", err)
	}
}

// covers: MA-750, R37
// Seal refuses — so a writer never commits — a pointer a reader would refuse: no
// generation, an inventory generation after its own, a bad bucket count, a chunk with
// no name, one named twice (across sections too), sealed after the pointer or at 0,
// empty or over the bound, or entries out of order. Each is the typed ErrPointer.
func TestAPointerThatBreaksTheRulesIsNeverSealed(t *testing.T) {
	cases := map[string]func(p *Pointer){
		"generation 0":            func(p *Pointer) { p.Gen = 0 },
		"another format":          func(p *Pointer) { p.Format = 2 },
		"inventory after pointer": func(p *Pointer) { p.Inv = p.Gen + 1 },
		"bucket count 15":         func(p *Pointer) { p.Buckets = 15 },
		"bucket count 24":         func(p *Pointer) { p.Buckets = 24 },
		"negative index image":    func(p *Pointer) { p.IndexImage = -1 },
		"unnamed chunk":           func(p *Pointer) { p.History[0].Name = Name{} },
		"a name twice":            func(p *Pointer) { p.Index[0].Name = p.Manifest[0].Name },
		"sealed after pointer":    func(p *Pointer) { p.Index[1].Gen = p.Gen + 1 },
		"sealed at 0":             func(p *Pointer) { p.Manifest[2].Gen = 0 },
		"empty chunk":             func(p *Pointer) { p.Manifest[1].Size = 0 },
		"over the bound":          func(p *Pointer) { p.Index[0].Size = MaxChunkSize + 1 },
		"out of order":            func(p *Pointer) { p.Index[0].I, p.Index[1].I = 1, 0 },
	}
	for label, mutate := range cases {
		t.Run(label, func(t *testing.T) {
			p := samplePointer()
			mutate(&p)
			out, err := Seal(keys[0], p)
			if !errors.Is(err, ErrPointer) {
				t.Fatalf("want ErrPointer, got %v", err)
			}
			rc, msg := refusal(err)
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names("record pointer"))
			if out != nil {
				t.Fatalf("a refused pointer returned %d bytes", len(out))
			}
		})
	}
	if _, err := Seal(keys[0], Pointer{Head: Head{Format: vault.FormatVersion, Gen: 1, Buckets: 0}}); err != nil {
		t.Fatalf("positive twin: a pointer with no manifest chunks needs no bucket count: %v", err)
	}
}

// covers: MA-750, R37
// Open applies the same rules to what authenticates: a pointer whose plaintext breaks
// them (sealed through the raw vault call, bypassing Seal's check), whose head
// generation disagrees with its file's, whose head has an unknown field, or which has
// trailing bytes, is ErrPointer — and a pointer sealed as another container's is ErrAuth.
func TestOpenRefusesARuleBreakingPointerThatAuthenticates(t *testing.T) {
	file := func(gen uint64, plain []byte, kind vault.ContainerKind) []byte {
		sealed, err := keys[0].SealPointer(kind, gen, plain)
		if err != nil {
			t.Fatal(err)
		}
		b := append([]byte(pointerMagic), 0, 1)
		b = binary.BigEndian.AppendUint64(b, gen)
		return append(b, sealed...)
	}
	twice := samplePointer()
	twice.Index[0].Name = twice.Manifest[0].Name
	plainTwice, _ := twice.encode()
	plainGood, _ := samplePointer().encode()
	other := samplePointer()
	other.Gen = 4
	plainGen4, _ := other.encode()
	head := `{"format":1,"gen":3,"extra":1}`
	unknown := append(binary.BigEndian.AppendUint32(nil, uint32(len(head))), head...)
	unknown = append(unknown, make([]byte, 12)...)

	for label, raw := range map[string][]byte{
		"a name twice":       file(3, plainTwice, vault.ContainerManifest),
		"head generation 4":  file(3, plainGen4, vault.ContainerManifest),
		"unknown head field": file(3, unknown, vault.ContainerManifest),
		"trailing bytes":     file(3, append(plainGood, 0), vault.ContainerManifest),
		"truncated table":    file(3, plainGood[:len(plainGood)-1], vault.ContainerManifest),
	} {
		if _, err := Open(keys[0], raw); !errors.Is(err, ErrPointer) {
			t.Errorf("%s: want ErrPointer, got %v", label, err)
		}
	}
	if _, err := Open(keys[0], file(3, plainGood, vault.ContainerHistory)); !errors.Is(err, vault.ErrAuth) {
		t.Errorf("a history-kind pointer: want ErrAuth, got %v", err)
	}
	if _, err := Open(keys[0], file(3, plainGood, vault.ContainerManifest)); err != nil {
		t.Errorf("positive twin: the same plaintext sealed properly opens: %v", err)
	}
}

// covers: MA-750, R37
// A chunk name is 128 fresh random bits, and its path is chunks/<hh>/<32 hex>.
func TestChunkNamesAreFreshAndShaped(t *testing.T) {
	seen := map[Name]bool{}
	for i := 0; i < 64; i++ {
		n, err := NewName()
		if err != nil {
			t.Fatal(err)
		}
		if seen[n] || n == (Name{}) {
			t.Fatalf("name %s repeated or zero", n)
		}
		seen[n] = true
		if p := n.Path(); !filePath.MatchString(p) || !strings.HasPrefix(p, "chunks/"+n.String()[:2]+"/") {
			t.Fatalf("chunk path %q", p)
		}
	}
}
