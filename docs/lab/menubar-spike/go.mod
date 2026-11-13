// A separate module, so the product's `go test ./...` and builds never compile the spike
// (design-menubar §9). Build it on a Mac with:
//   CGO_ENABLED=0 go build -o spike .        (and ./noloop for the no-event-loop probe)
module spikemenubar

go 1.26.0

require github.com/ebitengine/purego v0.8.4
