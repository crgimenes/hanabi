package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	crumbleRun = 2100 * time.Millisecond
	// The four acts, as fractions of the run: colour drains, characters fall
	// apart, the dust is swept up, the text comes back.
	crumbleDrain   = 0.22
	crumbleDust    = 0.44
	crumbleSweep   = 0.68
	crumbleStagger = 0.22
	// How often the grit is re-drawn while a cell is falling apart.
	crumbleFlicker = 70 * time.Millisecond
)

var (
	crumbleAsh  = canvas.RGB(120, 118, 112)
	crumbleGrit = []rune(".,'`\":;")
)

// crumble drains the colour out of the text, breaks the characters into grit,
// sweeps the grit away and then puts the text back. It is the only effect here
// that takes the text apart and returns it: the rest reveal, recolour or move.
type crumble struct {
	w     int
	phase []float64
	done  time.Duration
}

func newCrumble(target *canvas.Canvas, rnd *rand.Rand) Effect {
	c := &crumble{
		w:     max(target.W, 1),
		phase: make([]float64, len(target.Cells)),
		done:  crumbleRun,
	}
	for i := range c.phase {
		// Each cell runs the same four acts slightly out of step with its
		// neighbours, so the text falls apart raggedly rather than all at once.
		c.phase[i] = rnd.Float64() * crumbleStagger
	}
	return c
}

// #nosec G115 -- a slice index and a slice length are never negative, so
// neither conversion can wrap.
func grit(cell int, tick uint64) rune {
	return crumbleGrit[mix(uint64(cell), tick)%uint64(len(crumbleGrit))]
}

func (e *crumble) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= e.done {
		return false
	}
	tick := sliceAt(t, crumbleFlicker)
	base := float64(t) / float64(crumbleRun)

	for y := range c.H {
		for x := range c.W {
			cell := c.At(x, y)
			if cell.R == ' ' {
				continue
			}
			i := y*e.w + x
			if i >= len(e.phase) {
				continue
			}
			p := base - e.phase[i]
			switch {
			case p < 0:
			case p < crumbleDrain:
				cell.FG = mixToward(cell.FG, crumbleAsh, p/crumbleDrain)
			case p < crumbleDust:
				cell.FG = crumbleAsh
				cell.R = grit(i, tick)
			case p < crumbleSweep:
				cell = canvas.Blank
			default:
				// Back as it was, one cell at a time.
			}
			c.Set(x, y, cell)
		}
	}
	return true
}
