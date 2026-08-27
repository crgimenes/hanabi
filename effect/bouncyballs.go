package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	bouncyballsRun    = 1900 * time.Millisecond
	bouncyballsSpread = 700 * time.Millisecond
	bouncyballsDrop   = 1.5
	// Bounces before the character comes to rest. More than a few and the
	// arrival reads as a flicker rather than as settling.
	bouncyballsHops = 2.5
)

// bouncyballs drops the characters and lets them bounce before they stay put.
// rain also falls; the difference is entirely in the path, which is what the
// flight machinery exists to make cheap.
type bouncyballs struct{ *flight }

func newBouncyballs(target *canvas.Canvas, rnd *rand.Rand) Effect {
	height := float64(target.H) * bouncyballsDrop
	return &bouncyballs{newFlight(
		target,
		stagger(len(target.Cells), bouncyballsRun, bouncyballsSpread, rnd),
		func(_ int, home point, p float64) point {
			// The absolute sine gives the hops; damping by the time left makes
			// each one lower than the last, and takes the height to nothing on
			// arrival.
			left := 1 - p
			hop := math.Abs(math.Sin(p*math.Pi*bouncyballsHops)) * left * left
			return point{x: home.x, y: home.y - height*hop}
		},
	)}
}
