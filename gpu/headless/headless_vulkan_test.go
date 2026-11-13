// SPDX-License-Identifier: Unlicense OR MIT

//go:build (linux || freebsd) && !novulkan

package headless

import (
	"image"
	"image/color"
	"testing"
	"unsafe"

	"gioui.org/f32"
	"gioui.org/internal/gpuchoice"
	"gioui.org/internal/vk"
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

// An image drawn smaller samples its mipmap levels, which Vulkan fills by
// blits in C (vk.CmdGenerateMipmaps): each must hold the image.
func TestVulkanMipmaps(t *testing.T) {
	ctx, err := newVulkanContext()
	if err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	w, err := newWindow(ctx, 32, 16)
	if err != nil {
		t.Skipf("no Vulkan window: %v", err)
	}
	defer w.Release()
	img := image.NewRGBA(image.Rect(0, 0, 256, 128))
	for y := range 128 {
		for x := range 256 {
			c := color.RGBA{R: 0xff, A: 0xff}
			if x >= 128 {
				c = color.RGBA{B: 0xff, A: 0xff}
			}
			img.SetRGBA(x, y, c)
		}
	}
	ops := new(op.Ops)
	op.Affine(f32.AffineId().Scale(f32.Point{}, f32.Pt(1.0/16, 1.0/16))).Add(ops)
	paint.NewImageOp(img).Add(ops)
	paint.PaintOp{}.Add(ops)
	if err := w.Frame(ops); err != nil {
		t.Fatal(err)
	}
	shot := image.NewRGBA(image.Rect(0, 0, 32, 16))
	if err := w.Screenshot(shot); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		x, y int
		want color.RGBA
	}{{2, 4, color.RGBA{R: 0xff, A: 0xff}}, {13, 4, color.RGBA{B: 0xff, A: 0xff}}} {
		if got := shot.RGBAAt(c.x, c.y); got != c.want {
			t.Errorf("at (%d, %d) %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

// A headless window is drawn as the program's windows are: with Vulkan
// first where they are, of their instance, which it holds until it is
// released, instead of OpenGL's drivers and an instance of every Vulkan
// driver.
func TestHeadlessFollowsWindows(t *testing.T) {
	defer func(p func() gpuchoice.API, v func() (unsafe.Pointer, uint32, uint32, func(), error)) {
		gpuchoice.Prefer, gpuchoice.VulkanInstance = p, v
	}(gpuchoice.Prefer, gpuchoice.VulkanInstance)
	inst, err := vk.CreateInstance()
	if err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	defer vk.DestroyInstance(inst)
	held := 0
	gpuchoice.Prefer = func() gpuchoice.API { return gpuchoice.Vulkan }
	gpuchoice.VulkanInstance = func() (unsafe.Pointer, uint32, uint32, func(), error) {
		held++
		return unsafe.Pointer(inst), 0, 0, func() { held-- }, nil
	}
	w, err := NewWindow(16, 16)
	if err != nil {
		t.Skipf("no headless window: %v", err)
	}
	c, ok := w.ctx.(*vkContext)
	if !ok || c.inst != inst || held != 1 {
		t.Fatalf("with windows on Vulkan, a headless window drew with %T, holding their instance %d times", w.ctx, held)
	}
	w.Release()
	if held != 0 {
		t.Fatal("the windows' instance was not let go")
	}
}
