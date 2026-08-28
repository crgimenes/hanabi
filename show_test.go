package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseShowRecordsStepsInOrder(t *testing.T) {
	sh, err := parseShow(`
		(shot "wipe" "hello")
		(pause 1.5)
		(wait-key)
		(clear)
		(shot "decrypt,burn" "again")
	`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	steps := sh.steps
	kinds := make([]showStepKind, 0, len(steps))
	for _, st := range steps {
		kinds = append(kinds, st.kind)
	}
	want := []showStepKind{stepShot, stepPause, stepWaitKey, stepClear, stepShot}
	if len(kinds) != len(want) {
		t.Fatalf("recorded %d steps, want %d", len(kinds), len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("step %d is kind %d, want %d", i, kinds[i], want[i])
		}
	}
	if steps[1].hold != 1500*time.Millisecond {
		t.Fatalf("pause recorded %v, want 1.5s", steps[1].hold)
	}
	if len(steps[4].entries) != 2 {
		t.Fatalf("the second shot recorded %d effects, want 2", len(steps[4].entries))
	}
}

// The script language is the loop mechanism: repeating a shot is (map ... (range n)),
// and the steps land in order because evaluation is in order.
func TestParseShowLoopsViaTheLanguage(t *testing.T) {
	sh, err := parseShow(`(map (fn (i) (shot "wipe" "tick")) (range 3))`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	steps := sh.steps
	if len(steps) != 3 {
		t.Fatalf("recorded %d steps, want 3", len(steps))
	}
}

func TestParseShowResolvesFilesAgainstTheScriptDir(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "art.txt"), []byte("the art\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := parseShow(`(shot "wipe" (file "art.txt"))`, dir)
	if err != nil {
		t.Fatal(err)
	}
	steps := sh.steps
	got := strings.TrimRight(rowText(steps[0]), " \n")
	if got != "the art" {
		t.Fatalf("the shot holds %q, want the file's text", got)
	}
}

func rowText(st showStep) string {
	var b strings.Builder
	for x := range st.target.W {
		b.WriteRune(st.target.At(x, 0).R)
	}
	return b.String()
}

// Every mistake a script can make has to come back as an error that names the
// builtin, not as a silent no-op or a crash.
func TestParseShowRefusesBadScripts(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "unknown effect", src: `(shot "nosuch" "x")`, want: "unknown effect"},
		{name: "empty text", src: `(shot "wipe" "")`, want: "empty"},
		{name: "shot arity", src: `(shot "wipe")`, want: "shot"},
		{name: "pause range", src: `(pause 9999)`, want: "0..3600"},
		{name: "pause type", src: `(pause "x")`, want: "pause"},
		{name: "wait-key arity", src: `(wait-key 1)`, want: "wait-key"},
		{name: "missing file", src: `(shot "wipe" (file "absent.txt"))`, want: "file"},
		{name: "no steps", src: `(+ 1 2)`, want: "no steps"},
		{name: "parse error", src: `(shot "wipe"`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseShow(tt.src, t.TempDir())
			if err == nil {
				t.Fatal("the script was accepted")
			}
			if tt.want != "" && !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("the error does not mention %q: %v", tt.want, err)
			}
		})
	}
}

// The cap is what stands between a runaway loop and half a gigabyte of
// playlist. The limits have to catch it whichever gives out first.
func TestParseShowCapsARunawayLoop(t *testing.T) {
	_, err := parseShow(`(map (fn (i) (shot "wipe" "x")) (range 100000))`, t.TempDir())
	if err == nil {
		t.Fatal("a hundred thousand shots were accepted")
	}
}

func TestParseShowRefusesNonUTF8Files(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "cp437.ans"), []byte{0x1b, '[', '1', 'm', 0xDC}, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseShow(`(shot "wipe" (file "cp437.ans"))`, dir)
	if err == nil || !strings.Contains(err.Error(), "iconv") {
		t.Fatalf("the error does not teach the iconv fix: %v", err)
	}
}

func TestShowTextCarriesEveryShotForPipes(t *testing.T) {
	sh, err := parseShow(`
		(shot "wipe" "one")
		(wait-key)
		(shot "burn" "two")
	`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	steps := sh.steps
	out := showText(steps)
	for _, want := range []string{"one", "two"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the pipe text is missing %q: %q", want, out)
		}
	}
}

func testSession(keys chan key) *session {
	return &session{
		ctx:    context.Background(),
		keys:   keys,
		ticker: time.NewTicker(time.Millisecond),
		keymap: defaultKeymap(),
	}
}

// The keys arrive after holdKey is already waiting: anything sent before it
// starts is a stale key from the animation, and the drain throws those away.
func TestHoldKeyAdvancesOnAnyKeyAndQuitsOnQ(t *testing.T) {
	press := func(k key) error {
		keys := make(chan key, 1)
		s := testSession(keys)
		defer s.ticker.Stop()
		done := make(chan error, 1)
		go func() { done <- holdKey(s) }()
		time.Sleep(20 * time.Millisecond)
		keys <- k
		select {
		case err := <-done:
			return err
		case <-time.After(2 * time.Second):
			t.Fatal("holdKey never returned")
			return nil
		}
	}

	if err := press(key('x')); err != nil {
		t.Fatalf("an ordinary key gave %v, want to advance", err)
	}
	if err := press(key('q')); !errors.Is(err, errQuit) {
		t.Fatalf("q gave %v, want errQuit", err)
	}
}

// Keys pressed during the animation must not skip screens: an escape sequence
// arrives as several bytes, each of which lands as its own advance.
func TestHoldKeyDrainsStaleKeysBeforeWaiting(t *testing.T) {
	keys := make(chan key, 1)
	s := testSession(keys)
	defer s.ticker.Stop()
	keys <- key('x')

	done := make(chan error, 1)
	go func() { done <- holdKey(s) }()
	select {
	case err := <-done:
		t.Fatalf("holdKey returned %v on a stale key; it must wait for a fresh one", err)
	case <-time.After(50 * time.Millisecond):
	}
	keys <- key('x')
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the fresh key gave %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the fresh key never advanced")
	}
}

func TestHoldForWaitsAndReactsToQuit(t *testing.T) {
	s := testSession(make(chan key, 1))
	defer s.ticker.Stop()
	start := time.Now()
	err := holdFor(s, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 25*time.Millisecond {
		t.Fatal("holdFor returned early")
	}

	keys := make(chan key, 1)
	s2 := testSession(keys)
	defer s2.ticker.Stop()
	keys <- key('q')
	err = holdFor(s2, time.Hour)
	if !errors.Is(err, errQuit) {
		t.Fatalf("q during a pause gave %v, want errQuit", err)
	}
}

// The examples ship in the repository, so unlike the art corpus they are always
// present: every one of them has to parse, and its (file ...) references have
// to resolve. This is what keeps the examples from rotting as builtins change.
func TestEveryExampleShowParses(t *testing.T) {
	entries, err := os.ReadDir("examples")
	if err != nil {
		t.Fatal(err)
	}
	shows := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".filo") {
			continue
		}
		shows++
		t.Run(e.Name(), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join("examples", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			sh, err := parseShow(string(b), "examples")
			if err != nil {
				t.Fatal(err)
			}
			steps := sh.steps
			hasShot := false
			for _, st := range steps {
				if st.kind == stepShot {
					hasShot = true
				}
			}
			if !hasShot {
				t.Fatal("the example records no shot")
			}
		})
	}
	if shows == 0 {
		t.Fatal("no example shows found")
	}
}

// A function body holds several expressions and all of them run: the moods
// example leans on (fn (face) (shot ...) (pause ...)) recording two steps per
// face, and a language change that dropped that would thin the show silently.
func TestFnBodyRecordsEveryExpression(t *testing.T) {
	sh, err := parseShow(`
		(map (fn (x) (shot "wipe" "tick") (pause 0.1)) (range 2))
	`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	steps := sh.steps
	shots, pauses := 0, 0
	for _, st := range steps {
		switch st.kind {
		case stepShot:
			shots++
		case stepPause:
			pauses++
		}
	}
	if shots != 2 || pauses != 2 {
		t.Fatalf("recorded %d shots and %d pauses, want 2 and 2", shots, pauses)
	}
}

func TestShotAcceptsASpeedAndRefusesANonsenseOne(t *testing.T) {
	sh, err := parseShow(`(shot "wipe" "x" 0.5)`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	steps := sh.steps
	if steps[0].speed != 0.5 {
		t.Fatalf("recorded speed %v, want 0.5", steps[0].speed)
	}

	_, err = parseShow(`(shot "wipe" "x" 99)`, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "0.1..10") {
		t.Fatalf("a nonsense speed was accepted: %v", err)
	}
	_, err = parseShow(`(shot "wipe" "x" "fast")`, t.TempDir())
	if err == nil {
		t.Fatal("a string speed was accepted")
	}
}

// A stop key must never be thrown away. holdKey drains what the animation left
// behind, and draining a q loses the reader's decision: the show sits there, or
// the q surfaces later and closes a screen nobody was looking at.
func TestHoldKeyNeverDrainsAwayAStopKey(t *testing.T) {
	for _, tt := range []struct {
		name string
		k    key
		want error
	}{
		{name: "q", k: key('q'), want: errQuit},
		{name: "ctrl-c", k: keyInterrupt, want: context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			keys := make(chan key, 1)
			s := testSession(keys)
			defer s.ticker.Stop()
			// Already queued when the wait begins: pressed a moment before the
			// animation ended, which is exactly when it is easy to lose.
			keys <- tt.k

			done := make(chan error, 1)
			go func() { done <- holdKey(s) }()
			select {
			case err := <-done:
				if !errors.Is(err, tt.want) {
					t.Fatalf("got %v, want %v", err, tt.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("the key was drained away and the show hung")
			}
		})
	}
}

func showOf(t *testing.T, src string) *show {
	t.Helper()
	sh, err := parseShow(src+"\n(shot \"wipe\" \"x\")", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return sh
}

func keymapOf(t *testing.T, src string) keymap {
	t.Helper()
	return showOf(t, src).keys
}

// The script owns the keyboard: q stops the animation only because the default
// map says so, and a script that says otherwise is obeyed.
func TestSetKeyRebindsAndUnbinds(t *testing.T) {
	if !errors.Is(keymapOf(t, "").stopErr('q'), errQuit) {
		t.Fatal("q does not quit when the script says nothing")
	}

	// Disabled: pressing it by accident costs nothing.
	if got := keymapOf(t, `(set-key "q" (none))`).stopErr('q'); got != nil {
		t.Fatalf("q still does %v after being unbound", got)
	}

	// Moved somewhere else.
	m := keymapOf(t, `(set-key "q" (none)) (set-key "x" (quit))`)
	if m.stopErr('q') != nil {
		t.Fatal("q was not released")
	}
	if !errors.Is(m.stopErr('x'), errQuit) {
		t.Fatal("x was not given the job")
	}
}

// Ctrl-C is the reader's way out and no script may take it, whatever it binds.
func TestSetKeyCannotTakeTheAbort(t *testing.T) {
	_, err := parseShow("(set-key \"\x03\" (none))\n(shot \"wipe\" \"x\")", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "Ctrl-C") {
		t.Fatalf("a script was allowed to unbind the abort: %v", err)
	}
	if !errors.Is(keymapOf(t, `(set-key "q" (none))`).stopErr(keyInterrupt), context.Canceled) {
		t.Fatal("Ctrl-C stopped aborting")
	}
}

func TestSetKeyRefusesNonsense(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "two keys", src: `(set-key "qq" (quit))`, want: "single key"},
		{name: "not an action", src: `(set-key "q" "exit")`, want: "second argument"},
		{name: "arity", src: `(set-key "q")`, want: "set-key"},
		{name: "goto nowhere", src: `(set-key "m" (goto "absent"))`, want: "no (label) names"},
		{name: "empty label", src: `(label "")`, want: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseShow(tt.src+"\n(shot \"wipe\" \"x\")", t.TempDir())
			if err == nil {
				t.Fatal("the script was accepted")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("the error does not mention %q: %v", tt.want, err)
			}
		})
	}
}

// A jump has to carry its destination in the error, not beside it: two waits
// racing on a shared field would send the reader somewhere they did not press.
func TestJumpCarriesItsDestination(t *testing.T) {
	m := keymapOf(t, `(label "menu") (set-key "m" (goto "menu")) (set-key "n" (next))`)
	var jump jumpError
	if !errors.As(m.stopErr('m'), &jump) || jump.to.kind != actGoto || jump.to.label != "menu" {
		t.Fatalf("m did not report a jump to menu: %+v", jump)
	}
	if !errors.As(m.stopErr('n'), &jump) || jump.to.kind != actNext {
		t.Fatalf("n did not report a skip: %+v", jump)
	}
}

// A label is a place, not a screen: it must not make the show pause or draw.
func TestLabelDrawsNothing(t *testing.T) {
	sh, err := parseShow(`(label "top") (shot "wipe" "only")`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	steps := sh.steps
	if steps[0].kind != stepLabel || findLabel(steps, "top") != 0 {
		t.Fatal("the label was not recorded where it was written")
	}
	if strings.Count(showText(steps), "only") != 1 {
		t.Fatal("the label contributed text of its own")
	}
}

// Filo's own (exit) ends the script where it stands, which is how a show is
// written conditionally: everything recorded up to there is the show, and that
// is not an error.
func TestFiloExitEndsTheScriptAndKeepsWhatWasRecorded(t *testing.T) {
	sh, err := parseShow(`
		(shot "wipe" "one")
		(shot "wipe" "two")
		(if #t (exit) 0)
		(shot "wipe" "never")
	`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	steps := sh.steps
	if len(steps) != 2 {
		t.Fatalf("recorded %d steps, want the 2 written before the exit", len(steps))
	}
	if strings.Contains(showText(steps), "never") {
		t.Fatal("a step after the exit was recorded")
	}
}

// (exit) and (quit) sit next to each other in the vocabulary and mean different
// things, so reaching for the wrong one has to say which is which.
func TestExitAsAnActionPointsAtQuit(t *testing.T) {
	_, err := parseShow(`(set-key "q" (exit)) (shot "wipe" "x")`, t.TempDir())
	if err == nil {
		t.Fatal("(exit) was accepted as an action")
	}
	for _, want := range []string{"Filo's own", "(quit)"} {
		if strings.Contains(err.Error(), want) {
			continue
		}
		t.Errorf("the error does not mention %q: %v", want, err)
	}
}

// A function decides when the key is pressed, so the same key can mean
// different things at different moments. That is the whole reason to have it.
func TestHandlerFunctionDecidesAtPressTime(t *testing.T) {
	sh := showOf(t, `
		(label "a")
		(set visits 0)
		(set-key "n" (fn ()
			(set visits (+ visits 1))
			(if (< visits 2) (goto "a") (quit))))
	`)

	first, err := sh.hand.call('n')
	if err != nil {
		t.Fatal(err)
	}
	if first.kind != actGoto || first.label != "a" {
		t.Fatalf("first press gave %+v, want a jump to a", first)
	}

	second, err := sh.hand.call('n')
	if err != nil {
		t.Fatal(err)
	}
	if second.kind != actExit {
		t.Fatalf("second press gave %+v, want a quit; the handler forgot the first", second)
	}
}

// A handler sees what the script left behind, or a closure over the show's own
// variables would go undefined the moment it is called.
func TestHandlerSeesTheScriptsGlobals(t *testing.T) {
	sh := showOf(t, `
		(label "target")
		(set destino "target")
		(set-key "g" (fn () (goto destino)))
	`)
	a, err := sh.hand.call('g')
	if err != nil {
		t.Fatal(err)
	}
	if a.kind != actGoto || a.label != "target" {
		t.Fatalf("got %+v, want the jump the global named", a)
	}
}

// The one place a show evaluates Filo while it plays has to stay bounded, or a
// handler with a runaway loop takes the terminal with it.
func TestHandlerIsBounded(t *testing.T) {
	sh := showOf(t, `(set-key "b" (fn () (fold (fn (a c) (+ a c)) 0 (range 100000))))`)
	_, err := sh.hand.call('b')
	if err == nil {
		t.Fatal("a runaway handler ran to completion")
	}
	if !strings.Contains(err.Error(), "step limit") {
		t.Fatalf("the error does not name the limit that stopped it: %v", err)
	}
}

func TestHandlerMustReturnAnAction(t *testing.T) {
	sh := showOf(t, `(set-key "b" (fn () "nao e uma acao"))`)
	_, err := sh.hand.call('b')
	if err == nil || !strings.Contains(err.Error(), "must return") {
		t.Fatalf("a handler returning rubbish was accepted: %v", err)
	}
}

// Binding a function and then binding a plain action to the same key has to
// forget the function, or the stale one would answer for the new binding.
func TestRebindingOverAFunctionForgetsIt(t *testing.T) {
	sh := showOf(t, `(set-key "k" (fn () (quit))) (set-key "k" (next))`)
	if sh.keys['k'].kind != actNext {
		t.Fatalf("the key is %+v, want a plain next", sh.keys['k'])
	}
	if _, ok := sh.hand.fns['k']; ok {
		t.Fatal("the function stayed behind")
	}
}

// (jump) is the show moving on its own, which is what lets a section end by
// returning to the menu instead of falling into whatever was written next.
func TestJumpStepSendsTheShowBack(t *testing.T) {
	sh, err := parseShow(`
		(label "top")
		(shot "wipe" "first")
		(jump "end")
		(shot "wipe" "skipped")
		(label "end")
		(shot "wipe" "last")
	`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if at := findLabel(sh.steps, "end"); at < 0 {
		t.Fatal("the label was not recorded")
	}
	// The pipe rendering walks the steps as written, so the proof that the jump
	// is a step and not a value is that it was recorded at all.
	found := false
	for _, st := range sh.steps {
		if st.kind == stepJump && st.name == "end" {
			found = true
		}
	}
	if !found {
		t.Fatal("(jump) recorded nothing; it evaluated to a value and was dropped")
	}
}

func TestJumpToNowhereIsRefused(t *testing.T) {
	_, err := parseShow(`(shot "wipe" "x") (jump "absent")`, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no (label) to land on") {
		t.Fatalf("a jump to nowhere was accepted: %v", err)
	}
}

// Filo keeps strings and maths in packages a host registers on purpose. A show
// wants both -- captions get built, paces get computed -- and must not have the
// two that would spoil it: rand breaks replaying from a seed, and print writes
// into the middle of the animation.
func TestShowRegistersTheRightExtensions(t *testing.T) {
	available := []struct{ name, src string }{
		{name: "str-join", src: `(shot "wipe" (str-join "," (list "a" "b")))`},
		{name: "str-fmt", src: `(shot "wipe" (str-fmt "%v" 42))`},
		{name: "abs", src: `(shot "wipe" "x" (abs -1))`},
	}
	for _, tt := range available {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseShow(tt.src, t.TempDir())
			if err != nil {
				t.Fatalf("%s is not available to a show: %v", tt.name, err)
			}
		})
	}

	withheld := []struct{ name, src string }{
		{name: "rand-int", src: `(shot "wipe" (string (rand-int 3)))`},
		{name: "println", src: `(println "x") (shot "wipe" "y")`},
	}
	for _, tt := range withheld {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseShow(tt.src, t.TempDir())
			if err == nil {
				t.Fatalf("%s reached a show; it must not", tt.name)
			}
		})
	}
}

// The show language is Filo, not a subset of it: a script uses def, cond,
// closures and list handling to build its structure, and only the recording is
// ours. This pins that the pieces menu2.filo leans on actually work together.
func TestAShowIsAProgram(t *testing.T) {
	sh, err := parseShow(`
		(def seen (list))
		(def already? (fn (name) (> (length (filter (fn (s) (= s name)) seen)) 0)))
		(def pick (fn ()
			(cond
				((not (already? "one")) (goto "one"))
				(else (goto "done")))))
		(set-key "n" pick)
		(label "one")
		(shot "wipe" (str-join " " (list "built" "from" "a" "list")))
		(label "done")
		(shot "wipe" "end")
	`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(showText(sh.steps), "built from a list") {
		t.Fatal("the text the script assembled is not in the show")
	}
	a, err := sh.hand.call('n')
	if err != nil {
		t.Fatal(err)
	}
	if a.kind != actGoto || a.label != "one" {
		t.Fatalf("the handler decided %+v, want the first unseen section", a)
	}
}

// Pressing the same key twice has to get somewhere new. A handler that decides
// without recording keeps choosing the same section for ever, and asking it
// once cannot tell the difference -- which is how menu2.filo shipped broken.
func TestAWanderingHandlerMakesProgress(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("examples", "menu2.filo"))
	if err != nil {
		t.Fatal(err)
	}
	sh, err := parseShow(string(b), "examples")
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for range 3 {
		a, err := sh.hand.call('n')
		if err != nil {
			t.Fatal(err)
		}
		if a.kind != actGoto {
			t.Fatalf("the handler gave %+v, want a jump", a)
		}
		seen[a.label] = true
	}
	if len(seen) < 3 {
		t.Fatalf("three presses reached %v; the handler is not remembering where it has been", seen)
	}
}
