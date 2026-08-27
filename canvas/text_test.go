package canvas

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

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

// The escape sequences must not take up cells of their own: a line whose colour
// changes six times is still as wide as the characters you can see.
func TestFromTextGivesEscapeSequencesNoWidth(t *testing.T) {
	c := FromText("\x1b[31mab\x1b[0mcd\x1b[38;2;1;2;3me\n", Default)
	if c.W != 5 || c.H != 1 {
		t.Fatalf("got %dx%d, want 5x1", c.W, c.H)
	}
	if got := rowString(c, 0); got != "abcde" {
		t.Fatalf("row = %q", got)
	}
}

func TestFromTextReadsColours(t *testing.T) {
	// The forms the art in ~/dotfiles/ansi actually uses, plus 256-colour.
	c := FromText("\x1b[30;40mA\x1b[97mB\x1b[48;2;255;255;255m\x1b[38;2;0;0;0mC"+
		"\x1b[mD\x1b[38;5;9mE\x1b[38;5;196mF\x1b[38;5;244mG\n", Default)

	want := []Cell{
		{R: 'A', FG: Palette(0), BG: Palette(0)},
		{R: 'B', FG: Palette(15), BG: Palette(0)},
		{R: 'C', FG: RGB(0, 0, 0), BG: RGB(255, 255, 255)},
		{R: 'D', FG: Default, BG: Default},
		// 0-15 stay palette indices; 196 is in the 6x6x6 cube and 244 in the
		// grey ramp, and those are genuine RGB.
		{R: 'E', FG: Palette(9), BG: Default},
		{R: 'F', FG: RGB(255, 0, 0), BG: Default},
		{R: 'G', FG: RGB(128, 128, 128), BG: Default},
	}
	for x, w := range want {
		if got := c.At(x, 0); got != w {
			t.Fatalf("cell %d = %+v, want %+v", x, got, w)
		}
	}
}

// A palette colour has to reach the terminal as its own code, not as baked RGB,
// or ANSI art stops following the reader's theme.
func TestPaletteColoursSurviveTheRoundTrip(t *testing.T) {
	c := New(4, 1)
	c.Cells[0] = Cell{R: 'a', FG: Palette(1), BG: Palette(0)}
	c.Cells[1] = Cell{R: 'b', FG: Palette(15), BG: Palette(9)}
	c.Cells[2] = Cell{R: 'c', FG: RGB(1, 2, 3), BG: Default}
	c.Cells[3] = Blank

	var out strings.Builder
	r := NewRenderer(&out, 4, 1)
	err := r.Draw(c)
	if err != nil {
		t.Fatalf("Draw: %v", err)
	}
	if strings.Contains(out.String(), "38;2;") && !strings.Contains(out.String(), "38;2;1;2;3") {
		t.Fatalf("a palette colour was written as truecolor: %q", out.String())
	}

	p := newReplay(t, 4, 1)
	p.run([]byte(out.String()))
	diff := firstDiff(c, p.grid)
	if diff != "" {
		t.Fatal(diff)
	}
}

// artDirs point outside the repository on purpose, and must keep doing so.
// Some of that art is licensed, not ours to redistribute, so no file from these
// directories may be copied in as a test fixture -- committing one would publish
// it. Reading it in place on a machine that already has it is fine; the tests
// skip everywhere else, which is every CI runner.
//
// Real art is also the point: the parser is meant to be caught out by files
// someone else wrote, not by samples written to match what it already does.
var artDirs = []string{"../../../dotfiles/ansi", "../../ansigarden"}

// looksLikeArt keeps the README and the dotfiles that share those directories
// out of the corpus, without a list of extensions to keep up to date.
func looksLikeArt(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	if bytes.Contains(b, []byte{0x1b}) {
		return true
	}
	for _, r := range "\u2588\u2580\u2584\u2591\u2592\u2593" {
		if bytes.ContainsRune(b, r) {
			return true
		}
	}
	return false
}

func artCorpus(t *testing.T) map[string][]byte {
	t.Helper()
	corpus := map[string][]byte{}
	for _, dir := range artDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			b, err := os.ReadFile(dir + "/" + e.Name())
			if err != nil || !looksLikeArt(b) {
				continue
			}
			corpus[e.Name()] = b
		}
	}
	if len(corpus) == 0 {
		t.Skip("no ansi art available here")
	}
	return corpus
}

// handledSGR must track the switch in applySGR. Dropping a code the art uses is
// silent by nature -- the canvas round-trip cannot catch it, because the parser
// and the renderer then agree on the same wrong picture. Bold got through that
// way: 95 uses of SGR 1 in one file, ignored, and the art came out in the wrong
// half of the palette.
func handledSGR(n int) bool {
	switch {
	case n == 0 || n == 1 || n == 22 || n == 39 || n == 49:
		return true
	case n >= 30 && n <= 37, n >= 40 && n <= 47:
		return true
	case n >= 90 && n <= 97, n >= 100 && n <= 107:
		return true
	case n == 38 || n == 48:
		return true
	}
	return false
}

func extendedRunLength(fields []string, i int) int {
	if i+1 >= len(fields) {
		return 2
	}
	switch fields[i+1] {
	case "2":
		return 5
	case "5":
		return 3
	}
	return 2
}

func TestTheParserHandlesEverySGRCodeTheArtUses(t *testing.T) {
	sgr := regexp.MustCompile(`\x1b\[([0-9;]*)m`)
	for name, b := range artCorpus(t) {
		t.Run(name, func(t *testing.T) {
			for _, match := range sgr.FindAllSubmatch(b, -1) {
				if len(match[1]) == 0 {
					continue
				}
				fields := strings.Split(string(match[1]), ";")
				for i := 0; i < len(fields); {
					n, err := strconv.Atoi(fields[i])
					if err != nil {
						i++
						continue
					}
					// 38 and 48 swallow their own arguments. Counting those as
					// codes is how this test first accused the parser of
					// ignoring "2" and "255".
					if n == 38 || n == 48 {
						i += extendedRunLength(fields, i)
						continue
					}
					if !handledSGR(n) {
						t.Errorf("SGR %d is used by this art and applySGR ignores it", n)
					}
					i++
				}
			}
		})
	}
}

// Guards the parser against the real files rather than against what I imagined
// they contain, and closes the loop: art parsed into a canvas, rendered, and
// replayed has to come back as the same canvas. Skips where the art is absent,
// which is every CI runner.
func TestRealAnsiArtSurvivesTheRoundTrip(t *testing.T) {
	for name, b := range artCorpus(t) {
		t.Run(name, func(t *testing.T) {
			art := FromText(string(b), Default)
			if art.W < 8 || art.H < 2 {
				t.Fatalf("parsed to %dx%d, too small to be the art", art.W, art.H)
			}
			for i, cell := range art.Cells {
				if cell.R != 0x1b && cell.R != '[' {
					continue
				}
				t.Fatalf("cell %d kept part of an escape sequence: %+v", i, cell)
			}

			var out strings.Builder
			r := NewRenderer(&out, art.W, art.H)
			err := r.Draw(art)
			if err != nil {
				t.Fatalf("Draw: %v", err)
			}
			p := newReplay(t, art.W, art.H)
			p.run([]byte(out.String()))
			if diff := firstDiff(art, p.grid); diff != "" {
				t.Fatal(diff)
			}

			// Plain block art carries no escapes at all, and must round-trip
			// just the same. Only a file that does carry them has to end up
			// with colour on the canvas.
			if !bytes.Contains(b, []byte{0x1b}) {
				return
			}
			coloured := 0
			for _, cell := range art.Cells {
				if cell.FG != Default || cell.BG != Default {
					coloured++
				}
			}
			if coloured == 0 {
				t.Fatal("no cell carried a colour; the SGR parsing did nothing")
			}
		})
	}
}

func rowString(c *Canvas, y int) string {
	var b strings.Builder
	for x := range c.W {
		b.WriteRune(c.At(x, y).R)
	}
	return b.String()
}

// Cursor forward is how old art skips blanks instead of writing spaces. Dropping
// it slides the rest of the line left, which is invisible in a canvas round-trip
// because the parser and the renderer then agree on the shifted picture.
func TestFromTextHonoursCursorForward(t *testing.T) {
	c := FromText("a\x1b[3mb\x1b[3Cc\x1b[Cd\n", Default)
	if got := rowString(c, 0); got != "ab   c d" {
		t.Fatalf("row = %q, want %q", got, "ab   c d")
	}
	// The skipped cells are stepped over, not painted, so they must not carry
	// the colour that was in force.
	c = FromText("\x1b[41ma\x1b[2Cb\n", Default)
	for _, x := range []int{1, 2} {
		if got := c.At(x, 0); got.BG != Default {
			t.Fatalf("skipped cell %d was painted: %+v", x, got)
		}
	}
}
