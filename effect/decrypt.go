package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	decryptSwap   = 45 * time.Millisecond
	decryptSpread = 1400 * time.Millisecond
)

var (
	cipher      = []rune("!@#$%&*()_+-=[]{}|;:,.<>?/~0123456789ABCDEFabcdef")
	cipherColor = canvas.RGB(0, 190, 90)
)

type decrypt struct {
	w      int
	settle []time.Duration
	done   time.Duration
}

func newDecrypt(target *canvas.Canvas, rnd *rand.Rand) Effect {
	d := &decrypt{
		w:      target.W,
		settle: make([]time.Duration, len(target.Cells)),
	}
	for i := range d.settle {
		d.settle[i] = rnd.N(decryptSpread)
		if d.settle[i] > d.done {
			d.done = d.settle[i]
		}
	}
	return d
}

func (d *decrypt) Frame(c *canvas.Canvas, t time.Duration) bool {
	// #nosec G115 -- t is elapsed time and i is a slice index; neither is ever
	// negative, so the conversions cannot wrap.
	tick := uint64(t / decryptSwap)
	for y := range c.H {
		for x := range c.W {
			// A blank is either real whitespace or a cell an earlier effect
			// hid; scrambling it would paint over both.
			cell := c.At(x, y)
			if cell.R == ' ' {
				continue
			}
			i := y*d.w + x
			if i >= len(d.settle) || t >= d.settle[i] {
				continue
			}
			// The background is left alone: in ANSI art it carries half the
			// picture, and replacing it would flash the image to bare terminal.
			cell.R = glyph(i, tick)
			cell.FG = cipherColor
			c.Set(x, y, cell)
		}
	}
	return t < d.done
}

// #nosec G115 -- a slice index and a slice length are never negative, so
// neither conversion can wrap.
func glyph(cell int, tick uint64) rune {
	return cipher[mix(uint64(cell), tick)%uint64(len(cipher))]
}

// mix is splitmix64 over the cell index and the frame tick. The scrambled glyph
// has to be a pure function of both: Frame can be called at any t, so drawing
// from the rand stream would make the animation depend on the frame rate.
func mix(a, b uint64) uint64 {
	x := a*0x9e3779b97f4a7c15 + b
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}
