package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	binarypathRun     = 1700 * time.Millisecond
	binarypathStagger = 600 * time.Millisecond
	binarypathFlicker = 60 * time.Millisecond
)

var binarypathColour = canvas.RGB(70, 200, 130)

// binarypath walks each character in along its own row as a binary digit, which
// turns into the character when it arrives. Where scattered throws characters
// across the canvas, every traveller here stays on its own line.
type binarypath struct {
	w     int
	until []time.Duration
	buf   *canvas.Canvas
	done  time.Duration
}

func newBinarypath(target *canvas.Canvas, rnd *rand.Rand) Effect {
	e := &binarypath{
		w:     max(target.W, 1),
		until: make([]time.Duration, len(target.Cells)),
		buf:   canvas.New(target.W, target.H),
	}
	for i := range e.until {
		// Spread widely on purpose: bunched arrivals mean the whole text is
		// digits until the last moment and then becomes words all at once.
		e.until[i] = binarypathRun*2/5 + rnd.N(binarypathRun*3/5+binarypathStagger)
		if e.until[i] > e.done {
			e.done = e.until[i]
		}
	}
	return e
}

func (e *binarypath) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= e.done {
		return false
	}
	tick := sliceAt(t, binarypathFlicker)
	e.buf.CopyFrom(c)
	c.Fill(canvas.Blank)

	for y := range e.buf.H {
		for x := range e.buf.W {
			cell := e.buf.At(x, y)
			if cell.R == ' ' {
				continue
			}
			i := y*e.w + x
			if i >= len(e.until) {
				continue
			}
			p := ease(float64(t) / float64(e.until[i]))
			if p >= 1 {
				c.Set(x, y, cell)
				continue
			}
			// Still travelling: a digit, somewhere between the left edge and
			// the place the character belongs.
			at := int(math.Round(float64(x) * p))
			c.Set(at, y, canvas.Cell{R: bit(i, tick), Bold: cell.Bold, FG: binarypathColour, BG: cell.BG})
		}
	}
	return true
}

// #nosec G115 -- a slice index is never negative, so the conversion cannot wrap.
func bit(cell int, tick uint64) rune {
	if mix(uint64(cell), tick)&1 == 0 {
		return '0'
	}
	return '1'
}
