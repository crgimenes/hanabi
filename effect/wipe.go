package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

// Derived from the extent for the same reason as slide: the sweep should read
// as the same gesture on a small logo and on a full screen of art.
const wipeRun = 700 * time.Millisecond

// wipe never reads the text, only hides what the sweep has not reached yet, so
// it composes with any effect on either side of it.
type wipe struct {
	span int
	step time.Duration
}

func newWipe(target *canvas.Canvas, _ *rand.Rand) Effect {
	// Taken from the target rather than the frame so a resize mid-run cannot
	// change how long the sweep lasts.
	span := target.W + target.H
	return &wipe{
		span: span,
		// The floor keeps a pathologically wide input from dividing to zero.
		step: max(wipeRun/time.Duration(max(span, 1)), time.Nanosecond),
	}
}

func (w *wipe) Frame(c *canvas.Canvas, t time.Duration) bool {
	edge := int(t / w.step)
	for y := range c.H {
		for x := range c.W {
			if x+y <= edge {
				continue
			}
			c.Set(x, y, canvas.Blank)
		}
	}
	return edge < w.span
}
