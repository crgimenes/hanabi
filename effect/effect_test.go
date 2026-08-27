package effect

import (
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

const sample = "hanabi\nzero dependencies\nterminal text effects\n"

// Every effect exists to end with the text readable on screen. An effect that
// reports itself finished while still showing scrambled or hidden cells leaves
// the terminal wrong in a way no other test would catch. Chains are covered as
// well as single effects: layering is where an effect that ends correctly on its
// own can still be left mangled by the one after it.
func TestChainsEndOnTheTarget(t *testing.T) {
	for _, names := range chains() {
		t.Run(strings.Join(names, "+"), func(t *testing.T) {
			target := canvas.FromText(sample, canvas.Default)
			dst := canvas.New(target.W, target.H)
			chain := build(t, names, target)

			const step = 16 * time.Millisecond
			const limit = 60 * time.Second
			elapsed := time.Duration(0)
			frames := 0
			for {
				dst.CopyFrom(target)
				more := chain.Frame(dst, elapsed)
				frames++
				if !more {
					break
				}
				elapsed += step
				if elapsed > limit {
					t.Fatalf("still running after %s of simulated time", limit)
				}
			}
			if frames < 2 {
				t.Fatal("finished on the first frame; nothing would be animated")
			}
			for y := range target.H {
				for x := range target.W {
					want, got := target.At(x, y), dst.At(x, y)
					if want == got {
						continue
					}
					t.Fatalf("after %d frames cell (%d,%d) = %+v, want %+v", frames, x, y, got, want)
				}
			}
		})
	}
}

// Each effect draws from a stream keyed to its position, so an effect earlier in
// the chain consuming a lot of randomness no longer shifts how a later one
// animates. The two chains here differ only in how greedy the first slot is.
func TestChainGivesEachSlotItsOwnStream(t *testing.T) {
	target := canvas.FromText(sample, canvas.Default)
	decrypt, ok := Get("decrypt")
	if !ok {
		t.Fatal("decrypt is not registered")
	}

	render := func(greed int) []byte {
		chain := NewChain([]Entry{greedy(greed), decrypt}, target, 1)
		dst := canvas.New(target.W, target.H)
		var out bytes.Buffer
		r := canvas.NewRenderer(&out, target.W, target.H)
		for elapsed := time.Duration(0); elapsed < 2*time.Second; elapsed += 16 * time.Millisecond {
			dst.CopyFrom(target)
			chain.Frame(dst, elapsed)
			_ = r.Draw(dst)
		}
		return out.Bytes()
	}

	thin, fat := render(1), render(500)
	if len(thin) == 0 {
		t.Fatal("nothing was drawn")
	}
	if !bytes.Equal(thin, fat) {
		t.Fatal("how much randomness the first slot took changed what the second one drew")
	}
}

// greedy draws n numbers at construction and then does nothing, which is the
// whole point: only its appetite for the stream is visible.
func greedy(n int) Entry {
	return Entry{
		Name: "greedy",
		Desc: "consumes randomness and draws nothing",
		New: func(_ *canvas.Canvas, rnd *rand.Rand) Effect {
			for range n {
				_ = rnd.Uint64()
			}
			return drawsNothing{}
		},
	}
}

type drawsNothing struct{}

func (drawsNothing) Frame(*canvas.Canvas, time.Duration) bool { return false }

// A layered effect must not undo what an earlier one in the chain did. wipe
// hides everything the sweep has not reached, so nothing downstream of it may
// put ink back into that region -- that is the whole difference between
// layering the effects and merely running the last one.
func TestLayeringPreservesTheMaskOfAnEarlierEffect(t *testing.T) {
	target := canvas.FromText(sample, canvas.Default)
	dst := canvas.New(target.W, target.H)
	alone := canvas.New(target.W, target.H)

	chain := build(t, []string{"wipe", "decrypt"}, target)
	onlyWipe := build(t, []string{"wipe"}, target)

	scrambled := 0
	for _, elapsed := range []time.Duration{0, 40 * time.Millisecond, 120 * time.Millisecond} {
		dst.CopyFrom(target)
		chain.Frame(dst, elapsed)
		alone.CopyFrom(target)
		onlyWipe.Frame(alone, elapsed)

		for y := range target.H {
			for x := range target.W {
				if alone.At(x, y) != canvas.Blank {
					if dst.At(x, y) != target.At(x, y) {
						scrambled++
					}
					continue
				}
				if dst.At(x, y) != canvas.Blank {
					t.Fatalf("t=%s: cell (%d,%d) is hidden by wipe but the chain drew %+v",
						elapsed, x, y, dst.At(x, y))
				}
			}
		}
	}
	if scrambled == 0 {
		t.Fatal("decrypt never scrambled a revealed cell; the chain ran wipe alone")
	}
}

func chains() [][]string {
	var out [][]string
	for _, a := range List() {
		out = append(out, []string{a.Name})
		for _, b := range List() {
			if a.Name == b.Name {
				continue
			}
			out = append(out, []string{a.Name, b.Name})
		}
	}
	return out
}

func build(t *testing.T, names []string, target *canvas.Canvas) *Chain {
	t.Helper()
	entries := make([]Entry, 0, len(names))
	for _, n := range names {
		e, ok := Get(n)
		if !ok {
			t.Fatalf("unknown effect %q", n)
		}
		entries = append(entries, e)
	}
	return NewChain(entries, target, 1)
}

func TestEffectsAreReproducibleFromASeed(t *testing.T) {
	for _, entry := range List() {
		t.Run(entry.Name, func(t *testing.T) {
			a := renderRun(entry, 7)
			b := renderRun(entry, 7)
			if !bytes.Equal(a, b) {
				t.Fatal("two runs with the same seed produced different output")
			}
		})
	}
}

// renderRun drives an effect through the real renderer, which is also what
// makes the byte counts in BenchmarkEffects mean something.
func renderRun(entry Entry, seed uint64) []byte {
	target := canvas.FromText(sample, canvas.Default)
	rnd := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	ef := entry.New(target, rnd)
	dst := canvas.New(target.W, target.H)

	var out bytes.Buffer
	r := canvas.NewRenderer(&out, target.W, target.H)
	elapsed := time.Duration(0)
	for {
		dst.CopyFrom(target)
		more := ef.Frame(dst, elapsed)
		_ = r.Draw(dst)
		if !more {
			return out.Bytes()
		}
		elapsed += 16 * time.Millisecond
	}
}

// Bytes per frame is the number that decides whether the animation survives a
// screen-sharing link, so it is measured rather than assumed.
func BenchmarkEffects(b *testing.B) {
	for _, entry := range List() {
		b.Run(entry.Name, func(b *testing.B) {
			var frames, total int
			for range b.N {
				out := renderRun(entry, 7)
				total += len(out)
				frames += countFrames(entry)
			}
			if frames > 0 {
				b.ReportMetric(float64(total)/float64(frames), "bytes/frame")
			}
		})
	}
}

func countFrames(entry Entry) int {
	target := canvas.FromText(sample, canvas.Default)
	rnd := rand.New(rand.NewPCG(7, 7^0x9e3779b97f4a7c15))
	ef := entry.New(target, rnd)
	dst := canvas.New(target.W, target.H)
	n := 0
	elapsed := time.Duration(0)
	for {
		n++
		dst.CopyFrom(target)
		if !ef.Frame(dst, elapsed) {
			return n
		}
		elapsed += 16 * time.Millisecond
	}
}

// slide is the one effect that reads and writes the same cells, so the walk
// direction is load-bearing: going the other way smears the leftmost column
// across the row. The end-state test cannot see that, because slide's last
// frame returns without touching the canvas at all -- an effect whose final
// frame is a no-op has its whole animation unguarded otherwise.
func TestSlideShiftsWithoutSmearing(t *testing.T) {
	target := canvas.FromText("abcdef\nghijkl\nmnopqr\n", canvas.Default)
	entry, ok := Get("slide")
	if !ok {
		t.Fatal("slide is not registered")
	}
	ef := entry.New(target, rand.New(rand.NewPCG(1, 2)))
	dst := canvas.New(target.W, target.H)

	for _, off := range []int{5, 3, 1} {
		elapsed := time.Duration(target.W-off) * (slideRun / time.Duration(target.W))
		dst.CopyFrom(target)
		ef.Frame(dst, elapsed)
		for y := range target.H {
			for x := range target.W {
				want := target.At(x-off, y)
				if got := dst.At(x, y); got != want {
					t.Fatalf("offset %d: cell (%d,%d) = %+v, want %+v", off, x, y, got, want)
				}
			}
		}
	}
}

// An effect that quietly does nothing would pass every other test here: the
// canvas starts as the text and has to end as the text, and doing nothing
// satisfies both ends.
func TestEveryEffectChangesTheCanvasWhileItRuns(t *testing.T) {
	for _, entry := range List() {
		t.Run(entry.Name, func(t *testing.T) {
			target := canvas.FromText(sample, canvas.Default)
			ef := entry.New(target, rand.New(rand.NewPCG(3, 4)))
			dst := canvas.New(target.W, target.H)

			for elapsed := time.Duration(0); elapsed < 3*time.Second; elapsed += 20 * time.Millisecond {
				dst.CopyFrom(target)
				ef.Frame(dst, elapsed)
				for i := range dst.Cells {
					if dst.Cells[i] != target.Cells[i] {
						return
					}
				}
			}
			t.Fatal("the canvas never differed from the text; the effect draws nothing")
		})
	}
}

const typed = "hello world.\nthe second line is here\nand a third\n"

func typingRun(t *testing.T, seed uint64) (*canvas.Canvas, Effect, time.Duration) {
	t.Helper()
	target := canvas.FromText(typed, canvas.Default)
	entry, ok := Get("typing")
	if !ok {
		t.Fatal("typing is not registered")
	}
	ef := entry.New(target, rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)))
	ty, ok := ef.(*typing)
	if !ok {
		t.Fatalf("typing is a %T", ef)
	}
	return target, ef, ty.done
}

// The end-state test cannot see this one: typing's last frame returns without
// touching the canvas, so a mistake that never gets backspaced away would leave
// the wrong text on screen for the whole run and still pass. What has to hold is
// that the text is complete just before the effect declares itself finished.
func TestTypingFinishesTheTextBeforeItStops(t *testing.T) {
	// Seed 1 makes four mistakes over this text, so this also proves the
	// corrections land rather than merely that nothing went wrong.
	target, ef, done := typingRun(t, 1)
	dst := canvas.New(target.W, target.H)

	dst.CopyFrom(target)
	ef.Frame(dst, done-time.Millisecond)
	for y := range target.H {
		for x := range target.W {
			got, want := dst.At(x, y), target.At(x, y)
			if got == want || got == cursorCell {
				continue
			}
			t.Fatalf("just before finishing, cell (%d,%d) = %+v, want %+v", x, y, got, want)
		}
	}
}

// Every cell is either already typed, not yet typed, the cursor, or the one
// mistake being made right now -- and that mistake has to be a key the finger
// could plausibly have hit instead.
//
// Several seeds, because whether a given seed produces a typo at all is a coin
// toss: seed 11 makes none over this text. Asserting on one draw would be a test
// that passes by luck.
func TestTypingOnlyEverDiffersAtOneCellAndOnlyByANeighbouringKey(t *testing.T) {
	seeds := []uint64{1, 7, 11, 42, 99}
	total := 0
	for _, seed := range seeds {
		target, ef, done := typingRun(t, seed)
		dst := canvas.New(target.W, target.H)

		for elapsed := time.Duration(0); elapsed < done; elapsed += 8 * time.Millisecond {
			dst.CopyFrom(target)
			ef.Frame(dst, elapsed)

			wrong := 0
			for y := range target.H {
				for x := range target.W {
					got, want := dst.At(x, y), target.At(x, y)
					if got == want || got == canvas.Blank || got == cursorCell {
						continue
					}
					wrong++
					near, ok := qwertyNeighbours[toLower(want.R)]
					if !ok || !strings.ContainsRune(near, toLower(got.R)) {
						t.Fatalf("seed %d, t=%s: cell (%d,%d) shows %q, which is not a neighbour of %q",
							seed, elapsed, x, y, got.R, want.R)
					}
				}
			}
			if wrong > 1 {
				t.Fatalf("seed %d, t=%s: %d cells wrong at once; only the key under the finger may be",
					seed, elapsed, wrong)
			}
			total += wrong
		}
	}
	if total == 0 {
		t.Fatalf("no mistake was made across %d seeds; the typo path is dead code", len(seeds))
	}
}

// A typist who never varies is a teleprinter. This measures the intervals as a
// whole, which is what a viewer perceives; it does not isolate the speed drift
// from the deliberate pauses at punctuation and line ends, and a mutation that
// pins the drift to a constant still passes here.
func TestTypingSpeedIsUneven(t *testing.T) {
	target := canvas.FromText(typed, canvas.Default)
	entry, _ := Get("typing")
	ty, ok := entry.New(target, rand.New(rand.NewPCG(11, 13))).(*typing)
	if !ok {
		t.Fatal("typing is a different type")
	}

	gaps := make([]time.Duration, 0, len(ty.steps))
	prev := time.Duration(0)
	for _, s := range ty.steps {
		gaps = append(gaps, s.at-prev)
		prev = s.at
	}
	if len(gaps) < 20 {
		t.Fatalf("only %d keystrokes scheduled", len(gaps))
	}
	slices.Sort(gaps)
	lo, hi := gaps[len(gaps)/10], gaps[len(gaps)*9/10]
	if hi < lo*2 {
		t.Fatalf("p90 gap %v is not meaningfully longer than p10 %v; the typing is metronomic", hi, lo)
	}
}

// Correcting a mistake is a gesture, not just an outcome: the wrong character
// has to be seen, then seen to go, then replaced. Dropping the erase step still
// ends with the right text, so nothing else here would notice.
func TestTypingBackspacesOverItsMistakes(t *testing.T) {
	const (
		untouched = iota
		showedWrong
		wasErased
	)
	// Seed 1 makes four mistakes over this text.
	target, ef, done := typingRun(t, 1)
	dst := canvas.New(target.W, target.H)
	state := make([]int, len(target.Cells))
	corrected := 0

	for elapsed := time.Duration(0); elapsed < done; elapsed += 6 * time.Millisecond {
		dst.CopyFrom(target)
		ef.Frame(dst, elapsed)
		for i := range target.Cells {
			x, y := i%target.W, i/target.W
			got, want := dst.At(x, y), target.At(x, y)
			blank := got == canvas.Blank || got == cursorCell
			switch {
			case got != want && !blank:
				state[i] = showedWrong
			case state[i] == showedWrong && blank:
				state[i] = wasErased
			case state[i] == wasErased && got == want:
				state[i] = untouched
				corrected++
			}
		}
	}
	if corrected == 0 {
		t.Fatal("no mistake was erased before the right character went in")
	}
}

func spotlightAt(t *testing.T, target *canvas.Canvas, phase float64) *spotlight {
	t.Helper()
	entry, ok := Get("spotlight")
	if !ok {
		t.Fatal("spotlight is not registered")
	}
	s, ok := entry.New(target, rand.New(rand.NewPCG(1, 2))).(*spotlight)
	if !ok {
		t.Fatalf("spotlight is a %T", s)
	}
	s.phase = phase
	return s
}

func blanksIn(dst, target *canvas.Canvas) (blank, lit int) {
	for i := range dst.Cells {
		if target.Cells[i].R == ' ' {
			continue
		}
		if dst.Cells[i] == canvas.Blank {
			blank++
			continue
		}
		lit++
	}
	return blank, lit
}

// The beam has to leave part of the text in the dark while it sweeps, and none
// of it by the time it has opened out. Both halves are invisible to the
// end-state test, which only ever sees the frame after the effect gives up.
func TestSpotlightLightsPartOfTheTextThenOpensOut(t *testing.T) {
	target := canvas.FromText(sample, canvas.Default)
	s := spotlightAt(t, target, 0.13)
	dst := canvas.New(target.W, target.H)

	dst.CopyFrom(target)
	s.Frame(dst, spotlightRun/4)
	blank, lit := blanksIn(dst, target)
	if blank == 0 {
		t.Fatal("mid-sweep the whole text was lit; the beam masks nothing")
	}
	if lit == 0 {
		t.Fatal("mid-sweep nothing was lit; the beam covers nothing")
	}

	dst.CopyFrom(target)
	s.Frame(dst, spotlightRun-time.Millisecond)
	blank, _ = blanksIn(dst, target)
	if blank != 0 {
		t.Fatalf("%d cells were still dark when the beam should have opened out", blank)
	}
}

// Terminal cells are taller than they are wide, so a beam computed without that
// correction lights an area twice as tall as it should be. Phase zero puts the
// beam in the middle of the canvas at t=0, which is what makes it measurable.
func TestSpotlightBeamIsRoundOnScreenNotInCells(t *testing.T) {
	target := canvas.FromText(strings.Repeat("#", 60)+"\n", canvas.Default)
	for range 30 {
		target = canvas.FromText(strings.Repeat("#", 60)+"\n"+rowsOf(target), canvas.Default)
	}
	s := spotlightAt(t, target, 0)
	dst := canvas.New(target.W, target.H)
	dst.CopyFrom(target)
	s.Frame(dst, 0)

	minX, maxX, minY, maxY := target.W, -1, target.H, -1
	for y := range target.H {
		for x := range target.W {
			if dst.At(x, y) == canvas.Blank {
				continue
			}
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
		}
	}
	w, h := maxX-minX+1, maxY-minY+1
	if h < 1 || w < 1 {
		t.Fatal("nothing was lit")
	}
	// Twice as wide as tall, give or take the rounding of whole cells.
	ratio := float64(w) / float64(h)
	if ratio < 1.5 || ratio > 2.6 {
		t.Fatalf("lit area is %dx%d cells, ratio %.2f; want about 2 (cells are twice as tall as wide)", w, h, ratio)
	}
}

func rowsOf(c *canvas.Canvas) string {
	var b strings.Builder
	for y := range c.H {
		for x := range c.W {
			b.WriteRune(c.At(x, y).R)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// tornRows reports how many rows are displaced at a moment, and fails if any
// displaced row is not simply a shifted copy of itself -- an in-place shift that
// walks the wrong way smears one cell across the whole row instead.
func tornRows(t *testing.T, dst, target *canvas.Canvas) int {
	t.Helper()
	torn := 0
	for y := range target.H {
		if sameRow(dst, target, y, 0) {
			continue
		}
		torn++
		found := false
		for shift := -glitchShift; shift <= glitchShift && !found; shift++ {
			found = shift != 0 && sameRow(dst, target, y, shift)
		}
		if !found {
			t.Fatalf("row %d is not the row shifted by anything in range: %q", y, rowRunes(dst, y))
		}
	}
	return torn
}

// sameRow compares a drawn row against the text shifted sideways, ignoring the
// colour wash and treating cells pulled in from off the row as blank.
func sameRow(dst, target *canvas.Canvas, y, shift int) bool {
	for x := range target.W {
		want := target.At(x-shift, y)
		got := dst.At(x, y)
		if got.R != want.R {
			return false
		}
	}
	return true
}

func rowRunes(c *canvas.Canvas, y int) string {
	var b strings.Builder
	for x := range c.W {
		b.WriteRune(c.At(x, y).R)
	}
	return b.String()
}

// The tearing has to happen, has to be a shift rather than a smear, and has to
// die down: a glitch that never settles would never hand the text back.
func TestGlitchTearsRowsAndSettles(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghijklmnopqrst\n", 12), canvas.Default)
	entry, ok := Get("glitch")
	if !ok {
		t.Fatal("glitch is not registered")
	}
	ef := entry.New(target, rand.New(rand.NewPCG(4, 9)))
	dst := canvas.New(target.W, target.H)

	early, late := 0, 0
	for at := time.Duration(0); at < glitchRun/4; at += glitchSlice {
		dst.CopyFrom(target)
		ef.Frame(dst, at)
		early += tornRows(t, dst, target)
	}
	for at := glitchRun * 3 / 4; at < glitchRun; at += glitchSlice {
		dst.CopyFrom(target)
		ef.Frame(dst, at)
		late += tornRows(t, dst, target)
	}

	if early == 0 {
		t.Fatal("no row was ever torn")
	}
	if late >= early {
		t.Fatalf("%d rows torn late against %d early; the glitch never settles", late, early)
	}
}

// The wash is what sells it as signal loss rather than as the text moving.
func TestGlitchWashesTheColourOfATornRow(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghijklmnopqrst\n", 12), canvas.RGB(20, 200, 40))
	entry, _ := Get("glitch")
	ef := entry.New(target, rand.New(rand.NewPCG(4, 9)))
	dst := canvas.New(target.W, target.H)

	washed := 0
	for at := time.Duration(0); at < glitchRun/3; at += glitchSlice {
		dst.CopyFrom(target)
		ef.Frame(dst, at)
		for i := range dst.Cells {
			if dst.Cells[i].R != ' ' && dst.Cells[i].FG == glitchWash {
				washed++
			}
		}
	}
	if washed == 0 {
		t.Fatal("no torn cell had its colour washed out")
	}
}

// inked reports the bounding box of everything drawn, and how much of it there
// is, which is how the two movers are measured without knowing where any
// particular character went.
func inked(c *canvas.Canvas) (count, minX, maxX, minY, maxY int) {
	minX, minY = c.W, c.H
	maxX, maxY = -1, -1
	for y := range c.H {
		for x := range c.W {
			if c.At(x, y).R == ' ' {
				continue
			}
			count++
			minX, maxX = min(minX, x), max(maxX, x)
			minY, maxY = min(minY, y), max(maxY, y)
		}
	}
	return count, minX, maxX, minY, maxY
}

// Thrown apart and back again. Both ends are invisible to the end-state test,
// which only sees the frame after the effect has given up.
func TestScatteredThrowsCharactersOutAndBringsThemHome(t *testing.T) {
	target := canvas.FromText(sample, canvas.Default)
	entry, ok := Get("scattered")
	if !ok {
		t.Fatal("scattered is not registered")
	}
	ef := entry.New(target, rand.New(rand.NewPCG(5, 6)))
	dst := canvas.New(target.W, target.H)
	home, _, _, _, _ := inked(target)

	// Early: most characters are still out beyond the edges, so far fewer of
	// them are on screen than the text has.
	dst.CopyFrom(target)
	ef.Frame(dst, scatteredRun/10)
	early, _, _, _, _ := inked(dst)
	if early >= home {
		t.Fatalf("%d characters on screen against %d in the text; nothing was thrown", early, home)
	}

	// Late: everything has landed, in its own place.
	dst.CopyFrom(target)
	ef.Frame(dst, scatteredRun+scatteredStagger-time.Millisecond)
	for y := range target.H {
		for x := range target.W {
			got, want := dst.At(x, y), target.At(x, y)
			if got == want {
				continue
			}
			t.Fatalf("just before settling, cell (%d,%d) = %+v, want %+v", x, y, got, want)
		}
	}
}

// The picture has to be smaller than itself while it grows, and whole by the
// end. A scale stuck at one would pass every other test here.
func TestExpandGrowsFromTheMiddle(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghijklmnopqrst\n", 12), canvas.Default)
	entry, ok := Get("expand")
	if !ok {
		t.Fatal("expand is not registered")
	}
	ef := entry.New(target, rand.New(rand.NewPCG(5, 6)))
	dst := canvas.New(target.W, target.H)

	_, wantMinX, wantMaxX, wantMinY, wantMaxY := inked(target)
	wantW, wantH := wantMaxX-wantMinX+1, wantMaxY-wantMinY+1

	dst.CopyFrom(target)
	ef.Frame(dst, expandRun/4)
	count, minX, maxX, minY, maxY := inked(dst)
	if count == 0 {
		t.Fatal("nothing was drawn a quarter of the way in")
	}
	w, h := maxX-minX+1, maxY-minY+1
	if w >= wantW || h >= wantH {
		t.Fatalf("a quarter of the way in the picture is %dx%d against %dx%d; it is not growing", w, h, wantW, wantH)
	}

	dst.CopyFrom(target)
	ef.Frame(dst, expandRun-time.Millisecond)
	_, minX, maxX, minY, maxY = inked(dst)
	if maxX-minX+1 != wantW || maxY-minY+1 != wantH {
		t.Fatalf("at the end the picture is %dx%d, want %dx%d", maxX-minX+1, maxY-minY+1, wantW, wantH)
	}
}

// colourOnly names the effects that promise to recolour and nothing else: they
// never hide a cell, never move one, and never change what character is in it.
// That promise is what lets any of them layer over any of the maskers, so it is
// worth holding them to it as a family rather than one at a time.
var colourOnly = []string{"beams", "colorshift", "highlight", "laseretch", "smoke", "sweep", "waves"}

func sampleTimes(run time.Duration) []time.Duration {
	var out []time.Duration
	for i := 1; i < 10; i++ {
		out = append(out, run*time.Duration(i)/10)
	}
	return out
}

func TestColourOnlyEffectsNeverHideOrMoveAnything(t *testing.T) {
	target := canvas.FromText(sample, canvas.RGB(120, 130, 140))
	for _, name := range colourOnly {
		t.Run(name, func(t *testing.T) {
			entry, ok := Get(name)
			if !ok {
				t.Fatalf("%s is not registered", name)
			}
			ef := entry.New(target, rand.New(rand.NewPCG(2, 3)))
			dst := canvas.New(target.W, target.H)

			for _, at := range sampleTimes(3 * time.Second) {
				dst.CopyFrom(target)
				ef.Frame(dst, at)
				for y := range target.H {
					for x := range target.W {
						got, want := dst.At(x, y), target.At(x, y)
						if got.R == want.R {
							continue
						}
						t.Fatalf("t=%s: cell (%d,%d) holds %q, want %q; this effect may only recolour",
							at, x, y, got.R, want.R)
					}
				}
			}
		})
	}
}

func TestColourOnlyEffectsActuallyRecolour(t *testing.T) {
	target := canvas.FromText(sample, canvas.RGB(120, 130, 140))
	for _, name := range colourOnly {
		t.Run(name, func(t *testing.T) {
			entry, _ := Get(name)
			ef := entry.New(target, rand.New(rand.NewPCG(2, 3)))
			dst := canvas.New(target.W, target.H)

			for _, at := range sampleTimes(3 * time.Second) {
				dst.CopyFrom(target)
				ef.Frame(dst, at)
				for i := range dst.Cells {
					if dst.Cells[i] != target.Cells[i] {
						return
					}
				}
			}
			t.Fatal("never differed from the text; the effect draws nothing")
		})
	}
}

// Seven effects written in one sitting off one idea is exactly where two of them
// come out as the same thing under different names.
func TestColourOnlyEffectsDifferFromEachOther(t *testing.T) {
	target := canvas.FromText(sample, canvas.RGB(120, 130, 140))
	seen := map[string]string{}

	for _, name := range colourOnly {
		entry, _ := Get(name)
		ef := entry.New(target, rand.New(rand.NewPCG(2, 3)))
		dst := canvas.New(target.W, target.H)
		var out bytes.Buffer
		r := canvas.NewRenderer(&out, target.W, target.H)
		for _, at := range sampleTimes(3 * time.Second) {
			dst.CopyFrom(target)
			ef.Frame(dst, at)
			_ = r.Draw(dst)
		}
		key := out.String()
		if other, clash := seen[key]; clash {
			t.Fatalf("%s and %s draw exactly the same thing", name, other)
		}
		seen[key] = name
	}
}

// colourDistance measures how far a drawn cell has been pushed from the text,
// rather than whether it moved at all. A cell blended a tenth of the way toward
// some hue is visually the text; counting it as "changed" is what made the first
// version of this test call every wind-down a light switch.
func colourDistance(got, want canvas.Cell) int {
	d := 0
	if got.Bold != want.Bold {
		d += 64
	}
	gr, gg, gb, gok := got.FG.RGB()
	wr, wg, wb, wok := want.FG.RGB()
	if !gok || !wok {
		if got.FG != want.FG {
			d += 255
		}
		return d
	}
	d += abs(int(gr)-int(wr)) + abs(int(gg)-int(wg)) + abs(int(gb)-int(wb))
	return d
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// An effect that is simply switched off at the end pops: the reader sees a
// coloured screen become a plain one between two frames. Each of these has to
// have faded most of the way back before it reports itself finished.
//
// It holds the ones that colour the whole text -- colorshift, waves, smoke --
// where an abrupt end is most glaring. It does not reliably hold the sparse
// ones: beams covers a narrow, shifting band, so measuring its last frame
// against its own peak cannot tell a fade from the beams simply being elsewhere.
func TestColourOnlyEffectsWindDownBeforeTheyStop(t *testing.T) {
	target := canvas.FromText(sample, canvas.RGB(120, 130, 140))
	for _, name := range colourOnly {
		t.Run(name, func(t *testing.T) {
			entry, _ := Get(name)
			ef := entry.New(target, rand.New(rand.NewPCG(2, 3)))
			dst := canvas.New(target.W, target.H)

			// Finely, so that "the last frame drawn" really is the last one and
			// not whichever coarse sample happened to land before the end.
			peak, last := 0, 0
			for at := time.Duration(0); at < 4*time.Second; at += 10 * time.Millisecond {
				dst.CopyFrom(target)
				more := ef.Frame(dst, at)
				total := 0
				for i := range dst.Cells {
					total += colourDistance(dst.Cells[i], target.Cells[i])
				}
				peak = max(peak, total)
				if more {
					last = total
				}
			}
			if peak == 0 {
				t.Fatal("nothing was ever recoloured")
			}
			if last*4 > peak {
				t.Fatalf("the last frame drawn is still at %d%% of the effect's strongest; it ends abruptly",
					last*100/peak)
			}
		})
	}
}

func frameAt(t *testing.T, name string, target *canvas.Canvas, seed uint64, at time.Duration) *canvas.Canvas {
	t.Helper()
	entry, ok := Get(name)
	if !ok {
		t.Fatalf("%s is not registered", name)
	}
	ef := entry.New(target, rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)))
	dst := canvas.New(target.W, target.H)
	dst.CopyFrom(target)
	ef.Frame(dst, at)
	return dst
}

func wrongCells(dst, target *canvas.Canvas) int {
	n := 0
	for i := range dst.Cells {
		if dst.Cells[i].R != target.Cells[i].R {
			n++
		}
	}
	return n
}

// errorcorrect is the only effect that shows the text complete but wrong. What
// makes it read as a mistake rather than as damage is that every misplaced
// character is one that genuinely belongs somewhere else in the text.
func TestErrorcorrectSwapsRealCharactersAndWorksThroughThem(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghij\n", 8), canvas.Default)
	present := map[rune]bool{}
	for _, cell := range target.Cells {
		present[cell.R] = true
	}

	first := frameAt(t, "errorcorrect", target, 3, 0)
	wrong := wrongCells(first, target)
	if wrong == 0 {
		t.Fatal("nothing was out of place at the start")
	}
	for i := range first.Cells {
		if first.Cells[i] == canvas.Blank && target.Cells[i] != canvas.Blank {
			t.Fatalf("cell %d was hidden; this effect hides nothing", i)
		}
		if !present[first.Cells[i].R] {
			t.Fatalf("cell %d holds %q, which is nowhere in the text; it was invented, not swapped",
				i, first.Cells[i].R)
		}
	}

	late := wrongCells(frameAt(t, "errorcorrect", target, 3, errorcorrectRun*4/5), target)
	if late >= wrong {
		t.Fatalf("%d cells wrong late against %d at the start; the corrections never land", late, wrong)
	}
}

// Rows above the cut arrive from one side and rows below from the other. A slice
// that moved both the same way would be slide with extra steps.
func TestSliceBringsTheHalvesInFromOppositeSides(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghijklmnop\n", 8), canvas.Default)
	cut := target.H / 2

	moved := 0
	for _, at := range []time.Duration{sliceRun / 5, sliceRun / 3, sliceRun / 2} {
		dst := frameAt(t, "slice", target, 3, at)
		for y := range target.H {
			shift := shiftOfRow(dst, target, y)
			if shift == 0 {
				continue
			}
			moved++
			if y < cut && shift > 0 {
				t.Fatalf("t=%s: row %d is above the cut but came from the right", at, y)
			}
			if y >= cut && shift < 0 {
				t.Fatalf("t=%s: row %d is below the cut but came from the left", at, y)
			}
		}
	}
	if moved == 0 {
		t.Fatal("no row was ever displaced")
	}
}

// shiftOfRow finds the displacement that turns the text's row into the drawn
// one, and fails if no displacement does -- a shift that smears would land here.
func shiftOfRow(dst, target *canvas.Canvas, y int) int {
	for shift := -target.W; shift <= target.W; shift++ {
		if sameRow(dst, target, y, shift) {
			return shift
		}
	}
	return 0
}

// The middle row opens first and the rest follows, so the drawn text is wider
// than it is tall early on and squares up later.
func TestMiddleoutOpensAcrossBeforeItOpensOut(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghijklmnopqrst\n", 12), canvas.Default)

	_, _, _, earlyTop, earlyBottom := inked(frameAt(t, "middleout", target, 3, middleoutRun/3))
	_, _, _, lateTop, lateBottom := inked(frameAt(t, "middleout", target, 3, middleoutRun*9/10))
	if earlyBottom < earlyTop {
		t.Fatal("nothing was drawn a third of the way in")
	}
	early, late := earlyBottom-earlyTop+1, lateBottom-lateTop+1
	if early >= late {
		t.Fatalf("%d rows showing early against %d late; it never unfolds", early, late)
	}
	// The order is the whole idea: nothing above or below the middle until the
	// middle row itself has opened. Two rows, because an even height has no
	// single middle one.
	if early > 2 {
		t.Fatalf("%d rows showing before the middle row has opened; both axes are opening at once", early)
	}
}

// The plainest reveal there is: a cell is either not there yet or right. Showing
// anything else would make it decrypt.
func TestRandomsequenceOnlyEverShowsBlankOrTheRightCharacter(t *testing.T) {
	target := canvas.FromText(sample, canvas.Default)
	counts := []int{}
	for _, at := range []time.Duration{0, randomsequenceRun / 3, randomsequenceRun * 2 / 3} {
		dst := frameAt(t, "randomsequence", target, 3, at)
		shown := 0
		for i := range dst.Cells {
			if dst.Cells[i] == target.Cells[i] {
				shown++
				continue
			}
			if dst.Cells[i] == canvas.Blank {
				continue
			}
			t.Fatalf("t=%s: cell %d holds %+v, which is neither blank nor the text", at, i, dst.Cells[i])
		}
		counts = append(counts, shown)
	}
	if counts[0] >= counts[2] {
		t.Fatalf("%d cells showing at the start against %d later; nothing appears", counts[0], counts[2])
	}
}

func firstDiff(want, got *canvas.Canvas) string {
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

// crumble is the only effect that takes the text away and brings it back, so
// what has to be true is that it really does go: a version that only greyed the
// characters would pass every other test here.
func TestCrumbleSweepsTheTextAwayBeforeReturningIt(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghij\n", 8), canvas.Default)
	inkedTotal, _, _, _, _ := inked(target)

	gone := 0
	for _, at := range []time.Duration{crumbleRun / 2, crumbleRun * 3 / 5} {
		count, _, _, _, _ := inked(frameAt(t, "crumble", target, 3, at))
		gone = max(gone, inkedTotal-count)
	}
	if gone*3 < inkedTotal {
		t.Fatalf("at most %d of %d cells were ever swept away; the text never leaves", gone, inkedTotal)
	}

	// And it comes back, which the end-state test cannot see: crumble's last
	// frame returns without touching the canvas.
	last := frameAt(t, "crumble", target, 3, crumbleRun-time.Millisecond)
	if diff := firstDiff(target, last); diff != "" {
		t.Fatalf("just before finishing: %s", diff)
	}
}

// The grid is what makes this one different from every other reveal, so it has
// to actually be drawn, and drawn before the text fills it in.
func TestSynthgridRulesTheGridBeforeFillingIt(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghijklmnopqrst\n", 12), canvas.Default)
	// Well inside the ruling phase: not one cell may hold the text yet.
	dst := frameAt(t, "synthgrid", target, 3, time.Duration(float64(synthgridRun)*synthgridDraw/2))

	lines, text := 0, 0
	for i := range dst.Cells {
		switch dst.Cells[i].R {
		case synthgridCross, synthgridRow, synthgridCol:
			lines++
		case target.Cells[i].R:
			if target.Cells[i].R != ' ' {
				text++
			}
		}
	}
	if lines == 0 {
		t.Fatal("no grid was ruled")
	}
	if text > 0 {
		t.Fatalf("%d cells already hold the text while the grid is still being ruled", text)
	}
}

// A row showing a different row is the whole effect. Displacing rows sideways
// would be glitch.
func TestOverflowPutsWholeRowsInTheWrongPlace(t *testing.T) {
	target := canvas.FromText("aaaa\nbbbb\ncccc\ndddd\neeee\nffff\n", canvas.Default)
	borrowed := 0
	for _, at := range []time.Duration{overflowRun / 5, overflowRun / 3, overflowRun / 2} {
		dst := frameAt(t, "overflow", target, 3, at)
		for y := range target.H {
			if sameRow(dst, target, y, 0) {
				continue
			}
			from := -1
			for other := range target.H {
				if rowRunes(dst, y) == rowRunes(target, other) {
					from = other
				}
			}
			if from < 0 {
				t.Fatalf("t=%s: row %d holds %q, which is no row of the text", at, y, rowRunes(dst, y))
			}
			borrowed++
		}
	}
	if borrowed == 0 {
		t.Fatal("no row was ever taken from somewhere else")
	}
}

// The digits travel: they are never to the right of where their character
// belongs, and they give way to the character on arrival.
func TestBinarypathWalksDigitsInAlongTheRow(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghijklmnop\n", 6), canvas.Default)
	digits, arrived := 0, 0
	for _, at := range []time.Duration{binarypathRun / 4, binarypathRun / 2} {
		dst := frameAt(t, "binarypath", target, 3, at)
		lastDigit, lastHome := -1, -1
		for y := range target.H {
			for x := range target.W {
				got := dst.At(x, y)
				if target.At(x, y).R != ' ' {
					lastHome = max(lastHome, x)
				}
				if got.R == '0' || got.R == '1' {
					digits++
					lastDigit = max(lastDigit, x)
					continue
				}
				if got.R == target.At(x, y).R && got.R != ' ' {
					arrived++
				}
			}
		}
		// Travelling means being short of home: a digit standing on the last
		// column of the text has not moved, it was drawn where it belongs.
		if lastDigit >= lastHome {
			t.Fatalf("t=%s: a digit sits at column %d, as far right as the text reaches; nothing travelled",
				at, lastDigit)
		}
	}
	if digits == 0 {
		t.Fatal("no digit was ever on its way")
	}
	if arrived == 0 {
		t.Fatal("no character ever arrived")
	}
}

// Three acts, and the middle one is what names the effect: the characters have
// to leave the area the text occupies.
func TestUnstableJumblesThenBlowsApart(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghij\n", 8), canvas.Default)

	held := frameAt(t, "unstable", target, 3, time.Duration(float64(unstableRun)*unstableHold/2))
	if wrongCells(held, target) == 0 {
		t.Fatal("the text was already right in the first act; nothing was jumbled")
	}

	// The canvas is exactly the size of the text, so characters blown past its
	// edge are dropped rather than drawn somewhere further out: the sign of the
	// blast is how many have left, not a wider bounding box.
	home, _, _, _, _ := inked(target)
	blast := frameAt(t, "unstable", target, 3, time.Duration(float64(unstableRun)*(unstableHold+unstableBlast)/2))
	blown, _, _, _, _ := inked(blast)
	if blown*2 > home {
		t.Fatalf("%d of %d characters are still on screen mid-blast; they never left", blown, home)
	}

	settled := frameAt(t, "unstable", target, 3, unstableRun-time.Millisecond)
	if diff := firstDiff(target, settled); diff != "" {
		t.Fatalf("just before finishing: %s", diff)
	}
}

// flights are the effects built on the shared path machinery. They promise the
// same two things whatever their path: characters leave their places, and every
// one of them is home by the end.
var flights = []string{
	"blackhole", "bouncyballs", "bubbles", "fireworks", "orbittingvolley",
	"pour", "rain", "rings", "spray", "swarm", "thunderstorm",
}

func TestFlightsCarryCharactersAwayAndConverge(t *testing.T) {
	target := canvas.FromText(sample, canvas.Default)
	inkedTotal, _, _, _, _ := inked(target)

	for _, name := range flights {
		t.Run(name, func(t *testing.T) {
			entry, ok := Get(name)
			if !ok {
				t.Fatalf("%s is not registered", name)
			}
			ef := entry.New(target, rand.New(rand.NewPCG(4, 5)))
			dst := canvas.New(target.W, target.H)

			away, last := 0, time.Duration(0)
			for at := time.Duration(0); at < 8*time.Second; at += 20 * time.Millisecond {
				dst.CopyFrom(target)
				if !ef.Frame(dst, at) {
					break
				}
				last = at
				away = max(away, wrongCells(dst, target))
			}
			if away == 0 {
				t.Fatal("no character ever left its place")
			}
			if last == 0 {
				t.Fatal("the flight was over before it started")
			}

			// By the last live frame all but the stragglers have landed. The
			// frame after this one is the text itself, untouched.
			dst.CopyFrom(target)
			ef.Frame(dst, last)
			stillOut := wrongCells(dst, target)
			if stillOut*10 > inkedTotal {
				t.Fatalf("%d of %d characters are still out on the last frame; the paths do not converge",
					stillOut, inkedTotal)
			}
		})
	}
}

// rowsOverTime follows one character down a tall canvas, which is the only way
// to tell one path from another: on a full page there is no saying which drawn
// cell belongs to which character.
// seenOverTime follows one character across a canvas with room on every side,
// which is the only way to tell one path from another: on a full page there is
// no saying which drawn cell belongs to which character.
func seenOverTime(t *testing.T, name string, steps int) (seen []point, home point) {
	t.Helper()
	// Room on every side, or a path that goes the wrong way is simply clipped
	// off the canvas and the test sees nothing at all.
	home = point{x: 10, y: 4}
	row := strings.Repeat(" ", 10) + "X"
	target := canvas.FromText("\n\n\n\n"+row+"\n\n\n\n\n\n\n\n\n", canvas.Default)
	entry, ok := Get(name)
	if !ok {
		t.Fatalf("%s is not registered", name)
	}
	ef := entry.New(target, rand.New(rand.NewPCG(9, 9)))
	dst := canvas.New(target.W, target.H)

	for i := range steps {
		at := time.Duration(i) * 40 * time.Millisecond
		dst.CopyFrom(target)
		if !ef.Frame(dst, at) {
			break
		}
		count, left, _, top, _ := inked(dst)
		if count == 0 {
			// Off the canvas for the moment.
			continue
		}
		seen = append(seen, point{x: float64(left), y: float64(top)})
	}
	return seen, home
}

func topRows(seen []point) []int {
	rows := make([]int, 0, len(seen))
	for _, s := range seen {
		rows = append(rows, int(s.y))
	}
	return rows
}

// Falling is monotonic; bouncing is not. That is the whole difference between
// these two, and it lives in four lines of path.
func TestRainFallsStraightWhileBouncyballsBounces(t *testing.T) {
	seen, homeAt := seenOverTime(t, "rain", 60)
	fall, home := topRows(seen), int(homeAt.y)
	if len(fall) < 3 {
		t.Fatalf("rain was only visible for %d frames", len(fall))
	}
	for i, row := range fall {
		if row > home {
			t.Fatalf("rain was seen at row %d, below its place at %d; it comes from above", row, home)
		}
		if i == 0 || fall[i] >= fall[i-1] {
			continue
		}
		t.Fatalf("rain went back up, from row %d to %d; it should only fall", fall[i-1], fall[i])
	}

	bounceSeen, _ := seenOverTime(t, "bouncyballs", 60)
	bounce := topRows(bounceSeen)
	if len(bounce) < 3 {
		t.Fatalf("bouncyballs was only visible for %d frames", len(bounce))
	}
	rose := false
	for i := 1; i < len(bounce); i++ {
		if bounce[i] < bounce[i-1] {
			rose = true
		}
	}
	if !rose {
		t.Fatal("bouncyballs never came back up; it is just falling")
	}
}

// The pull is what names it: at the bottom of the fall the text occupies far
// less of the screen than it does at rest.
func TestBlackholePullsTheTextIntoAPoint(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghijklmnopqrst\n", 12), canvas.Default)
	_, homeL, homeR, homeT, homeB := inked(target)
	homeArea := (homeR - homeL + 1) * (homeB - homeT + 1)

	pulled := frameAt(t, "blackhole", target, 4, time.Duration(float64(blackholeRun)*blackholeFall))
	count, l, r, tp, b := inked(pulled)
	if count == 0 {
		t.Fatal("nothing was on screen at the bottom of the fall")
	}
	area := (r - l + 1) * (b - tp + 1)
	if area*2 > homeArea {
		t.Fatalf("at the bottom of the fall the text covers %d cells against %d at rest; nothing was pulled in",
			area, homeArea)
	}
}

func spanOf(seen []point) (minX, maxX, minY, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, s := range seen {
		minX, maxX = math.Min(minX, s.x), math.Max(maxX, s.x)
		minY, maxY = math.Min(minY, s.y), math.Max(maxY, s.y)
	}
	return minX, maxX, minY, maxY
}

// Rising is the easy half; the sway is what makes it a bubble rather than rain
// running backwards, and it is four lines of path that nothing else would miss.
func TestBubblesRiseAndSway(t *testing.T) {
	seen, home := seenOverTime(t, "bubbles", 80)
	if len(seen) < 5 {
		t.Fatalf("bubbles was only visible for %d frames", len(seen))
	}
	minX, maxX, _, _ := spanOf(seen)
	for _, s := range seen {
		if s.y >= home.y {
			continue
		}
		t.Fatalf("a bubble was seen at row %v, above its place at %v; they come from below", s.y, home.y)
	}
	if maxX-minX < 2 {
		t.Fatalf("the bubble wandered %v columns; it went straight up", maxX-minX)
	}
}

// The same drop as rain, with weather on it: the wind is the whole difference.
func TestThunderstormLeansWhileRainDoesNot(t *testing.T) {
	storm, _ := seenOverTime(t, "thunderstorm", 80)
	still, _ := seenOverTime(t, "rain", 80)
	if len(storm) < 5 {
		t.Fatalf("thunderstorm was only visible for %d frames", len(storm))
	}
	stormMinX, stormMaxX, _, _ := spanOf(storm)
	rainMinX, rainMaxX, _, _ := spanOf(still)
	if stormMaxX-stormMinX <= rainMaxX-rainMinX {
		t.Fatalf("the storm drifted %v columns against rain's %v; there is no wind",
			stormMaxX-stormMinX, rainMaxX-rainMinX)
	}
}

// Shells launch from below the text, which is what separates the climb from
// bouncyballs -- both go up and down, but only one starts underneath.
func TestFireworksLaunchFromBelow(t *testing.T) {
	seen, home := seenOverTime(t, "fireworks", 80)
	if len(seen) < 5 {
		t.Fatalf("fireworks was only visible for %d frames", len(seen))
	}
	_, _, _, lowest := spanOf(seen)
	if lowest <= home.y {
		t.Fatalf("the shell never got below its place at %v; it did not launch", home.y)
	}
}

// The order is the effect: the lower rows are already sitting there while the
// upper ones are still on their way.
func TestPourFillsFromTheBottomUp(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghij\n", 10), canvas.Default)
	dst := frameAt(t, "pour", target, 6, pourRun/2)

	top, bottom := 0, 0
	half := target.H / 2
	for y := range target.H {
		for x := range target.W {
			if dst.At(x, y) != target.At(x, y) {
				continue
			}
			if y < half {
				top++
				continue
			}
			bottom++
		}
	}
	if bottom <= top {
		t.Fatalf("%d cells settled in the lower half against %d in the upper; it is not filling from the bottom",
			bottom, top)
	}
}

// A flock is a part of the text, not a random draw: put neighbours in different
// flocks and they arrive from different directions, which is scattered wearing
// another name.
func TestSwarmFlocksAreRegionsNotARandomDraw(t *testing.T) {
	together, apart := 0, 0
	for y := range 20 {
		for x := range 20 {
			if swarmGroupOf(x, y) == swarmGroupOf(x+1, y) {
				together++
			}
			if swarmGroupOf(x, y) != swarmGroupOf(x+swarmBlock*2, y) {
				apart++
			}
		}
	}
	// Neighbours nearly always share; a draw at random would agree one time in
	// swarmGroups, so anything near that is not grouping by region at all.
	if together*swarmGroups < 20*20*2 {
		t.Fatalf("only %d of 400 neighbouring pairs share a flock", together)
	}
	if apart == 0 {
		t.Fatal("distant characters always share a flock; there is only one")
	}
}

// The orbit is what names it. A single character on a wide canvas is the only
// way to watch one: its bearing from the middle has to swing round, where every
// other flight walks a line.
func TestRingsCarryCharactersRoundTheMiddle(t *testing.T) {
	seen, _ := seenOverTime(t, "rings", 80)
	if len(seen) < 6 {
		t.Fatalf("rings was only visible for %d frames", len(seen))
	}
	// Turning shows up as movement on both axes; a straight run at any angle
	// keeps a constant ratio between them.
	minX, maxX, minY, maxY := spanOf(seen)
	if maxX-minX < 3 || maxY-minY < 2 {
		t.Fatalf("the character moved %v across and %v down; it did not go round",
			maxX-minX, maxY-minY)
	}
}

// Scaled changes nothing but the clock: at factor 2 the animation at t is the
// unscaled animation at 2t, frame for frame, and it finishes in half the time.
func TestScaledOnlyBendsTheClock(t *testing.T) {
	target := canvas.FromText(sample, canvas.Default)
	entry, ok := Get("wipe")
	if !ok {
		t.Fatal("wipe is not registered")
	}
	plain := entry.New(target, rand.New(rand.NewPCG(1, 2)))
	fast := Scaled(entry.New(target, rand.New(rand.NewPCG(1, 2))), 2)

	a := canvas.New(target.W, target.H)
	b := canvas.New(target.W, target.H)
	for _, at := range []time.Duration{100 * time.Millisecond, 300 * time.Millisecond} {
		a.CopyFrom(target)
		plain.Frame(a, 2*at)
		b.CopyFrom(target)
		fast.Frame(b, at)
		for i := range a.Cells {
			if a.Cells[i] != b.Cells[i] {
				t.Fatalf("t=%s: scaled frame differs from the plain frame at twice the time", at)
			}
		}
	}

	end := time.Duration(0)
	dst := canvas.New(target.W, target.H)
	for at := time.Duration(0); at < 5*time.Second; at += 10 * time.Millisecond {
		dst.CopyFrom(target)
		if !fast.Frame(dst, at) {
			end = at
			break
		}
	}
	plainEnd := time.Duration(0)
	fresh := entry.New(target, rand.New(rand.NewPCG(1, 2)))
	for at := time.Duration(0); at < 5*time.Second; at += 10 * time.Millisecond {
		dst.CopyFrom(target)
		if !fresh.Frame(dst, at) {
			plainEnd = at
			break
		}
	}
	if end >= plainEnd {
		t.Fatalf("scaled by 2 ended at %v, unscaled at %v; it is not faster", end, plainEnd)
	}
}

func TestScaledLeavesTheIdentityAlone(t *testing.T) {
	entry, _ := Get("wipe")
	target := canvas.FromText(sample, canvas.Default)
	e := entry.New(target, rand.New(rand.NewPCG(1, 2)))
	if Scaled(e, 1) != e {
		t.Fatal("factor 1 wrapped the effect for nothing")
	}
	if Scaled(e, -3) != e {
		t.Fatal("a nonsense factor must fall back to the effect itself")
	}
}

func doomfireAt(t *testing.T, target *canvas.Canvas, seed uint64, times []time.Duration) *canvas.Canvas {
	t.Helper()
	entry, ok := Get("doomfire")
	if !ok {
		t.Fatal("doomfire is not registered")
	}
	ef := entry.New(target, rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)))
	dst := canvas.New(target.W, target.H)
	for _, at := range times {
		dst.CopyFrom(target)
		ef.Frame(dst, at)
	}
	return dst
}

// Mid-burn the flames have to own a real share of the canvas, and by the end
// every one of them has to be gone. The end-state test cannot see the second
// half: the final frame draws nothing, which is exactly the promise.
func TestDoomfireBurnsHighAndDiesOut(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghijklmnopqrst\n", 12), canvas.Default)

	mid := doomfireAt(t, target, 3, []time.Duration{2 * time.Second})
	flames := 0
	shades := map[rune]bool{}
	for i := range mid.Cells {
		switch mid.Cells[i].R {
		case '█', '▓', '▒', '░':
			flames++
			shades[mid.Cells[i].R] = true
		}
	}
	if flames*4 < len(mid.Cells) {
		t.Fatalf("%d of %d cells aflame at the peak; the fire never caught", flames, len(mid.Cells))
	}
	// The decay is what grades the flame: without it every burning cell sits at
	// full intensity and the fire is a solid white block, not fire.
	if len(shades) < 3 {
		t.Fatalf("only %d flame shades at the peak; the fire has no gradient", len(shades))
	}

	// Stepped past the cut, the way the player advances time.
	times := []time.Duration{}
	for at := time.Duration(0); at < 60*time.Second; at += 100 * time.Millisecond {
		times = append(times, at)
	}
	entry, _ := Get("doomfire")
	ef := entry.New(target, rand.New(rand.NewPCG(3, 3^0x9e3779b97f4a7c15)))
	dst := canvas.New(target.W, target.H)
	ended := false
	for _, at := range times {
		dst.CopyFrom(target)
		if !ef.Frame(dst, at) {
			ended = true
			break
		}
	}
	if !ended {
		t.Fatal("the fire never went out")
	}
	if diff := firstDiff(target, dst); diff != "" {
		t.Fatalf("after dying out: %s", diff)
	}
}

// The simulation advances by steps derived from t, so two clocks sampling at
// different rates have to agree wherever they land on the same moment. This is
// the property that keeps a simulation inside the pure-function-of-t contract.
func TestDoomfireIsFrameRateIndependent(t *testing.T) {
	target := canvas.FromText(strings.Repeat("abcdefghij\n", 8), canvas.Default)

	coarse := doomfireAt(t, target, 7, []time.Duration{
		480 * time.Millisecond, 960 * time.Millisecond, 1920 * time.Millisecond,
	})
	fine := doomfireAt(t, target, 7, func() []time.Duration {
		var ts []time.Duration
		for at := 16 * time.Millisecond; at <= 1920*time.Millisecond; at += 16 * time.Millisecond {
			ts = append(ts, at)
		}
		return ts
	}())

	for i := range coarse.Cells {
		if coarse.Cells[i] != fine.Cells[i] {
			t.Fatalf("cell %d differs between a 60fps clock and a coarse one; the sim leaks frame rate", i)
		}
	}
}
