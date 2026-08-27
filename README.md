# hanabi

Terminal text effects with no runtime dependencies: one static binary that
reads text and animates it into place.

```console
$ figlet hanabi | hanabi decrypt
```

Inspired by [Terminal Text Effects](https://github.com/ChrisBuilds/terminaltexteffects),
rewritten in Go from scratch. See [NOTICE.md](NOTICE.md).

## Why

The Python original is a pleasure to watch and a chore to install, and it
repaints far more of the screen than it needs to. hanabi is built around one
constraint: **a frame costs bytes proportional to what moved**, not to the size
of the grid. That matters when the terminal is on the other side of a screen
share rather than on your desk.

Measured on 80x24 ANSI art, a frame costs 411 bytes for `wipe` and 2.8kB for
`slide`, which moves every cell on every frame and is the worst case here. A
full repaint of that grid is around 38kB.

## Install

```console
$ go install github.com/crgimenes/hanabi@latest
```

Linux and macOS, amd64 and arm64. Builds with `CGO_ENABLED=0`.

## Use

```console
$ hanabi -list
$ hanabi wipe logo.txt
$ cat logo.txt | hanabi decrypt
$ hanabi -seed 42 -fps 30 decrypt logo.txt
$ hanabi wipe,decrypt logo.txt
```

Naming more than one effect **layers** them: they run together, over the same
frames, rather than one after the other. Each frame is rebuilt from the finished
text and handed down the chain, so an effect transforms what the one before it
produced -- `wipe,decrypt` sweeps the text in while the revealed characters are
still descrambling, and takes as long as the slower of the two.

Effects that mask, substitute or recolour compose in any order. Two effects that
both move characters will fight over the same cells; the last one wins.

`-loop` replays until interrupted -- the idle screen the project was written for,
something to leave running on a shared terminal. It runs straight through by
default; `-dwell` holds the finished text between passes when you want a pause.
Press **q** to jump straight to the finished text and exit; Ctrl-C aborts where
it is.

## ANSI art

Input may carry SGR colour sequences, so ANSI art animates like any other text:

```console
$ hanabi -loop wipe,decrypt ~/art/skull.ans
```

The 16 named colours are passed through as their own codes rather than resolved
to fixed RGB, so art written against the palette still follows the reader's
terminal theme. Bold is carried too, because on the named colours it selects the
bright half of the palette and art leans on that. 256-colour and truecolor are
read as the exact values they name.

Cursor-forward (`ESC[17C`), which old art uses to skip blanks instead of writing
spaces, is honoured. Any other escape sequence is dropped: drawing it would put
cells on screen that show nothing and shift the rest of the line.

**Input must be UTF-8.** That is a decision, not an omission -- guessing an
encoding only moves the mojibake somewhere harder to see. BBS-era `.ans` art is
usually CP437, and hanabi refuses it with the command that converts it:

```console
$ iconv -f CP437 -t UTF-8 old.ans | hanabi wipe
```

```console
$ hanabi -loop wipe,decrypt logo.txt
$ hanabi -loop -dwell 5s burn,matrix logo.txt
$ hanabi -loop "$(hanabi -list | cut -d';' -f1 | paste -sd, -)" logo.txt
```

The seed advances on every pass, so a loop does not repeat itself; a given
`-seed` still replays the whole sequence frame for frame.

The animation runs in a region reserved at the cursor and leaves the finished
text on screen, so it works inside a prompt or a script without taking over the
terminal. When standard output is not a terminal the text is printed unchanged,
which makes the command safe in the middle of a pipe.

`-debug` writes frame counts, byte totals and p50/p95/max frame build times to
standard error.

## Effects

| Name | Description |
|---|---|
| `burn` | A band of fire sweeps up through the text, recolouring as it goes |
| `decrypt` | Random glyphs settle one by one into the text |
| `matrix` | Columns of glyphs rain down, revealing the text behind them |
| `slide` | The whole picture slides in from the left |
| `typing` | Somebody types the text, uneven, pausing, backspacing over mistakes |
| `wipe` | A diagonal sweep reveals the text from the top-left |

`burn` only recolours and `slide` only moves; the rest hide and substitute. That
is what decides how they layer: anything composes with `burn`, and `slide` is the
only one that would fight another mover for the same cells.

`typing` runs at human speed -- around 160ms a character, mistakes and all -- so
it suits a line or a short paragraph. A thousand characters take over two
minutes, and a run that reaches the five-minute ceiling stops the way `q` does,
with the whole text on screen.

## Dependencies

`golang.org/x/term`, for the terminal size and the is-a-terminal check. That is
the whole list.

## License

MIT. See [LICENSE](LICENSE).
