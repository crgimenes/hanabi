package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	sprayRun    = 1400 * time.Millisecond
	spraySpread = 1100 * time.Millisecond
)

// spray sends every character out from one point, each on its own line and its
// own schedule, so the text builds up from a spout rather than settling out of
// the air.
type spray struct{ *flight }

func newSpray(target *canvas.Canvas, rnd *rand.Rand) Effect {
	// Low and to the left, off the text, so the spread is visible rather than
	// the characters simply growing out of the middle.
	origin := point{x: -2, y: float64(target.H)}
	return &spray{newFlight(
		target,
		stagger(len(target.Cells), sprayRun, spraySpread, rnd),
		func(_ int, home point, p float64) point {
			e := ease(p)
			return point{
				x: origin.x + (home.x-origin.x)*e,
				y: origin.y + (home.y-origin.y)*e,
			}
		},
	)}
}
