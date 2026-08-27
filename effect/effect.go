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
	{"beams", "Bright lines sweep across at their own angles", newBeams},
	{"binarypath", "Characters travel in along their row as binary digits", newBinarypath},
	{"blackhole", "The text is pulled into a point and let back out with a twist", newBlackhole},
	{"bouncyballs", "Characters drop in and bounce before they settle", newBouncyballs},
	{"bubbles", "Characters float up from below, swaying as they rise", newBubbles},
	{"burn", "A band of fire sweeps up through the text, recolouring as it goes", newBurn},
	{"colorshift", "A moving spectrum runs through the text", newColorshift},
	{"crumble", "The text drains, breaks into grit, is swept away and returns", newCrumble},
	{"decrypt", "Random glyphs settle one by one into the text", newDecrypt},
	{"errorcorrect", "Characters sit in each other places until each pair is put right", newErrorcorrect},
	{"expand", "The picture grows out of a point in the middle", newExpand},
	{"fireworks", "Shells climb, burst, and scatter the characters into place", newFireworks},
	{"glitch", "Rows tear sideways and lose their colour, then settle", newGlitch},
	{"highlight", "A specular band shines across, changing no colour", newHighlight},
	{"laseretch", "A white-hot point walks the text, trailing heat", newLaseretch},
	{"matrix", "Columns of glyphs rain down, revealing the text behind them", newMatrix},
	{"middleout", "The middle row opens, then the rest unfolds above and below", newMiddleout},
	{"orbittingvolley", "Four orbiting launchers fire the characters inward", newOrbittingvolley},
	{"overflow", "Rows scroll past out of order and settle into place", newOverflow},
	{"rain", "Characters fall straight down into place, gathering speed", newRain},
	{"pour", "The text fills from the bottom up, streaming in from the left", newPour},
	{"randomsequence", "Characters appear in no particular order", newRandomsequence},
	{"rings", "Characters gather onto turning circles that draw in", newRings},
	{"scattered", "Characters are thrown apart and find their way back", newScattered},
	{"slice", "The text is cut in half and the halves come in from opposite sides", newSlice},
	{"slide", "The whole picture slides in from the left", newSlide},
	{"smoke", "Drifting smoke greys whatever it crosses", newSmoke},
	{"spotlight", "A beam sweeps over the text, lighting only what it falls on", newSpotlight},
	{"spray", "Characters are sprayed out from one point, each on its own line", newSpray},
	{"swarm", "Flocks of characters wander together before settling", newSwarm},
	{"sweep", "A front lays colour down, then a second takes it away", newSweep},
	{"synthgrid", "A grid is ruled over the space and the text dissolves into it", newSynthgrid},
	{"thunderstorm", "Characters fall on a wind that gusts and slackens", newThunderstorm},
	{"typing", "Somebody types the text, uneven, pausing, backspacing over mistakes", newTyping},
	{"unstable", "Jumbled characters blow apart and settle where they belong", newUnstable},
	{"waves", "Bands of colour roll through the text", newWaves},
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
