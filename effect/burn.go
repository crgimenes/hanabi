package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	burnRise   = 26 * time.Millisecond
	burnJitter = 120 * time.Millisecond
	burnFade   = 320 * time.Millisecond
)

// fire runs white-hot to embers; a cell walks it once and then keeps the colour
// it was always meant to have.
var fire = []canvas.Color{
	canvas.RGB(255, 250, 220),
	canvas.RGB(255, 215, 90),
	canvas.RGB(255, 150, 30),
	canvas.RGB(225, 70, 20),
	canvas.RGB(140, 30, 10),
}

// burn sweeps a band of fire up through the text, recolouring as it goes. It
// hides nothing and moves nothing, which makes it the one effect here that can
// be layered under any other without changing what that other one draws.
type burn struct {
	w      int
	ignite []time.Duration
	done   time.Duration
}

func newBurn(target *canvas.Canvas, rnd *rand.Rand) Effect {
	b := &burn{
		w:      target.W,
		ignite: make([]time.Duration, len(target.Cells)),
	}
	for i := range b.ignite {
		row := i / max(target.W, 1)
		fromBottom := target.H - 1 - row
		b.ignite[i] = time.Duration(fromBottom)*burnRise + rnd.N(burnJitter)
		end := b.ignite[i] + burnFade
		if end > b.done {
			b.done = end
		}
	}
	return b
}

func (b *burn) Frame(c *canvas.Canvas, t time.Duration) bool {
	for y := range c.H {
		for x := range c.W {
			if x >= b.w {
				continue
			}
			i := y*b.w + x
			if i >= len(b.ignite) || t < b.ignite[i] || t >= b.ignite[i]+burnFade {
				continue
			}
			cell := c.At(x, y)
			if cell.R == ' ' {
				continue
			}
			step := int((t - b.ignite[i]) * time.Duration(len(fire)) / burnFade)
			cell.FG = fire[min(step, len(fire)-1)]
			c.Set(x, y, cell)
		}
	}
	return t < b.done
}
