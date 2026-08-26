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

Measured on the benchmark grid, a frame of `decrypt` costs about 48 bytes; a
full repaint of the same grid runs past a kilobyte.

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
```

`-loop` replays until interrupted, cycling through the effects you name, with
`-dwell` holding the finished text in between. This is the idle screen the
project was written for -- something to leave running on a shared terminal.

```console
$ hanabi -loop -dwell 10s decrypt,wipe logo.txt
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
| `decrypt` | Random glyphs settle one by one into the text |
| `wipe` | A diagonal sweep reveals the text from the top-left |

## Dependencies

`golang.org/x/term`, for the terminal size and the is-a-terminal check. That is
the whole list.

## License

MIT. See [LICENSE](LICENSE).
