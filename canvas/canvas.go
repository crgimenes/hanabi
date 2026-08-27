// Package canvas holds the cell grid the effects draw into and the renderer
// that turns it into terminal output.
package canvas

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// Default renders with the terminal's own foreground or background.
	Default Color = 1 << 24
	// palette tags one of the 16 named colors. ANSI art written against the
	// named colors means them to follow the reader's theme, so the index is
	// carried through to the output instead of being resolved to fixed RGB.
	palette Color = 1 << 25
)

// Color is 0xRRGGBB, a palette index, or Default.
type Color uint32

func RGB(r, g, b uint8) Color {
	return Color(r)<<16 | Color(g)<<8 | Color(b)
}

// Palette names one of the 16 ANSI colors, 0-7 normal and 8-15 bright.
func Palette(i int) Color {
	return palette | Color(i&0xf)
}

// RGB reports the colour's components, and whether it has any: Default and a
// palette index name a colour the terminal chooses, so there is nothing to read.
func (c Color) RGB() (r, g, b uint8, ok bool) {
	if c == Default || c&palette != 0 {
		return 0, 0, 0, false
	}
	r, g, b = c.parts()
	return r, g, b, true
}

func (c Color) parts() (r, g, b uint8) {
	r = uint8((c >> 16) & 0xff)
	g = uint8((c >> 8) & 0xff)
	b = uint8(c & 0xff)
	return r, g, b
}

type Cell struct {
	R rune
	// Bold is carried rather than folded into FG: on the named colors a
	// terminal renders it as the bright half of the palette, and ANSI art
	// relies on that to double the number of colors it can use.
	Bold bool
	FG   Color
	BG   Color
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

// CopyFrom fills c with the top-left corner of src, blanking whatever src does
// not reach. It is how each frame starts from the finished text before the
// effects transform it.
func (c *Canvas) CopyFrom(src *Canvas) {
	for y := range c.H {
		row := y * c.W
		for x := range c.W {
			c.Cells[row+x] = src.At(x, y)
		}
	}
}

const (
	tabWidth = 4
	// A line wider than this is a malformed file, not art.
	maxLineWidth = 4096
)

// FromText lays text out on a canvas wide enough for its longest line, honouring
// the colour and cursor-forward sequences that ANSI art carries. Input must be
// UTF-8; the caller checks that, because guessing an encoding here would only
// move the mojibake. Any other escape sequence is dropped: drawing it would put
// cells on screen that show nothing and shift the rest of the line.
func FromText(s string, fg Color) *Canvas {
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")

	blank := Cell{R: ' ', FG: fg, BG: Default}
	pen := blank
	rows := [][]Cell{{}}

	for i := 0; i < len(s); {
		switch s[i] {
		case '\n':
			rows = append(rows, []Cell{})
			i++
		case '\r':
			i++
		case 0x1b:
			params, final, next := scanEscape(s, i)
			i = next
			switch final {
			case 'm':
				pen = applySGR(params, pen, blank)
			case 'C':
				// Cursor forward. Art uses it to skip blanks instead of writing
				// spaces, and it is not painting: the cells it steps over keep
				// the default colours, not the pen's.
				last := len(rows) - 1
				for range cursorSteps(params) {
					rows[last] = append(rows[last], blank)
				}
			}
		case '\t':
			last := len(rows) - 1
			for range tabWidth - len(rows[last])%tabWidth {
				rows[last] = append(rows[last], Cell{R: ' ', Bold: pen.Bold, FG: pen.FG, BG: pen.BG})
			}
			i++
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			i += n
			width := runeWidth(r)
			if width == 0 {
				// A combining mark has no column of its own. Our cells hold one
				// rune each, so it is dropped: "e" without its accent still puts
				// the rest of the line where it belongs, which a shifted row
				// does not.
				continue
			}
			last := len(rows) - 1
			rows[last] = append(rows[last], Cell{R: r, Bold: pen.Bold, FG: pen.FG, BG: pen.BG})
			if width == 2 {
				rows[last] = append(rows[last], Cell{R: continuation, FG: pen.FG, BG: pen.BG})
			}
		}
	}

	w := 0
	for _, row := range rows {
		if len(row) > w {
			w = len(row)
		}
	}

	c := New(w, len(rows))
	for y, row := range rows {
		copy(c.Cells[y*w:], row)
	}
	return c
}

// scanEscape consumes one escape sequence starting at i and reports its
// parameters, its final byte, and where the text resumes. A sequence that runs
// off the end of s is swallowed whole.
func scanEscape(s string, i int) (params string, final byte, next int) {
	if i+1 >= len(s) || s[i+1] != '[' {
		return "", 0, i + 1
	}
	end := i + 2
	for end < len(s) && (s[end] < 0x40 || s[end] > 0x7e) {
		end++
	}
	if end >= len(s) {
		return "", 0, len(s)
	}
	return s[i+2 : end], s[end], end + 1
}

// cursorSteps reads the count of a cursor-movement sequence, which defaults to
// one when the parameter is absent, and is capped so a malformed file cannot
// ask for a row of a billion cells.
func cursorSteps(params string) int {
	if params == "" {
		return 1
	}
	n, err := strconv.Atoi(params)
	if err != nil || n < 1 {
		return 0
	}
	return min(n, maxLineWidth)
}

func applySGR(params string, pen, blank Cell) Cell {
	if params == "" {
		return blank
	}
	f := strings.Split(params, ";")
	for i := 0; i < len(f); {
		n, err := strconv.Atoi(f[i])
		if err != nil {
			i++
			continue
		}
		switch {
		case n == 0:
			pen = blank
			i++
		case n == 1:
			pen.Bold = true
			i++
		case n == 22:
			pen.Bold = false
			i++
		case n == 39:
			pen.FG = blank.FG
			i++
		case n == 49:
			pen.BG = Default
			i++
		case n >= 30 && n <= 37:
			pen.FG = Palette(n - 30)
			i++
		case n >= 90 && n <= 97:
			pen.FG = Palette(n - 90 + 8)
			i++
		case n >= 40 && n <= 47:
			pen.BG = Palette(n - 40)
			i++
		case n >= 100 && n <= 107:
			pen.BG = Palette(n - 100 + 8)
			i++
		case n == 38 || n == 48:
			c, next, ok := scanExtended(f, i)
			i = next
			if !ok {
				continue
			}
			if n == 38 {
				pen.FG = c
			} else {
				pen.BG = c
			}
		default:
			i++
		}
	}
	return pen
}

// scanExtended reads the tail of a 38/48 run: ";2;R;G;B" for truecolor or
// ";5;N" for the 256-colour cube.
func scanExtended(f []string, i int) (c Color, next int, ok bool) {
	if i+1 >= len(f) {
		return 0, len(f), false
	}
	switch f[i+1] {
	case "2":
		if i+4 >= len(f) {
			return 0, len(f), false
		}
		return RGB(component(f[i+2]), component(f[i+3]), component(f[i+4])), i + 5, true
	case "5":
		if i+2 >= len(f) {
			return 0, len(f), false
		}
		n, err := strconv.Atoi(f[i+2])
		if err != nil || n < 0 || n > 255 {
			return 0, i + 3, false
		}
		return xterm256(n), i + 3, true
	}
	return 0, i + 2, false
}

func component(s string) uint8 {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 255 {
		return 0
	}
	return uint8(n)
}

// xterm256 maps a 256-colour index: 0-15 are the named colors, 16-231 a 6x6x6
// cube, 232-255 a grey ramp.
func xterm256(n int) Color {
	if n < 16 {
		return Palette(n)
	}
	if n >= 232 {
		// #nosec G115 -- n is bounded to 232..255 above, so the ramp tops out
		// at 238 and cannot wrap.
		v := uint8(8 + (n-232)*10)
		return RGB(v, v, v)
	}
	n -= 16
	steps := [6]uint8{0, 95, 135, 175, 215, 255}
	return RGB(steps[n/36], steps[n/6%6], steps[n%6])
}
