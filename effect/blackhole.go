package effect

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	blackholeRun    = 2000 * time.Millisecond
	blackholeSpread = 500 * time.Millisecond
	// Share of the journey spent falling in. The rest is the way back out.
	blackholeFall = 0.45
)

// blackhole pulls the whole text into one point and lets it back out. Alone
// among the flights it leaves from home rather than arriving from somewhere
// else, which makes the text vanish before it returns.
type blackhole struct{ *flight }

func newBlackhole(target *canvas.Canvas, rnd *rand.Rand) Effect {
	centre := point{x: float64(target.W-1) / 2, y: float64(target.H-1) / 2}
	// A turn each character adds on the way out, so the text comes back with a
	// twist instead of retracing the line it fell along.
	spin := make([]float64, len(target.Cells))
	for i := range spin {
		spin[i] = (rnd.Float64() - 0.5) * 2.4
	}

	return &blackhole{newFlight(
		target,
		stagger(len(target.Cells), blackholeRun, blackholeSpread, rnd),
		func(i int, home point, p float64) point {
			pull := p / blackholeFall
			if p > blackholeFall {
				pull = 1 - (p-blackholeFall)/(1-blackholeFall)
			}
			pull = ease(min(pull, 1))

			dx, dy := home.x-centre.x, (home.y-centre.y)*cellAspect
			a := math.Atan2(dy, dx) + spin[i]*pull
			r := math.Hypot(dx, dy) * (1 - pull)
			return point{
				x: centre.x + math.Cos(a)*r,
				y: centre.y + math.Sin(a)*r/cellAspect,
			}
		},
	)}
}
