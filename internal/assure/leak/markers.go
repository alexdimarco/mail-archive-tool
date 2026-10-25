// Package leak is the encrypted-archive leak scanner the Phase-2a tests share
// (design-encrypted-archives-phase2a §7 items 2–4): unique markers to plant in every
// field of a message, a scanner that looks for them in file NAMES and contents in
// every encoding the design names, and the allow-list grammar of what an encrypted
// archive's folder may hold. It never creates or opens a key, so the reachability
// gate (MA-726) is untouched; it lives under internal/assure, which the covers-map
// meta-test skips (its proof is the positive twin in each test that uses it).
package leak

import (
	"crypto/rand"
	"math/big"
)

// Field is a place a message's text can be: each gets its own marker.
type Field string

const (
	Subject     Field = "subject"
	Body        Field = "body"
	Folder      Field = "folder"
	Sender      Field = "sender"
	Recipient   Field = "recipient"
	Mailbox     Field = "mailbox"
	MessageID   Field = "message-id"
	AttachName  Field = "attachment-name"
	AttachBytes Field = "attachment-bytes"
	Store       Field = "store"
	SourcePath  Field = "source-path"
)

// Fields is every field, in a fixed order.
var Fields = []Field{Subject, Body, Folder, Sender, Recipient, Mailbox, MessageID, AttachName, AttachBytes, Store, SourcePath}

// Deco is the non-ASCII decoration a fixture joins to a marker where the field
// allows Unicode, so the message carries encoded forms (RFC 2047, quoted-printable,
// HTML entities, percent-encoding) and not only plain bytes.
const Deco = "Zoë Ångström ñ"

// Markers is one marker per field.
type Markers map[Field]string

// alphabet is lowercase letters outside [a-f]: a marker can never be read as hex
// (a unit name, a digest, a token), and survives every slug and segment cleaner.
const alphabet = "ghjkmnpqrstuvwxyz"

// New draws a fresh marker for every field: "zq", a two-letter field tag, and 14
// random letters of alphabet.
func New() Markers {
	ms := Markers{}
	for i, f := range Fields {
		tag := string([]byte{alphabet[i%len(alphabet)], alphabet[(i/len(alphabet)+3)%len(alphabet)]})
		ms[f] = "zq" + tag + randomLetters(14)
	}
	return ms
}

func randomLetters(n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		b[i] = alphabet[v.Int64()]
	}
	return string(b)
}
