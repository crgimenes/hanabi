package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	scatteredRun     = 1500 * time.Millisecond
	scatteredStagger = 500 * time.Millisecond
	// How far a character starts from home, as a share of the text's extent.
	scatteredThrow = 1.4
)

// scattered throws every character away from where it belongs and lets each one
// find its way back on its own schedule. It is the only effect here that moves
// cells independently: slide carries the whole picture and glitch whole rows.
type scattered struct {
	w     int
	from  []point
	until []time.Duration
	buf   *canvas.Canvas
	done  time.Duration
}

type point struct{ x, y float64 }

func newScattered(target *canvas.Canvas, rnd *rand.Rand) Effect {
	s := &scattered{
		w:     max(target.W, 1),
		from:  make([]point, len(target.Cells)),
		until: make([]time.Duration, len(target.Cells)),
		buf:   canvas.New(target.W, target.H),
	}
	reach := scatteredThrow * math.Hypot(float64(target.W), float64(target.H)*cellAspect)
	for i := range s.from {
		a := rnd.Float64() * 2 * math.Pi
		d := reach * (0.35 + 0.65*rnd.Float64())
		s.from[i] = point{x: math.Cos(a) * d, y: math.Sin(a) * d / cellAspect}
		s.until[i] = scatteredRun + rnd.N(scatteredStagger)
		if s.until[i] > s.done {
			s.done = s.until[i]
		}
	}
	return s
}

func (s *scattered) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= s.done {
		return false
	}
	// The picture has to be read whole before anything moves, or a character
	// already carried into a cell would be read again as if it lived there.
	s.buf.CopyFrom(c)
	c.Fill(canvas.Blank)

	for y := range s.buf.H {
		for x := range s.buf.W {
			cell := s.buf.At(x, y)
			if cell.R == ' ' {
				continue
			}
			i := y*s.w + x
			if i >= len(s.from) {
				continue
			}
			p := ease(float64(t) / float64(s.until[i]))
			dx := s.from[i].x * (1 - p)
			dy := s.from[i].y * (1 - p)
			c.Set(x+int(math.Round(dx)), y+int(math.Round(dy)), cell)
		}
	}
	return true
}

// ease is smootherstep: leaves fast, arrives gently, which is what makes the
// characters look like they are settling rather than snapping into place.
func ease(p float64) float64 {
	p = min(max(p, 0), 1)
	return p * p * p * (p*(p*6-15) + 10)
}
