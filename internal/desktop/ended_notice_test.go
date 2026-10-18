package desktop

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/instance"
)

// covers: MA-638, R5, S59
// After a launch ends an older instance of this account, the new dashboard's
// header and `status` name it — the pid and program — and say a run it held
// reads RED until the next run (§4.5.2). Positive twin: no line when nothing was
// ended. The ended program is control-stripped on the way into the instance file.
func TestMA638_EndedOlderInstanceNamed(t *testing.T) {
	const endedPID = 4321
	const endedProgram = "/old/place/MailArchive Desktop"

	g := newDRig(t)
	g.probe = instance.FormerPreUpgrade
	g.endOld = true
	g.endedPID = endedPID
	g.endedProgram = endedProgram
	g.rec.arrive = false
	g.start()

	if g.info.Former != instance.FormerEnded {
		t.Fatalf("recorded former %q, want %q after a successful end", g.info.Former, instance.FormerEnded)
	}
	if g.info.Ended == nil || g.info.Ended.PID != endedPID {
		t.Fatalf("the instance file names no ended pid: %+v", g.info.Ended)
	}

	// The dashboard header names the ended instance (through the session the
	// opener's link gives).
	_, ck := g.cookieFromLink()
	_, page := get(t, "http://"+g.info.Dashboard+g.prefix, ck)
	page = strings.ReplaceAll(page, "&#39;", "'")
	if !strings.Contains(page, `id="formerNotice"`) || !strings.Contains(page, strconv.Itoa(endedPID)) {
		t.Errorf("the dashboard header does not name the ended instance (pid %d):\n%s", endedPID, page)
	}

	// `status`: the same finding, read from the instance file, as a WARN.
	in := health.Input{Out: g.out, DesktopFormer: instance.FormerIn(g.dir, ""), DesktopEnded: instance.EndedFor(g.dir, "")}
	if in.DesktopFormer != instance.FormerEnded {
		t.Fatalf("status reads former %q from the file, want %q", in.DesktopFormer, instance.FormerEnded)
	}
	rep := health.Assess(in, time.Now())
	if !strings.Contains(strings.Join(rep.Codes, ","), "desktop_ended_older") {
		t.Errorf("status codes %q lack desktop_ended_older", rep.Codes)
	}
	g.stop()

	// Positive twin: an older instance that could NOT be ended stays pre-upgrade,
	// with no ended line.
	g2 := newDRig(t)
	g2.probe = instance.FormerPreUpgrade
	g2.endOld = false
	g2.rec.arrive = false
	g2.start()
	if g2.info.Former == instance.FormerEnded || g2.info.Ended != nil {
		t.Errorf("nothing was ended, yet the file records an end: former %q ended %+v", g2.info.Former, g2.info.Ended)
	}
	if ef := instance.EndedFor(g2.dir, ""); ef != nil {
		t.Errorf("EndedFor names an ended instance though none was ended: %+v", ef)
	}
	g2.stop()
}
