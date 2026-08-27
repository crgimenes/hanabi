package canvas

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// replay applies the renderer's own output to a grid. It understands only the
// sequences Draw emits and fails on anything else, so a new sequence cannot
// reach a real terminal without a test covering it first.
type replay struct {
	t    *testing.T
	grid *Canvas
	fg   Color
	bg   Color
	bold bool
	x    int
	y    int
}

func newReplay(t *testing.T, w, h int) *replay {
	t.Helper()
	return &replay{t: t, grid: New(w, h), fg: Default, bg: Default}
}

func (p *replay) run(b []byte) {
	p.t.Helper()
	for len(b) > 0 {
		if b[0] == '\r' {
			p.x = 0
			b = b[1:]
			continue
		}
		if b[0] == '\n' {
			p.y++
			b = b[1:]
			continue
		}
		if b[0] != 0x1b {
			r, n := utf8.DecodeRune(b)
			p.put(r)
			b = b[n:]
			continue
		}
		b = p.escape(b)
	}
}

func (p *replay) escape(b []byte) []byte {
	if len(b) < 3 || b[1] != '[' {
		p.t.Fatalf("replay: not a CSI sequence at %q", clip(b))
	}
	end := 2
	for end < len(b) && !isFinal(b[end]) {
		end++
	}
	if end == len(b) {
		p.t.Fatalf("replay: unterminated sequence at %q", clip(b))
	}
	params, final := string(b[2:end]), b[end]
	rest := b[end+1:]

	switch final {
	case 'A':
		p.y -= p.count(params)
	case 'B':
		p.y += p.count(params)
	case 'C':
		p.x += p.count(params)
	case 'm':
		p.sgr(params)
	case 'l', 'h':
		// Mode changes come from Begin and End, not from a frame.
		p.t.Fatalf("replay: unexpected mode change %q inside a frame", params)
	default:
		p.t.Fatalf("replay: unsupported final byte %q", final)
	}
	return rest
}

func (p *replay) count(params string) int {
	n, err := strconv.Atoi(params)
	if err != nil {
		p.t.Fatalf("replay: bad numeric parameter %q", params)
	}
	if n == 0 {
		p.t.Fatalf("replay: zero-step cursor move is wasted output")
	}
	return n
}

func (p *replay) sgr(params string) {
	if params == "0" || params == "" {
		p.fg, p.bg, p.bold = Default, Default, false
		return
	}
	f := strings.Split(params, ";")
	for i := 0; i < len(f); {
		switch f[i] {
		case "1":
			p.bold = true
			i++
		case "22":
			p.bold = false
			i++
		case "39":
			p.fg = Default
			i++
		case "49":
			p.bg = Default
			i++
		case "30", "31", "32", "33", "34", "35", "36", "37",
			"90", "91", "92", "93", "94", "95", "96", "97":
			p.fg = Palette(paletteIndex(f[i]))
			i++
		case "40", "41", "42", "43", "44", "45", "46", "47",
			"100", "101", "102", "103", "104", "105", "106", "107":
			p.bg = Palette(paletteIndex(f[i]))
			i++
		case "38", "48":
			if i+4 >= len(f) || f[i+1] != "2" {
				p.t.Fatalf("replay: truncated truecolor run %q", params)
			}
			c := RGB(p.byteAt(f[i+2]), p.byteAt(f[i+3]), p.byteAt(f[i+4]))
			if f[i] == "38" {
				p.fg = c
			} else {
				p.bg = c
			}
			i += 5
		default:
			p.t.Fatalf("replay: unsupported SGR parameter %q", f[i])
		}
	}
}

// paletteIndex undoes the 30/40/90/100 bases the renderer writes.
func paletteIndex(s string) int {
	n, _ := strconv.Atoi(s)
	switch {
	case n >= 90 && n <= 97:
		return n - 90 + 8
	case n >= 100 && n <= 107:
		return n - 100 + 8
	case n >= 40 && n <= 47:
		return n - 40
	}
	return n - 30
}

func (p *replay) byteAt(s string) uint8 {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 255 {
		p.t.Fatalf("replay: bad color component %q", s)
	}
	return uint8(n)
}

func (p *replay) put(r rune) {
	if p.x < 0 || p.y < 0 || p.x >= p.grid.W || p.y >= p.grid.H {
		p.t.Fatalf("replay: wrote %q outside the region at (%d,%d)", r, p.x, p.y)
	}
	p.grid.Cells[p.y*p.grid.W+p.x] = Cell{R: r, Bold: p.bold, FG: p.fg, BG: p.bg}
	// Autowrap is off for the whole run, so the cursor stalls in the last
	// column instead of moving to the next row.
	if p.x < p.grid.W-1 {
		p.x++
	}
}

func isFinal(b byte) bool {
	return b >= 0x40 && b <= 0x7e
}

func clip(b []byte) string {
	if len(b) > 16 {
		b = b[:16]
	}
	return string(b)
}

func randomCanvas(rnd *rand.Rand, w, h int) *Canvas {
	c := New(w, h)
	glyphs := []rune("abcdefgh #.*")
	colors := []Color{Default, RGB(255, 0, 0), RGB(0, 128, 255), RGB(9, 9, 9)}
	for i := range c.Cells {
		c.Cells[i] = Cell{
			R:    glyphs[rnd.IntN(len(glyphs))],
			Bold: rnd.IntN(2) == 0,
			FG:   colors[rnd.IntN(len(colors))],
			BG:   colors[rnd.IntN(len(colors))],
		}
	}
	return c
}

// The renderer only ever sends a difference, so correctness cannot be read off
// a single frame: the terminal's state is the accumulation of every frame sent
// so far. The replay grid carries that state across frames the same way.
func TestDrawReplaysToTheSameGrid(t *testing.T) {
	const w, h, frames = 17, 6, 40
	rnd := rand.New(rand.NewPCG(7, 11))
	var out bytes.Buffer
	r := NewRenderer(&out, w, h)
	p := newReplay(t, w, h)

	for f := range frames {
		want := randomCanvas(rnd, w, h)
		out.Reset()
		err := r.Draw(want)
		if err != nil {
			t.Fatalf("frame %d: Draw: %v", f, err)
		}
		p.x, p.y = 0, 0
		p.run(out.Bytes())
		if p.x != 0 || p.y != 0 {
			t.Fatalf("frame %d: cursor left at (%d,%d), want the region origin", f, p.x, p.y)
		}
		diff := firstDiff(want, p.grid)
		if diff != "" {
			t.Fatalf("frame %d: %s", f, diff)
		}
	}
}

func TestDrawSendsNothingForAnUnchangedFrame(t *testing.T) {
	rnd := rand.New(rand.NewPCG(3, 5))
	c := randomCanvas(rnd, 12, 4)
	var out bytes.Buffer
	r := NewRenderer(&out, 12, 4)

	err := r.Draw(c)
	if err != nil {
		t.Fatalf("first Draw: %v", err)
	}
	first := out.Len()

	out.Reset()
	err = r.Draw(c)
	if err != nil {
		t.Fatalf("second Draw: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a frame identical to the last one cost %d bytes: %q (the first cost %d)",
			out.Len(), out.String(), first)
	}
}

func TestDrawOneChangedCellIsCheap(t *testing.T) {
	rnd := rand.New(rand.NewPCG(13, 17))
	c := randomCanvas(rnd, 80, 24)
	var out bytes.Buffer
	r := NewRenderer(&out, 80, 24)
	err := r.Draw(c)
	if err != nil {
		t.Fatalf("first Draw: %v", err)
	}

	c.Set(40, 12, Cell{R: 'Z', FG: RGB(1, 2, 3), BG: Default})
	out.Reset()
	err = r.Draw(c)
	if err != nil {
		t.Fatalf("second Draw: %v", err)
	}
	// A whole repaint of this grid runs past 20kB; one cell must not.
	if out.Len() > 64 {
		t.Fatalf("one changed cell cost %d bytes: %q", out.Len(), out.String())
	}
}

func firstDiff(want, got *Canvas) string {
	for y := range want.H {
		for x := range want.W {
			a, b := want.At(x, y), got.At(x, y)
			if a == b {
				continue
			}
			return fmt.Sprintf("cell (%d,%d) = %+v, want %+v", x, y, b, a)
		}
	}
	return ""
}

// Begin and End frame the whole run: whatever Begin scrolls into view, End has
// to walk back out of, or the shell prompt lands on top of the text. The error
// path matters as much as the happy one, since End runs deferred even when the
// run never drew a frame.
func TestBeginAndEndLeaveTheCursorBelowTheRegion(t *testing.T) {
	for _, h := range []int{1, 2, 7} {
		for _, drew := range []bool{false, true} {
			var out bytes.Buffer
			r := NewRenderer(&out, 10, h)
			err := r.Begin()
			if err != nil {
				t.Fatalf("h=%d: Begin: %v", h, err)
			}
			if drew {
				err = r.Draw(New(10, h))
				if err != nil {
					t.Fatalf("h=%d: Draw: %v", h, err)
				}
			}
			err = r.End()
			if err != nil {
				t.Fatalf("h=%d: End: %v", h, err)
			}
			got := verticalTravel(t, out.Bytes())
			if got != h {
				t.Fatalf("h=%d drew=%v: cursor travelled %d rows, want %d", h, drew, got, h)
			}
		}
	}
}

// verticalTravel sums every newline and CUU/CUD in a byte stream.
func verticalTravel(t *testing.T, b []byte) int {
	t.Helper()
	rows := 0
	for i := 0; i < len(b); {
		if b[i] == '\n' {
			rows++
			i++
			continue
		}
		if b[i] != 0x1b {
			i++
			continue
		}
		end := i + 2
		for end < len(b) && !isFinal(b[end]) {
			end++
		}
		if end >= len(b) {
			t.Fatalf("unterminated sequence at %q", clip(b[i:]))
		}
		n, err := strconv.Atoi(string(b[i+2 : end]))
		switch {
		case err != nil:
			// A non-numeric parameter is a mode change, not a move.
		case b[end] == 'A':
			rows -= n
		case b[end] == 'B':
			rows += n
		}
		i = end + 1
	}
	return rows
}

// A resize is the one case where the diff has nothing to diff against: the
// previous frame describes a grid that no longer exists. Resetting the front
// buffer is not enough -- a cell that is blank in both buffers gets skipped
// while the terminal still shows what the older, larger frame put there. Only
// a full repaint clears it.
func TestDrawRepaintsInFullAfterAResize(t *testing.T) {
	const maxW, maxH = 20, 5
	var out bytes.Buffer
	r := NewRenderer(&out, maxW, maxH)
	// Stands in for the terminal, so it keeps what earlier frames left behind
	// instead of starting clean at every size.
	p := newReplay(t, maxW, maxH)

	steps := []struct {
		w, h int
		cell Cell
	}{
		{maxW, maxH, Cell{R: 'X', FG: RGB(200, 0, 0), BG: Default}},
		{10, 3, Blank},
		{14, 5, Cell{R: 'o', FG: Default, BG: RGB(0, 0, 90)}},
		{1, 1, Blank},
		{maxW, maxH, Cell{R: '#', FG: Default, BG: Default}},
	}

	for i, s := range steps {
		want := New(s.w, s.h)
		want.Fill(s.cell)
		out.Reset()
		err := r.Draw(want)
		if err != nil {
			t.Fatalf("step %d (%dx%d): Draw: %v", i, s.w, s.h, err)
		}
		p.x, p.y = 0, 0
		p.run(out.Bytes())
		for y := range s.h {
			for x := range s.w {
				got := p.grid.At(x, y)
				if got == s.cell {
					continue
				}
				t.Fatalf("step %d (%dx%d): cell (%d,%d) = %+v, want %+v",
					i, s.w, s.h, x, y, got, s.cell)
			}
		}
	}
}
