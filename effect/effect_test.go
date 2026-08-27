package effect

import (
	"bytes"
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
