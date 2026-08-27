package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

// Chain runs several effects over one canvas and is itself an Effect, so the
// caller draws a chain the same way it draws a single effect.
type Chain struct {
	effects []Effect
}

// NewChain keys each effect's random stream off its position in the chain, so
// how much randomness the effects before it happen to consume does not shift
// the way it animates.
func NewChain(entries []Entry, target *canvas.Canvas, seed uint64) *Chain {
	c := &Chain{effects: make([]Effect, 0, len(entries))}
	var i uint64
	for _, e := range entries {
		sub := mix(seed, i)
		// #nosec G404 -- the animation is seeded so a given -seed replays frame
		// for frame; unpredictability would be a defect here, not a safeguard.
		c.effects = append(c.effects, e.New(target, rand.New(rand.NewPCG(sub, sub^0x9e3779b97f4a7c15))))
		i++
	}
	return c
}

// Frame hands the canvas down the chain, so an effect transforms what the one
// before it produced. It is not short-circuited: every effect has to advance its
// own state, whether or not another one still has work to do.
func (c *Chain) Frame(dst *canvas.Canvas, t time.Duration) bool {
	more := false
	for _, ef := range c.effects {
		if ef.Frame(dst, t) {
			more = true
		}
	}
	return more
}
