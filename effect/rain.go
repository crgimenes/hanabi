package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	rainRun    = 1500 * time.Millisecond
	rainSpread = 900 * time.Millisecond
	rainDrop   = 1.8
)

// rain drops every character straight down into its place, gathering speed as it
// falls. The straight line and the acceleration are all that separate it from
// the other flights.
type rain struct{ *flight }

func newRain(target *canvas.Canvas, rnd *rand.Rand) Effect {
	height := float64(target.H) * rainDrop
	return &rain{newFlight(
		target,
		stagger(len(target.Cells), rainRun, rainSpread, rnd),
		func(_ int, home point, p float64) point {
			// Squared, so it starts slowly and arrives fast, the way falling
			// looks. Linear reads as a lift, not a drop.
			left := (1 - p) * (1 - p)
			return point{x: home.x, y: home.y - height*left}
		},
	)}
}
