CLI := mailarchive
CLI_PKG := ./cmd/mailarchive
BUILD_DIR := bin

# The Windows no-console build of the one program (-H=windowsgui): mailarchivew.exe.
GUI_WIN_LDFLAGS := -H=windowsgui

.PHONY: all build build-windows build-macos build-macos-app dist test vet fmt tidy run serve clean

all: vet test build

## build: compile the one program for the host platform
build:
	go build -o $(BUILD_DIR)/$(CLI) $(CLI_PKG)

## build-windows: cross-compile the Windows pair of the one program — the console
## build (mailarchive.exe) and the no-console build (mailarchivew.exe) — plus the
## two former-name copies of mailarchivew.exe kept this release (§7.1, MA-614).
build-windows:
	GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/$(CLI).exe $(CLI_PKG)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(GUI_WIN_LDFLAGS)" -o $(BUILD_DIR)/$(CLI)w.exe $(CLI_PKG)
	cp $(BUILD_DIR)/$(CLI)w.exe $(BUILD_DIR)/$(CLI)-desktop.exe
	cp $(BUILD_DIR)/$(CLI)w.exe $(BUILD_DIR)/$(CLI)-gui.exe

## build-macos: cross-compile the one program for macOS (Apple Silicon + Intel)
build-macos:
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(CLI)-darwin-arm64 $(CLI_PKG)
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(CLI)-darwin-amd64 $(CLI_PKG)

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
