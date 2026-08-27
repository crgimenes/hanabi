package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseShowRecordsStepsInOrder(t *testing.T) {
	steps, err := parseShow(`
		(shot "wipe" "hello")
		(pause 1.5)
		(wait-key)
		(clear)
		(shot "decrypt,burn" "again")
	`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]showStepKind, 0, len(steps))
	for _, st := range steps {
		kinds = append(kinds, st.kind)
	}
	want := []showStepKind{stepShot, stepPause, stepWaitKey, stepClear, stepShot}
	if len(kinds) != len(want) {
		t.Fatalf("recorded %d steps, want %d", len(kinds), len(want))
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("step %d is kind %d, want %d", i, kinds[i], want[i])
		}
	}
	if steps[1].hold != 1500*time.Millisecond {
		t.Fatalf("pause recorded %v, want 1.5s", steps[1].hold)
	}
	if len(steps[4].entries) != 2 {
		t.Fatalf("the second shot recorded %d effects, want 2", len(steps[4].entries))
	}
}

// The script language is the loop mechanism: repeating a shot is (map ... (range n)),
// and the steps land in order because evaluation is in order.
func TestParseShowLoopsViaTheLanguage(t *testing.T) {
	steps, err := parseShow(`(map (fn (i) (shot "wipe" "tick")) (range 3))`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 3 {
		t.Fatalf("recorded %d steps, want 3", len(steps))
	}
}

func TestParseShowResolvesFilesAgainstTheScriptDir(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "art.txt"), []byte("the art\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	steps, err := parseShow(`(shot "wipe" (file "art.txt"))`, dir)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimRight(rowText(steps[0]), " \n")
	if got != "the art" {
		t.Fatalf("the shot holds %q, want the file's text", got)
	}
}

func rowText(st showStep) string {
	var b strings.Builder
	for x := range st.target.W {
		b.WriteRune(st.target.At(x, 0).R)
	}
	return b.String()
}

// Every mistake a script can make has to come back as an error that names the
// builtin, not as a silent no-op or a crash.
func TestParseShowRefusesBadScripts(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "unknown effect", src: `(shot "nosuch" "x")`, want: "unknown effect"},
		{name: "empty text", src: `(shot "wipe" "")`, want: "empty"},
		{name: "shot arity", src: `(shot "wipe")`, want: "shot"},
		{name: "pause range", src: `(pause 9999)`, want: "0..3600"},
		{name: "pause type", src: `(pause "x")`, want: "pause"},
		{name: "wait-key arity", src: `(wait-key 1)`, want: "wait-key"},
		{name: "missing file", src: `(shot "wipe" (file "absent.txt"))`, want: "file"},
		{name: "no steps", src: `(+ 1 2)`, want: "no steps"},
		{name: "parse error", src: `(shot "wipe"`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseShow(tt.src, t.TempDir())
			if err == nil {
				t.Fatal("the script was accepted")
			}
			if tt.want != "" && !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("the error does not mention %q: %v", tt.want, err)
			}
		})
	}
}

// The cap is what stands between a runaway loop and half a gigabyte of
// playlist. The limits have to catch it whichever gives out first.
func TestParseShowCapsARunawayLoop(t *testing.T) {
	_, err := parseShow(`(map (fn (i) (shot "wipe" "x")) (range 100000))`, t.TempDir())
	if err == nil {
		t.Fatal("a hundred thousand shots were accepted")
	}
}

func TestParseShowRefusesNonUTF8Files(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "cp437.ans"), []byte{0x1b, '[', '1', 'm', 0xDC}, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseShow(`(shot "wipe" (file "cp437.ans"))`, dir)
	if err == nil || !strings.Contains(err.Error(), "iconv") {
		t.Fatalf("the error does not teach the iconv fix: %v", err)
	}
}

func TestShowTextCarriesEveryShotForPipes(t *testing.T) {
	steps, err := parseShow(`
		(shot "wipe" "one")
		(wait-key)
		(shot "burn" "two")
	`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out := showText(steps)
	for _, want := range []string{"one", "two"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the pipe text is missing %q: %q", want, out)
		}
	}
}

func testSession(keys chan key) *session {
	return &session{
		ctx:    context.Background(),
		keys:   keys,
		ticker: time.NewTicker(time.Millisecond),
	}
}

// The keys arrive after holdKey is already waiting: anything sent before it
// starts is a stale key from the animation, and the drain throws those away.
func TestHoldKeyAdvancesOnAnyKeyAndQuitsOnQ(t *testing.T) {
	press := func(k key) error {
		keys := make(chan key, 1)
		s := testSession(keys)
		defer s.ticker.Stop()
		done := make(chan error, 1)
		go func() { done <- holdKey(s) }()
		time.Sleep(20 * time.Millisecond)
		keys <- k
		select {
		case err := <-done:
			return err
		case <-time.After(2 * time.Second):
			t.Fatal("holdKey never returned")
			return nil
		}
	}

	if err := press(keyAdvance); err != nil {
		t.Fatalf("an ordinary key gave %v, want to advance", err)
	}
	if err := press(keyQuit); !errors.Is(err, errQuit) {
		t.Fatalf("q gave %v, want errQuit", err)
	}
}

// Keys pressed during the animation must not skip screens: an escape sequence
// arrives as several bytes, each of which lands as its own advance.
func TestHoldKeyDrainsStaleKeysBeforeWaiting(t *testing.T) {
	keys := make(chan key, 1)
	s := testSession(keys)
	defer s.ticker.Stop()
	keys <- keyAdvance

	done := make(chan error, 1)
	go func() { done <- holdKey(s) }()
	select {
	case err := <-done:
		t.Fatalf("holdKey returned %v on a stale key; it must wait for a fresh one", err)
	case <-time.After(50 * time.Millisecond):
	}
	keys <- keyAdvance
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the fresh key gave %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the fresh key never advanced")
	}
}

func TestHoldForWaitsAndReactsToQuit(t *testing.T) {
	s := testSession(make(chan key, 1))
	defer s.ticker.Stop()
	start := time.Now()
	err := holdFor(s, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 25*time.Millisecond {
		t.Fatal("holdFor returned early")
	}

	keys := make(chan key, 1)
	s2 := testSession(keys)
	defer s2.ticker.Stop()
	keys <- keyQuit
	err = holdFor(s2, time.Hour)
	if !errors.Is(err, errQuit) {
		t.Fatalf("q during a pause gave %v, want errQuit", err)
	}
}
