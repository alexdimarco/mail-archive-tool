package desktop

import (
	"context"
	"errors"
	"os"
	"runtime"
	"time"

	"mail-archive-tool/internal/dialogfront"
	"mail-archive-tool/internal/instance"
)

// handoffAct asks the running instance for act (choose or wizard) in the second
// request form (design-menubar §7.1, MB-3) and tells the person the outcome in this
// launch's own session. It reports done when the launch has its outcome: the instance
// showed the window (or was already busy showing one), or the person was told why
// not. Not done with a nil error means "do today's open instead": the instance refused
// the second form once (403, MB-3 item 2) or cannot show dialogs (MB-9). Not done with
// an error means the instance did not answer yet; the caller tries again until its
// deadline.
func handoffAct(ctx context.Context, dir string, info instance.Info, key []byte, act string, env *launchEnv, window time.Duration, stillHeld func() bool) (done bool, err error) {
	// Windows lets only the program the person just clicked bring a window forward:
	// the launcher passes that right to the instance, but only to the pid the lock's
	// holder record confirms, never to one named only in the instance file (§7.3, MB-17).
	if pid := instance.HolderPID(dir, env.Session); pid != 0 && pid == info.PID && env.AllowForeground != nil {
		env.AllowForeground(pid)
	}
	ar := instance.ActRequest{Act: act, Version: env.Version, Session: env.GUISession, Token: activationToken()}
	rep, err := reachAct(ctx, info.Dashboard, key, ar, window, stillHeld)
	switch {
	case errors.Is(err, instance.ErrRefused):
		return false, nil
	case err != nil:
		return false, err
	case rep.Busy, rep.Shown:
		return true, nil
	}
	switch rep.Reason {
	case instance.ReasonAlreadyOpen:
		if act == instance.ActWizard {
			env.ShowNotice(instance.WizardOpenText)
		} else {
			env.ShowNotice(choiceOpenText)
		}
		return true, nil
	case instance.ReasonOtherSession:
		env.ShowNotice(otherSessionText) // R50: in this session, never on the other screen
		return true, nil
	}
	return false, nil // ReasonCantShow
}

// reachAct is instance.Reach for the second form: it retries an instance that does not
// answer yet (a slow start, a rewritten file) every half second for up to window while
// the lock is held, but never retries a refusal, which the caller answers with the
// first form once (MB-3 item 2), so a launch never says "not responding" of an
// instance that only did not know the request.
func reachAct(ctx context.Context, addr string, key []byte, ar instance.ActRequest, window time.Duration, stillHeld func() bool) (instance.Reply, error) {
	deadline := time.Now().Add(window)
	for {
		rep, err := instance.CallAct(ctx, addr, key, false, ar)
		if err == nil || errors.Is(err, instance.ErrRefused) {
			return rep, err
		}
		if ctx.Err() != nil || time.Now().Add(500*time.Millisecond).After(deadline) || (stillHeld != nil && !stillHeld()) {
			return instance.Reply{}, err
		}
		select {
		case <-ctx.Done():
			return instance.Reply{}, err
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// activationToken is the launch's Wayland activation token, when the desktop gave it
// one (design-menubar §7.4): the instance hands it to the dialog it starts so the
// choice can come to the front. Bounded by the protocol (MB-3 item 5).
func activationToken() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	return os.Getenv("XDG_ACTIVATION_TOKEN")
}

// HandOffWizard is what the `wizard` verb asks first (design-menubar D5): when this
// account's MailArchive runs and knows the wizard action, it is asked to run the wizard
// and this launch ends — true. false runs the wizard in this process, under the wizard
// lock: no instance, an instance of an older version, one that cannot show dialogs, or
// one that does not answer.
// It installs the bring-to-front watch and arms first: the person just started the
// wizard, so a line it shows in this session is one they asked for (R39, MB-8).
func HandOffWizard() bool {
	dialogfront.Install()
	dialogfront.Arm()
	return handOffWizard(context.Background(), osEnv())
}

func handOffWizard(ctx context.Context, env *launchEnv) bool {
	dir, err := env.StateDir()
	if err != nil || !instance.LockIsHeld(dir, env.Session) {
		return false
	}
	info, err := instance.Read(instance.FilePathIn(dir, env.Session))
	if err != nil || !info.HasAct(instance.ActWizard) {
		return false
	}
	if pid := instance.HolderPID(dir, env.Session); pid != 0 && pid != info.PID {
		return false
	}
	key, err := info.KeyBytes()
	if err != nil {
		return false
	}
	window := env.Window
	if window <= 0 {
		window = instance.DefaultWait
	}
	done, _ := handoffAct(ctx, dir, info, key, instance.ActWizard, env, window, func() bool { return instance.LockIsHeld(dir, env.Session) })
	return done
}
