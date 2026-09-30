// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"testing"

	"gioui.org/app/internal/windows"
)

func TestReframedKeepsWhatIsSeen(t *testing.T) {
	seen := windows.Rect{Left: 100, Top: 50, Right: 1300, Bottom: 810}
	unseen := windows.Rect{Left: 7, Right: 7, Bottom: 7}
	// Without the system's frame the window is what is seen of it.
	if got := reframed(seen, false, unseen); got != seen {
		t.Errorf("without a frame: %+v, want %+v", got, seen)
	}
	// With it the window has the borders that are not seen around that.
	framed := reframed(seen, true, unseen)
	if want := (windows.Rect{Left: 93, Top: 50, Right: 1307, Bottom: 817}); framed != want {
		t.Errorf("with the frame: %+v, want %+v", framed, want)
	}
	// And what is seen of that window is what was seen before.
	back := windows.Rect{Left: framed.Left + unseen.Left, Top: framed.Top + unseen.Top, Right: framed.Right - unseen.Right, Bottom: framed.Bottom - unseen.Bottom}
	if back != seen {
		t.Errorf("there and back: %+v, want %+v", back, seen)
	}
}

func TestUnseenBordersOfAnOverlappedWindow(t *testing.T) {
	b := unseenBorders(windows.WS_OVERLAPPEDWINDOW)
	// The frame is at the sides and below; above is the caption, all seen.
	if b.Top != 0 || b.Left <= 0 || b.Left != b.Right || b.Left != b.Bottom {
		t.Errorf("the borders that are not seen: %+v", b)
	}
}
