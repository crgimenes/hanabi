package effect

import (
	"bytes"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const sample = "hanabi\nzero dependencies\nterminal text effects\n"

// Every effect exists to end with the text readable on screen. An effect that
// reports itself finished while still showing scrambled or hidden cells leaves
// the terminal wrong in a way no other test would catch. Chains are covered as
// well as single effects: layering is where an effect that ends correctly on
// its own can still be left mangled by the one after it.
func TestChainsEndOnTheTarget(t *testing.T) {
	for _, names := range chains() {
		t.Run(strings.Join(names, "+"), func(t *testing.T) {
			target := canvas.FromText(sample, canvas.Default)
			dst := canvas.New(target.W, target.H)
			chain := build(t, names, target, rand.New(rand.NewPCG(1, 2)))

			const step = 16 * time.Millisecond
			const limit = 30 * time.Second
			elapsed := time.Duration(0)
			frames := 0
			for {
				dst.CopyFrom(target)
				more := false
				for _, ef := range chain {
					if ef.Frame(dst, elapsed) {
						more = true
					}
				}
				frames++
				if !more {
					break
				}
				elapsed += step
				if elapsed > limit {
					t.Fatalf("still running after %s of simulated time", limit)
				}
			}
			if frames < 2 {
				t.Fatal("finished on the first frame; nothing would be animated")
			}
			for y := range target.H {
				for x := range target.W {
					want, got := target.At(x, y), dst.At(x, y)
					if want == got {
						continue
					}
					t.Fatalf("after %d frames cell (%d,%d) = %+v, want %+v", frames, x, y, got, want)
				}
			}
		})
	}
}

// A layered effect must not undo what an earlier one in the chain did. wipe
// hides everything the sweep has not reached, so nothing downstream of it may
// put ink back into that region -- that is the whole difference between
// layering the effects and merely running the last one.
func TestLayeringPreservesTheMaskOfAnEarlierEffect(t *testing.T) {
	target := canvas.FromText(sample, canvas.Default)
	dst := canvas.New(target.W, target.H)
	alone := canvas.New(target.W, target.H)

	chain := build(t, []string{"wipe", "decrypt"}, target, rand.New(rand.NewPCG(5, 8)))
	onlyWipe := build(t, []string{"wipe"}, target, rand.New(rand.NewPCG(5, 8)))

	scrambled := 0
	for _, elapsed := range []time.Duration{0, 40 * time.Millisecond, 120 * time.Millisecond} {
		dst.CopyFrom(target)
		for _, ef := range chain {
			ef.Frame(dst, elapsed)
		}
		alone.CopyFrom(target)
		onlyWipe[0].Frame(alone, elapsed)

		for y := range target.H {
			for x := range target.W {
				if alone.At(x, y) != canvas.Blank {
					if dst.At(x, y) != target.At(x, y) {
						scrambled++
					}
					continue
				}
				if dst.At(x, y) != canvas.Blank {
					t.Fatalf("t=%s: cell (%d,%d) is hidden by wipe but the chain drew %+v",
						elapsed, x, y, dst.At(x, y))
				}
			}
		}
	}
	if scrambled == 0 {
		t.Fatal("decrypt never scrambled a revealed cell; the chain ran wipe alone")
	}
}

func chains() [][]string {
	var out [][]string
	for _, a := range List() {
		out = append(out, []string{a.Name})
		for _, b := range List() {
			if a.Name == b.Name {
				continue
			}
			out = append(out, []string{a.Name, b.Name})
		}
	}
	return out
}

func build(t *testing.T, names []string, target *canvas.Canvas, rnd *rand.Rand) []Effect {
	t.Helper()
	chain := make([]Effect, 0, len(names))
	for _, n := range names {
		e, ok := Get(n)
		if !ok {
			t.Fatalf("unknown effect %q", n)
		}
		chain = append(chain, e.New(target, rnd))
	}
	return chain
}

func TestEffectsAreReproducibleFromASeed(t *testing.T) {
	for _, entry := range List() {
		t.Run(entry.Name, func(t *testing.T) {
			a := renderRun(entry, 7)
			b := renderRun(entry, 7)
			if !bytes.Equal(a, b) {
				t.Fatal("two runs with the same seed produced different output")
			}
		})
	}
}

// renderRun drives an effect through the real renderer, which is also what
// makes the byte counts in BenchmarkEffects mean something.
func renderRun(entry Entry, seed uint64) []byte {
	target := canvas.FromText(sample, canvas.Default)
	rnd := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	ef := entry.New(target, rnd)
	dst := canvas.New(target.W, target.H)

	var out bytes.Buffer
	r := canvas.NewRenderer(&out, target.W, target.H)
	elapsed := time.Duration(0)
	for {
		dst.CopyFrom(target)
		more := ef.Frame(dst, elapsed)
		_ = r.Draw(dst)
		if !more {
			return out.Bytes()
		}
		elapsed += 16 * time.Millisecond
	}
}

// Bytes per frame is the number that decides whether the animation survives a
// screen-sharing link, so it is measured rather than assumed.
func BenchmarkEffects(b *testing.B) {
	for _, entry := range List() {
		b.Run(entry.Name, func(b *testing.B) {
			var frames, total int
			for range b.N {
				out := renderRun(entry, 7)
				total += len(out)
				frames += countFrames(entry)
			}
			if frames > 0 {
				b.ReportMetric(float64(total)/float64(frames), "bytes/frame")
			}
		})
	}
}

func countFrames(entry Entry) int {
	target := canvas.FromText(sample, canvas.Default)
	rnd := rand.New(rand.NewPCG(7, 7^0x9e3779b97f4a7c15))
	ef := entry.New(target, rnd)
	dst := canvas.New(target.W, target.H)
	n := 0
	elapsed := time.Duration(0)
	for {
		n++
		dst.CopyFrom(target)
		if !ef.Frame(dst, elapsed) {
			return n
		}
		elapsed += 16 * time.Millisecond
	}
}

// slide is the one effect that reads and writes the same cells, so the walk
// direction is load-bearing: going the other way smears the leftmost column
// across the row. The end-state test cannot see that, because slide's last
// frame returns without touching the canvas at all -- an effect whose final
// frame is a no-op has its whole animation unguarded otherwise.
func TestSlideShiftsWithoutSmearing(t *testing.T) {
	target := canvas.FromText("abcdef\nghijkl\nmnopqr\n", canvas.Default)
	entry, ok := Get("slide")
	if !ok {
		t.Fatal("slide is not registered")
	}
	ef := entry.New(target, rand.New(rand.NewPCG(1, 2)))
	dst := canvas.New(target.W, target.H)

	for _, off := range []int{5, 3, 1} {
		elapsed := time.Duration(target.W-off) * (slideRun / time.Duration(target.W))
		dst.CopyFrom(target)
		ef.Frame(dst, elapsed)
		for y := range target.H {
			for x := range target.W {
				want := target.At(x-off, y)
				if got := dst.At(x, y); got != want {
					t.Fatalf("offset %d: cell (%d,%d) = %+v, want %+v", off, x, y, got, want)
				}
			}
		}
	}
}

// An effect that quietly does nothing would pass every other test here: the
// canvas starts as the text and has to end as the text, and doing nothing
// satisfies both ends.
func TestEveryEffectChangesTheCanvasWhileItRuns(t *testing.T) {
	for _, entry := range List() {
		t.Run(entry.Name, func(t *testing.T) {
			target := canvas.FromText(sample, canvas.Default)
			ef := entry.New(target, rand.New(rand.NewPCG(3, 4)))
			dst := canvas.New(target.W, target.H)

			for elapsed := time.Duration(0); elapsed < 3*time.Second; elapsed += 20 * time.Millisecond {
				dst.CopyFrom(target)
				ef.Frame(dst, elapsed)
				for i := range dst.Cells {
					if dst.Cells[i] != target.Cells[i] {
						return
					}
				}
			}
			t.Fatal("the canvas never differed from the text; the effect draws nothing")
		})
	}
}
