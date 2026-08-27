package canvas

import (
	"io"
	"strconv"
	"unicode/utf8"
)

const (
	csi        = "\x1b["
	sgrReset   = "\x1b[0m"
	cursorHide = "\x1b[?25l"
	cursorShow = "\x1b[?25h"
	wrapOff    = "\x1b[?7l"
	wrapOn     = "\x1b[?7h"
)

// Renderer writes frames as the difference against what the terminal already
// shows, so a frame costs bytes proportional to what moved rather than to the
// size of the grid. That ratio is what makes the output survive a slow link.
//
// Positioning is relative (CUU/CUD/CR/CUF), never absolute, so the animation
// runs in a region reserved at the cursor and leaves the scrollback above it
// intact.
type Renderer struct {
	w     io.Writer
	front *Canvas
	buf   []byte

	fg   Color
	bg   Color
	bold bool
	x    int
	y    int

	full  bool
	Bytes int64
}

func NewRenderer(w io.Writer, width, height int) *Renderer {
	return &Renderer{
		w:     w,
		front: New(width, height),
		full:  true,
	}
}

// Begin reserves the region by scrolling it into view, then returns the cursor
// to its top-left corner. Autowrap stays off for the whole run: a rune written
// in the last column must not push the cursor to the next line.
func (r *Renderer) Begin() error {
	r.buf = r.buf[:0]
	for range r.front.H - 1 {
		// Explicit CR: reading keys puts the terminal in raw mode, and that
		// turns off the driver's newline translation.
		r.buf = append(r.buf, '\r', '\n')
	}
	// The newlines left the cursor on the last row of the region; without
	// recording that, End would move down twice the distance and scroll.
	r.y = r.front.H - 1
	r.up(r.front.H - 1)
	r.buf = append(r.buf, cursorHide...)
	r.buf = append(r.buf, wrapOff...)
	return r.flush()
}

// End parks the cursor below the region so the shell prompt lands after the
// finished text instead of on top of it.
func (r *Renderer) End() error {
	r.buf = r.buf[:0]
	r.buf = append(r.buf, sgrReset...)
	r.down(r.front.H - 1 - r.y)
	r.buf = append(r.buf, '\r', '\n')
	r.buf = append(r.buf, wrapOn...)
	r.buf = append(r.buf, cursorShow...)
	return r.flush()
}

// Full forces the next Draw to repaint every cell.
func (r *Renderer) Full() {
	r.full = true
}

// Draw expects the cursor at the region origin and leaves it there.
func (r *Renderer) Draw(back *Canvas) error {
	if r.front.W != back.W || r.front.H != back.H {
		r.front.Resize(back.W, back.H)
		r.full = true
	}

	r.buf = r.buf[:0]
	r.buf = append(r.buf, sgrReset...)
	body := len(r.buf)
	r.fg, r.bg, r.bold = Default, Default, false
	r.x, r.y = 0, 0

	for y := range back.H {
		row := y * back.W
		for x := range back.W {
			cell := back.Cells[row+x]
			if !r.full && cell == r.front.Cells[row+x] {
				continue
			}
			r.moveTo(x, y)
			r.pen(cell.FG, cell.BG, cell.Bold)
			r.buf = utf8.AppendRune(r.buf, cell.R)
			r.x++
		}
	}

	r.moveTo(0, 0)
	if len(r.buf) == body {
		// Nothing moved. Even the pair of resets would be bytes spent on a
		// frame the reader cannot tell from the one before it, and a sparse
		// effect spends most of its frames here: typing changes one cell every
		// tenth of a second and is asked for sixty.
		return nil
	}
	r.buf = append(r.buf, sgrReset...)
	copy(r.front.Cells, back.Cells)
	r.full = false
	return r.flush()
}

func (r *Renderer) flush() error {
	n, err := r.w.Write(r.buf)
	r.Bytes += int64(n)
	return err
}

func (r *Renderer) moveTo(x, y int) {
	if y != r.y {
		r.down(y - r.y)
	}
	if x == r.x {
		return
	}
	// The column is always re-addressed from the start of the row: with
	// autowrap off the cursor stalls in the last column, so r.x cannot be
	// trusted as a base for a relative move.
	r.buf = append(r.buf, '\r')
	if x > 0 {
		r.buf = append(r.buf, csi...)
		r.buf = strconv.AppendInt(r.buf, int64(x), 10)
		r.buf = append(r.buf, 'C')
	}
	r.x = x
}

func (r *Renderer) down(n int) {
	if n == 0 {
		return
	}
	r.y += n
	dir := byte('B')
	if n < 0 {
		n = -n
		dir = 'A'
	}
	r.buf = append(r.buf, csi...)
	r.buf = strconv.AppendInt(r.buf, int64(n), 10)
	r.buf = append(r.buf, dir)
	r.x = -1
}

func (r *Renderer) up(n int) {
	r.down(-n)
}

func (r *Renderer) pen(fg, bg Color, bold bool) {
	if fg == r.fg && bg == r.bg && bold == r.bold {
		return
	}
	r.buf = append(r.buf, csi...)
	wrote := false
	if bold != r.bold {
		code := 22
		if bold {
			code = 1
		}
		r.buf = strconv.AppendInt(r.buf, int64(code), 10)
		wrote = true
	}
	if fg != r.fg {
		wrote = r.sep(wrote)
		r.buf = appendColor(r.buf, fg, 38, 39)
	}
	if bg != r.bg {
		r.sep(wrote)
		r.buf = appendColor(r.buf, bg, 48, 49)
	}
	r.buf = append(r.buf, 'm')
	r.fg, r.bg, r.bold = fg, bg, bold
}

func (r *Renderer) sep(wrote bool) bool {
	if wrote {
		r.buf = append(r.buf, ';')
	}
	return true
}

func appendColor(b []byte, c Color, set, unset int) []byte {
	if c == Default {
		return strconv.AppendInt(b, int64(unset), 10)
	}
	if c&palette != 0 {
		// 30-37 and 40-47 for the normal colors, 90-97 and 100-107 for the
		// bright half. set is 38 or 48, so set-8 gives the right base.
		code := set - 8 + int(c&0x7)
		if c&0x8 != 0 {
			code += 60
		}
		return strconv.AppendInt(b, int64(code), 10)
	}
	red, green, blue := c.parts()
	b = strconv.AppendInt(b, int64(set), 10)
	b = append(b, ";2;"...)
	b = strconv.AppendInt(b, int64(red), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(green), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(blue), 10)
	return b
}
