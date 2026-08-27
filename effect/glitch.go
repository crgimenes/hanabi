package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	glitchRun   = 1400 * time.Millisecond
	glitchSlice = 80 * time.Millisecond
	glitchShift = 7
	// Share of rows torn at the very start. All of them at once reads as mush
	// rather than as a signal breaking up.
	glitchPeak = 0.55
)

var glitchWash = canvas.RGB(205, 220, 230)

// glitch tears rows sideways and washes their colour out, the way tape loses
// tracking, and settles as it goes until no row tears at all.
//
// Which rows tear is a pure function of the row and the moment, so there is no
// schedule to store and the animation does not change with the frame rate.
type glitch struct {
	base uint64
}

func newGlitch(_ *canvas.Canvas, rnd *rand.Rand) Effect {
	return &glitch{base: rnd.Uint64()}
}

func (g *glitch) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= glitchRun {
		return false
	}
	slice := sliceAt(t)
	chance := glitchPeak * (1 - float64(t)/float64(glitchRun))

	var row uint64
	for y := range c.H {
		h := mix(g.base+row, slice)
		row++
		if float64(h%1000)/1000 >= chance {
			continue
		}
		tear(c, y, shiftFor(h))
	}
	return true
}

// #nosec G115 -- elapsed time is never negative, so the conversion cannot wrap.
func sliceAt(t time.Duration) uint64 {
	return uint64(t / glitchSlice)
}

// #nosec G115 -- the remainder is below 2*glitchShift+1, far inside an int.
func shiftFor(h uint64) int {
	return int(h>>20%(2*glitchShift+1)) - glitchShift
}

// tear slides one row sideways in place, walking against the direction of travel
// so every cell is read before anything overwrites it. Cells pulled in from off
// the row arrive blank, which is what makes the tear visible at the edge.
func tear(c *canvas.Canvas, y, shift int) {
	if shift == 0 {
		return
	}
	if shift > 0 {
		for x := c.W - 1; x >= 0; x-- {
			c.Set(x, y, washed(c.At(x-shift, y)))
		}
		return
	}
	for x := range c.W {
		c.Set(x, y, washed(c.At(x-shift, y)))
	}
}

func washed(cell canvas.Cell) canvas.Cell {
	if cell.R == ' ' {
		return cell
	}
	cell.FG = glitchWash
	return cell
}
