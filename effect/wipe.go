package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const wipeStep = 9 * time.Millisecond

type wipe struct {
	target *canvas.Canvas
}

func newWipe(target *canvas.Canvas, _ *rand.Rand) Effect {
	return &wipe{target: target}
}

func (w *wipe) Frame(dst *canvas.Canvas, t time.Duration) bool {
	edge := int(t / wipeStep)
	for y := range dst.H {
		for x := range dst.W {
			if x+y > edge {
				dst.Set(x, y, canvas.Blank)
				continue
			}
			dst.Set(x, y, w.target.At(x, y))
		}
	}
	return edge < w.target.W+w.target.H
}
