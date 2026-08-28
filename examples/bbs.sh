#!/usr/bin/env bash
# A make-believe BBS front door, animated by hanabi.
#   examples/bbs.sh
#
# Every screen is one hanabi run. A key pressed during an animation only skips
# it -- the menu reads the next press, the way a real board redrew the screen
# before listening.
set -euo pipefail
cd "$(dirname "$0")"
HANABI=${HANABI:-hanabi}

cls() {
	printf '\033[2J\033[H'
}

key() {
	read -rsn1 </dev/tty
}

# slow (github.com/crgimenes/slow) paces text at the bps of the modem the
# screen pretends to be; without it installed the text just arrives at once.
# Point SLOW at a binary to override. Keys do not skip it -- the line is the
# line -- though Ctrl-C cuts the screen short.
SLOW=${SLOW:-slow}
command -v "$SLOW" >/dev/null || SLOW=
modem() {
	if [ -n "$SLOW" ]; then
		"$SLOW" -b "$1"
	else
		cat
	fi
}

dial() {
	cls
	"$HANABI" -speed 2 typing <<-'EOF'
		ATDT 5550134
	EOF
	sleep 1
	modem 300 <<-'EOF'

		CONNECT 2400/ARQ

	EOF
	sleep 0.5
	{
		cat art/hanabi.txt
		printf '\n      b  b  s  ·  node 1  ·  est. 1994\n'
	} | "$HANABI" fireworks,burn
	sleep 1
}

menu() {
	cls
	"$HANABI" -speed 10 typing <<-'EOF'
		┌───────────────────────────────┐
		│          hanabi bbs           │
		│   free callers since  1994    │
		├───────────────────────────────┤
		│   [1]  message boards         │
		│   [2]  file areas             │
		│   [3]  door games             │
		│   [4]  page the sysop         │
		│   [g]  goodbye                │
		└───────────────────────────────┘
	EOF
	read -rsn1 choice </dev/tty
}

# The one screen with no effect at all: a message board post arrives at line
# speed, which was the original animation.
boards() {
	cls
	modem 1200 <<-'EOF'
		:: message boards ::

		from: sysop
		subj: welcome, caller

		  the fireworks are free.
		  leave a message after the beep.

		(any key returns to the menu)
	EOF
	key
}

files() {
	cls
	"$HANABI" wipe art/iceberg.txt
	key
}

games() {
	cls
	{
		cat art/invader.txt
		printf '\ninsert coin -- any key returns\n'
	} | "$HANABI" matrix,burn
	key
}

sysop() {
	cls
	{
		cat art/bunny.txt
		printf '\nthe sysop is asleep.\nthe bunny took the page.\n'
	} | "$HANABI" scattered
	key
}

goodbye() {
	cls
	# Bacchikoi -- バッチコイ, "bring it on!" -- drawn in braille by crg.
	"$HANABI" doomfire art/bacchikoi.txt
	modem 300 <<-'EOF'

		+++
		NO CARRIER
	EOF
}

dial
while true; do
	menu
	case $choice in
	1) boards ;;
	2) files ;;
	3) games ;;
	4) sysop ;;
	g | G) break ;;
	esac
done
goodbye
