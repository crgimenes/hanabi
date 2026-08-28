package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/crgimenes/filo"
	"github.com/crgimenes/filo/filomath"
	"github.com/crgimenes/filo/filostrings"
	"github.com/crgimenes/hanabi/canvas"
	"github.com/crgimenes/hanabi/effect"
)

// A show script runs in two phases: the Filo program is evaluated once, under
// tight limits, and every builtin it calls only records a step. Playing the
// steps is the player's business, afterwards. The animation therefore never
// runs inside the evaluator -- a show may last an hour while the script that
// described it stays bounded to milliseconds -- and the same script always
// builds the same playlist.
const (
	// A show is written by hand; a thousand steps is a runaway loop, not a
	// presentation.
	maxShowSteps = 512

	showStepLimit = 200_000
	showRecursion = 64
	showTimeout   = 2 * time.Second
)

type showStepKind int

const (
	stepShot showStepKind = iota
	stepPause
	stepWaitKey
	stepClear
	stepLabel
	stepJump
)

type showStep struct {
	kind    showStepKind
	entries []effect.Entry
	target  *canvas.Canvas
	hold    time.Duration
	// speed is the shot's own pace on top of the command line's: the script
	// says how the show reads, the flag scales the whole sitting.
	speed float64
	// name labels a place a (goto) can land on.
	name string
}

// actionTag marks the tuples the action builtins return, so set-key can tell an
// action from any other value the script happens to hand it.
const actionTag = "hanabi/action"

func actionValue(kind actionKind, label string) filo.Value {
	return filo.VTuple([]filo.Value{
		filo.VString(actionTag),
		filo.VNum(float64(kind)),
		filo.VString(label),
	})
}

func asAction(v filo.Value) (action, bool) {
	parts, err := v.AsTuple()
	if err != nil || len(parts) != 3 {
		return action{}, false
	}
	tag, err := parts[0].AsString()
	if err != nil || tag != actionTag {
		return action{}, false
	}
	kind, err := parts[1].AsNumber()
	if err != nil {
		return action{}, false
	}
	label, err := parts[2].AsString()
	if err != nil {
		return action{}, false
	}
	return action{kind: actionKind(kind), label: label}, true
}

// parseShow evaluates the script and returns the steps it recorded. dir is the
// script's own directory: paths in the script resolve against it, so a show can
// sit next to its art and run from anywhere.
// handlers holds what a key bound to a Filo function needs at press time: the
// engine that compiled it, the globals the script left behind so a closure can
// still see them, and the functions themselves.
//
// This is the one place a show evaluates Filo while it plays. It is bounded the
// same way the script was, and it happens on a key press -- never per frame, so
// a two-hour show still needs the evaluator exactly as often as the reader
// touches the keyboard.
type handlers struct {
	eng     *filo.Engine
	globals map[string]filo.Value
	fns     map[key]filo.Value
}

const handlerName = "__hanabi_handler"

// call runs the function bound to k and reports what it decided.
func (h *handlers) call(k key) (action, error) {
	fn, ok := h.fns[k]
	if !ok {
		return action{}, fmt.Errorf("no handler is bound to %q", string(rune(k)))
	}
	globals := make(map[string]filo.Value, len(h.globals)+1)
	maps.Copy(globals, h.globals)
	globals[handlerName] = fn

	result, updated, err := h.eng.RunScript(context.Background(), "("+handlerName+")", globals, filo.EvalConfig{
		StepLimit:      showStepLimit,
		RecursionLimit: showRecursion,
		Timeout:        showTimeout,
	})
	if err != nil {
		return action{}, fmt.Errorf("the handler for %q: %w", string(rune(k)), explainExit(err))
	}
	// Kept, so a handler may remember things between presses with (set ...).
	delete(updated, handlerName)
	h.globals = updated

	a, ok := asAction(result)
	if !ok {
		return action{}, fmt.Errorf(
			"the handler for %q returned %v; it must return (next), (none) or (goto \"label\")",
			string(rune(k)), result)
	}
	return a, nil
}

type show struct {
	steps []showStep
	keys  keymap
	hand  *handlers
}

func parseShow(src, dir string) (*show, error) {
	steps := make([]showStep, 0, 16)
	keys := defaultKeymap()
	fns := map[key]filo.Value{}
	record := func(st showStep) (filo.Value, error) {
		if len(steps) >= maxShowSteps {
			return filo.VBool(false), fmt.Errorf("more than %d steps; this is a loop, not a show", maxShowSteps)
		}
		steps = append(steps, st)
		return filo.VBool(true), nil
	}

	eng := filo.NewEngine()
	// Filo ships these separately so a host decides what a script may reach.
	// Strings and maths are what a show wants: captions get built, paces get
	// computed. filorand is deliberately left out -- it says so itself that it
	// is non-deterministic, and a show is meant to replay from its seed. So is
	// filoprint: anything it wrote would land in the middle of the animation.
	filostrings.RegisterBuiltins(eng)
	filomath.RegisterBuiltins(eng)

	err := errors.Join(
		eng.RegisterBuiltin("shot", shotBuiltin(record)),
		eng.RegisterBuiltin("file", fileBuiltin(dir)),
		eng.RegisterBuiltin("pause", pauseBuiltin(record)),
		eng.RegisterBuiltin("wait-key", markerBuiltin("wait-key", stepWaitKey, record)),
		eng.RegisterBuiltin("clear", markerBuiltin("clear", stepClear, record)),
		eng.RegisterBuiltin("label", labelBuiltin(record)),
		eng.RegisterBuiltin("jump", jumpBuiltin(record)),
		eng.RegisterBuiltin("next", actionBuiltin("next", actNext)),
		eng.RegisterBuiltin("none", actionBuiltin("none", actAdvance)),
		eng.RegisterBuiltin("goto", gotoBuiltin()),
		eng.RegisterBuiltin("set-key", setKeyBuiltin(keys, fns)),
	)
	if err != nil {
		return nil, err
	}

	_, globals, err := eng.RunScript(context.Background(), src, nil, filo.EvalConfig{
		StepLimit:      showStepLimit,
		RecursionLimit: showRecursion,
		Timeout:        showTimeout,
	})
	if err != nil {
		return nil, explainExit(err)
	}
	if len(steps) == 0 {
		return nil, errors.New("the script recorded no steps; a show needs at least one shot")
	}
	for k, a := range keys {
		if a.kind != actGoto || findLabel(steps, a.label) >= 0 {
			continue
		}
		return nil, fmt.Errorf("set-key %q jumps to %q, which no (label) names", string(rune(k)), a.label)
	}
	for _, st := range steps {
		if st.kind != stepJump || findLabel(steps, st.name) >= 0 {
			continue
		}
		return nil, fmt.Errorf("(jump %q) has no (label) to land on", st.name)
	}
	return &show{
		steps: steps,
		keys:  keys,
		hand:  &handlers{eng: eng, globals: globals, fns: fns},
	}, nil
}

// explainExit names the one confusing collision in the vocabulary. (exit) is
// Filo's own, and it ends the script -- which is a useful thing to do, just not
// a value a binding can hold. Reaching for it there is the natural first guess,
// and Filo answers with a bare "exit" that explains nothing.
//
// Matched on the message because the signal type is unexported in Filo. It
// would read better as an exported sentinel there; the cost of guessing wrong
// here is only that a good error stays a poor one.
func explainExit(err error) error {
	if !strings.HasSuffix(err.Error(), ": exit") {
		return err
	}
	return fmt.Errorf("%w\n(exit) is Filo's own, and ends the script where it stands. "+
		"A key that leaves is (goto \"label\") with nothing recorded after that label", err)
}

func findLabel(steps []showStep, name string) int {
	for i, st := range steps {
		if st.kind == stepLabel && st.name == name {
			return i
		}
	}
	return -1
}

func labelBuiltin(record recordFunc) filo.Builtin {
	return func(_ context.Context, args []filo.Value) (filo.Value, error) {
		if len(args) != 1 {
			return filo.VBool(false), errors.New(`label: want (label "name")`)
		}
		name, err := args[0].AsString()
		if err != nil {
			return filo.VBool(false), fmt.Errorf("label: %w", err)
		}
		if name == "" {
			return filo.VBool(false), errors.New("label: the name is empty")
		}
		return record(showStep{kind: stepLabel, name: name})
	}
}

// jumpBuiltin is the show jumping of its own accord, where (goto) is what a key
// does. The two are separate because one is a step and the other is a value: a
// (goto) written where a step belongs would only build an action nobody holds.
func jumpBuiltin(record recordFunc) filo.Builtin {
	return func(_ context.Context, args []filo.Value) (filo.Value, error) {
		if len(args) != 1 {
			return filo.VBool(false), errors.New(`jump: want (jump "label")`)
		}
		name, err := args[0].AsString()
		if err != nil {
			return filo.VBool(false), fmt.Errorf("jump: %w", err)
		}
		return record(showStep{kind: stepJump, name: name})
	}
}

func actionBuiltin(name string, kind actionKind) filo.Builtin {
	return func(_ context.Context, args []filo.Value) (filo.Value, error) {
		if len(args) != 0 {
			return filo.VBool(false), fmt.Errorf("%s: takes no arguments", name)
		}
		return actionValue(kind, ""), nil
	}
}

func gotoBuiltin() filo.Builtin {
	return func(_ context.Context, args []filo.Value) (filo.Value, error) {
		if len(args) != 1 {
			return filo.VBool(false), errors.New(`goto: want (goto "label")`)
		}
		label, err := args[0].AsString()
		if err != nil {
			return filo.VBool(false), fmt.Errorf("goto: %w", err)
		}
		return actionValue(actGoto, label), nil
	}
}

// setKeyBuiltin binds one key. Ctrl-C is refused: a script that could take the
// abort could leave the reader with no way out.
func setKeyBuiltin(keys keymap, fns map[key]filo.Value) filo.Builtin {
	return func(_ context.Context, args []filo.Value) (filo.Value, error) {
		if len(args) != 2 {
			return filo.VBool(false), errors.New(`set-key: want (set-key "k" (goto "label"))`)
		}
		s, err := args[0].AsString()
		if err != nil {
			return filo.VBool(false), fmt.Errorf("set-key: %w", err)
		}
		if len([]byte(s)) != 1 {
			return filo.VBool(false), fmt.Errorf("set-key: %q is not a single key", s)
		}
		k := key(s[0])
		if k == keyInterrupt {
			return filo.VBool(false), errors.New("set-key: Ctrl-C always aborts and cannot be bound")
		}
		if fn := args[1]; fn.Kind == filo.KFunc {
			// A function decides when the key is pressed rather than now, which
			// is what lets a menu remember where the reader has been.
			fns[k] = fn
			keys[k] = action{kind: actCall}
			return filo.VBool(true), nil
		}
		a, ok := asAction(args[1])
		if !ok {
			return filo.VBool(false), errors.New(
				`set-key: the second argument must be (next), (none), (goto "label") or a function returning one`)
		}
		delete(fns, k)
		if a.kind == actAdvance {
			// (none) unbinds. An unbound key does nothing during an animation
			// and advances at a (wait-key), which is what every other key does:
			// the point is that pressing it by accident costs nothing.
			delete(keys, k)
			return filo.VBool(true), nil
		}
		keys[k] = a
		return filo.VBool(true), nil
	}
}

type recordFunc func(showStep) (filo.Value, error)

func shotBuiltin(record recordFunc) filo.Builtin {
	return func(_ context.Context, args []filo.Value) (filo.Value, error) {
		if len(args) != 2 && len(args) != 3 {
			return filo.VBool(false), errors.New(`shot: want (shot "effect,effect" text [speed])`)
		}
		names, err := args[0].AsString()
		if err != nil {
			return filo.VBool(false), fmt.Errorf("shot: effects: %w", err)
		}
		entries, err := parseEffects(names)
		if err != nil {
			return filo.VBool(false), fmt.Errorf("shot: %w", err)
		}
		text, err := args[1].AsString()
		if err != nil {
			return filo.VBool(false), fmt.Errorf("shot: text: %w", err)
		}
		target := canvas.FromText(text, canvas.Default)
		if target.W == 0 || target.H == 0 {
			return filo.VBool(false), errors.New("shot: the text is empty")
		}
		speed := 1.0
		if len(args) == 3 {
			speed, err = args[2].AsNumber()
			if err != nil {
				return filo.VBool(false), fmt.Errorf("shot: speed: %w", err)
			}
			if speed < 0.1 || speed > 10 {
				return filo.VBool(false), fmt.Errorf("shot: speed %v is outside 0.1..10", speed)
			}
		}
		return record(showStep{kind: stepShot, entries: entries, target: target, speed: speed})
	}
}

func fileBuiltin(dir string) filo.Builtin {
	return func(_ context.Context, args []filo.Value) (filo.Value, error) {
		if len(args) != 1 {
			return filo.VBool(false), errors.New(`file: want (file "path")`)
		}
		path, err := args[0].AsString()
		if err != nil {
			return filo.VBool(false), fmt.Errorf("file: %w", err)
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		b, err := os.ReadFile(path) // #nosec G304 -- the path comes from the user's own script.
		if err != nil {
			return filo.VBool(false), fmt.Errorf("file: %w", err)
		}
		if !utf8.Valid(b) {
			return filo.VBool(false), fmt.Errorf(
				"file: %s is not valid UTF-8; old .ans art is usually CP437:\n  iconv -f CP437 -t UTF-8 %s", path, path)
		}
		return filo.VString(string(b)), nil
	}
}

func pauseBuiltin(record recordFunc) filo.Builtin {
	return func(_ context.Context, args []filo.Value) (filo.Value, error) {
		if len(args) != 1 {
			return filo.VBool(false), errors.New("pause: want (pause seconds)")
		}
		s, err := args[0].AsNumber()
		if err != nil {
			return filo.VBool(false), fmt.Errorf("pause: %w", err)
		}
		if s < 0 || s > 3600 {
			return filo.VBool(false), fmt.Errorf("pause: %v seconds is outside 0..3600", s)
		}
		return record(showStep{kind: stepPause, hold: time.Duration(s * float64(time.Second))})
	}
}

// markerBuiltin covers the steps that carry nothing: the builtin's whole job is
// to be called at the right place in the script.
func markerBuiltin(name string, kind showStepKind, record recordFunc) filo.Builtin {
	return func(_ context.Context, args []filo.Value) (filo.Value, error) {
		if len(args) != 0 {
			return filo.VBool(false), fmt.Errorf("%s: takes no arguments", name)
		}
		return record(showStep{kind: kind})
	}
}

// runShow plays the recorded steps in order. Each shot reserves its own region
// at the cursor and leaves its text behind, so the show reads down the terminal
// the way the effects always have; (clear) is the script asking for a clean
// screen instead.
func runShow(sh *show, o opts) int {
	return withSession(o.fps, func(s *session) int {
		s.keymap = sh.keys
		seed := o.seed
		for {
			status, quit := playSteps(s, sh, seed, o)
			if quit || status != 0 || !o.loop {
				return status
			}
			err := holdFor(s, o.dwell)
			if err != nil {
				return exitFor(err)
			}
			seed++
		}
	})
}

// playSteps walks the steps by index rather than ranging over them, because a
// binding may send the reader somewhere else entirely.
func playSteps(s *session, sh *show, seed uint64, o opts) (status int, quit bool) {
	steps := sh.steps
	for i := 0; i < len(steps); i++ {
		st := steps[i]
		var err error
		switch st.kind {
		case stepShot:
			// #nosec G115 -- a step index is never negative. The multiplier keeps
			// shot seeds apart from the pass-advance, which only adds one.
			status, quit, err = playShot(s, st, seed^(uint64(i+1)*0x9e3779b97f4a7c15), st.speed*o.speed)
			if err == nil && (quit || status != 0) {
				return status, quit
			}
		case stepPause:
			err = holdFor(s, st.hold)
		case stepWaitKey:
			err = holdKey(s)
		case stepClear:
			_, _ = os.Stdout.WriteString("\x1b[2J\x1b[H")
		case stepJump:
			i = findLabel(steps, st.name)
			continue
		case stepLabel:
		}
		if err == nil {
			continue
		}
		var jump jumpError
		if !errors.As(err, &jump) {
			return quitStatus(err)
		}
		to := jump.to
		if to.kind == actCall {
			to, err = sh.hand.call(to.on)
			if err != nil {
				fmt.Fprintln(os.Stderr, "hanabi:", err)
				return 1, true
			}
		}
		switch to.kind {
		case actExit:
			return 0, true
		case actGoto:
			at := findLabel(steps, to.label)
			if at < 0 {
				fmt.Fprintf(os.Stderr, "hanabi: nothing is labelled %q\n", to.label)
				return 1, true
			}
			i = at
		default:
			// actNext and actAdvance both carry on: the loop increment is the
			// next step, which is what a handler returning (none) asks for.
		}
	}
	return 0, false
}

// quitStatus folds an interruption into the pair playSteps reports: quitting is
// a clean end, anything else keeps its exit code.
func quitStatus(err error) (status int, quit bool) {
	if errors.Is(err, errQuit) {
		return 0, true
	}
	return exitFor(err), true
}

func playShot(s *session, st showStep, seed uint64, speed float64) (status int, quit bool, jump error) {
	cols, rows := terminalSize()
	w := min(st.target.W, cols)
	h := min(st.target.H, rows)
	if w < st.target.W || h < st.target.H {
		fmt.Fprintf(os.Stderr, "hanabi: a shot is %dx%d but the terminal is %dx%d; the rest is cut\n",
			st.target.W, st.target.H, cols, rows)
	}

	r := canvas.NewRenderer(os.Stdout, w, h)
	err := r.Begin()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hanabi:", err)
		return 1, true, nil
	}
	p := &play{
		r:        r,
		dst:      canvas.New(w, h),
		target:   st.target,
		ticker:   s.ticker,
		winch:    s.winch,
		keys:     s.keys,
		reserved: h,
		maxRun:   maxRun,
	}
	err = p.once(s.ctx, effect.Scaled(effect.NewChain(st.entries, st.target, seed), speed))
	var moved jumpError
	switch {
	case errors.Is(err, errQuit):
		p.finish()
		err = nil
		quit = true
	case errors.As(err, &moved):
		// Leaving early, but leaving the text whole: the reader asked to move
		// on, not to see a half-drawn screen.
		p.finish()
		err = nil
		jump = moved
	}
	endErr := r.End()
	switch {
	case err != nil:
		return exitFor(err), true, nil
	case endErr != nil:
		fmt.Fprintln(os.Stderr, "hanabi:", endErr)
		return 1, true, nil
	}
	return 0, quit, jump
}

func holdFor(s *session, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case k := <-s.keys:
			err := s.keymap.stopErr(k)
			if err != nil {
				return err
			}
		case <-timer.C:
			return nil
		}
	}
}

// holdKey waits for the reader.
//
// Keys still in flight from the animation are dropped first -- a stray escape
// sequence arrives as several bytes, and acting on the stale ones would skip
// screens nobody asked to skip. Only advances are dropped, though: q and Ctrl-C
// are decisions, and throwing one away either hangs the show or lets it surface
// later and close a screen the reader was still looking at.
func holdKey(s *session) error {
	for {
		select {
		case k := <-s.keys:
			err := s.keymap.stopErr(k)
			if err != nil {
				return err
			}
			continue
		default:
		}
		break
	}
	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case k := <-s.keys:
			err := s.keymap.stopErr(k)
			if err != nil {
				return err
			}
			return nil
		}
	}
}

// showText is the pipe fallback: with no terminal on stdout the show cannot
// play, so its texts pass through in order, which is the data the show carries.
func showText(steps []showStep) string {
	var b strings.Builder
	for _, st := range steps {
		if st.kind != stepShot {
			continue
		}
		for y := range st.target.H {
			for x := range st.target.W {
				b.WriteRune(st.target.At(x, y).R)
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}
