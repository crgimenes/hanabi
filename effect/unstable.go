package effect

import (
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	unstableRun = 2200 * time.Millisecond
	// The three acts: the text sits jumbled, it blows apart, it comes back.
	unstableHold  = 0.18
	unstableBlast = 0.55
)

// unstable shows the text with its characters in each other's places, blows them
// out to the edges and lets them settle where they belong. errorcorrect also
// starts jumbled but never moves; scattered also flies home but is never wrong.
// This one is both, in three acts.
type unstable struct {
	w      int
	target []int
	angle  []float64
	buf    *canvas.Canvas
	reach  float64
}

func newUnstable(target *canvas.Canvas, rnd *rand.Rand) Effect {
	e := &unstable{
		w:     max(target.W, 1),
		angle: make([]float64, len(target.Cells)),
		buf:   canvas.New(target.W, target.H),
		reach: math.Hypot(float64(target.W), float64(target.H)*cellAspect) / 2,
	}
	for i := range e.angle {
		e.angle[i] = rnd.Float64() * 2 * math.Pi
	}

	// A permutation of the inked cells only, so the jumble moves characters
	// among places that hold characters instead of into the empty margins.
	inked := make([]int, 0, len(target.Cells))
	for i, cell := range target.Cells {
		if cell.R == ' ' {
			continue
		}
		inked = append(inked, i)
	}
	e.target = make([]int, len(target.Cells))
	for i := range e.target {
		e.target[i] = i
	}
	shuffled := slices.Clone(inked)
	rnd.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	for k, from := range inked {
		e.target[from] = shuffled[k]
	}
	return e
}

func (e *unstable) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= unstableRun {
		return false
	}
	p := float64(t) / float64(unstableRun)

	// jumble falls from one to zero across the second and third acts: the
	// characters drift from the wrong cell to the right one while they fly.
	jumble := 1.0
	push := 0.0
	switch {
	case p < unstableHold:
	case p < unstableBlast:
		push = (p - unstableHold) / (unstableBlast - unstableHold)
	default:
		back := (p - unstableBlast) / (1 - unstableBlast)
		push = 1 - ease(back)
		jumble = 1 - ease(back)
	}

	e.buf.CopyFrom(c)
	c.Fill(canvas.Blank)

	for y := range e.buf.H {
		for x := range e.buf.W {
			cell := e.buf.At(x, y)
			if cell.R == ' ' {
				continue
			}
			i := y*e.w + x
			if i >= len(e.target) {
				continue
			}
			wrong := e.target[i]
			hx := float64(x)*(1-jumble) + float64(wrong%e.w)*jumble
			hy := float64(y)*(1-jumble) + float64(wrong/e.w)*jumble
			blast := push * e.reach
			c.Set(
				int(math.Round(hx+math.Cos(e.angle[i])*blast)),
				int(math.Round(hy+math.Sin(e.angle[i])*blast/cellAspect)),
				cell,
			)
		}
	}
	return true
}
