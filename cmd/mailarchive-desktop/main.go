// Command mailarchive-desktop is the in-process dashboard-and-launcher: a
// loopback-only local web control panel for capturing, browsing, and scheduling a
// Microsoft 365 archive, calling the engine's internal packages directly (never a
// child binary). See docs/design-mailarchive-desktop.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/graphconfig"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "mailarchive-desktop: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("mailarchive-desktop", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8097", "loopback address for the dashboard (must be loopback)")
	readerAddr := fs.String("reader-addr", "127.0.0.1:8099", "loopback address for the archive reader (must be loopback)")
	out := fs.String("out", "", "archive directory to manage")
	cfg := fs.String("config", "", "Graph config file (default: under your OS config dir)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Both surfaces are control/reader surfaces served only on this machine; never
	// the network (design P2/DC7).
	if !desktop.Loopback(*addr) {
		return fmt.Errorf("-addr %s is not a loopback address: the dashboard is served only on 127.0.0.1/localhost", *addr)
	}
	if !desktop.Loopback(*readerAddr) {
		return fmt.Errorf("-reader-addr %s is not a loopback address: the archive reader is served only on 127.0.0.1/localhost", *readerAddr)
	}
	cfgPath := *cfg
	if cfgPath == "" {
		p, err := graphconfig.DefaultConfigPath()
		if err != nil {
			return err
		}
		cfgPath = p
	}
	store, _ := graphconfig.DefaultSecretStore() // informational shell tolerates a nil store
	settingsPath := filepath.Join(filepath.Dir(cfgPath), "desktop-settings.json")
	dcfg := desktop.Config{Out: *out, ConfigPath: cfgPath, Store: store, SettingsPath: settingsPath, ReaderURL: "http://" + *readerAddr + "/"}

	dash := &http.Server{Addr: *addr, Handler: desktop.DashboardHandler(dcfg), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	reader := &http.Server{Addr: *readerAddr, Handler: desktop.ReaderHandler(dcfg), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 2)
	go func() { errc <- dash.ListenAndServe() }()
	go func() { errc <- reader.ListenAndServe() }()

	fmt.Printf("MailArchive Desktop — open this page in a browser:\n  http://%s/\n", *addr)
	fmt.Printf("Archive reader: http://%s/\n", *readerAddr)
	fmt.Printf("Config: %s\n", cfgPath)
	fmt.Println("Press Ctrl-C to stop.")

	select {
	case err := <-errc:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = reader.Shutdown(shutCtx)
		return dash.Shutdown(shutCtx)
	}
}
