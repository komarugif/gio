// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/unit"
)

// The valuators of an event are packed: each bit set in the mask, lowest
// first, has the next value.
func TestXI2Valuators(t *testing.T) {
	got := xi2Valuators([]byte{0b1011, 0b10}, []float64{1, 2, 3, 4})
	want := []xi2Valuator{{0, 1}, {1, 2}, {3, 3}, {9, 4}}
	if len(got) != len(want) {
		t.Fatalf("decoded %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("decoded %v, want %v", got, want)
		}
	}
	// A mask with more bits than values decodes what there is.
	if got := xi2Valuators([]byte{0b111}, []float64{5}); len(got) != 1 || got[0] != (xi2Valuator{0, 5}) {
		t.Fatalf("short values decoded as %v", got)
	}
}

// A touchpad of xf86-input-libinput 1.4, as on Linux Mint 22: valuators 0
// and 1 move the pointer, 2 and 3 scroll, by 120 a unit, a notch of a
// wheel, which its fingers make in 15 of libinput's units, 8 each.
func TestXI2Scroll(t *testing.T) {
	d := &xi2Device{axes: []xi2Axis{{number: 2, horizontal: true, increment: 120}, {number: 3, increment: 120}}, touchpad: true}
	step := func(emulated bool, vals ...xi2Valuator) (f32.Point, bool) {
		t.Helper()
		return d.scroll(vals, emulated)
	}
	// The first value is where scrolling starts from.
	if s, moves := step(false, xi2Valuator{3, 1000}); s != (f32.Point{}) || moves {
		t.Fatalf("first event: scroll %v, moves %v", s, moves)
	}
	// A unit down is a notch's xi2ScrollUnit; fingers' 15 units an eighth.
	if s, _ := step(false, xi2Valuator{3, 1120}); s != f32.Pt(0, xi2ScrollUnit) {
		t.Fatalf("a unit down scrolled %v", s)
	}
	if s, _ := step(false, xi2Valuator{3, 1120 - 15}); s != f32.Pt(0, -xi2ScrollUnit/8.) {
		t.Fatalf("15 up scrolled %v", s)
	}
	// Horizontal scrolling starts from its own value.
	step(false, xi2Valuator{2, -50})
	if s, moves := step(false, xi2Valuator{2, 70}, xi2Valuator{3, 1105 + 60}); s != f32.Pt(xi2ScrollUnit, xi2ScrollUnit/2.) || moves {
		t.Fatalf("both axes scrolled %v, moves %v", s, moves)
	}
	// A motion of the pointer moves; it scrolls as well when it has the
	// scroll valuators.
	if s, moves := step(false, xi2Valuator{0, 3}, xi2Valuator{1, 4}); s != (f32.Point{}) || !moves {
		t.Fatalf("pointer motion: scroll %v, moves %v", s, moves)
	}
	// Valuators the server emulates from a wheel's buttons are not counted,
	// the buttons are; the next event goes on from them.
	if s, _ := step(true, xi2Valuator{3, 1165 + 120}); s != (f32.Point{}) {
		t.Fatalf("emulated scrolling scrolled %v", s)
	}
	if s, _ := step(false, xi2Valuator{3, 1285 + 120}); s != f32.Pt(0, xi2ScrollUnit) {
		t.Fatalf("after emulated scrolling a unit scrolled %v", s)
	}
	// After a reset, as when the pointer comes back, the next value starts
	// over, whatever the device scrolled elsewhere.
	d.reset()
	if s, _ := step(false, xi2Valuator{3, 99999}); s != (f32.Point{}) {
		t.Fatalf("after a reset scrolled %v", s)
	}
	// A device not known, or not scrolling, only moves.
	var unknown *xi2Device
	if s, moves := unknown.scroll([]xi2Valuator{{3, 5}}, false); s != (f32.Point{}) || !moves {
		t.Fatalf("unknown device: scroll %v, moves %v", s, moves)
	}
	plain := &xi2Device{}
	if s, moves := plain.scroll([]xi2Valuator{{0, 5}, {1, 6}}, false); s != (f32.Point{}) || !moves {
		t.Fatalf("plain device: scroll %v, moves %v", s, moves)
	}
}

// The first event of a wheel after the pointer came in has nothing to
// scroll from; the buttons the server emulates from it scroll instead, so
// that its notch is not lost, and only then. A touchpad's are not.
func TestXI2FirstNotch(t *testing.T) {
	wheel := &xi2Device{axes: []xi2Axis{{number: 2, horizontal: true, increment: 120}, {number: 3, increment: 120}}}
	if wheel.countsEmulated(5) {
		t.Fatal("emulated buttons counted before any scrolling")
	}
	wheel.scroll([]xi2Valuator{{3, 120}}, false)
	if !wheel.countsEmulated(5) || wheel.countsEmulated(7) {
		t.Fatalf("after the first vertical notch: vertical %v, horizontal %v", wheel.countsEmulated(5), wheel.countsEmulated(7))
	}
	wheel.scroll([]xi2Valuator{{3, 240}}, false)
	if wheel.countsEmulated(5) {
		t.Fatal("emulated buttons counted after a notch that scrolled")
	}
	wheel.reset()
	wheel.scroll([]xi2Valuator{{2, -120}}, false)
	if !wheel.countsEmulated(6) || wheel.countsEmulated(4) {
		t.Fatalf("after a reset and a horizontal notch: horizontal %v, vertical %v", wheel.countsEmulated(6), wheel.countsEmulated(4))
	}
	touchpad := &xi2Device{axes: wheel.axes, touchpad: true}
	touchpad.scroll([]xi2Valuator{{3, 50}}, false)
	if touchpad.countsEmulated(5) {
		t.Fatal("a touchpad's emulated buttons counted")
	}
}

// X11 does not tell when a touchpad's fingers leave it: a pause of
// xi2FlingPause in its scrolling stands for that, and the list goes on as
// Gio makes it go on on Wayland. A new scroll or a press stops it.
func TestXI2Fling(t *testing.T) {
	m := unit.Metric{PxPerDp: 1, PxPerSp: 1}
	start := time.Unix(1000, 0)
	var f xi2Fling
	var got f32.Point
	emit := func(e pointer.Event) { got = got.Add(e.Scroll) }
	// swipe scrolls n events 7 ms apart, as a touchpad does, by units each,
	// and returns when the last came.
	swipe := func(n int, units float32, gap time.Duration) time.Time {
		var now time.Time
		for i := range n {
			at := time.Duration(i) * gap
			now = start.Add(at)
			f.scrolled(pointer.Event{Kind: pointer.Scroll, Scroll: f32.Pt(0, units), Time: at}, now)
		}
		return now
	}
	if w := f.wait(start); w != -1 {
		t.Fatalf("waits %d ms with nothing scrolling", w)
	}
	last := swipe(20, 10, 7*time.Millisecond)
	if w := f.wait(last.Add(10 * time.Millisecond)); w != 30 {
		t.Fatalf("10 ms after the last event, waits %d ms", w)
	}
	if f.step(last.Add(39*time.Millisecond), m, emit) || got != (f32.Point{}) {
		t.Fatalf("flung %v before the pause", got)
	}
	if !f.step(last.Add(xi2FlingPause), m, emit) {
		t.Fatal("no fling after the pause")
	}
	for ms := 41; f.step(last.Add(time.Duration(ms)*time.Millisecond), m, emit); ms++ {
	}
	// 10 units in 7 ms is about 1430 a second, which Gio's drag of 4.2 a
	// second brings to rest in 340 units, the way the fingers went.
	if got.X != 0 || got.Y < 320 || got.Y > 345 {
		t.Fatalf("flung %v, want about 340 down", got)
	}

	// Fingers that all but stop do not fling.
	f, got = xi2Fling{}, f32.Point{}
	last = swipe(20, 0.3, 9*time.Millisecond)
	for ms := 0; ms < 500; ms++ {
		f.step(last.Add(time.Duration(ms)*time.Millisecond), m, emit)
	}
	if got != (f32.Point{}) {
		t.Fatalf("a slow scroll flung %v", got)
	}

	// A new scroll stops the fling, as does a press (stop).
	f, got = xi2Fling{}, f32.Point{}
	last = swipe(20, -10, 7*time.Millisecond)
	f.step(last.Add(xi2FlingPause), m, emit)
	f.step(last.Add(xi2FlingPause+5*time.Millisecond), m, emit)
	if !f.anim.Active() || got.Y >= 0 {
		t.Fatalf("an upward swipe flung %v", got)
	}
	f.scrolled(pointer.Event{Kind: pointer.Scroll, Scroll: f32.Pt(0, 1), Time: time.Second}, last.Add(50*time.Millisecond))
	before := got
	if f.step(last.Add(51*time.Millisecond), m, emit) || got != before {
		t.Fatal("the fling went on after a new scroll")
	}
	// A press in a swipe, or right after it, leaves nothing to fling.
	f.stop()
	if f.wait(last.Add(51*time.Millisecond)) != -1 || f.step(last.Add(50*time.Millisecond+xi2FlingPause), m, emit) || got != before {
		t.Fatal("a fling started after a press")
	}
}
