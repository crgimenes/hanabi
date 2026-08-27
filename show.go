package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/crgimenes/filo"
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
)

type showStep struct {
	kind    showStepKind
	entries []effect.Entry
	target  *canvas.Canvas
	hold    time.Duration
	// speed is the shot's own pace on top of the command line's: the script
	// says how the show reads, the flag scales the whole sitting.
	speed float64
}

// parseShow evaluates the script and returns the steps it recorded. dir is the
// script's own directory: paths in the script resolve against it, so a show can
// sit next to its art and run from anywhere.
func parseShow(src, dir string) ([]showStep, error) {
	steps := make([]showStep, 0, 16)
	record := func(st showStep) (filo.Value, error) {
		if len(steps) >= maxShowSteps {
			return filo.VBool(false), fmt.Errorf("more than %d steps; this is a loop, not a show", maxShowSteps)
		}
		steps = append(steps, st)
		return filo.VBool(true), nil
	}

	eng := filo.NewEngine()
	err := errors.Join(
		eng.RegisterBuiltin("shot", shotBuiltin(record)),
		eng.RegisterBuiltin("file", fileBuiltin(dir)),
		eng.RegisterBuiltin("pause", pauseBuiltin(record)),
		eng.RegisterBuiltin("wait-key", markerBuiltin("wait-key", stepWaitKey, record)),
		eng.RegisterBuiltin("clear", markerBuiltin("clear", stepClear, record)),
	)
	if err != nil {
		return nil, err
	}

	_, _, err = eng.RunScript(context.Background(), src, nil, filo.EvalConfig{
		StepLimit:      showStepLimit,
		RecursionLimit: showRecursion,
		Timeout:        showTimeout,
	})
	if err != nil {
		return nil, err
	}
	if len(steps) == 0 {
		return nil, errors.New("the script recorded no steps; a show needs at least one shot")
	}
	return steps, nil
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
func runShow(steps []showStep, o opts) int {
	return withSession(o.fps, func(s *session) int {
		seed := o.seed
		for {
			status, quit := playSteps(s, steps, seed, o)
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

func playSteps(s *session, steps []showStep, seed uint64, o opts) (status int, quit bool) {
	for i, st := range steps {
		switch st.kind {
		case stepShot:
			// #nosec G115 -- a step index is never negative. The multiplier keeps
			// shot seeds apart from the pass-advance, which only adds one.
			status, quit = playShot(s, st, seed^(uint64(i+1)*0x9e3779b97f4a7c15), st.speed*o.speed)
			if quit || status != 0 {
				return status, quit
			}
		case stepPause:
			err := holdFor(s, st.hold)
			if err != nil {
				return quitStatus(err)
			}
		case stepWaitKey:
			err := holdKey(s)
			if err != nil {
				return quitStatus(err)
			}
		case stepClear:
			_, _ = os.Stdout.WriteString("\x1b[2J\x1b[H")
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

func playShot(s *session, st showStep, seed uint64, speed float64) (status int, quit bool) {
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
		return 1, true
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
	if errors.Is(err, errQuit) {
		p.finish()
		err = nil
		quit = true
	}
	endErr := r.End()
	switch {
	case err != nil:
		return exitFor(err), true
	case endErr != nil:
		fmt.Fprintln(os.Stderr, "hanabi:", endErr)
		return 1, true
	}
	return 0, quit
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
			err := stopErr(k)
			if err != nil {
				return err
			}
		case <-timer.C:
			return nil
		}
	}
}

// holdKey waits for the reader. Keys already in flight are drained first: a
// stray escape sequence pressed during the animation arrives as several bytes,
// and acting on the stale ones would skip screens nobody asked to skip.
func holdKey(s *session) error {
	for {
		select {
		case <-s.keys:
		default:
			goto wait
		}
	}
wait:
	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case k := <-s.keys:
			err := stopErr(k)
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
