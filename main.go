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
is given or the file is "-". The finished text is left on screen.

  -loop          replay until interrupted, cycling through the named effects
  -dwell dur     how long the finished text is held in -loop (default 3s)
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
  hanabi -loop -dwell 10s decrypt,wipe logo.txt
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

func readInput(args []string) (string, error) {
	if len(args) == 0 || args[0] == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading standard input: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(args[0])
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type opts struct {
	fps   int
	seed  uint64
	debug bool
	loop  bool
	dwell time.Duration
}

// play carries the state one effect run needs and the counters that outlive it.
type play struct {
	r        *canvas.Renderer
	dst      *canvas.Canvas
	target   *canvas.Canvas
	ticker   *time.Ticker
	winch    <-chan os.Signal
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
		reserved: h,
		samples:  make([]time.Duration, 0, 256),
	}

	start := time.Now()
	status := 0
	seed := o.seed
	idx := 0

	for {
		// #nosec G404 -- the animation is seeded so a given -seed replays frame
		// for frame; unpredictability would be a defect here. The seed advances
		// per run so a loop does not repeat itself, and still replays as a whole.
		rnd := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
		err = p.once(ctx, entries[idx].New(target, rnd))
		if err != nil {
			status = exitFor(err)
			break
		}
		if !o.loop {
			break
		}
		err = pause(ctx, o.dwell)
		if err != nil {
			status = exitFor(err)
			break
		}
		seed++
		idx = (idx + 1) % len(entries)
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

// once plays a single effect to completion, or until the context is cancelled.
func (p *play) once(ctx context.Context, ef effect.Effect) error {
	start := time.Now()
	for {
		p.refit()

		elapsed := time.Since(start)
		frameStart := time.Now()
		more := ef.Frame(p.dst, elapsed)
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
		case <-p.ticker.C:
		}
	}
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

func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
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
