package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/crgimenes/hanabi/canvas"
	"github.com/crgimenes/hanabi/effect"
	"golang.org/x/term"
)

var Version = "dev"

// Guards a single effect run, in loop mode too: an effect that never reports
// itself finished would otherwise hold the terminal with no way out but a
// signal. Reaching it is not a failure -- the run ends the way q ends it, with
// the finished text on screen. The loop itself is unbounded on purpose, and
// ends on the interrupt.
//
// It is a real ceiling, not a theoretical one: typing runs at human speed, so a
// thousand characters take over two minutes.
const maxRun = 5 * time.Minute

const maxSamples = 4096

func main() {
	os.Exit(run())
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `usage: hanabi [flags] <effect[,effect...]> [file]
       hanabi [flags] <show.filo>

Animate text in the terminal. Reads the file, or standard input when no file
is given or the file is "-". The finished text is left on screen. Naming more
than one effect layers them: they run together, on the same frames.

An argument ending in .filo is a show script: a Filo program that records a
sequence of steps, played in order. Its builtins are (shot "effects" text),
(file "path"), (pause seconds), (wait-key) and (clear); paths resolve against
the script's own directory. During (wait-key) any key advances; q still quits
and Ctrl-C still aborts.

Press q to jump to the finished text and exit; Ctrl-C aborts where it is.

  -loop          replay until interrupted
  -dwell dur     hold the finished text this long between passes (default none)
  -list          list the available effects and exit
  -json          machine-readable output for -list
  -fps int       frames per second (default 60)
  -seed uint     seed for effects that use randomness (default 1)
  -debug         write timing and byte counts to stderr when the run ends
  -version       print the version and exit
  -h, --help     print this help and exit

Output is the animation on standard output; diagnostics go to standard error.
When standard output is not a terminal the text is printed unchanged, so the
command is safe in a pipe.

  figlet hanabi | hanabi decrypt
  hanabi -loop wipe,decrypt logo.txt
  hanabi -loop "$(hanabi -list | cut -d';' -f1 | paste -sd, -)" logo.txt
  hanabi demo.filo
`)
}

func run() int {
	fs := flag.NewFlagSet("hanabi", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := bindFlags(fs)

	err := fs.Parse(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		usage(os.Stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hanabi:", err)
		return 2
	}

	if o.showVersion {
		fmt.Println(Version)
		return 0
	}
	if o.list {
		printList(os.Stdout, o.asJSON)
		return 0
	}
	if o.fps < 1 || o.fps > 240 {
		fmt.Fprintln(os.Stderr, "hanabi: -fps must be between 1 and 240")
		return 2
	}

	args := fs.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "hanabi: no effect given")
		usage(os.Stderr)
		return 2
	}

	if strings.HasSuffix(args[0], ".filo") {
		if len(args) > 1 {
			fmt.Fprintln(os.Stderr, "hanabi: a show script takes no further arguments")
			return 2
		}
		return runShowFile(args[0], o)
	}

	entries, err := parseEffects(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "hanabi:", err)
		fmt.Fprintln(os.Stderr, "run 'hanabi -list' to see the available effects")
		return 2
	}
	if o.dwell < 0 {
		fmt.Fprintln(os.Stderr, "hanabi: -dwell cannot be negative")
		return 2
	}

	text, err := readInput(args[1:])
	if errors.Is(err, errNoInput) {
		fmt.Fprintln(os.Stderr, "hanabi:", err)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hanabi:", err)
		return 1
	}

	target := canvas.FromText(text, canvas.Default)
	if target.H == 0 || target.W == 0 {
		return 0
	}

	// Not a terminal: the escape sequences would be noise in the consumer's
	// input, so the text passes through untouched.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Print(text)
		return 0
	}

	return animate(entries, target, *o)
}

// runShowFile reads and evaluates the script, then either plays it or, off a
// terminal, passes its texts through the way plain input does.
func runShowFile(path string, o *opts) int {
	b, err := os.ReadFile(path) // #nosec G304 -- the path is the user's own argument.
	if err != nil {
		fmt.Fprintln(os.Stderr, "hanabi:", err)
		return 1
	}
	if !utf8.Valid(b) {
		fmt.Fprintf(os.Stderr, "hanabi: %s is not valid UTF-8\n", path)
		return 1
	}
	steps, err := parseShow(string(b), filepath.Dir(path))
	if err != nil {
		fmt.Fprintf(os.Stderr, "hanabi: %s: %v\n", path, err)
		return 1
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Print(showText(steps))
		return 0
	}
	return runShow(steps, *o)
}

// bindFlags keeps the flag surface in one place so a test can walk it and check
// that the hand-written usage text still documents every one of them.
func bindFlags(fs *flag.FlagSet) *opts {
	o := &opts{}
	fs.BoolVar(&o.list, "list", false, "")
	fs.BoolVar(&o.asJSON, "json", false, "")
	fs.IntVar(&o.fps, "fps", 60, "")
	fs.Uint64Var(&o.seed, "seed", 1, "")
	fs.BoolVar(&o.debug, "debug", false, "")
	fs.BoolVar(&o.loop, "loop", false, "")
	fs.DurationVar(&o.dwell, "dwell", 0, "")
	fs.BoolVar(&o.showVersion, "version", false, "")
	return o
}

func printList(w io.Writer, asJSON bool) {
	entries := effect.List()
	if asJSON {
		type item struct {
			Name string `json:"name"`
			Desc string `json:"description"`
		}
		out := make([]item, 0, len(entries))
		for _, e := range entries {
			out = append(out, item{Name: e.Name, Desc: e.Desc})
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	for _, e := range entries {
		_, _ = fmt.Fprintf(w, "%s;%s\n", e.Name, e.Desc)
	}
}

// UTF-8 is the only encoding read, by decision rather than by omission. Old
// .ans art is usually CP437, and decoding it silently to replacement runes
// would put mojibake on screen with nothing to explain it, so it is refused
// with the command that fixes it.
func readInput(args []string) (string, error) {
	b, source, err := readSource(args)
	if err != nil {
		return "", err
	}
	if utf8.Valid(b) {
		return string(b), nil
	}
	name := source
	if len(args) == 0 || args[0] == "-" {
		name = "file.ans"
	}
	return "", fmt.Errorf("%s is not valid UTF-8; old .ans art is usually CP437:\n"+
		"  iconv -f CP437 -t UTF-8 %s | hanabi <effect>", source, name)
}

// errNoInput is a usage mistake, not a failure: the file was forgotten.
var errNoInput = errors.New(`no input: name a file, or pipe text in
  hanabi wipe art.ans
  cat art.ans | hanabi wipe
  hanabi wipe -        (to type it, ending with Ctrl-D)`)

func readSource(args []string) (b []byte, source string, err error) {
	if len(args) == 0 || args[0] == "-" {
		// A terminal on standard input with no file named means the argument
		// was forgotten. Without this the program sits mute until the reader
		// works out that it is waiting for Ctrl-D. Asking for "-" explicitly is
		// taken at its word, the way cat does.
		if len(args) == 0 && term.IsTerminal(int(os.Stdin.Fd())) {
			return nil, "", errNoInput
		}
		b, err = io.ReadAll(os.Stdin)
		if err != nil {
			return nil, "", fmt.Errorf("reading standard input: %w", err)
		}
		return b, "standard input", nil
	}
	b, err = os.ReadFile(args[0])
	if err != nil {
		return nil, "", err
	}
	return b, args[0], nil
}

type opts struct {
	list        bool
	asJSON      bool
	debug       bool
	loop        bool
	showVersion bool
	fps         int
	seed        uint64
	dwell       time.Duration
}

// errQuit is the reader asking to stop and keep the text, not a failure.
var errQuit = errors.New("quit")

type key int

const (
	keyQuit key = iota
	keyInterrupt
	// Any other key. Animations ignore it; (wait-key) in a show consumes it.
	keyAdvance
)

// watchKeys puts the controlling terminal in raw mode and reports the keys the
// animation reacts to. With no terminal to read from it returns a nil channel,
// which a select never fires on, so the caller needs no special case.
func watchKeys() (keys <-chan key, stop func()) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return nil, func() {}
	}
	fd := int(tty.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		_ = tty.Close()
		return nil, func() {}
	}
	ch := make(chan key, 1)
	go readKeys(tty, ch)
	return ch, func() {
		_ = term.Restore(fd, state)
		_ = tty.Close()
	}
}

func readKeys(tty *os.File, keys chan key) {
	buf := make([]byte, 16)
	for {
		n, err := tty.Read(buf)
		if err != nil {
			return
		}
		for _, b := range buf[:n] {
			k, ok := keyFor(b)
			if !ok {
				continue
			}
			select {
			case keys <- k:
				continue
			default:
			}
			// The channel holds one key. An ordinary key can be dropped on the
			// floor, but q and Ctrl-C must displace whatever is sitting there:
			// mashing keys and then pressing q has to quit.
			if k == keyAdvance {
				continue
			}
			select {
			case <-keys:
			default:
			}
			select {
			case keys <- k:
			default:
			}
		}
	}
}

func keyFor(b byte) (key, bool) {
	switch b {
	case 'q', 'Q':
		return keyQuit, true
	case 0x03:
		// Raw mode stops the driver turning this into SIGINT, so Ctrl-C has to
		// be recognised here or it would do nothing at all.
		return keyInterrupt, true
	}
	return keyAdvance, true
}

// stopErr says what a key means to whatever is in progress: nil is "not for
// you, carry on".
func stopErr(k key) error {
	switch k {
	case keyQuit:
		return errQuit
	case keyInterrupt:
		return context.Canceled
	}
	return nil
}

// play carries the state one effect run needs and the counters that outlive it.
type play struct {
	r        *canvas.Renderer
	dst      *canvas.Canvas
	target   *canvas.Canvas
	ticker   *time.Ticker
	winch    <-chan os.Signal
	keys     <-chan key
	reserved int
	// Injectable so a test can reach the guard without waiting for it.
	maxRun time.Duration

	frames  int
	samples []time.Duration
}

func parseEffects(arg string) ([]effect.Entry, error) {
	names := strings.Split(arg, ",")
	entries := make([]effect.Entry, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		e, ok := effect.Get(name)
		if !ok {
			return nil, fmt.Errorf("unknown effect %q", name)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// session is the terminal wiring shared by however much runs in one process:
// one signal context, one resize channel, one key reader, one frame ticker.
type session struct {
	ctx    context.Context
	keys   <-chan key
	winch  <-chan os.Signal
	ticker *time.Ticker
}

func withSession(fps int, fn func(s *session) int) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	keys, stopKeys := watchKeys()
	defer stopKeys()

	ticker := time.NewTicker(time.Second / time.Duration(fps))
	defer ticker.Stop()

	return fn(&session{ctx: ctx, keys: keys, winch: winch, ticker: ticker})
}

func animate(entries []effect.Entry, target *canvas.Canvas, o opts) int {
	cols, rows := terminalSize()
	w := min(target.W, cols)
	h := min(target.H, rows)
	if w < target.W || h < target.H {
		fmt.Fprintf(os.Stderr, "hanabi: text is %dx%d but the terminal is %dx%d; the rest is cut\n",
			target.W, target.H, cols, rows)
	}

	return withSession(o.fps, func(s *session) int {
		r := canvas.NewRenderer(os.Stdout, w, h)
		err := r.Begin()
		if err != nil {
			fmt.Fprintln(os.Stderr, "hanabi:", err)
			return 1
		}
		defer func() {
			_ = r.End()
		}()

		p := &play{
			r:        r,
			dst:      canvas.New(w, h),
			target:   target,
			ticker:   s.ticker,
			winch:    s.winch,
			keys:     s.keys,
			reserved: h,
			maxRun:   maxRun,
			samples:  make([]time.Duration, 0, 256),
		}

		start := time.Now()
		status := p.show(s.ctx, entries, o)

		if o.debug {
			reportDebug(os.Stderr, entries, p.frames, time.Since(start), r.Bytes, p.samples)
		}
		return status
	})
}

// show plays the pass loop and reports the exit status. It is separate from
// animate because this is the part that decides anything -- animate around it
// only wires up the terminal, and the two want testing on very different terms.
func (p *play) show(ctx context.Context, entries []effect.Entry, o opts) int {
	seed := o.seed
	for {
		err := p.once(ctx, effect.NewChain(entries, p.target, seed))
		if err == nil && o.loop {
			err = p.pause(ctx, o.dwell)
		}
		switch {
		case errors.Is(err, errQuit):
			p.finish()
		case err != nil:
			return exitFor(err)
		case o.loop:
			seed++
			continue
		}
		return 0
	}
}

func exitFor(err error) int {
	if errors.Is(err, context.Canceled) {
		return 130
	}
	fmt.Fprintln(os.Stderr, "hanabi:", err)
	return 1
}

// once plays one pass to completion, or until the context is cancelled.
func (p *play) once(ctx context.Context, ef effect.Effect) error {
	start := time.Now()
	for {
		p.refit()

		elapsed := time.Since(start)
		frameStart := time.Now()
		// Each frame is rebuilt from the finished text and handed to the chain,
		// so an effect transforms what the one before it produced.
		p.dst.CopyFrom(p.target)
		more := ef.Frame(p.dst, elapsed)
		err := p.r.Draw(p.dst)
		p.frames++
		if len(p.samples) < maxSamples {
			p.samples = append(p.samples, time.Since(frameStart))
		}
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
		// Cut short by the guard rather than finished: stop like q does, so the
		// reader is left with the whole text instead of a half-drawn frame.
		if elapsed > p.maxRun {
			return errQuit
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case k := <-p.keys:
			err = stopErr(k)
			if err != nil {
				return err
			}
		case <-p.ticker.C:
		}
	}
}

// finish jumps straight to the last frame, so quitting part-way through leaves
// the whole text on screen instead of a half-drawn one.
func (p *play) finish() {
	p.dst.CopyFrom(p.target)
	_ = p.r.Draw(p.dst)
}

// refit reacts to a terminal resize. The region was scrolled into view once, at
// Begin: it can shrink with the terminal, but it must never grow past what was
// reserved, because the rows below it belong to whatever comes after.
func (p *play) refit() {
	select {
	case <-p.winch:
	default:
		return
	}
	cols, rows := terminalSize()
	p.dst.Resize(min(p.target.W, cols), min(p.target.H, rows, p.reserved))
	p.r.Full()
}

func (p *play) pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case k := <-p.keys:
			err := stopErr(k)
			if err != nil {
				return err
			}
		case <-timer.C:
			return nil
		}
	}
}

func terminalSize() (cols, rows int) {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols < 1 || rows < 1 {
		// Terminals that report no size are common enough (some pty wrappers,
		// CI runners) that refusing to run would cost more than the assumption.
		return 80, 24
	}
	return cols, rows
}

func reportDebug(w io.Writer, entries []effect.Entry, frames int, wall time.Duration, bytes int64, samples []time.Duration) {
	slices.Sort(samples)
	perFrame := int64(0)
	if frames > 0 {
		perFrame = bytes / int64(frames)
	}
	_, _ = fmt.Fprintf(w,
		"effect=%s frames=%d wall=%dms bytes=%d bytes_per_frame=%d build_p50=%s build_p95=%s build_max=%s\n",
		effectNames(entries), frames, wall.Milliseconds(), bytes, perFrame,
		percentile(samples, 50), percentile(samples, 95), percentile(samples, 100))
}

func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := (len(sorted) - 1) * p / 100
	return sorted[i]
}

func effectNames(entries []effect.Entry) string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return strings.Join(names, ",")
}
