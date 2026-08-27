package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	spotlightRun = 1800 * time.Millisecond
	// Fraction of the run spent sweeping before the beam opens out to take in
	// the whole text.
	spotlightSweep = 0.72
	// Terminal cells are about twice as tall as they are wide, so vertical
	// distance counts double or the beam comes out as a flattened ellipse.
	cellAspect = 2.0
)

// spotlight sweeps a beam over the text and lights only what falls inside it,
// then opens out. It masks by distance from a point rather than by a straight
// frontier, which is the one shape the other effects here do not make.
type spotlight struct {
	w     float64
	h     float64
	beam  float64
	full  float64
	phase float64
}

func newSpotlight(target *canvas.Canvas, rnd *rand.Rand) Effect {
	w, h := float64(target.W), float64(target.H)
	return &spotlight{
		w:    w,
		h:    h,
		beam: max(min(w, h*cellAspect)/3, 3),
		// Reaching a corner from the middle, so the open beam covers everything.
		full:  math.Hypot(w, h*cellAspect),
		phase: rnd.Float64(),
	}
}

func (s *spotlight) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= spotlightRun {
		return false
	}
	p := float64(t) / float64(spotlightRun)
	r := s.beam
	if p > spotlightSweep {
		open := (p - spotlightSweep) / (1 - spotlightSweep)
		r = s.beam + open*(s.full-s.beam)
	}

	// A figure of eight: the beam crosses the middle twice and reaches the far
	// corners without retracing its own path.
	a := 2 * math.Pi * (p + s.phase)
	cx := s.w/2 + (s.w/2-s.beam/2)*math.Sin(a)
	cy := s.h/2 + s.h/2*math.Sin(2*a)
	r2 := r * r

	for y := range c.H {
		dy := (float64(y) - cy) * cellAspect
		for x := range c.W {
			dx := float64(x) - cx
			if dx*dx+dy*dy <= r2 {
				continue
			}
			c.Set(x, y, canvas.Blank)
		}
	}
	return true
}
