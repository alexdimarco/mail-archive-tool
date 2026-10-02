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
	out := fs.String("out", "", "archive directory to manage")
	cfg := fs.String("config", "", "Graph config file (default: under your OS config dir)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// The dashboard is a control surface (it captures, signs in, schedules); it is
	// never exposed to the network (design P2).
	if !desktop.Loopback(*addr) {
		return fmt.Errorf("-addr %s is not a loopback address: the dashboard is served only on 127.0.0.1/localhost", *addr)
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

	srv := &http.Server{
		Addr:              *addr,
		Handler:           desktop.DashboardHandler(desktop.Config{Out: *out, ConfigPath: cfgPath, Store: store}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	fmt.Printf("MailArchive Desktop — open this page in a browser:\n  http://%s/\n", *addr)
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
		return srv.Shutdown(shutCtx)
	}
}
