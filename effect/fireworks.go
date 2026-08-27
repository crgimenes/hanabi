package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	fireworksRun    = 1800 * time.Millisecond
	fireworksSpread = 700 * time.Millisecond
	// Share of the journey spent climbing before the shell bursts.
	fireworksClimb = 0.42
)

// fireworks send the characters up in a shell that bursts and scatters them to
// their places. Two legs rather than one: everything else here travels in a
// single movement, and the break is what makes this read as an explosion.
type fireworks struct{ *flight }

func newFireworks(target *canvas.Canvas, rnd *rand.Rand) Effect {
	launch := point{x: float64(target.W-1) / 2, y: float64(target.H) + 2}
	burst := make([]point, len(target.Cells))
	for i := range burst {
		// Shells burst at different heights and a little off centre, or every
		// character comes out of exactly the same spot.
		burst[i] = point{
			x: launch.x + (rnd.Float64()-0.5)*float64(target.W)/3,
			y: float64(target.H)*0.25 - rnd.Float64()*float64(target.H)/4,
		}
	}
	return &fireworks{newFlight(
		target,
		stagger(len(target.Cells), fireworksRun, fireworksSpread, rnd),
		func(i int, home point, p float64) point {
			if p < fireworksClimb {
				// Climbing, and slowing as the shell runs out of push.
				c := p / fireworksClimb
				c = 1 - (1-c)*(1-c)
				return point{
					x: launch.x + (burst[i].x-launch.x)*c,
					y: launch.y + (burst[i].y-launch.y)*c,
				}
			}
			// Thrown out fast and falling into place.
			f := ease((p - fireworksClimb) / (1 - fireworksClimb))
			return point{
				x: burst[i].x + (home.x-burst[i].x)*f,
				y: burst[i].y + (home.y-burst[i].y)*f,
			}
		},
	)}
}
