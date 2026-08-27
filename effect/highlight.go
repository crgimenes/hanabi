package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	highlightRun   = 1100 * time.Millisecond
	highlightWidth = 4.0
)

// highlight runs a specular band across the text. Alone among these it changes
// no colour at all -- it only turns cells bold -- so it reads as a shine over
// ANSI art instead of painting over the picture.
type highlight struct {
	span float64
}

func newHighlight(target *canvas.Canvas, _ *rand.Rand) Effect {
	return &highlight{span: float64(max(target.W+target.H, 1))}
}

func (e *highlight) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= highlightRun {
		return false
	}
	p := float64(t) / float64(highlightRun)
	front := -highlightWidth + p*(e.span+2*highlightWidth)

	for y := range c.H {
		for x := range c.W {
			cell := c.At(x, y)
			if cell.R == ' ' {
				continue
			}
			d := float64(x+y) - front
			if d < -highlightWidth || d > highlightWidth {
				continue
			}
			cell.Bold = true
			c.Set(x, y, cell)
		}
	}
	return true
}
