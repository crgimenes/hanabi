package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

// The step is derived from the width rather than fixed, so the slide takes the
// same time whatever the art measures. A fixed per-column step made a nine-wide
// logo flash past in 63ms while an eighty-wide one took most of a second.
const slideRun = 900 * time.Millisecond

// slide carries the whole picture in from the left. It is the one effect here
// that moves cells rather than recolouring or hiding them, so chaining it with
// another mover would have the two fight over the same cells.
type slide struct {
	span int
	step time.Duration
}

func newSlide(target *canvas.Canvas, _ *rand.Rand) Effect {
	return &slide{
		span: target.W,
		step: max(slideRun/time.Duration(max(target.W, 1)), time.Nanosecond),
	}
}

func (s *slide) Frame(c *canvas.Canvas, t time.Duration) bool {
	off := s.span - int(t/s.step)
	if off <= 0 {
		return false
	}
	// Right to left: each cell reads from a column further left, which the
	// descending walk has not overwritten yet, so no scratch buffer is needed.
	for y := range c.H {
		for x := c.W - 1; x >= 0; x-- {
			c.Set(x, y, c.At(x-off, y))
		}
	}
	return true
}
