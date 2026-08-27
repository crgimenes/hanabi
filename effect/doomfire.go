package effect

import (
	"math/rand/v2"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

// The PSX DOOM fire: an intensity grid fed from below, each cell taking the
// value under it minus a random decay, drifted a step sideways. Algorithm and
// palette lifted from crgimenes/DOOMFire (main.go, MIT), which renders it to a
// window; here the flames climb over the text and die down to reveal it.
const (
	doomfireStep = 45 * time.Millisecond
	doomfireTopI = 36
)

// firePalette is the 37-entry ramp from the original, black to white.
var firePalette = []canvas.Color{
	canvas.RGB(7, 7, 7), canvas.RGB(31, 7, 7), canvas.RGB(47, 15, 7),
	canvas.RGB(71, 15, 7), canvas.RGB(87, 23, 7), canvas.RGB(103, 31, 7),
	canvas.RGB(119, 31, 7), canvas.RGB(143, 39, 7), canvas.RGB(159, 47, 7),
	canvas.RGB(175, 63, 7), canvas.RGB(191, 71, 7), canvas.RGB(199, 71, 7),
	canvas.RGB(223, 79, 7), canvas.RGB(223, 87, 7), canvas.RGB(223, 87, 7),
	canvas.RGB(215, 95, 7), canvas.RGB(215, 95, 7), canvas.RGB(215, 103, 15),
	canvas.RGB(207, 111, 15), canvas.RGB(207, 119, 15), canvas.RGB(207, 127, 15),
	canvas.RGB(207, 135, 23), canvas.RGB(199, 135, 23), canvas.RGB(199, 143, 23),
	canvas.RGB(199, 151, 31), canvas.RGB(191, 159, 31), canvas.RGB(191, 159, 31),
	canvas.RGB(191, 167, 39), canvas.RGB(191, 167, 39), canvas.RGB(191, 175, 47),
	canvas.RGB(183, 175, 47), canvas.RGB(183, 183, 47), canvas.RGB(183, 183, 55),
	canvas.RGB(207, 207, 111), canvas.RGB(223, 223, 159), canvas.RGB(239, 239, 199),
	canvas.RGB(255, 255, 255),
}

// doomfire is a simulation rather than a pure function of t: the grid at step
// N depends on the grid at step N-1. Frame advances the simulation until its
// step count matches t, so the state is a function of the step count and the
// seeded stream, and the animation stays the same at any frame rate.
type doomfire struct {
	w, h  int
	grid  []int
	rnd   *rand.Rand
	step  int
	decay int
	// feedUntil is the step the fuel is cut at; after it the fire dies from
	// below and the run ends when nothing burns, which is the target.
	feedUntil int
}

func newDoomfire(target *canvas.Canvas, rnd *rand.Rand) Effect {
	w, h := max(target.W, 1), max(target.H, 1)
	d := &doomfire{
		w:   w,
		h:   h,
		rnd: rnd,
		// One extra row below the canvas holds the fuel, so cutting it kills
		// the fire from underneath instead of leaving a pegged white bar.
		grid: make([]int, w*(h+1)),
		// The original decays about one step per row over 168 rows; a terminal
		// has a dozen. Scaled so the flames top out around the canvas height
		// instead of saturating everything white.
		decay:     max(1, (doomfireTopI*2)/(h*3)),
		feedUntil: 2*h + 24,
	}
	return d
}

func (d *doomfire) Frame(c *canvas.Canvas, t time.Duration) bool {
	want := int(t / doomfireStep)
	for d.step < want {
		d.advance()
	}

	burning := false
	for y := range d.h {
		for x := range d.w {
			i := d.grid[y*d.w+x]
			if i <= 0 {
				continue
			}
			burning = true
			cell := c.At(x, y)
			switch {
			case i >= 25:
				cell = canvas.Cell{R: '█'}
			case i >= 15:
				cell = canvas.Cell{R: '▓'}
			case i >= 8:
				cell = canvas.Cell{R: '▒'}
			case i >= 4:
				cell = canvas.Cell{R: '░'}
			}
			// Below that, only the text's colour catches the glow.
			cell.FG = firePalette[min(i, doomfireTopI)]
			cell.BG = canvas.Default
			c.Set(x, y, cell)
		}
	}
	return burning || d.step <= d.feedUntil
}

func (d *doomfire) advance() {
	d.step++
	fuel := 0
	if d.step <= d.feedUntil {
		fuel = doomfireTopI
	}
	for x := range d.w {
		d.grid[d.h*d.w+x] = fuel
	}

	// Bottom-up, as the original walks: each cell reads the row below, which
	// this pass has already refreshed.
	for y := d.h - 1; y >= 0; y-- {
		for x := range d.w {
			drift := d.rnd.IntN(2)
			from := d.grid[(y+1)*d.w+x]
			nx := x - drift
			if nx < 0 {
				continue
			}
			d.grid[y*d.w+nx] = max(from-drift*d.decay, 0)
		}
	}
}
