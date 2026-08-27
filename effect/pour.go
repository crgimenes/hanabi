package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	pourRun    = 1800 * time.Millisecond
	pourJitter = 120 * time.Millisecond
)

// pour fills the text from the bottom up, characters streaming in from the left.
// rain arrives in no order at all; here the order is the effect -- the lower
// rows are already sitting there while the upper ones are still coming.
type pour struct{ *flight }

func newPour(target *canvas.Canvas, rnd *rand.Rand) Effect {
	until := make([]time.Duration, len(target.Cells))
	rows := max(target.H, 1)
	for i := range until {
		// Bottom rows first, so the text stacks up the way liquid would.
		fromBottom := rows - 1 - i/max(target.W, 1)
		until[i] = pourRun*time.Duration(fromBottom+1)/time.Duration(rows+1) + rnd.N(pourJitter)
	}
	entry := -float64(target.W) / 3
	return &pour{newFlight(target, until, func(_ int, home point, p float64) point {
		return point{x: entry + (home.x-entry)*ease(p), y: home.y}
	})}
}
