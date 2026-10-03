// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"math"
	"math/bits"
	"time"

	"gioui.org/f32"
	"gioui.org/internal/fling"
	"gioui.org/io/pointer"
	"gioui.org/unit"
)

// xi2ScrollUnit is a unit of smooth scrolling of XInput 2.1, a scroll
// valuator's increment, in the units of Gio's scroll events on X11: the
// core protocol sends a notch of a wheel as a press and a release of its
// button, of 10 each. A notch scrolls as far either way.
const xi2ScrollUnit = 20

// xi2Axis is a scroll valuator of a device.
type xi2Axis struct {
	number     int
	horizontal bool
	// increment is the valuator's change for one unit of scrolling, a
	// notch of a wheel.
	increment float64
}

// xi2Device is what is known of a slave pointer device: its scroll
// valuators, if any, and whether it is a touchpad.
type xi2Device struct {
	name     string
	axes     []xi2Axis
	touchpad bool
	// last holds each scroll valuator's last value. Events carry the
	// valuators' running totals, so scrolling is the change from it.
	last map[int]float64
	// unbased tells, for the vertical and the horizontal axis, that the
	// last event of the device had no last value to scroll from, as the
	// first after the pointer came in. The buttons the server emulates
	// from that event scroll in its stead (countsEmulated), so that a
	// wheel's first notch is not lost.
	unbased [2]bool
}

// xi2Valuator is a valuator's value in an event.
type xi2Valuator struct {
	number int
	value  float64
}

// xi2Valuators decodes the valuators of an event: each bit set in mask,
// lowest first, has the next of the packed values.
func xi2Valuators(mask []byte, values []float64) []xi2Valuator {
	var out []xi2Valuator
	for i, b := range mask {
		for b != 0 {
			bit := bits.TrailingZeros8(b)
			b &^= 1 << bit
			if len(out) == len(values) {
				return out
			}
			out = append(out, xi2Valuator{number: i*8 + bit, value: values[len(out)]})
		}
	}
	return out
}

// reset forgets the last values, so that the next event scrolls nothing
// rather than by what the device scrolled where this window did not see
// it, as when the pointer was over another window.
func (d *xi2Device) reset() {
	clear(d.last)
	d.unbased = [2]bool{}
}

// scroll turns the valuators of a motion event into scrolling, in the
// units of xi2ScrollUnit, and tells whether the event moves the pointer
// too, through valuators that do not scroll. An emulated event, made by
// the server from the buttons of a wheel that sends them, is not counted:
// those buttons are. Its values are kept still, as the next event goes on
// from them.
func (d *xi2Device) scroll(vals []xi2Valuator, emulated bool) (s f32.Point, moves bool) {
	if d == nil {
		return f32.Point{}, len(vals) > 0
	}
	for _, v := range vals {
		axis, ok := d.axis(v.number)
		if !ok {
			moves = true
			continue
		}
		last, known := d.last[v.number]
		if d.last == nil {
			d.last = map[int]float64{}
		}
		d.last[v.number] = v.value
		if !emulated {
			d.unbased[axisIndex(axis.horizontal)] = !known
		}
		if !known || emulated || axis.increment == 0 {
			continue
		}
		delta := float32((v.value - last) / axis.increment * xi2ScrollUnit)
		if axis.horizontal {
			s.X += delta
		} else {
			s.Y += delta
		}
	}
	return s, moves
}

// countsEmulated tells whether an emulated press or release of button,
// 4 to 7, scrolls: when the event it was made from had nothing to scroll
// from. A touchpad's are not counted; a fraction of a notch is lost there.
func (d *xi2Device) countsEmulated(button uint) bool {
	if d == nil || d.touchpad {
		return false
	}
	return d.unbased[axisIndex(button >= 6)]
}

func axisIndex(horizontal bool) int {
	if horizontal {
		return 1
	}
	return 0
}

func (d *xi2Device) axis(number int) (xi2Axis, bool) {
	if d == nil {
		return xi2Axis{}, false
	}
	for _, a := range d.axes {
		if a.number == number {
			return a, true
		}
	}
	return xi2Axis{}, false
}

// xi2FlingPause is how long a touchpad sends no scrolling before its
// fingers are taken to have left it. X11 does not tell: libinput ends a
// scroll with a stop, which xf86-input-libinput passes on as no change,
// and the server drops. A touchpad scrolling sends an event every 7 to
// 9 ms; longer pauses come when its fingers all but stop, too slowly to
// fling. 40 ms is fling.Extrapolation's longest gap within a gesture.
const xi2FlingPause = 40 * time.Millisecond

// xi2Fling is the kinetic scrolling after a touchpad's, as Gio makes it on
// Wayland after the compositor tells the fingers left (axis_stop).
type xi2Fling struct {
	x, y fling.Extrapolation
	anim fling.Animation
	dir  f32.Point
	// velocity is the kinetic scrolling's at its start, for tracing.
	velocity float32
	// scrolling is set while a touchpad scrolls: since its last event,
	// at last, less than xi2FlingPause passed.
	scrolling bool
	last      time.Time
	// ev is the touchpad's last scroll event, for the place, the buttons
	// and the modifiers of the kinetic scrolling.
	ev pointer.Event
}

// scrolled samples a touchpad's scroll event e, which came at now, for the
// kinetic scrolling after it, and stops the one before.
func (f *xi2Fling) scrolled(e pointer.Event, now time.Time) {
	f.anim = fling.Animation{}
	if !f.scrolling {
		f.x, f.y = fling.Extrapolation{}, fling.Extrapolation{}
	}
	f.scrolling, f.last, f.ev = true, now, e
	// As on Wayland: the estimate is of the opposite of the distance.
	f.x.SampleDelta(e.Time, -e.Scroll.X)
	f.y.SampleDelta(e.Time, -e.Scroll.Y)
}

// stop ends the kinetic scrolling, and forgets the touchpad's.
func (f *xi2Fling) stop() {
	f.anim = fling.Animation{}
	f.scrolling = false
}

// wait is how long, in milliseconds from now, until the touchpad's
// scrolling is taken to have ended; -1 when none scrolls.
func (f *xi2Fling) wait(now time.Time) int {
	if !f.scrolling {
		return -1
	}
	return max(0, int(math.Ceil(float64(f.last.Add(xi2FlingPause).Sub(now))/float64(time.Millisecond))))
}

// step starts the kinetic scrolling when the touchpad's has ended, as it
// does on Wayland, and moves it on to now, giving its scroll events to
// emit. It tells whether it goes on.
func (f *xi2Fling) step(now time.Time, m unit.Metric, emit func(pointer.Event)) bool {
	if f.scrolling && now.Sub(f.last) >= xi2FlingPause {
		f.scrolling = false
		estx, esty := f.x.Estimate(), f.y.Estimate()
		vel := float32(math.Hypot(float64(estx.Velocity), float64(esty.Velocity)))
		if f.anim.Start(m, now, vel) {
			f.dir = f32.Pt(estx.Velocity/vel, esty.Velocity/vel)
			f.velocity = vel
		}
	}
	if !f.anim.Active() {
		return false
	}
	if d := f.anim.Tick(now); d != 0 {
		e := f.ev
		e.Scroll = f.dir.Mul(float32(d))
		e.Time += now.Sub(f.last)
		emit(e)
	}
	return f.anim.Active()
}
