package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crgimenes/hanabi/canvas"
	"github.com/crgimenes/hanabi/effect"
)

// The usage text is written by hand while the flags are declared separately, so
// the two drift apart the first time someone adds a flag in a hurry.
func TestUsageDocumentsEveryFlag(t *testing.T) {
	fs := flag.NewFlagSet("hanabi", flag.ContinueOnError)
	bindFlags(fs)

	var out strings.Builder
	usage(&out)
	text := out.String()

	fs.VisitAll(func(f *flag.Flag) {
		if strings.Contains(text, "-"+f.Name) {
			return
		}
		t.Errorf("flag -%s is not mentioned in the usage text", f.Name)
	})
}

func TestUsageMentionsTheKeysItAcceptsAndTheDefaults(t *testing.T) {
	var out strings.Builder
	usage(&out)
	text := out.String()
	for _, want := range []string{"q ", "Ctrl-C", "standard input", "standard error"} {
		if strings.Contains(text, want) {
			continue
		}
		t.Errorf("usage does not mention %q", want)
	}
}

func TestParseEffects(t *testing.T) {
	tests := []struct {
		name  string
		arg   string
		want  []string
		fails bool
	}{
		{name: "single", arg: "wipe", want: []string{"wipe"}},
		{name: "chain", arg: "wipe,decrypt", want: []string{"wipe", "decrypt"}},
		{name: "order is kept", arg: "decrypt,wipe", want: []string{"decrypt", "wipe"}},
		{name: "spaces around names", arg: " wipe , burn ", want: []string{"wipe", "burn"}},
		{name: "unknown name", arg: "wipe,nosuch", fails: true},
		{name: "empty", arg: "", fails: true},
		{name: "trailing comma", arg: "wipe,", fails: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEffects(tt.arg)
			if tt.fails {
				if err == nil {
					t.Fatalf("parseEffects(%q) succeeded, want an error", tt.arg)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEffects(%q): %v", tt.arg, err)
			}
			names := make([]string, 0, len(got))
			for _, e := range got {
				names = append(names, e.Name)
			}
			if strings.Join(names, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("got %v, want %v", names, tt.want)
			}
		})
	}
}

func TestParseEffectsNamesTheOffendingEffect(t *testing.T) {
	_, err := parseEffects("wipe,nosuch")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "nosuch") {
		t.Fatalf("error does not name the bad effect: %v", err)
	}
}

// The refusal has to be actionable: an encoding error nobody can act on is the
// same as mojibake, only slower.
func TestReadInputRefusesNonUTF8WithTheFixCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cp437.ans")
	// 0xDC and 0xDF are the half blocks in CP437 and invalid on their own in UTF-8.
	err := os.WriteFile(path, []byte{0x1b, '[', '1', 'm', 0xDC, 0xDF, '\n'}, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	_, err = readInput([]string{path})
	if err == nil {
		t.Fatal("read a CP437 file without complaining")
	}
	for _, want := range []string{path, "UTF-8", "iconv -f CP437 -t UTF-8"} {
		if strings.Contains(err.Error(), want) {
			continue
		}
		t.Errorf("error message is missing %q: %v", want, err)
	}
}

func TestReadInputReadsAFileAndReportsAMissingOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "art.txt")
	err := os.WriteFile(path, []byte("hello\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	got, err := readInput([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello\n" {
		t.Fatalf("got %q", got)
	}

	_, err = readInput([]string{filepath.Join(t.TempDir(), "absent")})
	if err == nil {
		t.Fatal("a missing file was read without an error")
	}
}

// Reading standard input when no file is named is deliberate: it is what makes
// `cat art.ans | hanabi burn` work.
func TestReadInputFallsBackToStandardInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piped")
	err := os.WriteFile(path, []byte("piped text\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	saved := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = saved })

	for _, args := range [][]string{nil, {"-"}} {
		_, err = f.Seek(0, 0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := readInput(args)
		if err != nil {
			t.Fatalf("args %v: %v", args, err)
		}
		if got != "piped text\n" {
			t.Fatalf("args %v: got %q", args, got)
		}
	}
}

func TestKeyFor(t *testing.T) {
	tests := []struct {
		in   byte
		want key
		ok   bool
	}{
		{in: 'q', want: keyQuit, ok: true},
		{in: 'Q', want: keyQuit, ok: true},
		{in: 0x03, want: keyInterrupt, ok: true},
		{in: 'a', ok: false},
		{in: 0x1b, ok: false},
	}
	for _, tt := range tests {
		got, ok := keyFor(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("keyFor(%#x) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// q leaves the finished text and reports success; Ctrl-C is an abort and must
// not be mistaken for one.
func TestErrForSeparatesQuittingFromInterrupting(t *testing.T) {
	if got := exitFor(errFor(keyInterrupt)); got != 130 {
		t.Errorf("interrupt exits %d, want 130", got)
	}
	if !strings.Contains(errFor(keyQuit).Error(), "quit") {
		t.Errorf("quit does not report itself as a quit")
	}
	if got := exitFor(context.Canceled); got != 130 {
		t.Errorf("cancellation exits %d, want 130", got)
	}
}

func TestPercentile(t *testing.T) {
	sorted := []time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	tests := []struct {
		p    int
		want time.Duration
	}{{p: 0, want: 1}, {p: 50, want: 5}, {p: 95, want: 9}, {p: 100, want: 10}}
	for _, tt := range tests {
		if got := percentile(sorted, tt.p); got != tt.want {
			t.Errorf("percentile(p%d) = %v, want %v", tt.p, got, tt.want)
		}
	}
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile of nothing = %v, want 0", got)
	}
	if got := percentile([]time.Duration{7}, 95); got != 7 {
		t.Errorf("percentile of one sample = %v, want 7", got)
	}
}

// Scripts read the list; a stray banner or a changed separator breaks them
// silently, which is exactly what the structured mode exists to prevent.
func TestPrintListIsMachineReadable(t *testing.T) {
	var text strings.Builder
	printList(&text, false)
	lines := strings.Split(strings.TrimSuffix(text.String(), "\n"), "\n")
	if len(lines) != len(effect.List()) {
		t.Fatalf("got %d lines for %d effects", len(lines), len(effect.List()))
	}
	for _, line := range lines {
		if strings.Count(line, ";") >= 1 {
			continue
		}
		t.Fatalf("line %q is not name;description", line)
	}

	var raw strings.Builder
	printList(&raw, true)
	var items []struct {
		Name string `json:"name"`
		Desc string `json:"description"`
	}
	err := json.Unmarshal([]byte(raw.String()), &items)
	if err != nil {
		t.Fatalf("json mode is not valid JSON: %v", err)
	}
	if len(items) != len(effect.List()) {
		t.Fatalf("json listed %d effects, registry has %d", len(items), len(effect.List()))
	}
	for _, item := range items {
		if item.Name != "" && item.Desc != "" {
			continue
		}
		t.Fatalf("json entry is incomplete: %+v", item)
	}
}

func TestEffectNames(t *testing.T) {
	entries, err := parseEffects("wipe,burn")
	if err != nil {
		t.Fatal(err)
	}
	if got := effectNames(entries); got != "wipe,burn" {
		t.Fatalf("got %q, want %q", got, "wipe,burn")
	}
	if got := effectNames(nil); got != "" {
		t.Fatalf("got %q for no effects", got)
	}
}

// Tests run without a terminal, so this exercises the fallback rather than the
// ioctl. A zero here would divide by zero and reserve a region of no rows.
func TestTerminalSizeAlwaysReturnsAUsableSize(t *testing.T) {
	cols, rows := terminalSize()
	if cols < 1 || rows < 1 {
		t.Fatalf("got %dx%d", cols, rows)
	}
}

func TestReportDebugCarriesTheNumbersWorthReading(t *testing.T) {
	entries, err := parseEffects("wipe,burn")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	reportDebug(&out, entries, 10, 2*time.Second, 4000, []time.Duration{1, 2, 3})
	line := out.String()
	for _, want := range []string{
		"effect=wipe,burn", "frames=10", "wall=2000ms", "bytes=4000",
		"bytes_per_frame=400", "build_p50=", "build_p95=", "build_max=",
	} {
		if strings.Contains(line, want) {
			continue
		}
		t.Errorf("debug line is missing %q: %s", want, line)
	}
}

func testPlay(out io.Writer, target *canvas.Canvas, keys <-chan key, winch <-chan os.Signal) *play {
	return &play{
		r:        canvas.NewRenderer(out, target.W, target.H),
		dst:      canvas.New(target.W, target.H),
		target:   target,
		ticker:   time.NewTicker(time.Millisecond),
		winch:    winch,
		keys:     keys,
		reserved: target.H,
		maxRun:   maxRun,
		samples:  make([]time.Duration, 0, 8),
	}
}

func testTarget() *canvas.Canvas {
	return canvas.FromText("hanabi\nzero deps\n", canvas.Default)
}

func testChain(t *testing.T, names string, target *canvas.Canvas) []effect.Effect {
	t.Helper()
	entries, err := parseEffects(names)
	if err != nil {
		t.Fatal(err)
	}
	rnd := rand.New(rand.NewPCG(1, 2))
	chain := make([]effect.Effect, 0, len(entries))
	for _, e := range entries {
		chain = append(chain, e.New(target, rnd))
	}
	return chain
}

func TestOnceRunsTheChainToCompletion(t *testing.T) {
	target := testTarget()
	var out bytes.Buffer
	p := testPlay(&out, target, nil, nil)
	defer p.ticker.Stop()

	err := p.once(context.Background(), testChain(t, "wipe", target))
	if err != nil {
		t.Fatalf("once: %v", err)
	}
	if p.frames < 2 {
		t.Fatalf("ran %d frames", p.frames)
	}
	if len(p.samples) != p.frames {
		t.Fatalf("recorded %d samples for %d frames", len(p.samples), p.frames)
	}
	if out.Len() == 0 {
		t.Fatal("drew nothing")
	}
}

func TestOnceStopsOnQAndOnInterrupt(t *testing.T) {
	tests := []struct {
		name string
		sent key
		want error
	}{
		{name: "q", sent: keyQuit, want: errQuit},
		{name: "ctrl-c", sent: keyInterrupt, want: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := testTarget()
			keys := make(chan key, 1)
			keys <- tt.sent
			p := testPlay(io.Discard, target, keys, nil)
			defer p.ticker.Stop()

			// decrypt runs well over a second, so anything that returns quickly
			// returned because of the key.
			err := p.once(context.Background(), testChain(t, "decrypt", target))
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestOnceStopsWhenTheContextIsCancelled(t *testing.T) {
	target := testTarget()
	p := testPlay(io.Discard, target, nil, nil)
	defer p.ticker.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.once(ctx, testChain(t, "decrypt", target))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

// This is what q buys: the run ends part-way through and the reader is still
// left looking at the whole text.
func TestFinishDrawsTheWholeText(t *testing.T) {
	target := testTarget()
	var out bytes.Buffer
	p := testPlay(&out, target, nil, nil)
	defer p.ticker.Stop()

	// Stop a scrambling effect early, then finish.
	chain := testChain(t, "decrypt", target)
	chain[0].Frame(p.dst, 0)
	err := p.r.Draw(p.dst)
	if err != nil {
		t.Fatal(err)
	}
	p.finish()

	for y := range target.H {
		for x := range target.W {
			if got, want := p.dst.At(x, y), target.At(x, y); got != want {
				t.Fatalf("cell (%d,%d) = %+v, want %+v", x, y, got, want)
			}
		}
	}
}

func TestRefitForcesAFullRepaintOnlyAfterAResize(t *testing.T) {
	target := testTarget()
	var out bytes.Buffer
	winch := make(chan os.Signal, 1)
	p := testPlay(&out, target, nil, winch)
	defer p.ticker.Stop()

	p.dst.CopyFrom(target)
	err := p.r.Draw(p.dst)
	if err != nil {
		t.Fatal(err)
	}

	// No signal pending: the same frame costs almost nothing.
	out.Reset()
	p.refit()
	err = p.r.Draw(p.dst)
	if err != nil {
		t.Fatal(err)
	}
	quiet := out.Len()

	winch <- syscall.SIGWINCH
	out.Reset()
	p.refit()
	p.dst.CopyFrom(target)
	err = p.r.Draw(p.dst)
	if err != nil {
		t.Fatal(err)
	}
	if out.Len() <= quiet {
		t.Fatalf("after a resize the frame cost %d bytes, an idle one costs %d; nothing was repainted", out.Len(), quiet)
	}
}

func TestPauseWaitsAndStaysInterruptible(t *testing.T) {
	target := testTarget()
	p := testPlay(io.Discard, target, nil, nil)
	defer p.ticker.Stop()

	start := time.Now()
	err := p.pause(context.Background(), 30*time.Millisecond)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond {
		t.Fatalf("pause returned after %v, want about 30ms", elapsed)
	}

	keys := make(chan key, 1)
	keys <- keyQuit
	p = testPlay(io.Discard, target, keys, nil)
	defer p.ticker.Stop()
	err = p.pause(context.Background(), time.Hour)
	if !errors.Is(err, errQuit) {
		t.Fatalf("a key during the pause gave %v, want errQuit", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p = testPlay(io.Discard, target, nil, nil)
	defer p.ticker.Stop()
	err = p.pause(ctx, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelling during the pause gave %v, want context.Canceled", err)
	}
}

func TestReadKeysTranslatesBytesAndDropsTheRest(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	keys := make(chan key, 1)
	go readKeys(r, keys)

	_, err = w.Write([]byte("abcq"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-keys:
		if got != keyQuit {
			t.Fatalf("got %v, want keyQuit", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no key arrived")
	}
	_ = w.Close()
}

// Exit status is part of the contract: 0 is success and everything else is a
// failure a script can act on.
func TestRunExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{name: "help", args: []string{"-h"}, want: 0},
		{name: "list", args: []string{"-list"}, want: 0},
		{name: "version", args: []string{"-version"}, want: 0},
		{name: "unknown flag", args: []string{"-nope"}, want: 2},
		{name: "no effect", args: nil, want: 2},
		{name: "unknown effect", args: []string{"nosuch"}, want: 2},
		{name: "fps out of range", args: []string{"-fps", "0", "wipe"}, want: 2},
		{name: "negative dwell", args: []string{"-dwell", "-1s", "wipe"}, want: 2},
		{name: "missing file", args: []string{"wipe", "/nonexistent/art.ans"}, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runWithArgs(t, tt.args)
			if got != tt.want {
				t.Fatalf("exit %d, want %d", got, tt.want)
			}
		})
	}
}

// runWithArgs drives run() with the process streams pointed at files, which is
// also what keeps the test output readable.
func runWithArgs(t *testing.T, args []string) int {
	t.Helper()
	dir := t.TempDir()
	swap := func(target **os.File, name string) {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		saved := *target
		*target = f
		t.Cleanup(func() {
			*target = saved
			_ = f.Close()
		})
	}
	swap(&os.Stdout, "stdout")
	swap(&os.Stderr, "stderr")
	// Empty input, so a run that gets this far finishes instead of blocking.
	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	savedIn := os.Stdin
	os.Stdin = in
	t.Cleanup(func() {
		os.Stdin = savedIn
		_ = in.Close()
	})

	savedArgs := os.Args
	os.Args = append([]string{"hanabi"}, args...)
	t.Cleanup(func() { os.Args = savedArgs })

	return run()
}

func TestShowPlaysOncePerPassAndLoopsOnlyWhenAsked(t *testing.T) {
	target := testTarget()
	entries, err := parseEffects("wipe")
	if err != nil {
		t.Fatal(err)
	}

	p := testPlay(io.Discard, target, nil, nil)
	defer p.ticker.Stop()
	if status := p.show(context.Background(), entries, opts{}); status != 0 {
		t.Fatalf("a single pass exited %d", status)
	}
	single := p.frames

	// Looping, stopped by q after the first pass would have finished.
	keys := make(chan key, 1)
	p = testPlay(io.Discard, target, keys, nil)
	defer p.ticker.Stop()
	go func() {
		time.Sleep(900 * time.Millisecond)
		keys <- keyQuit
	}()
	if status := p.show(context.Background(), entries, opts{loop: true}); status != 0 {
		t.Fatalf("q exited %d, want 0", status)
	}
	if p.frames <= single {
		t.Fatalf("looping ran %d frames, a single pass ran %d; it did not loop", p.frames, single)
	}
}

// Note: seed advancement between passes is not asserted here. Observing it
// from outside means comparing drawn output between runs, and the number of
// frames a pass draws depends on the wall clock -- the test would be timing
// dependent, which is worse than no test. The effect package covers the half
// that matters deterministically: same seed, same animation.

func TestExitForReportsAnUnexpectedFailure(t *testing.T) {
	saved := os.Stderr
	f, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = f
	t.Cleanup(func() {
		os.Stderr = saved
		_ = f.Close()
	})
	if got := exitFor(errors.New("disk on fire")); got != 1 {
		t.Fatalf("a write failure exits %d, want 1", got)
	}
}

// Environment dependent by nature: it needs a real terminal on standard input,
// which no CI runner has. It never reads from the terminal -- the check returns
// before that -- so it cannot hang.
func TestReadInputRefusesATerminalWhenTheFileWasForgotten(t *testing.T) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		t.Skip("no controlling terminal here:", err)
	}
	defer func() { _ = tty.Close() }()

	saved := os.Stdin
	os.Stdin = tty
	t.Cleanup(func() { os.Stdin = saved })

	_, err = readInput(nil)
	if !errors.Is(err, errNoInput) {
		t.Fatalf("got %v, want errNoInput", err)
	}
	for _, want := range []string{"name a file", "cat art.ans |", "Ctrl-D"} {
		if strings.Contains(err.Error(), want) {
			continue
		}
		t.Errorf("the message does not teach the fix, missing %q: %v", want, err)
	}

	// The "-" case is deliberately not exercised here. With a terminal it
	// blocks until someone types, which is correct, and testing that needs a
	// goroutine racing the test's own lifetime. That path is covered against a
	// pipe in TestReadInputFallsBackToStandardInput.
}

// The guard exists to stop a runaway effect, not to leave a half-drawn screen
// behind. typing is the effect that can actually reach it: it runs at human
// speed, so a long file takes minutes.
func TestOnceCutShortByTheGuardStillLeavesTheWholeText(t *testing.T) {
	target := testTarget()
	p := testPlay(io.Discard, target, nil, nil)
	defer p.ticker.Stop()
	p.maxRun = 20 * time.Millisecond

	chain := testChain(t, "decrypt", target)
	err := p.once(context.Background(), []effect.Effect{forever{inner: chain[0]}})
	if !errors.Is(err, errQuit) {
		t.Fatalf("got %v, want errQuit so the caller draws the finished text", err)
	}
}

// forever never reports itself finished, which is the shape maxRun is there for.
type forever struct{ inner effect.Effect }

func (f forever) Frame(c *canvas.Canvas, t time.Duration) bool {
	f.inner.Frame(c, t)
	return true
}
