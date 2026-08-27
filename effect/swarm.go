package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	swarmRun    = 2200 * time.Millisecond
	swarmSpread = 500 * time.Millisecond
	swarmGroups = 5
	swarmLoops  = 1.4
	// Flocks are regions of the text, not a random draw. Grouping at random
	// puts neighbours in different flocks, which is the opposite of a swarm.
	swarmBlock = 6
)

// swarm keeps the characters in flocks that wander together before letting them
// go. What makes it a swarm and not scattered is that neighbours share a path:
// characters from the same part of the text travel as one and arrive from the
// same direction.
type swarm struct{ *flight }

// swarmGroupOf puts a character in the flock that owns its part of the text.
func swarmGroupOf(x, y int) int {
	return (x/swarmBlock + y/swarmBlock*3) % swarmGroups
}

func newSwarm(target *canvas.Canvas, rnd *rand.Rand) Effect {
	centre := point{x: float64(target.W-1) / 2, y: float64(target.H-1) / 2}
	span := math.Hypot(float64(target.W), float64(target.H)*cellAspect) / 2

	// One wandering path per flock, and every character in it rides the same one.
	phase := make([]float64, swarmGroups)
	tilt := make([]float64, swarmGroups)
	for g := range phase {
		phase[g] = rnd.Float64() * 2 * math.Pi
		tilt[g] = 0.6 + rnd.Float64()
	}

	return &swarm{newFlight(
		target,
		stagger(len(target.Cells), swarmRun, swarmSpread, rnd),
		func(i int, home point, p float64) point {
			g := swarmGroupOf(int(home.x), int(home.y))
			a := phase[g] + p*swarmLoops*2*math.Pi
			// A lissajous, so the flock loops rather than circling.
			fly := point{
				x: centre.x + math.Cos(a)*span,
				y: centre.y + math.Sin(a*tilt[g])*span/cellAspect,
			}
			e := ease(p)
			return point{x: fly.x + (home.x-fly.x)*e, y: fly.y + (home.y-fly.y)*e}
		},
	)}
}
