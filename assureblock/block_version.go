// Package assureblock vendors the assurance-kit CLAUDE-block meta-test for Go
// repos: drop this directory (block_version.go + claude_block_test.go) into an
// adopting module (e.g. assureblock/) and `go test ./...` polices the repo's
// instruction files.
//
// This file is the Go twin of python/assure/block_version.py and
// php/block_version.php. The kit's self-suite asserts all twins agree, so a
// BLOCK_VERSION bump is always a PAIRED bump across languages.
//
// Vendored from assurance-kit go/block_version.go -- fix there first, re-sync out.
package assureblock

// BlockVersion is the single bump point's Go mirror.
const BlockVersion = 3

// Sentinel grammar, kept identical to the python twin so all tooling agrees.
const SentinelRe = `<!--\s*assurance-kit-block\s+v(\d+)\s*-->`
const EndSentinel = "<!-- /assurance-kit-block -->"

// InstructionFiles mirrors python's INSTRUCTION_FILES (kit law, not per-repo
// config): every file on this list present at the repo root must carry the
// current block.
var InstructionFiles = []string{"CLAUDE.md", "AGENTS.md", "GEMINI.md"}
