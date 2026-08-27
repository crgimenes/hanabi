package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	smokeRun   = 2400 * time.Millisecond
	smokeScale = 0.16
	smokeDrift = 0.9
	// Below this the noise counts as clear air. Without a floor the whole text
	// is faintly grey the entire time and nothing reads as a shape moving.
	smokeFloor = 0.45
)

var smokeColour = canvas.RGB(190, 195, 205)

// smoke drifts a field of coherent noise over the text, greying whatever it
// crosses. It is the only one of these whose shape is irregular: the rest are
// bands and fronts, which repeat.
type smoke struct {
	seed float32
}

func newSmoke(_ *canvas.Canvas, rnd *rand.Rand) Effect {
	return &smoke{seed: float32(rnd.Float64() * 500)}
}

func (e *smoke) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= smokeRun {
		return false
	}
	p := float64(t) / float64(smokeRun)
	strength := math.Sin(p * math.Pi)
	z := e.seed + float32(p*smokeDrift*10)

	for y := range c.H {
		fy := float32(y) * smokeScale * cellAspect
		for x := range c.W {
			cell := c.At(x, y)
			if cell.R == ' ' {
				continue
			}
			n := float64(valueNoise3(float32(x)*smokeScale, fy, z))
			if n <= smokeFloor {
				continue
			}
			thick := (n - smokeFloor) / (1 - smokeFloor)
			cell.FG = mixToward(cell.FG, smokeColour, thick*strength)
			c.Set(x, y, cell)
		}
	}
	return true
}
