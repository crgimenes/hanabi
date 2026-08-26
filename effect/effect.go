// Package effect holds the animations and the table that names them.
package effect

import (
	"math/rand/v2"
	"slices"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

// Effect draws one frame of an animation.
type Effect interface {
	// Frame paints the state at elapsed time t into dst and reports whether
	// the animation still has work left. The last frame must equal the target
	// exactly, so the text stays on screen once the run ends.
	Frame(dst *canvas.Canvas, t time.Duration) bool
}

// Ctor builds an effect for one run. rnd is seeded by the caller; an effect
// must take every random decision from it and never from the global source, or
// the same seed stops reproducing the same animation.
type Ctor func(target *canvas.Canvas, rnd *rand.Rand) Effect

type Entry struct {
	Name string
	Desc string
	New  Ctor
}

var all = []Entry{
	{"decrypt", "Random glyphs settle one by one into the text", newDecrypt},
	{"wipe", "A diagonal sweep reveals the text from the top-left", newWipe},
}

func List() []Entry {
	return slices.Clone(all)
}

func Get(name string) (Entry, bool) {
	i := slices.IndexFunc(all, func(e Entry) bool { return e.Name == name })
	if i < 0 {
		return Entry{}, false
	}
	return all[i], true
}
