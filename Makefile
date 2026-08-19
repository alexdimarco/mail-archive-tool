CLI := mailarchive
GUI := mailarchive-gui
CLI_PKG := ./cmd/mailarchive
GUI_PKG := ./cmd/mailarchive-gui
BUILD_DIR := bin

# GUI Windows build has no console window.
GUI_WIN_LDFLAGS := -H=windowsgui

.PHONY: all build build-gui build-windows build-macos build-macos-app dist test vet fmt tidy run clean

all: vet test build build-gui

## build: compile the CLI for the host platform
build:
	go build -o $(BUILD_DIR)/$(CLI) $(CLI_PKG)

## build-gui: compile the GUI wizard for the host platform
build-gui:
	go build -o $(BUILD_DIR)/$(GUI) $(GUI_PKG)

## build-windows: cross-compile both Windows executables (CLI console + GUI no-console)
build-windows:
	GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/$(CLI).exe $(CLI_PKG)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(GUI_WIN_LDFLAGS)" -o $(BUILD_DIR)/$(GUI).exe $(GUI_PKG)

## build-macos: cross-compile macOS binaries (Apple Silicon arm64 + Intel amd64)
build-macos:
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(CLI)-darwin-arm64 $(CLI_PKG)
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(CLI)-darwin-amd64 $(CLI_PKG)
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(GUI)-darwin-arm64 $(GUI_PKG)
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(GUI)-darwin-amd64 $(GUI_PKG)

## build-macos-app: package the GUI as a universal, double-clickable .app (bin/MailArchive-macos.zip)
build-macos-app:
	bash packaging/macos/build-app.sh

## dist: build the full cross-platform release matrix into dist/ (+ SHA256SUMS)
dist:
	bash packaging/build-release.sh

## test: run all tests (integration tests use testdata/support.pst)
test:
	go test ./...

## vet: run go vet
vet:
	go vet ./...

## fmt: format all Go source
fmt:
	gofmt -w ./cmd ./internal

## tidy: tidy module dependencies
tidy:
	go mod tidy

## run: example incremental export of the bundled fixture into ./export
run: build
	$(BUILD_DIR)/$(CLI) -input testdata/support.pst -out ./export

## serve: start the search + reader UI over ./export
serve: build
	$(BUILD_DIR)/$(CLI) serve -out ./export

clean:
	rm -rf $(BUILD_DIR) ./export
