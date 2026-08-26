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
	"syscall"
	"time"

	"github.com/crgimenes/hanabi/canvas"
	"github.com/crgimenes/hanabi/effect"
	"golang.org/x/term"
)

var Version = "dev"

// A run is bounded so a misbehaving effect cannot hold the terminal forever.
// yagni: fixed ceiling, fine while every effect ends on its own; attract mode
// will have to lift it.
const maxRun = 5 * time.Minute

const maxSamples = 4096

func main() {
	os.Exit(run())
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `usage: hanabi [flags] <effect> [file]

Animate text in the terminal. Reads the file, or standard input when no file
is given or the file is "-". The finished text is left on screen.

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

	entry, ok := effect.Get(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "hanabi: unknown effect %q\nrun 'hanabi -list' to see the available effects\n", args[0])
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

	return animate(entry, target, *fps, *seed, *debug)
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

func animate(entry effect.Entry, target *canvas.Canvas, fps int, seed uint64, debug bool) int {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols < 1 || rows < 1 {
		// Terminals that report no size are common enough (some pty wrappers,
		// CI runners) that refusing to run would cost more than the assumption.
		cols, rows = 80, 24
	}
	w := min(target.W, cols)
	h := min(target.H, rows)
	if w < target.W || h < target.H {
		fmt.Fprintf(os.Stderr, "hanabi: text is %dx%d but the terminal is %dx%d; the rest is cut\n",
			target.W, target.H, cols, rows)
	}

	// #nosec G404 -- the animation is seeded so a given -seed replays
	// frame for frame; unpredictability would be a defect here.
	rnd := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	ef := entry.New(target, rnd)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dst := canvas.New(w, h)
	r := canvas.NewRenderer(os.Stdout, w, h)
	err = r.Begin()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hanabi:", err)
		return 1
	}
	defer func() {
		_ = r.End()
	}()

	ticker := time.NewTicker(time.Second / time.Duration(fps))
	defer ticker.Stop()

	samples := make([]time.Duration, 0, 256)
	start := time.Now()
	frames := 0
	status := 0

	for {
		elapsed := time.Since(start)
		frameStart := time.Now()
		more := ef.Frame(dst, elapsed)
		err = r.Draw(dst)
		if len(samples) < maxSamples {
			samples = append(samples, time.Since(frameStart))
		}
		frames++
		if err != nil {
			fmt.Fprintln(os.Stderr, "hanabi:", err)
			status = 1
			break
		}
		if !more || elapsed > maxRun {
			break
		}

		select {
		case <-ctx.Done():
			status = 130
		case <-ticker.C:
			continue
		}
		break
	}

	if debug {
		reportDebug(os.Stderr, entry.Name, frames, time.Since(start), r.Bytes, samples)
	}
	return status
}

func reportDebug(w io.Writer, name string, frames int, wall time.Duration, bytes int64, samples []time.Duration) {
	slices.Sort(samples)
	perFrame := int64(0)
	if frames > 0 {
		perFrame = bytes / int64(frames)
	}
	_, _ = fmt.Fprintf(w,
		"effect=%s frames=%d wall=%dms bytes=%d bytes_per_frame=%d build_p50=%s build_p95=%s build_max=%s\n",
		name, frames, wall.Milliseconds(), bytes, perFrame,
		percentile(samples, 50), percentile(samples, 95), percentile(samples, 100))
}

func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := (len(sorted) - 1) * p / 100
	return sorted[i]
}
