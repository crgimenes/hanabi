package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	middleoutRun = 1400 * time.Millisecond
	// Share of the run spent opening the middle row before the rest follows.
	middleoutSpread = 0.45
)

// middleout opens the middle row from its centre and then unfolds the rest above
// and below it. Two fronts in sequence, one along each axis: the other maskers
// here run a single front, in one direction.
type middleout struct {
	midX float64
	midY float64
}

func newMiddleout(target *canvas.Canvas, _ *rand.Rand) Effect {
	return &middleout{
		midX: float64(target.W-1) / 2,
		midY: float64(target.H-1) / 2,
	}
}

func (e *middleout) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= middleoutRun {
		return false
	}
	p := float64(t) / float64(middleoutRun)

	across := min(p/middleoutSpread, 1) * (e.midX + 1)
	// Half a cell to start with, not none: on an even number of rows the middle
	// falls between two of them, and a reach of zero would open neither.
	out := 0.5
	if p > middleoutSpread {
		out += (p - middleoutSpread) / (1 - middleoutSpread) * (e.midY + 0.5)
	}

	for y := range c.H {
		if absf(float64(y)-e.midY) > out {
			for x := range c.W {
				c.Set(x, y, canvas.Blank)
			}
			continue
		}
		for x := range c.W {
			if absf(float64(x)-e.midX) <= across {
				continue
			}
			c.Set(x, y, canvas.Blank)
		}
	}
	return true
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
