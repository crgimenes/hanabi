package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	wavesRun    = 1900 * time.Millisecond
	wavesLength = 9.0
	wavesSpeed  = 5.0
)

var (
	wavesTrough = canvas.RGB(20, 70, 150)
	wavesCrest  = canvas.RGB(90, 220, 235)
)

// waves rolls bands of colour through the text. Unlike colorshift, which colours
// everything at once from a single moving spectrum, the bands here have troughs
// the text shows through.
type waves struct {
	phase float64
}

func newWaves(_ *canvas.Canvas, rnd *rand.Rand) Effect {
	return &waves{phase: rnd.Float64() * 2 * math.Pi}
}

func (e *waves) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= wavesRun {
		return false
	}
	p := float64(t) / float64(wavesRun)
	// Rises and falls away again, so the text is left in its own colours.
	strength := math.Sin(p * math.Pi)

	for y := range c.H {
		for x := range c.W {
			cell := c.At(x, y)
			if cell.R == ' ' {
				continue
			}
			a := e.phase + float64(x+y/2)/wavesLength - p*wavesSpeed
			// From -1..1 to 0..1, squared so the crests stay narrow and the
			// troughs wide: broad bright bands read as a wash, not as waves.
			h := (math.Sin(a) + 1) / 2
			cell.FG = mixToward(cell.FG, mixToward(wavesTrough, wavesCrest, h*h), strength)
			c.Set(x, y, cell)
		}
	}
	return true
}
