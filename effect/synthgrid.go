package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	synthgridRun  = 1700 * time.Millisecond
	synthgridDraw = 0.38
	synthgridStep = 4
)

var (
	synthgridLine  = canvas.RGB(60, 130, 190)
	synthgridCross = '┼'
	synthgridRow   = '─'
	synthgridCol   = '│'
)

// synthgrid rules a grid over the empty space, then lets the text dissolve into
// it, square by square. The grid is the point: it gives the reveal a structure
// the eye can follow, where the other maskers move a bare front.
type synthgrid struct {
	w     int
	fill  []float64
	total float64
}

func newSynthgrid(target *canvas.Canvas, rnd *rand.Rand) Effect {
	e := &synthgrid{
		w:     max(target.W, 1),
		fill:  make([]float64, len(target.Cells)),
		total: float64(max(target.W+target.H, 1)),
	}
	for i := range e.fill {
		e.fill[i] = rnd.Float64()
	}
	return e
}

func (e *synthgrid) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= synthgridRun {
		return false
	}
	p := float64(t) / float64(synthgridRun)
	drawn := min(p/synthgridDraw, 1) * e.total
	filled := 0.0
	if p > synthgridDraw {
		filled = (p - synthgridDraw) / (1 - synthgridDraw)
	}

	for y := range c.H {
		for x := range c.W {
			i := y*e.w + x
			if i < len(e.fill) && e.fill[i] < filled {
				continue
			}
			if float64(x+y) < drawn {
				c.Set(x, y, gridCell(x, y))
				continue
			}
			c.Set(x, y, canvas.Blank)
		}
	}
	return true
}

func gridCell(x, y int) canvas.Cell {
	onCol := x%synthgridStep == 0
	onRow := y%synthgridStep == 0
	switch {
	case onCol && onRow:
		return canvas.Cell{R: synthgridCross, FG: synthgridLine, BG: canvas.Default}
	case onRow:
		return canvas.Cell{R: synthgridRow, FG: synthgridLine, BG: canvas.Default}
	case onCol:
		return canvas.Cell{R: synthgridCol, FG: synthgridLine, BG: canvas.Default}
	}
	return canvas.Blank
}
