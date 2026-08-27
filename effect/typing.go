package effect

import (
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const (
	typeBeat    = 52 * time.Millisecond
	typeJitter  = 40 * time.Millisecond
	typeSpace   = 30 * time.Millisecond
	typePunct   = 240 * time.Millisecond
	typeNewline = 360 * time.Millisecond
	typeThink   = 620 * time.Millisecond
	typeNotice  = 210 * time.Millisecond
	typeErase   = 120 * time.Millisecond
	typeTail    = 400 * time.Millisecond
	cursorBlink = 480 * time.Millisecond

	// One in this many keystrokes. Measured against nothing but taste: often
	// enough to be seen on a short line, rare enough not to read as a stutter.
	typoOdds  = 20
	thinkOdds = 26
)

var cursorCell = canvas.Cell{R: '█', FG: canvas.Default, BG: canvas.Default}

// A typo is only convincing when the wrong key is one the finger could have hit
// on the way. A uniformly random letter reads as corruption, not as a mistake.
var qwertyNeighbours = map[rune]string{
	'a': "qwsz", 'b': "vghn", 'c': "xdfv", 'd': "serfcx", 'e': "wsdr",
	'f': "drtgvc", 'g': "ftyhbv", 'h': "gyujnb", 'i': "ujko", 'j': "huikmn",
	'k': "jiolm", 'l': "kop", 'm': "njk", 'n': "bhjm", 'o': "iklp",
	'p': "ol", 'q': "wa", 'r': "edft", 's': "awedxz", 't': "rfgy",
	'u': "yhji", 'v': "cfgb", 'w': "qase", 'x': "zsdc", 'y': "tghu",
	'z': "asx",
}

// step is the whole visible state at a moment: the first upto cells hold their
// final text, and wrong (when set) is the character sitting at cell upto,
// waiting to be noticed and erased.
type step struct {
	at    time.Duration
	upto  int
	wrong rune
}

// typing plays the text back as somebody typing it: uneven speed, pauses where
// a reader would draw breath, and mistakes that get backspaced away. Everything
// is scheduled at construction, so a frame is a binary search and the animation
// does not change with the frame rate.
type typing struct {
	w     int
	steps []step
	done  time.Duration
}

func newTyping(target *canvas.Canvas, rnd *rand.Rand) Effect {
	ty := &typing{w: max(target.W, 1)}
	at := time.Duration(0)
	speed := 1.0

	for y := range target.H {
		last := lastInked(target, y)
		for x := 0; x <= last; x++ {
			i := y*ty.w + x
			r := target.Cells[i].R

			// A slow random walk, so the typist speeds up and flags in runs
			// rather than jittering around a mean.
			speed = min(max(speed+(rnd.Float64()-0.5)*0.28, 0.55), 1.75)

			if r != ' ' && rnd.IntN(typoOdds) == 0 {
				at += ty.beat(speed, rnd)
				ty.steps = append(ty.steps, step{at: at, upto: i, wrong: typoFor(r, rnd)})
				at += typeNotice
				ty.steps = append(ty.steps, step{at: at, upto: i})
				at += typeErase
			}

			at += ty.beat(speed, rnd)
			ty.steps = append(ty.steps, step{at: at, upto: i + 1})
			at += pauseAfter(r, x == last, rnd)
		}
	}

	if len(ty.steps) > 0 {
		ty.done = ty.steps[len(ty.steps)-1].at + typeTail
	}
	return ty
}

func (ty *typing) beat(speed float64, rnd *rand.Rand) time.Duration {
	d := typeBeat + rnd.N(typeJitter)
	return time.Duration(float64(d) / speed)
}

func (ty *typing) Frame(c *canvas.Canvas, t time.Duration) bool {
	if t >= ty.done {
		return false
	}
	upto, wrong := ty.stateAt(t)
	head := upto
	if wrong != 0 {
		head++
	}
	blink := (t/cursorBlink)%2 == 0

	for y := range c.H {
		for x := range c.W {
			if x >= ty.w {
				c.Set(x, y, canvas.Blank)
				continue
			}
			i := y*ty.w + x
			switch {
			case i < upto:
			case i == upto && wrong != 0:
				cell := c.At(x, y)
				cell.R = wrong
				c.Set(x, y, cell)
			case i == head && blink:
				c.Set(x, y, cursorCell)
			default:
				c.Set(x, y, canvas.Blank)
			}
		}
	}
	return true
}

func (ty *typing) stateAt(t time.Duration) (upto int, wrong rune) {
	// Never reports a match, so the result is the count of steps already due.
	n, _ := slices.BinarySearchFunc(ty.steps, t, func(s step, at time.Duration) int {
		if s.at <= at {
			return -1
		}
		return 1
	})
	if n == 0 {
		return 0, 0
	}
	return ty.steps[n-1].upto, ty.steps[n-1].wrong
}

// lastInked reports the final column worth typing on a row. Nobody types the
// blanks that run off the end of a line, and pausing over them would read as
// the typist having stalled.
func lastInked(target *canvas.Canvas, y int) int {
	for x := target.W - 1; x >= 0; x-- {
		if target.At(x, y).R != ' ' {
			return x
		}
	}
	return -1
}

func pauseAfter(r rune, endOfLine bool, rnd *rand.Rand) time.Duration {
	if endOfLine {
		return typeNewline + rnd.N(typeNewline/2)
	}
	if strings.ContainsRune(".,;:!?", r) {
		return typePunct + rnd.N(typePunct/2)
	}
	if rnd.IntN(thinkOdds) == 0 {
		return rnd.N(typeThink)
	}
	return 0
}

func typoFor(r rune, rnd *rand.Rand) rune {
	near, ok := qwertyNeighbours[toLower(r)]
	if !ok {
		return r
	}
	wrong := rune(near[rnd.IntN(len(near))])
	if r >= 'A' && r <= 'Z' {
		return wrong - 'a' + 'A'
	}
	return wrong
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r - 'A' + 'a'
	}
	return r
}
