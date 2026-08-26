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
		p.fg, p.bg = Default, Default
		return
	}
	f := strings.Split(params, ";")
	for i := 0; i < len(f); {
		switch f[i] {
		case "39":
			p.fg = Default
			i++
		case "49":
			p.bg = Default
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
	p.grid.Cells[p.y*p.grid.W+p.x] = Cell{R: r, FG: p.fg, BG: p.bg}
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
			R:  glyphs[rnd.IntN(len(glyphs))],
			FG: colors[rnd.IntN(len(colors))],
			BG: colors[rnd.IntN(len(colors))],
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
	if out.Len() >= first {
		t.Fatalf("repeat frame cost %d bytes, first frame cost %d; the diff is not working", out.Len(), first)
	}
	if bytes.ContainsRune(out.Bytes(), 'a') {
		t.Fatalf("repeat frame carried cell content: %q", out.String())
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

func TestFromTextExpandsTabsAndPadsShortLines(t *testing.T) {
	c := FromText("\ufeffab\n\tc\n", Default)
	if c.W != 5 || c.H != 2 {
		t.Fatalf("got %dx%d, want 5x2", c.W, c.H)
	}
	if got := rowString(c, 0); got != "ab   " {
		t.Fatalf("row 0 = %q", got)
	}
	if got := rowString(c, 1); got != "    c" {
		t.Fatalf("row 1 = %q", got)
	}
}

func rowString(c *Canvas, y int) string {
	var b strings.Builder
	for x := range c.W {
		b.WriteRune(c.At(x, y).R)
	}
	return b.String()
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
