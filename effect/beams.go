package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	beamsRun   = 1700 * time.Millisecond
	beamsCount = 3
	beamsWidth = 2.2
)

var beamsColour = canvas.RGB(255, 250, 215)

// beams sends a few bright lines across at their own angles and speeds. Where
// waves and colorshift colour the whole text, this leaves most of it alone: the
// picture stays readable and only what a beam crosses lights up.
type beams struct {
	beam [beamsCount]beam
	span float64
}

type beam struct {
	cos   float64
	sin   float64
	speed float64
	phase float64
}

func newBeams(target *canvas.Canvas, rnd *rand.Rand) Effect {
	b := &beams{span: math.Hypot(float64(target.W), float64(target.H)*cellAspect)}
	for i := range b.beam {
		a := rnd.Float64() * 2 * math.Pi
		b.beam[i] = beam{
			cos:   math.Cos(a),
			sin:   math.Sin(a),
			speed: 0.8 + rnd.Float64()*0.9,
			phase: rnd.Float64(),
		}
	}
	return b
}

func (e *beams) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= beamsRun {
		return false
	}
	p := float64(t) / float64(beamsRun)
	// Rises and dies away, like the others here: without it the beams are
	// simply switched off wherever they happen to be when time runs out.
	strength := math.Sin(p * math.Pi)

	for y := range c.H {
		fy := float64(y) * cellAspect
		for x := range c.W {
			cell := c.At(x, y)
			if cell.R == ' ' {
				continue
			}
			lit := 0.0
			for _, b := range e.beam {
				// Distance from the cell to the beam's line, in the beam's own
				// direction; the line itself slides along that direction.
				d := float64(x)*b.cos + fy*b.sin
				front := (b.phase + p*b.speed) * 2 * e.span
				front = math.Mod(front, 3*e.span) - e.span
				lit = max(lit, 1-min(math.Abs(d-front)/beamsWidth, 1))
			}
			if lit <= 0 {
				continue
			}
			cell.FG = mixToward(cell.FG, beamsColour, lit*strength)
			c.Set(x, y, cell)
		}
	}
	return true
}
