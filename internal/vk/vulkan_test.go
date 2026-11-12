// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux || freebsd

package vk

import "testing"

// A swapchain's images are taken with premultiplied alpha where the surface
// allows it, for a transparent window to be transparent.
func TestCompositeAlpha(t *testing.T) {
	// VkCompositeAlphaFlagBitsKHR: opaque 1, premultiplied 2,
	// postmultiplied 4, inherit 8.
	for _, c := range []struct{ supported, want uint32 }{
		{1 | 2 | 8, 2},
		{2, 2},
		{1 | 8, 1},
		{4 | 8, 4},
		{8, 8},
	} {
		if got := compositeAlpha(c.supported); got != c.want {
			t.Errorf("compositeAlpha(%#x) = %#x, want %#x", c.supported, got, c.want)
		}
	}
}
