// Package canvas holds the cell grid the effects draw into and the renderer
// that turns it into terminal output.
package canvas

import "strings"

// Default renders with the terminal's own foreground or background instead of
// an explicit color.
const Default Color = 1 << 24

// Color is 0xRRGGBB, or Default.
type Color uint32

func RGB(r, g, b uint8) Color {
	return Color(r)<<16 | Color(g)<<8 | Color(b)
}

func (c Color) parts() (r, g, b uint8) {
	r = uint8((c >> 16) & 0xff)
	g = uint8((c >> 8) & 0xff)
	b = uint8(c & 0xff)
	return r, g, b
}

type Cell struct {
	R  rune
	FG Color
	BG Color
}

var Blank = Cell{R: ' ', FG: Default, BG: Default}

type Canvas struct {
	W     int
	H     int
	Cells []Cell
}

func New(w, h int) *Canvas {
	c := &Canvas{}
	c.Resize(w, h)
	return c
}

func (c *Canvas) Resize(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	c.W, c.H = w, h
	n := w * h
	if cap(c.Cells) < n {
		c.Cells = make([]Cell, n)
	}
	c.Cells = c.Cells[:n]
	c.Fill(Blank)
}

func (c *Canvas) Fill(cell Cell) {
	for i := range c.Cells {
		c.Cells[i] = cell
	}
}

// At and Set clamp to the grid so an effect can work in coordinates that run
// off the canvas without every caller repeating the bounds check.
func (c *Canvas) At(x, y int) Cell {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return Blank
	}
	return c.Cells[y*c.W+x]
}

func (c *Canvas) Set(x, y int, cell Cell) {
	if x < 0 || y < 0 || x >= c.W || y >= c.H {
		return
	}
	c.Cells[y*c.W+x] = cell
}

const tabWidth = 4

// FromText lays plain text out on a canvas wide enough for its longest line.
// Escape sequences in s are not interpreted; they would occupy cells that draw
// nothing and shift the rest of the line.
func FromText(s string, fg Color) *Canvas {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")

	lines := strings.Split(s, "\n")
	rows := make([][]rune, len(lines))
	w := 0
	for i, ln := range lines {
		rows[i] = expandTabs(ln)
		if len(rows[i]) > w {
			w = len(rows[i])
		}
	}

	c := New(w, len(rows))
	for y, row := range rows {
		for x, r := range row {
			c.Cells[y*w+x] = Cell{R: r, FG: fg, BG: Default}
		}
	}
	return c
}

func expandTabs(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r != '\t' {
			out = append(out, r)
			continue
		}
		for range tabWidth - len(out)%tabWidth {
			out = append(out, ' ')
		}
	}
	return out
}
