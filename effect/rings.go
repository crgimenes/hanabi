package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	ringsRun    = 2000 * time.Millisecond
	ringsSpread = 400 * time.Millisecond
	ringsCount  = 4
	ringsTurns  = 1.6
)

// rings gather the characters onto turning circles that draw in to the text.
// The other flights travel; these orbit first, which makes the arrival read as
// a mechanism winding down rather than as a journey ending.
type rings struct{ *flight }

func newRings(target *canvas.Canvas, rnd *rand.Rand) Effect {
	centre := point{x: float64(target.W-1) / 2, y: float64(target.H-1) / 2}
	span := math.Hypot(float64(target.W), float64(target.H)*cellAspect) / 2

	radius := make([]float64, len(target.Cells))
	angle := make([]float64, len(target.Cells))
	for i := range radius {
		// Discrete radii, or the characters make a disc instead of rings.
		radius[i] = span * float64(1+rnd.IntN(ringsCount)) / ringsCount
		angle[i] = rnd.Float64() * 2 * math.Pi
	}

	return &rings{newFlight(
		target,
		stagger(len(target.Cells), ringsRun, ringsSpread, rnd),
		func(i int, home point, p float64) point {
			a := angle[i] + p*ringsTurns*2*math.Pi
			r := radius[i] * (1 - ease(p))
			on := point{
				x: centre.x + math.Cos(a)*r,
				y: centre.y + math.Sin(a)*r/cellAspect,
			}
			// The ring shrinks and the character slides off it onto its place;
			// without the second blend it would land at the centre, not home.
			e := ease(p)
			return point{x: on.x + (home.x-on.x)*e, y: on.y + (home.y-on.y)*e}
		},
	)}
}
