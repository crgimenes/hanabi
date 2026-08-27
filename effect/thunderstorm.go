package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	thunderstormRun    = 1700 * time.Millisecond
	thunderstormSpread = 900 * time.Millisecond
	thunderstormDrop   = 1.7
	thunderstormGust   = 7.0
)

// thunderstorm drives the characters down on the wind, which gusts and slackens
// so the whole fall leans one way and then the other. rain falls straight; this
// is the same drop with weather on it.
//
// The lightning is not here on purpose: a flash is a colour, and the colour
// effects already do that better than a flight could. Chain it -- thunderstorm
// with beams over the top is the storm with the sky lighting up.
type thunderstorm struct{ *flight }

func newThunderstorm(target *canvas.Canvas, rnd *rand.Rand) Effect {
	height := float64(target.H) * thunderstormDrop
	lean := rnd.Float64() * 2 * math.Pi
	return &thunderstorm{newFlight(
		target,
		stagger(len(target.Cells), thunderstormRun, thunderstormSpread, rnd),
		func(_ int, home point, p float64) point {
			left := (1 - p) * (1 - p)
			// The gust is a function of how far the character still has to
			// fall, so it dies as the character lands instead of dropping it
			// sideways at the last moment.
			gust := math.Sin(lean+p*4) * thunderstormGust * left
			return point{x: home.x + gust, y: home.y - height*left}
		},
	)}
}
