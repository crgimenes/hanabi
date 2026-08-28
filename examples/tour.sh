#!/usr/bin/env bash
# A slide deck: each slide is one hanabi run. Any key skips the animation on
# screen; the pauses between slides are the script's own.
#   examples/tour.sh
set -euo pipefail
cd "$(dirname "$0")"
HANABI=${HANABI:-hanabi}

cls() {
	printf '\033[2J\033[H'
}

# Wait for the reader between slides. A key pressed during the animation only
# skipped it, so this is a second, deliberate press.
key() {
	read -rsn1 </dev/tty
}

cls
"$HANABI" fireworks,burn art/hanabi.txt
sleep 1.2

cls
"$HANABI" matrix art/invader.txt
sleep 0.6
"$HANABI" -speed 0.6 spotlight art/invader.txt
"$HANABI" glitch,beams art/invader.txt
key

cls
"$HANABI" expand,colorshift art/heart.txt
sleep 1

cls
"$HANABI" -speed 4 typing art/iceberg.txt
key

cls
# Bacchikoi -- バッチコイ, "bring it on!" -- drawn in braille by crg.
"$HANABI" blackhole,sweep art/bacchikoi.txt
sleep 0.8
"$HANABI" doomfire art/bacchikoi.txt
key

cls
"$HANABI" fireworks,burn finale.txt
