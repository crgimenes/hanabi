package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/signal"
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
// signal. The loop itself is unbounded on purpose, and ends on the interrupt.
const maxRun = 5 * time.Minute

const maxSamples = 4096

func main() {
	os.Exit(run())
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `usage: hanabi [flags] <effect[,effect...]> [file]

Animate text in the terminal. Reads the file, or standard input when no file
is given or the file is "-". The finished text is left on screen. Naming more
than one effect layers them: they run together, on the same frames.

Press q to jump to the finished text and exit; Ctrl-C aborts where it is.

  -loop          replay until interrupted
  -dwell dur     how long the finished text is held between passes (default 3s)
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
  hanabi -loop -dwell 10s wipe,decrypt logo.txt
  hanabi -loop "$(hanabi -list | cut -d';' -f1 | paste -sd, -)" logo.txt
`)
}

func run() int {
	fs := flag.NewFlagSet("hanabi", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	list := fs.Bool("list", false, "")
	asJSON := fs.Bool("json", false, "")
	fps := fs.Int("fps", 60, "")
	seed := fs.Uint64("seed", 1, "")
	debug := fs.Bool("debug", false, "")
	loop := fs.Bool("loop", false, "")
	dwell := fs.Duration("dwell", 3*time.Second, "")
	showVersion := fs.Bool("version", false, "")

	err := fs.Parse(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		usage(os.Stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hanabi:", err)
		return 2
	}

	if *showVersion {
		fmt.Println(Version)
		return 0
	}
	if *list {
		printList(*asJSON)
		return 0
	}
	if *fps < 1 || *fps > 240 {
		fmt.Fprintln(os.Stderr, "hanabi: -fps must be between 1 and 240")
		return 2
	}

	args := fs.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "hanabi: no effect given")
		usage(os.Stderr)
		return 2
	}

	entries, err := parseEffects(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "hanabi:", err)
		fmt.Fprintln(os.Stderr, "run 'hanabi -list' to see the available effects")
		return 2
	}
	if *dwell < 0 {
		fmt.Fprintln(os.Stderr, "hanabi: -dwell cannot be negative")
		return 2
	}

	text, err := readInput(args[1:])
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

	return animate(entries, target, opts{
		fps:   *fps,
		seed:  *seed,
		debug: *debug,
		loop:  *loop,
		dwell: *dwell,
	})
}

func printList(asJSON bool) {
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
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	for _, e := range entries {
		fmt.Printf("%s;%s\n", e.Name, e.Desc)
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

func readSource(args []string) (b []byte, source string, err error) {
	if len(args) == 0 || args[0] == "-" {
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
	fps   int
	seed  uint64
	debug bool
	loop  bool
	dwell time.Duration
}

// errQuit is the reader asking to stop and keep the text, not a failure.
var errQuit = errors.New("quit")

type key int

const (
	keyQuit key = iota
	keyInterrupt
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

func readKeys(tty *os.File, keys chan<- key) {
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
			// Dropping a keystroke while one is already queued is fine: both
			// mean stop, and the queued one is about to be acted on.
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
	return 0, false
}

func errFor(k key) error {
	if k == keyInterrupt {
		return context.Canceled
	}
	return errQuit
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

func animate(entries []effect.Entry, target *canvas.Canvas, o opts) int {
	cols, rows := terminalSize()
	w := min(target.W, cols)
	h := min(target.H, rows)
	if w < target.W || h < target.H {
		fmt.Fprintf(os.Stderr, "hanabi: text is %dx%d but the terminal is %dx%d; the rest is cut\n",
			target.W, target.H, cols, rows)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	keys, stopKeys := watchKeys()
	defer stopKeys()

	r := canvas.NewRenderer(os.Stdout, w, h)
	err := r.Begin()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hanabi:", err)
		return 1
	}
	defer func() {
		_ = r.End()
	}()

	ticker := time.NewTicker(time.Second / time.Duration(o.fps))
	defer ticker.Stop()

	p := &play{
		r:        r,
		dst:      canvas.New(w, h),
		target:   target,
		ticker:   ticker,
		winch:    winch,
		keys:     keys,
		reserved: h,
		samples:  make([]time.Duration, 0, 256),
	}

	start := time.Now()
	status := 0
	seed := o.seed

	for {
		// #nosec G404 -- the animation is seeded so a given -seed replays frame
		// for frame; unpredictability would be a defect here. The seed advances
		// per pass so a loop does not repeat itself, and still replays as a whole.
		rnd := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
		chain := make([]effect.Effect, 0, len(entries))
		for _, e := range entries {
			chain = append(chain, e.New(target, rnd))
		}
		err = p.once(ctx, chain)
		if err == nil && o.loop {
			err = p.pause(ctx, o.dwell)
		}
		switch {
		case errors.Is(err, errQuit):
			p.finish()
		case err != nil:
			status = exitFor(err)
		case o.loop:
			seed++
			continue
		}
		break
	}

	if o.debug {
		reportDebug(os.Stderr, entries, p.frames, time.Since(start), r.Bytes, p.samples)
	}
	return status
}

func exitFor(err error) int {
	if errors.Is(err, context.Canceled) {
		return 130
	}
	fmt.Fprintln(os.Stderr, "hanabi:", err)
	return 1
}

// once plays the chain to completion, or until the context is cancelled. Every
// effect runs on the same frame, over the same elapsed time, so naming several
// of them layers their work instead of queueing it.
func (p *play) once(ctx context.Context, chain []effect.Effect) error {
	start := time.Now()
	for {
		p.refit()

		elapsed := time.Since(start)
		frameStart := time.Now()
		// Each frame is rebuilt from the finished text and handed down the
		// chain, so an effect transforms what the one before it produced.
		p.dst.CopyFrom(p.target)
		more := false
		for _, ef := range chain {
			// Not short-circuited: every effect has to advance its own state.
			if ef.Frame(p.dst, elapsed) {
				more = true
			}
		}
		err := p.r.Draw(p.dst)
		p.frames++
		if len(p.samples) < maxSamples {
			p.samples = append(p.samples, time.Since(frameStart))
		}
		if err != nil {
			return err
		}
		if !more || elapsed > maxRun {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case k := <-p.keys:
			return errFor(k)
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
	select {
	case <-ctx.Done():
		return ctx.Err()
	case k := <-p.keys:
		return errFor(k)
	case <-timer.C:
		return nil
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
