package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const sweepRun = 1600 * time.Millisecond

var sweepColour = canvas.RGB(255, 176, 60)

// sweep crosses once laying colour down and crosses back taking it away, so the
// text ends in the colours it started with and the effect needs no fade. The
// two fronts are what distinguish it: one edge advancing, then one retreating.
type sweep struct {
	span float64
}

func newSweep(target *canvas.Canvas, _ *rand.Rand) Effect {
	return &sweep{span: float64(max(target.W+target.H, 1))}
}

func (e *sweep) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= sweepRun {
		return false
	}
	p := float64(t) / float64(sweepRun)
	laying := p < 0.5
	front := p * 2 * e.span
	if !laying {
		front = (p - 0.5) * 2 * e.span
	}

	for y := range c.H {
		for x := range c.W {
			cell := c.At(x, y)
			if cell.R == ' ' {
				continue
			}
			at := float64(x + y)
			// Laying down: everything the front has passed is coloured. Taking
			// it away: everything the front has passed is back to its own.
			coloured := at < front
			if !laying {
				coloured = at >= front
			}
			if !coloured {
				continue
			}
			cell.FG = sweepColour
			c.Set(x, y, cell)
		}
	}
	return true
}
