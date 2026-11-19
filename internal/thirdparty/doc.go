// Package thirdparty holds the checks on third-party code this repository keeps a
// copy of. Today that is github.com/ncruces/zenity v0.10.12, kept in
// third_party/zenity so its Windows dialogs can be patched to fit their words
// (docs/design-windows-dialog-fit.md). The package has no code of its own: its
// tests pin the copy to the published module, and run the copy's own tests and vet
// (MA-1468).
package thirdparty
