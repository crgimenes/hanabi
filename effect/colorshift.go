package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	colorshiftRun = 2200 * time.Millisecond
	// Turns of hue crossed from one side of the text to the other. Under one
	// and it reads as a flat tint sliding by rather than as a spectrum.
	colorshiftSpan = 1.3
)

// colorshift runs a moving spectrum through the text. Every cell is coloured at
// every moment, so it has no edge and no front -- which is what separates it
// from the effects that sweep a band across.
type colorshift struct {
	span  float64
	phase float64
}

func newColorshift(target *canvas.Canvas, rnd *rand.Rand) Effect {
	return &colorshift{
		span:  float64(max(target.W+target.H, 1)),
		phase: rnd.Float64(),
	}
}

func (e *colorshift) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= colorshiftRun {
		return false
	}
	p := float64(t) / float64(colorshiftRun)
	// Fades back to the text's own colours at the end instead of stopping on
	// whatever hue it happened to be showing.
	strength := math.Sin(p * math.Pi)

	for y := range c.H {
		for x := range c.W {
			cell := c.At(x, y)
			if cell.R == ' ' {
				continue
			}
			hue := e.phase + p + colorshiftSpan*float64(x+y)/e.span
			cell.FG = mixToward(cell.FG, hsv(hue, 1), strength)
			c.Set(x, y, cell)
		}
	}
	return true
}

// mixToward blends only when the cell has a colour of its own to blend with. A
// palette index has no components to mix, so it is replaced outright or left
// alone -- guessing its RGB would throw away the reader's theme.
func mixToward(from, to canvas.Color, amount float64) canvas.Color {
	if amount <= 0 {
		return from
	}
	fr, fg, fb, ok := from.RGB()
	if !ok {
		return to
	}
	tr, tg, tb, _ := to.RGB()
	return canvas.RGB(
		chan8(lerp(float64(fr), float64(tr), amount)/255),
		chan8(lerp(float64(fg), float64(tg), amount)/255),
		chan8(lerp(float64(fb), float64(tb), amount)/255),
	)
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }
