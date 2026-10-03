// SPDX-License-Identifier: Unlicense OR MIT

package app

import "math"

// isWheelDelta tells whether a WM_MOUSEWHEEL distance is of whole notches
// of a wheel (WHEEL_DELTA, 120, each), which Windows joins when the wheel
// turns fast. A precision touchpad and a free-spinning wheel send finer
// distances. It is here, not in os_windows.go, to be tested everywhere.
func isWheelDelta(dist float32) bool {
	return dist != 0 && math.Mod(float64(dist), 120) == 0
}
