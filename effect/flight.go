package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

// flight is the machinery shared by every effect that carries characters to
// their places. It reads the whole picture, clears the canvas and redraws each
// character wherever its path says it is at this moment.
//
// An effect built on it writes only two things: how long each character takes,
// and where it is along the way. That is the whole difference between rain and a
// black hole -- the reading, the clearing, the rounding to cells and the promise
// that everything ends up home are settled here, once.
type flight struct {
	w     int
	until []time.Duration
	at    pathFunc
	buf   *canvas.Canvas
	done  time.Duration
}

// pathFunc reports where character i is when it is p of the way through its
// journey, 0 being the start and 1 being home. It is only ever asked for p below
// 1: arrival is not its business.
type pathFunc func(i int, home point, p float64) point

func newFlight(target *canvas.Canvas, until []time.Duration, at pathFunc) *flight {
	f := &flight{
		w:     max(target.W, 1),
		until: until,
		at:    at,
		buf:   canvas.New(target.W, target.H),
	}
	for _, u := range until {
		f.done = max(f.done, u)
	}
	return f
}

func (f *flight) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= f.done {
		return false
	}
	// The picture is read whole before anything moves: a character already
	// carried into a cell must not be read again as though it lived there.
	f.buf.CopyFrom(c)
	c.Fill(canvas.Blank)

	for y := range f.buf.H {
		for x := range f.buf.W {
			cell := f.buf.At(x, y)
			// A wide character's second column is left where it is, so the pair
			// is mended away for the flight and comes back whole on arrival.
			if cell.R == ' ' || cell.R == 0 {
				continue
			}
			i := y*f.w + x
			if i >= len(f.until) {
				continue
			}
			home := point{x: float64(x), y: float64(y)}
			p := 1.0
			if f.until[i] > 0 {
				p = float64(t) / float64(f.until[i])
			}
			if p >= 1 {
				c.Set(x, y, cell)
				continue
			}
			now := f.at(i, home, p)
			c.Set(int(math.Round(now.x)), int(math.Round(now.y)), cell)
		}
	}
	return true
}

// stagger builds the per-character schedule the flights share: a common run
// plus a spread, so characters do not all arrive on the same frame.
func stagger(n int, run, spread time.Duration, rnd *rand.Rand) []time.Duration {
	until := make([]time.Duration, n)
	for i := range until {
		until[i] = run + rnd.N(spread)
	}
	return until
}
