package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/procs"
	"mail-archive-tool/internal/server"
)

// runServe and runSetup are the verbs: they run until Ctrl-C (a handled interrupt
// ends them cleanly, so the instance file `serve -link` reads is removed).
func runServe(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveMain(ctx, args, osLinkEnv())
}

func runSetup(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return setupMain(ctx, args, osLinkEnv())
}

// lanCookieAge is how long a non-loopback serve's session cookie lives: one
// working day, so closing the browser does not end the session [CC18].
const lanCookieAge = 24 * time.Hour

// linkWindow is how long `serve -link` keeps asking a serve that has not answered
// yet, while its process is alive [CC21]. A var so a test need not wait it out.
var linkWindow = 15 * time.Second

// serveMain is `mailarchive serve`: the archive reader behind a session, reached
// by a one-time link the process opens or prints (design-local-confidentiality
// §4.C2–C3). With -link it does not serve: it asks the running serve of the
// archive for a fresh link and prints it [CC18].
func serveMain(ctx context.Context, args []string, env *linkEnv) error {
	fs := flag.NewFlagSet("mailarchive serve", flag.ContinueOnError)
	out := fs.String("out", ".", "the archive folder to serve (an encrypted archive is opened with its password or recovery key first, and served on this computer only)")
	addr := fs.String("addr", "127.0.0.1:8099", "address to listen on")
	noBrowser := fs.Bool("no-browser", false, "do not open the reader in a browser: print a link instead (none is opened without a display either)")
	link := fs.Bool("link", false, "print a fresh one-time link from the serve already running on -out, and exit (for a serve with no terminal, or another browser or device)")
	var allow stringSlice
	fs.Var(&allow, "allow-host", "with a non-loopback -addr: also accept requests addressed to this host name (repeatable); this machine's own IP addresses are accepted already")
	k := readKeyFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *link {
		return serveLink(ctx, *out, env)
	}
	allowHosts, err := allowHostNames(allow)
	if err != nil {
		return err
	}

	// An encrypted archive is served from its sealed record, unlocked first, and only
	// on this computer (phase3 §7.5); a folder inside one stays refused.
	var src server.Source
	count := 0
	if kerr := app.RefuseKeyless("serve it", *out); kerr != nil {
		dir := abspath(*out)
		if !app.UnlockableAt(dir) {
			return keylessRefusal(kerr, dir)
		}
		if !server.IsLoopback(*addr) {
			return fmt.Errorf("%s is an encrypted archive: it is served only on this computer, and -addr %s is not a loopback address — leave -addr out, or use 127.0.0.1", dir, *addr)
		}
		u, err := unlockForRead(stdAsker(), dir, "serving it", k)
		if err != nil {
			return interrupted(err, os.Stderr)
		}
		defer u.Destroy()
		r, err := keyring.ReaderSource(u, keyring.ReaderOptions{LockedNote: "mailarchive serve has stopped, so this archive is locked: start it again to read it."})
		if err != nil {
			return err
		}
		defer r.Close() // waits, so a scratch index's plaintext file is gone before the process ends
		src = r
		_ = r.UseIndex(func(ix *index.Index) error {
			count, _ = ix.Count()
			return nil
		})
	} else {
		if err := refuseReadKeyFlags(fs); err != nil {
			return err
		}
		ix, err := index.OpenReadonly(filepath.Join(*out, "search.db"))
		if err != nil {
			return err
		}
		defer ix.Close()
		count, _ = ix.Count()
		src = server.Folder(*out, ix)
	}

	if !server.IsLoopback(*addr) {
		fmt.Fprintln(env.Stderr, lanWarning(*addr, *out))
	} else if len(allowHosts) > 0 {
		fmt.Fprintf(env.Stderr, "note: -allow-host has no effect with a loopback -addr (%s): only 127.0.0.1, localhost, [::1] and the bound address itself are accepted there\n", *addr)
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}

	sess := newServeSession(*addr, env)
	served := servedURL(*addr, ln.Addr())
	l := &surfaceLinks{env: env, sess: sess, origin: strings.TrimSuffix(served, "/"), noBrowser: *noBrowser,
		hint: "get a link with: mailarchive serve -link -out " + quoteForHint(*out)}
	keyHex, key := instance.NewKey()
	h := &instance.Handoff{Key: key, Mint: l.mint, Open: l.handoffOpen(), Redeemed: sess.WaitRedeemed}

	srv := &http.Server{
		Handler:           serveHandler(src, ln.Addr(), allowHosts, sess, h),
		ReadHeaderTimeout: 10 * time.Second, // a stalled client cannot hold a connection open forever
		IdleTimeout:       2 * time.Minute,
		// OPTIONS * would be answered by net/http before the Host guard (AC8).
		DisableGeneralOptionsHandler: true,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	if cleanup := publishServe(env, *out, served, keyHex); cleanup != nil {
		defer cleanup()
	}
	fmt.Fprintf(env.Stdout, "Serving %d indexed messages from %s\n", count, *out)
	fmt.Fprintf(env.Stdout, "Search UI:  %s\n", served)
	// The address alone answers "not signed in": it opens through a private link
	// (P3-8 friction review, Tier C).
	fmt.Fprintln(env.Stdout, "            (that address opens only through a private link — the one below, or one from `mailarchive serve -link`)")
	l.start()
	fmt.Fprintln(env.Stdout, l.stopHint(l.enter(ctx)))
	if env.Ready != nil {
		env.Ready(l.origin)
	}
	return serveUntil(ctx, srv.Shutdown, errc)
}

// lanWarning is the warning a serve bound beyond loopback prints: the archive and
// the session cookie cross the network in clear text (there is no TLS), and each
// browser still needs a private link of its own. It no longer says serve "has no
// authentication": it has one, and says what it does and does not cover.
func lanWarning(addr, out string) string {
	return fmt.Sprintf("WARNING: serve is open to your network. Binding %s sends the archive and the session cookie in clear text to anyone who can read that traffic, and each browser still needs a private link (press Enter here, or run mailarchive serve -link -out %s); use 127.0.0.1 (the default) unless you mean it.", addr, quoteForHint(out))
}

// newServeSession is the session of a serve listening at addr: its notices go to
// stderr, and a LAN serve's cookie lasts a working day [CC18, CC24].
func newServeSession(addr string, env *linkEnv) *localweb.Session {
	sess := localweb.NewSession()
	sess.SetClock(env.Now)
	if !server.IsLoopback(addr) {
		sess.SetMaxAge(lanCookieAge)
	}
	announceSessions(sess, env)
	return sess
}

// serveUntil returns when the server fails or ctx ends, shutting it down with
// shutdown (the server's own Shutdown).
func serveUntil(ctx context.Context, shutdown func(context.Context) error, errc <-chan error) error {
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdown(shut)
		return nil
	}
}

// announceSessions prints one notice to stderr per redemption: a second one the
// user did not cause is the sign of a raced link (§8.4) [CC24].
func announceSessions(s *localweb.Session, env *linkEnv) {
	s.OnOpen(func(n int, at time.Time) {
		fmt.Fprintf(env.Stderr, "browser session opened at %s (%d this run)\n", at.Local().Format("15:04"), n)
	})
}

// publishServe records this serve for `serve -link`: a 0600 instance file in the
// per-account directory, keyed by the archive, holding its address and the key
// the handoff is proven with. A directory that cannot be had is a warning, not a
// refusal: the reader itself works, only `serve -link` will not. It returns the
// removal to run at exit, or nil.
func publishServe(env *linkEnv, out, served, keyHex string) func() {
	dir, err := env.StateDir()
	if err != nil {
		fmt.Fprintf(env.Stderr, "note: `mailarchive serve -link` will not work for this run: %v\n", err)
		return nil
	}
	exe, _ := os.Executable()
	u := strings.TrimSuffix(strings.TrimPrefix(served, "http://"), "/")
	path := filepath.Join(dir, instance.ServeFileName(out))
	abs, _ := filepath.Abs(out)
	info := instance.Info{PID: os.Getpid(), Exe: exe, Started: env.Now(), Session: instance.LogonSession(), Dashboard: u, Key: keyHex, Out: abs}
	if err := instance.Write(path, info); err != nil {
		fmt.Fprintf(env.Stderr, "note: `mailarchive serve -link` will not work for this run: %v\n", err)
		return nil
	}
	return func() {
		// Remove it only if it is still ours: a second serve of the same archive may
		// have taken the name since.
		if cur, err := instance.Read(path); err == nil && cur.Key == keyHex {
			os.Remove(path)
		}
	}
}

// serveLink is `serve -link -out DIR` [CC18]: it finds the running serve's
// instance file, proves itself to it with the key in the file, and prints the
// fresh 10-minute link the instance returns (the reply's MAC covers the link).
func serveLink(ctx context.Context, out string, env *linkEnv) error {
	dir, err := env.StateDir()
	if err != nil {
		return err
	}
	info, err := instance.Read(filepath.Join(dir, instance.ServeFileName(out)))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("no running serve found for %s. Why: `serve -link` asks the serve of your account that is running on this archive for a fresh link, and none has recorded itself. Do this: start it with `mailarchive serve -out %s` (in a terminal, or under nohup, launchd or systemd), then run this again", out, quoteForHint(out))
	case err != nil:
		return fmt.Errorf("the record of the running serve for %s cannot be used (%v). Do this: stop that serve and start `mailarchive serve -out %s` again", out, err, quoteForHint(out))
	}
	key, _ := info.KeyBytes()
	rep, err := instance.Reach(ctx, info.Dashboard, key, true, linkWindow, func() bool { return procs.Alive(info.PID) })
	switch {
	case errors.Is(err, instance.ErrBadProof):
		return fmt.Errorf("the program answering at %s did not prove it is the serve of your account for %s. Why: another program, or another account's serve, may be using that address. Do this: stop it, or start `mailarchive serve -out %s` again", info.Dashboard, out, quoteForHint(out))
	case err != nil:
		return fmt.Errorf("no serve is answering at %s for %s; its record is stale. Do this: start `mailarchive serve -out %s` again", info.Dashboard, out, quoteForHint(out))
	case rep.Link == "":
		return fmt.Errorf("the serve at %s gave no link; run this again in a moment", info.Dashboard)
	}
	fmt.Fprintln(env.Stdout, rep.Link)
	return nil
}

// setupMain is `mailarchive setup`: the Microsoft 365 setup wizard, a loopback
// surface behind a session like the reader, on a free port by default so it never
// collides with anything else (design §4.C3, [CC18]). It REFUSES a non-loopback
// bind: the surface writes credentials and must never be reachable from the
// network (design-graph-setup-wizard P4/W-C1).
func setupMain(ctx context.Context, args []string, env *linkEnv) error {
	fs := flag.NewFlagSet("mailarchive setup", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:0", "loopback address to serve the wizard on (must be loopback; port 0 takes any free port)")
	cfg := fs.String("config", "", "config file to write (default: under your OS config dir)")
	noBrowser := fs.Bool("no-browser", false, "do not open the wizard in a browser: print a link instead (none is opened without a display either)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !server.IsLoopback(*addr) {
		return fmt.Errorf("-addr %s is not a loopback address: the setup wizard writes credentials and is served only on 127.0.0.1/localhost", *addr)
	}
	cfgPath := *cfg
	if cfgPath == "" {
		p, err := graphconfig.DefaultConfigPath()
		if err != nil {
			return err
		}
		cfgPath = p
	}
	store, err := graphconfig.DefaultSecretStore()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	sess := localweb.NewSession()
	sess.SetClock(env.Now)
	announceSessions(sess, env)
	served := servedURL(*addr, ln.Addr())
	l := &surfaceLinks{env: env, sess: sess, origin: strings.TrimSuffix(served, "/"), noBrowser: *noBrowser,
		hint: "no link is printed: stdout is not a terminal. Run mailarchive setup from a terminal to get one"}
	srv := &http.Server{
		Handler:           setupHandler(cfgPath, store, ln.Addr(), sess),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// OPTIONS * would be answered by net/http before the Host guard (AC8).
		DisableGeneralOptionsHandler: true,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	fmt.Fprintf(env.Stdout, "Microsoft 365 setup:  %s\n", served)
	fmt.Fprintf(env.Stdout, "Config:        %s\n", cfgPath)
	fmt.Fprintf(env.Stdout, "Secret store:  %s\n", store.Name())
	l.start()
	fmt.Fprintln(env.Stdout, l.stopHint(l.enter(ctx)))
	if env.Ready != nil {
		env.Ready(l.origin)
	}
	return serveUntil(ctx, srv.Shutdown, errc)
}
