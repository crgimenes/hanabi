package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const randomsequenceRun = 1300 * time.Millisecond

// randomsequence prints the text in no order at all: each cell simply appears
// when its turn comes. decrypt has the same schedule but scrambles what it shows
// until then; here there is nothing to see until the character arrives, which
// makes it the plainest reveal in the set.
type randomsequence struct {
	w  int
	at []time.Duration
}

func newRandomsequence(target *canvas.Canvas, rnd *rand.Rand) Effect {
	e := &randomsequence{
		w:  max(target.W, 1),
		at: make([]time.Duration, len(target.Cells)),
	}
	for i := range e.at {
		e.at[i] = rnd.N(randomsequenceRun)
	}
	return e
}

func (e *randomsequence) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= randomsequenceRun {
		return false
	}
	for y := range c.H {
		for x := range c.W {
			i := y*e.w + x
			if i >= len(e.at) || t >= e.at[i] {
				continue
			}
			c.Set(x, y, canvas.Blank)
		}
	}
	return true
}
