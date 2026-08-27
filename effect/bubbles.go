package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	bubblesRun    = 1900 * time.Millisecond
	bubblesSpread = 800 * time.Millisecond
	// Kept low on purpose: starting far below the text means the bubble is only
	// visible for the last of its rise, by which point the sway has damped away
	// and it goes up in a straight line.
	bubblesRise = 0.8
	bubblesSway = 5.0
)

// bubbles float up from below, wandering sideways on the way. The sway is what
// separates them from rain running backwards: a bubble does not travel in a
// straight line.
type bubbles struct{ *flight }

func newBubbles(target *canvas.Canvas, rnd *rand.Rand) Effect {
	rise := float64(target.H) * bubblesRise
	phase := make([]float64, len(target.Cells))
	for i := range phase {
		phase[i] = rnd.Float64() * 2 * math.Pi
	}
	return &bubbles{newFlight(
		target,
		stagger(len(target.Cells), bubblesRun, bubblesSpread, rnd),
		func(i int, home point, p float64) point {
			left := 1 - p
			// The sway dies away with the rise, so the bubble arrives still.
			return point{
				x: home.x + math.Sin(phase[i]+p*6)*bubblesSway*left,
				y: home.y + rise*left,
			}
		},
	)}
}
