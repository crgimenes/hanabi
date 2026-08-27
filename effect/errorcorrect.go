package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	errorcorrectRun   = 1900 * time.Millisecond
	errorcorrectPairs = 14
	// How long a pair stays lit while it is being put right.
	errorcorrectFlash = 180 * time.Millisecond
)

var errorcorrectMark = canvas.RGB(255, 90, 90)

// errorcorrect starts with pairs of characters in each other's places and puts
// them right one pair at a time, marking each as it goes. Nothing is hidden and
// nothing travels: the text is complete from the first frame and simply wrong,
// which is a different kind of wrong from every other effect here.
type errorcorrect struct {
	w     int
	pairs []swap
	done  time.Duration
}

type swap struct {
	a, b int
	at   time.Duration
}

func newErrorcorrect(target *canvas.Canvas, rnd *rand.Rand) Effect {
	e := &errorcorrect{w: max(target.W, 1)}
	inked := make([]int, 0, len(target.Cells))
	for i, cell := range target.Cells {
		if cell.R == ' ' {
			continue
		}
		inked = append(inked, i)
	}
	if len(inked) < 2 {
		return e
	}

	rnd.Shuffle(len(inked), func(i, j int) { inked[i], inked[j] = inked[j], inked[i] })
	count := min(errorcorrectPairs, len(inked)/2)
	for k := range count {
		// Corrections are spread over the run in the order the pairs were
		// drawn, so they read as a list being worked through.
		at := errorcorrectRun * time.Duration(k+1) / time.Duration(count+1)
		e.pairs = append(e.pairs, swap{a: inked[2*k], b: inked[2*k+1], at: at})
		if at+errorcorrectFlash > e.done {
			e.done = at + errorcorrectFlash
		}
	}
	return e
}

func (e *errorcorrect) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= e.done {
		return false
	}
	for _, p := range e.pairs {
		ax, ay := p.a%e.w, p.a/e.w
		bx, by := p.b%e.w, p.b/e.w
		ca, cb := c.At(ax, ay), c.At(bx, by)

		if t < p.at {
			c.Set(ax, ay, cb)
			c.Set(bx, by, ca)
			continue
		}
		if t >= p.at+errorcorrectFlash {
			continue
		}
		// Just corrected: leave the characters where they belong and mark them,
		// so the reader sees which pair was the wrong one.
		ca.FG, cb.FG = errorcorrectMark, errorcorrectMark
		c.Set(ax, ay, ca)
		c.Set(bx, by, cb)
	}
	return true
}
