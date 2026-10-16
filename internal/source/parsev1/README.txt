internal/source/parsev1/ — the frozen v1 parser's dependencies (design-mime-fidelity MC27)

parsev1.go (in internal/source/) replays the parser that wrote users' archives. What it
calls is copied here, so a dependency update or a toolchain move cannot change what it
computes: its output is a message's identity, and an identity that moves duplicates or
loses archived mail.

Under _dep/, byte for byte except where noted:

  message, mail, textproto, charset   github.com/emersion/go-message v0.16.0 (MIT, LICENSE-go-message)
  textwrapper                          github.com/emersion/go-textwrapper 65d896831594 (MIT, LICENSE-go-textwrapper),
                                       which go-message's writer imports
  mime                                 Go 1.26.9 mime: encodedword.go, grammar.go, mediatype.go (WordDecoder,
                                       ParseMediaType); type.go, the host's media-type table, is left out
  quotedprintable                      Go 1.26.9 mime/quotedprintable
  netmail                              Go 1.26.9 net/mail: the address and date parser
  base64                               Go 1.26.9 encoding/base64 (ruling OR4)
  byteorder                            Go 1.26.9 internal/byteorder, which base64 imports
                                       (Go, BSD, LICENSE-go)

The only edits are the import paths, which now name these copies, and gofmt (import order,
doc-comment indentation). charset/gopst.go is not a copy: it writes out the labels go-pst
v6.0.2 added to go-message's table at init in the build that wrote the archives (the copy
sees neither go-pst's init nor openPST's), so the effective table is the same.

Still shared with the toolchain, the residual MA-331 watches: x/text's charmap and
ianaindex tables, net/textproto's header reader, and the unicode tables (MA-331 asserts
unicode.Version).

The directory name starts with an underscore on purpose: go vet ./... then does not reach
this third-party code (it reports findings in go-message that are inherent to it), as it does
not reach a vendor/ directory, whose name an import cannot use. MA-331 pins the sha256 of every
file here and of parsev1.go (testdata/mime/parsev1.pins); an edit is an operator decision under
design-mime-fidelity section 5, never a fix.
