package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const sliceRun = 900 * time.Millisecond

// slice cuts the text down the middle and brings the halves in from opposite
// sides. slide carries the whole picture one way; this is the only effect that
// moves two parts of it against each other.
type slice struct {
	cut  int
	span int
	buf  *canvas.Canvas
}

func newSlice(target *canvas.Canvas, _ *rand.Rand) Effect {
	return &slice{
		cut:  target.H / 2,
		span: max(target.W, 1),
		buf:  canvas.New(target.W, target.H),
	}
}

func (e *slice) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= sliceRun {
		return false
	}
	off := int(float64(e.span) * (1 - ease(float64(t)/float64(sliceRun))))
	if off == 0 {
		return true
	}

	e.buf.CopyFrom(c)
	c.Fill(canvas.Blank)
	for y := range e.buf.H {
		// Rows above the cut arrive from the left, rows below from the right.
		shift := -off
		if y >= e.cut {
			shift = off
		}
		for x := range e.buf.W {
			cell := e.buf.At(x, y)
			if cell.R == ' ' {
				continue
			}
			c.Set(x+shift, y, cell)
		}
	}
	return true
}
