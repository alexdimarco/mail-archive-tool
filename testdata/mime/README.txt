The MIME-fidelity corpus and its goldens (docs/design-mime-fidelity.md, section 9;
slice MF-S0). Nothing here may be edited by hand or regenerated to make a test pass.

What is here
  *.eml                  the corpus: one message per trigger of the design's section 1
                         (F24 charsets and transfer encodings, F25 bodies and names,
                         C1-1, C1-4, C1-5, C1-8, C1-10), the Apple Mail, mutt, Mailman,
                         Outlook and bounce shapes, controls, the paths that reach
                         fallbackParse, and one item for each of the 40 labels in
                         go-pst v6.0.2's nameMap (charset-<label>.eml).
  corpus.tsv             the index: file, group, trigger, the catalog rows that use it.
                         Every .eml is listed once, in file-name order.
  golden-identity.txt    Identity, Fingerprint, ContentDigest and the local and live
                         manifest keys (plain and #fp-qualified) of every corpus item,
                         of the constructed v1 stub, and of every item of
                         ../support.pst (the PST twin).
  golden-content.txt     every model.Message field and each attachment's name, type,
                         size and byte digest, for the same items. support.pst is
                         somebody else's real mail, so its items are pinned by length
                         and SHA-256 and their text is not copied here.

The goldens are canonical text, with Go-quoted strings so the bytes are exact. Their
"# " header is their provenance: the release tag and commit, how the tag was built, the
toolchain and the dependencies. They were recorded from the unmodified build at that tag,
before any fidelity change. Bytes matter: .gitattributes marks this directory -text, so
no checkout rewrites a line ending.

How they are used
  internal/source/mimecorpus_test.go holds TestMimeCorpusIsComplete and
  TestMimeGoldensHoldForTheBuild, which fail if the corpus, an index row, a golden or the
  build's own view of an item moves. MA-330 and MA-331 (slice S1) and MA-332 (slice S2)
  stand on them.

When one goes red
  The build no longer produces what the release wrote. That is identity drift (design
  section 5, MC28): stop and get an owner decision. Regenerating a golden is an
  operator-accepted act, never a fix, and TestRecordMimeGoldens writes only into a
  directory it is explicitly given.

Open
  No input in this corpus panics the v1 parse: a coverage-guided fuzz of it and a sweep of
  every charset label found none. The golden pins the stub's identity from the message
  safeParseMessage's recover returns, built directly, and MA-330's panicking-item case
  stays a skip encoder until a panicking input is found or the owner rules that enough.
