// Package effect holds the animations and the table that names them.
package effect

import (
	"math/rand/v2"
	"slices"
	"time"

	"github.com/crgimenes/hanabi/canvas"
)

// Effect transforms one frame of an animation.
type Effect interface {
	// Frame reworks c in place for elapsed time t and reports whether it still
	// has work left.
	//
	// c arrives holding the finished text, or the output of the effect before
	// it in the chain -- which is what lets effects be composed rather than
	// merely sequenced. An effect that only masks or substitutes cells (rather
	// than reading the original text) composes with anything.
	//
	// Once every effect in a chain reports itself finished, the canvas has to
	// equal the finished text, or the terminal is left showing the wrong thing.
	Frame(c *canvas.Canvas, t time.Duration) bool
}

// Ctor builds an effect for one run. target is the finished text, for an effect
// that needs its extent or a per-cell schedule; the canvas passed to Frame is
// the one to read and write. rnd is seeded by the caller; an effect must take
// every random decision from it and never from the global source, or the same
// seed stops reproducing the same animation.
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
