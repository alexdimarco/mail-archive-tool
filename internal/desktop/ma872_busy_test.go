package desktop

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// MA-872 (design-desktop-settings-card §11, slice A1): every Desktop button whose click sends a
// request goes through its page script's busy helper, and every style block carries a pressed
// and a busy rule. The scripts are not run here (there is no browser or JavaScript engine); they
// are read. A live check in Chrome is the friction note's.

// jsSkip returns the index of the last character of the string or comment that starts at i, or i
// when none starts there, reading the way jsBalanced does: an escape hides the next character.
func jsSkip(src string, i int) int {
	c := src[i]
	switch {
	case c == '\'' || c == '"' || c == '`':
		for i++; i < len(src) && src[i] != c; i++ {
			if src[i] == '\\' {
				i++
			}
		}
	case c == '/' && i+1 < len(src) && src[i+1] == '/':
		for i < len(src) && src[i] != '\n' {
			i++
		}
	case c == '/' && i+1 < len(src) && src[i+1] == '*':
		for i += 2; i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/'); i++ {
		}
		i++
	}
	return i
}

// jsClose returns the index just past the bracket that closes the one at open, or -1.
func jsClose(src string, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch c := src[i]; {
		case c == '\\':
			i++
		case c == '\'' || c == '"' || c == '`' || (c == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*')):
			i = jsSkip(src, i)
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// jsArrowBody returns the body of the arrow whose "=>" ends at i: a block, or an expression that
// runs to the first ; , or line end, or the bracket that closes around it.
func jsArrowBody(src string, i int) string {
	for i < len(src) && src[i] == ' ' {
		i++
	}
	if i < len(src) && src[i] == '{' {
		if j := jsClose(src, i); j > 0 {
			return src[i:j]
		}
		return src[i:]
	}
	depth := 0
	for j := i; j < len(src); j++ {
		switch c := src[j]; {
		case c == '\\':
			j++
		case c == '\'' || c == '"' || c == '`':
			j = jsSkip(src, j)
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			if depth == 0 {
				return src[i:j]
			}
			depth--
		case (c == ';' || c == ',' || c == '\n') && depth == 0:
			return src[i:j]
		}
	}
	return src[i:]
}

var (
	jsFuncRe  = regexp.MustCompile(`function\s+([A-Za-z_$][\w$]*)\s*\(`)
	jsArrowRe = regexp.MustCompile(`(?:^|[^\w$.])([A-Za-z_$][\w$]*)=(?:async\s*)?(?:\([^()]*\)|[A-Za-z_$][\w$]*)\s*=>`)
	// the code a statement registers or schedules, which the statement itself does not run
	jsLaterRe = regexp.MustCompile(`(?:addEventListener|setTimeout|setInterval)\(`)
)

// jsFuncs returns every named function's body (function NAME(…){…} and NAME=(…)=>…), the
// bodies of one name in different scopes joined.
func jsFuncs(src string) map[string]string {
	out := map[string]string{}
	for _, m := range jsFuncRe.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		p := jsClose(src, m[1]-1)
		if p < 0 {
			continue
		}
		b := strings.Index(src[p:], "{")
		if b < 0 {
			continue
		}
		if e := jsClose(src, p+b); e > 0 {
			out[name] += src[p+b:e] + "\n"
		}
	}
	for _, m := range jsArrowRe.FindAllStringSubmatchIndex(src, -1) {
		out[src[m[2]:m[3]]] += jsArrowBody(src, m[1]) + "\n"
	}
	return out
}

// jsNow is body without what it only registers or schedules: the handlers it adds and the
// callbacks it gives setTimeout and setInterval. A debounced count or the next poll is later
// work, not the reply to the click that ran the body.
func jsNow(body string) string {
	for {
		m := jsLaterRe.FindStringIndex(body)
		if m == nil {
			return body
		}
		e := jsClose(body, m[1]-1)
		if e < 0 {
			return body[:m[0]]
		}
		body = body[:m[0]] + body[e:]
	}
}

// jsCalls reports whether body calls name (not a method of the same name).
func jsCalls(body, name string) bool {
	re := regexp.MustCompile(`(?:^|[^\w$.])` + regexp.QuoteMeta(name) + `\(`)
	return re.MatchString(body)
}

// jsHandlers returns each click or submit handler: the second argument of
// addEventListener('click'|'submit', …), and the arrow given to .onclick=.
func jsHandlers(src string) []string {
	var out []string
	for _, ev := range []string{"addEventListener('click',", "addEventListener('submit',"} {
		for i := 0; ; {
			k := strings.Index(src[i:], ev)
			if k < 0 {
				break
			}
			open := i + k + len("addEventListener")
			e := jsClose(src, open)
			if e < 0 {
				break
			}
			out = append(out, src[i+k+len(ev):e-1])
			i = i + k + len(ev)
		}
	}
	for i := 0; ; {
		k := strings.Index(src[i:], ".onclick=")
		if k < 0 {
			break
		}
		a := strings.Index(src[i+k:], "=>")
		if a < 0 {
			break
		}
		out = append(out, jsArrowBody(src, i+k+a+2))
		i = i + k + a + 2
	}
	return out
}

// jsUnguarded returns how many click handlers a script has, how many of them send a request
// (fetch, directly or through the script's own functions), and those that send without going
// through busy: neither the handler itself nor every sending function it calls holds busy(.
func jsUnguarded(src string) (handlers, sending int, bad []string) {
	funcs := jsFuncs(src)
	sends := map[string]bool{"fetch": true}
	for changed := true; changed; {
		changed = false
		for name, body := range funcs {
			if sends[name] {
				continue
			}
			now := jsNow(body)
			for s := range sends {
				if jsCalls(now, s) {
					sends[name], changed = true, true
					break
				}
			}
		}
	}
	for _, h := range jsHandlers(src) {
		handlers++
		now := jsNow(h)
		var called []string
		for s := range sends {
			if jsCalls(now, s) {
				called = append(called, s)
			}
		}
		if len(called) == 0 {
			continue
		}
		sending++
		if strings.Contains(now, "busy(") {
			continue
		}
		guarded := true
		for _, s := range called {
			if s == "fetch" || !strings.Contains(jsNow(funcs[s]), "busy(") {
				guarded = false
			}
		}
		if !guarded {
			bad = append(bad, strings.TrimSpace(h))
		}
	}
	return handlers, sending, bad
}

// covers: MA-872, R37
// The checker itself, first, on a script of two buttons: one whose click sends without busy is
// named, one whose click goes through busy is not (positive twin); a click that only schedules
// a request for later, or only changes the page, sends nothing.
func TestBusyCheckerNamesAButtonThatSendsWithoutBusy(t *testing.T) {
	good := `function busy(b,label){b.disabled=true;return()=>{b.disabled=false;};}
function api(p){return fetch(p,{method:'POST'});}
function save(done){api('api/save').then(d=>{done();});}
$('a').addEventListener('click',()=>save(busy($('a'),'Saving…')));
$('b').addEventListener('click',()=>{const done=busy($('b'),'Working…');api('api/x').then(done);});
$('c').addEventListener('click',()=>{setTimeout(()=>api('api/count'),300);});
$('d').addEventListener('click',()=>{$('p').hidden=true;});
`
	h, s, bad := jsUnguarded(good)
	if h != 4 || s != 2 || len(bad) != 0 {
		t.Fatalf("on the guarded script the checker found %d handlers, %d sending, unguarded %q; want 4, 2, none", h, s, bad)
	}
	for name, script := range map[string]string{
		"direct":  good + "$('e').addEventListener('click',()=>{api('api/y');});\n",
		"through": good + "$('e').addEventListener('click',()=>save(()=>{}));\n",
		"onclick": good + "$('#e').onclick=()=>{fetch('api/z');};\n",
		"submit":  good + "$('f').addEventListener('submit',async ev=>{ev.preventDefault();await api('api/u');});\n",
	} {
		if _, _, bad := jsUnguarded(script); len(bad) != 1 {
			t.Errorf("%s: a button that sends without busy was not named (unguarded %q)", name, bad)
		}
	}
}

// covers: MA-872, R37
// Every Desktop page script (the Overview's dashboard.js and encryption.js, the sign-in script,
// workspace.js and exports.js) has one busy helper that disables the button, marks it
// aria-busy, swaps its label for the working one, says what is happening in a note, and hands
// back a restore that undoes all of it; and every click or submit handler that sends a request
// goes through it, so no Desktop button posts without showing that it was pressed.
func TestEveryDesktopButtonThatSendsGoesThroughBusy(t *testing.T) {
	total := 0
	for name, src := range map[string]string{"dashboard.js": dashboardJS, "encryption.js": encryptionJS, "signin.js": signinJS, "workspace.js": workspaceJS, "exports.js": exportsJS} {
		if n := strings.Count(src, "function busy("); n != 1 {
			t.Errorf("%s defines busy %d times, want once", name, n)
			continue
		}
		helper := jsFuncs(src)["busy"]
		for _, want := range []string{"b.disabled=true", "b.setAttribute('aria-busy','true')", "b.textContent=label", "b.removeAttribute('aria-busy')", "b.textContent=was", "b.disabled=!!keep"} {
			if !strings.Contains(helper, want) {
				t.Errorf("%s's busy does not %q:\n%s", name, want, helper)
			}
		}
		// the note: written into the given note element, or (the Encryption card) through its say
		if !strings.Contains(helper, "n.textContent=text") && !strings.Contains(helper, "if(text)say(text)") {
			t.Errorf("%s's busy writes no note saying what is happening:\n%s", name, helper)
		}
		handlers, sending, bad := jsUnguarded(src)
		for _, h := range bad {
			t.Errorf("%s: a button's click sends without busy:\n%s", name, h)
		}
		if sending == 0 || handlers < sending {
			t.Errorf("%s: %d click handlers found, %d of them sending: the checker read nothing", name, handlers, sending)
		}
		t.Logf("%s: %d click handlers, %d of them sending, %d without busy", name, handlers, sending, len(bad))
		total += sending
	}
	// the Overview alone has some thirty buttons that send: a count far below means the reading missed them
	assure.Reached(t, total >= 45, "at least 45 sending click handlers read across the Desktop's scripts")
}

// covers: MA-872, R37
// The Scheduled backup card's Repair and Remove keep their reply in sight: the state line the
// card redraws after the reply starts with what the press got ("Repaired.", "Removed." or the
// refusal), not the bare state, which reads the same before and after a Repair (the live friction
// walk of 2026-10-28 found the reply overwritten at once). Each button names its own word.
func TestTheCardsRepairAndRemoveReplyStaysAfterTheStateRedraw(t *testing.T) {
	f := jsFuncs(dashboardJS)
	cp, st := f["cardPost"], f["schedState"]
	if cp == "" || st == "" {
		t.Fatal("cardPost or schedState not found in the page script")
	}
	if !strings.Contains(cp, "schedState(said)") || strings.Contains(cp, "schedState()") {
		t.Errorf("cardPost redraws the state line without the reply:\n%s", cp)
	}
	// a Remove that went through draws the page again, after the reply was in sight, so the card
	// stops offering Repair and Run now and Settings stops showing the removed cadence
	if !strings.Contains(cp, "else if(d.ok&&path==='api/schedule-remove')setTimeout(()=>location.reload(),2500)") {
		t.Errorf("a Remove that went through leaves the card and Settings as they were:\n%s", cp)
	}
	// every branch of the state line starts with the reply (four since MA-949 added "Runs only
	// when you click Run now.")
	if n := strings.Count(st, "n.textContent=pre+"); n != 4 || strings.Count(st, "n.textContent=") != 4 {
		t.Errorf("schedState puts the reply in front of %d of its 4 state lines:\n%s", n, st)
	}
	for _, w := range []string{"'Repairing the scheduled backup…'),'Repaired.')", "'Removing the scheduled backup…'),'Removed.')"} {
		if !strings.Contains(dashboardJS, w) {
			t.Errorf("a card button does not name its own reply word %q", w)
		}
	}
}

// covers: MA-859, R37
// The card's state line says when an hourly backup runs in the words Settings uses ("hourly uses
// the minutes"): "hourly, at 00 minutes past each hour", never "hourly at 03:00", which reads as a
// once-a-day 3 a.m. run (the live walk of 2026-10-28). Daily and weekly keep their time of day.
func TestTheCardSaysAnHourlyBackupRunsAtItsMinutesPastEachHour(t *testing.T) {
	st := jsFuncs(dashboardJS)["schedState"]
	if st == "" {
		t.Fatal("schedState not found in the page script")
	}
	for _, w := range []string{"d.interval==='hourly'?', at '+d.at.slice(-2)+' minutes past each hour'", ":' at '+d.at"} {
		if !strings.Contains(st, w) {
			t.Errorf("the state line lacks %q:\n%s", w, st)
		}
	}
}

// covers: MA-872, R37
// Each Desktop style block (the Overview's, and the workspace and export pages') carries a pressed
// rule for .btn (darker, one pixel down) and a busy rule (progress cursor and a spinner that stops
// turning under prefers-reduced-motion), with [hidden] still winning; and the pages served carry them.
func TestDesktopStyleBlocksCarryThePressedAndBusyRules(t *testing.T) {
	rules := []string{
		".btn:active:not(:disabled){filter:brightness(.85);transform:translateY(1px)}",
		".btn[aria-busy=true],button[aria-busy=true]{cursor:progress;opacity:.75}",
		"button[aria-busy=true]::before{",
		"animation:busy-spin",
		"@keyframes busy-spin",
		"@media(prefers-reduced-motion:reduce){button[aria-busy=true]::before{animation:none}}",
		".busy-note{",
		"[hidden]{display:none!important}",
	}
	overview, _ := getPage(t, DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: filepath.Join(t.TempDir(), "archive"), GOOS: "darwin"}))
	e := newWsEnv(t)
	pages := map[string]string{"the Overview": overview, "workspace": e.get("/workspace").Body.String(), "exports": e.get("/exports").Body.String(), "pageCSS": pageCSS}
	for name, page := range pages {
		for _, want := range rules {
			if !strings.Contains(page, want) {
				t.Errorf("%s's style does not carry %q", name, want)
			}
		}
		// busy comes after :disabled, so a working button shows the progress cursor, not "not allowed"
		if d, b := strings.Index(page, ".btn:disabled{"), strings.Index(page, ".btn[aria-busy=true]"); d < 0 || b < d {
			t.Errorf("%s: the busy rule does not follow .btn:disabled (at %d, %d)", name, d, b)
		}
	}
}
