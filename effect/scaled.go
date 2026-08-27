package effect

import (
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

// Scaled plays an effect at a multiple of its own pace: 2 runs it twice as
// fast, 0.5 at half speed. It scales the clock the effect is shown rather than
// touching the effect, so it works on any of them, chains included, and the
// animation stays the pure function of time it always was.
func Scaled(e Effect, factor float64) Effect {
	if factor == 1 || factor <= 0 {
		return e
	}
	return scaled{inner: e, factor: factor}
}

type scaled struct {
	inner  Effect
	factor float64
}

func (s scaled) Frame(c *canvas.Canvas, t time.Duration) bool {
	return s.inner.Frame(c, time.Duration(float64(t)*s.factor))
}
