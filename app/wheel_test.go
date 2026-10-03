// SPDX-License-Identifier: Unlicense OR MIT

package app

import "testing"

// Distances measured on Windows 11: a wheel's notches, one or joined, and
// a precision touchpad's slow and fast scrolling.
func TestIsWheelDelta(t *testing.T) {
	for _, d := range []float32{120, -120, -240, -360} {
		if !isWheelDelta(d) {
			t.Errorf("%v is not told as notches", d)
		}
	}
	for _, d := range []float32{0, 1, 2, -4, -127, -183, -799} {
		if isWheelDelta(d) {
			t.Errorf("%v is told as notches", d)
		}
	}
}
