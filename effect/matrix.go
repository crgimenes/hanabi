package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	matrixTrail   = 6
	matrixSpread  = 900 * time.Millisecond
	matrixMinStep = 28 * time.Millisecond
	matrixJitter  = 34 * time.Millisecond
)

var (
	matrixHead = canvas.RGB(210, 255, 215)
	matrixBody = canvas.RGB(0, 190, 90)
	matrixTail = canvas.RGB(0, 95, 45)
)

// matrix drops a column of glyphs down each column, revealing the text behind
// the head as it passes. It substitutes and hides, never moves, so it layers
// with anything.
type matrix struct {
	delay []time.Duration
	step  []time.Duration
	done  time.Duration
}

func newMatrix(target *canvas.Canvas, rnd *rand.Rand) Effect {
	m := &matrix{
		delay: make([]time.Duration, target.W),
		step:  make([]time.Duration, target.W),
	}
	for x := range m.delay {
		m.delay[x] = rnd.N(matrixSpread)
		m.step[x] = matrixMinStep + rnd.N(matrixJitter)
		end := m.delay[x] + m.step[x]*time.Duration(target.H+matrixTrail+1)
		if end > m.done {
			m.done = end
		}
	}
	return m
}

func (m *matrix) Frame(c *canvas.Canvas, t time.Duration) bool {
	// #nosec G115 -- t is elapsed time and x is a slice index; neither is ever
	// negative, so the conversions cannot wrap.
	tick := uint64(t / matrixMinStep)
	for x := range c.W {
		if x >= len(m.delay) {
			continue
		}
		head := -1
		if t >= m.delay[x] {
			head = int((t - m.delay[x]) / m.step[x])
		}
		for y := range c.H {
			switch {
			case y > head:
				c.Set(x, y, canvas.Blank)
			case y > head-matrixTrail:
				cell := c.At(x, y)
				cell.R = glyph(y*c.W+x, tick)
				cell.FG = matrixShade(head - y)
				c.Set(x, y, cell)
			}
		}
	}
	return t < m.done
}

func matrixShade(behind int) canvas.Color {
	switch {
	case behind == 0:
		return matrixHead
	case behind < matrixTrail/2:
		return matrixBody
	}
	return matrixTail
}
