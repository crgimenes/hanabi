package effect

import (
	"bytes"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const sample = "hanabi\nzero dependencies\nterminal text effects\n"

// Every effect exists to end with the text readable on screen. An effect that
// reports itself finished while still showing scrambled cells leaves the
// terminal wrong in a way no other test would catch.
func TestEffectsEndOnTheTarget(t *testing.T) {
	for _, entry := range List() {
		t.Run(entry.Name, func(t *testing.T) {
			target := canvas.FromText(sample, canvas.Default)
			rnd := rand.New(rand.NewPCG(1, 2))
			ef := entry.New(target, rnd)
			dst := canvas.New(target.W, target.H)

			const step = 16 * time.Millisecond
			const limit = 30 * time.Second
			elapsed := time.Duration(0)
			frames := 0
			for ef.Frame(dst, elapsed) {
				elapsed += step
				frames++
				if elapsed > limit {
					t.Fatalf("still running after %s of simulated time", limit)
				}
			}
			if frames == 0 {
				t.Fatal("finished on the first frame; nothing would be animated")
			}
			for y := range target.H {
				for x := range target.W {
					want, got := target.At(x, y), dst.At(x, y)
					if want != got {
						t.Fatalf("after %d frames cell (%d,%d) = %+v, want %+v", frames, x, y, got, want)
					}
				}
			}
		})
	}
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
		if !ef.Frame(dst, elapsed) {
			return n
		}
		elapsed += 16 * time.Millisecond
	}
}
