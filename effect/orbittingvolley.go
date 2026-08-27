package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	orbittingvolleyRun       = 2100 * time.Millisecond
	orbittingvolleySpread    = 900 * time.Millisecond
	orbittingvolleyLaunchers = 4
	orbittingvolleyOrbit     = 1.2
)

// orbittingvolley fires the characters inward from four launchers going round
// the outside. Each character leaves from wherever its launcher was at the
// moment it was fired, so the arrivals come in volleys from four directions
// instead of from one origin the way spray does.
type orbittingvolley struct{ *flight }

func newOrbittingvolley(target *canvas.Canvas, rnd *rand.Rand) Effect {
	centre := point{x: float64(target.W-1) / 2, y: float64(target.H-1) / 2}
	span := math.Hypot(float64(target.W), float64(target.H)*cellAspect) * 0.75

	from := make([]point, len(target.Cells))
	until := stagger(len(target.Cells), orbittingvolleyRun, orbittingvolleySpread, rnd)
	for i := range from {
		// Fired when its own flight began, from where its launcher was then:
		// the launcher keeps orbiting afterwards, but the shot does not.
		fired := float64(until[i]-orbittingvolleyRun) / float64(orbittingvolleySpread)
		lane := float64(i % orbittingvolleyLaunchers)
		a := lane*(2*math.Pi/orbittingvolleyLaunchers) + fired*orbittingvolleyOrbit*2*math.Pi
		from[i] = point{
			x: centre.x + math.Cos(a)*span,
			y: centre.y + math.Sin(a)*span/cellAspect,
		}
	}

	return &orbittingvolley{newFlight(target, until, func(i int, home point, p float64) point {
		e := ease(p)
		return point{
			x: from[i].x + (home.x-from[i].x)*e,
			y: from[i].y + (home.y-from[i].y)*e,
		}
	})}
}
