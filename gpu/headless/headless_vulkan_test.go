// SPDX-License-Identifier: Unlicense OR MIT

//go:build (linux || freebsd) && !novulkan

package headless

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// A frame drawn with Vulkan allocates no Go memory for each command it
// records: barriers, viewports and descriptors went to C through pointers
// to Go memory, which escaped to the heap, about 150 allocations a frame of
// the messenger's.
func TestVulkanFrameAllocs(t *testing.T) {
	ctx, err := newVulkanContext()
	if err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	w, err := newWindow(ctx, 400, 300)
	if err != nil {
		t.Skipf("no Vulkan window: %v", err)
	}
	defer w.Release()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	imgOp := paint.NewImageOp(img)
	ops := new(op.Ops)
	for i := range 20 {
		r := image.Rect(i*15, i*10, i*15+60, i*10+40)
		st := clip.RRect{Rect: r, SE: 8, SW: 8, NE: 8, NW: 8}.Push(ops)
		if i%2 == 0 {
			imgOp.Add(ops)
		} else {
			paint.ColorOp{Color: color.NRGBA{R: byte(i * 10), A: 0xff}}.Add(ops)
		}
		paint.PaintOp{}.Add(ops)
		st.Pop()
	}
	for range 3 {
		if err := w.Frame(ops); err != nil {
			t.Fatal(err)
		}
	}
	allocs := testing.AllocsPerRun(20, func() {
		if err := w.Frame(ops); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 5 {
		t.Errorf("a frame allocates %v times", allocs)
	}
}
