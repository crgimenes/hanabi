package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	overflowRun   = 1600 * time.Millisecond
	overflowSlice = 70 * time.Millisecond
)

// overflow scrolls the text past itself out of order, rows landing in the wrong
// places, and settles row by row until the order is right. glitch displaces a
// row sideways; this one puts a different row there altogether.
type overflow struct {
	base    uint64
	settle  []float64
	scratch *canvas.Canvas
}

func newOverflow(target *canvas.Canvas, rnd *rand.Rand) Effect {
	e := &overflow{
		base:   rnd.Uint64(),
		settle: make([]float64, target.H),
	}
	for i := range e.settle {
		e.settle[i] = rnd.Float64()
	}
	return e
}

func (e *overflow) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= overflowRun {
		return false
	}
	p := float64(t) / float64(overflowRun)
	tick := sliceAt(t, overflowSlice)

	// Read the whole picture first: rows are about to be written over with
	// other rows, and a row already replaced must not be read as a source.
	buf := e.buf(c)

	var row uint64
	for y := range c.H {
		h := mix(e.base+row, tick)
		row++
		if y >= len(e.settle) || e.settle[y] < p {
			continue
		}
		from := sourceRow(h, c.H)
		for x := range c.W {
			c.Set(x, y, buf.At(x, from))
		}
	}
	return true
}

func (e *overflow) buf(c *canvas.Canvas) *canvas.Canvas {
	if e.scratch == nil || e.scratch.W != c.W || e.scratch.H != c.H {
		e.scratch = canvas.New(c.W, c.H)
	}
	e.scratch.CopyFrom(c)
	return e.scratch
}

// #nosec G115 -- a row count is never negative, so the conversion cannot wrap.
func sourceRow(h uint64, height int) int {
	if height < 1 {
		return 0
	}
	return int(h % uint64(height))
}
