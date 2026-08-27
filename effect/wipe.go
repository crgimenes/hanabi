package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const wipeStep = 9 * time.Millisecond

// wipe never reads the text, only hides what the sweep has not reached yet, so
// it composes with any effect on either side of it.
type wipe struct {
	span int
}

func newWipe(target *canvas.Canvas, _ *rand.Rand) Effect {
	// Taken from the target rather than the frame so a resize mid-run cannot
	// change how long the sweep lasts.
	return &wipe{span: target.W + target.H}
}

func (w *wipe) Frame(c *canvas.Canvas, t time.Duration) bool {
	edge := int(t / wipeStep)
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
