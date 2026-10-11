package source

import "mail-archive-tool/internal/model"

// stubSubject is the subject of the stub safeParseMessage leaves for a message the MIME
// stack panics on.
const stubSubject = "(unreadable message)"

// unreadableStub is that stub: an empty message whose only content is its subject.
func unreadableStub() *model.Message { return &model.Message{Subject: stubSubject} }

// stubIdentityTerms is the identity of a message the v1 parse panicked on. It
// reproduces the stub of the build that wrote the archive, which today is the constant
// stub with only its subject set, so the identity of every such message is the same:
// the content hash of its empty fields (F33). It is outside the frozen parser
// (parsev1.go) because fault-isolation owns it: its raw-preserving stub changes this
// one function, after the owner rules on the identity fold (rulings J5 and J8,
// ESC-1), and never the frozen file. data is the message's raw bytes, which a later
// stub may fold in.
func stubIdentityTerms(data []byte) model.IdentityTerms {
	return model.IdentityTerms{Subject: stubSubject}
}
