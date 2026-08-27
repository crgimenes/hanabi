package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	laseretchRun = 1500 * time.Millisecond
	// Cells still glowing behind the point. Short, or the trail stops reading
	// as heat and becomes a coloured region.
	laseretchTrail = 14.0
)

var (
	laseretchHot  = canvas.RGB(255, 245, 230)
	laseretchCool = canvas.RGB(190, 40, 15)
)

// laseretch walks a point through the text in reading order, white hot, with a
// short trail cooling behind it. Nothing is hidden: the text is all there the
// whole time, and only the heat moves.
type laseretch struct {
	w     int
	total float64
}

func newLaseretch(target *canvas.Canvas, _ *rand.Rand) Effect {
	return &laseretch{
		w:     max(target.W, 1),
		total: float64(max(len(target.Cells), 1)),
	}
}

func (e *laseretch) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= laseretchRun {
		return false
	}
	p := float64(t) / float64(laseretchRun)
	// Runs past the end so the last cells get their full trail rather than
	// being cut off still glowing.
	point := p * (e.total + laseretchTrail)

	for y := range c.H {
		for x := range c.W {
			cell := c.At(x, y)
			if cell.R == ' ' {
				continue
			}
			age := point - float64(y*e.w+x)
			if age < 0 || age > laseretchTrail {
				continue
			}
			heat := 1 - age/laseretchTrail
			cell.FG = mixToward(cell.FG, mixToward(laseretchCool, laseretchHot, heat*heat), heat)
			c.Set(x, y, cell)
		}
	}
	return true
}
