package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	expandRun = 1100 * time.Millisecond
	// Where the zoom starts. Not zero: at nothing at all the first frames are a
	// single cell and the text seems to appear rather than to grow.
	expandFrom = 0.08
)

// expand grows the whole picture out of a point in the middle. It scales rather
// than translating or masking, which is the one transformation the rest of these
// effects do not make.
type expand struct {
	buf *canvas.Canvas
}

func newExpand(target *canvas.Canvas, _ *rand.Rand) Effect {
	return &expand{buf: canvas.New(target.W, target.H)}
}

func (e *expand) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= expandRun {
		return false
	}
	scale := expandFrom + (1-expandFrom)*ease(float64(t)/float64(expandRun))

	e.buf.CopyFrom(c)
	c.Fill(canvas.Blank)

	cx := float64(e.buf.W-1) / 2
	cy := float64(e.buf.H-1) / 2
	for y := range e.buf.H {
		dy := (float64(y) - cy) * scale
		for x := range e.buf.W {
			cell := e.buf.At(x, y)
			if cell.R == ' ' {
				continue
			}
			// Mapped forwards, from source to screen. That works here only
			// because the picture never grows past its own size: several
			// sources land on one cell and none is left empty. Reverse the
			// scale and this maps outwards instead, throwing most of the
			// characters off the canvas.
			dx := (float64(x) - cx) * scale
			c.Set(int(math.Round(cx+dx)), int(math.Round(cy+dy)), cell)
		}
	}
	return true
}
