package main

import "testing"

// covers: MA-375, R12, R17, S45
// `graph` refuses a -token-cache or -client-secret-file inside -out, naming both
// paths and the default, and writes nothing: a credential kept in the archive
// travels with every copy and sync of it.
func TestGraphRefusesCredentialInsideArchive(t *testing.T) {
	t.Skip("MA-375: graph does not check where its credential files are against -out; lands in LC-C1")
}
