// SPDX-License-Identifier: Unlicense OR MIT

//go:build (linux || freebsd) && !novulkan

package vulkan

import (
	"testing"
	"unsafe"

	"gioui.org/gpu/internal/driver"
	"gioui.org/internal/vk"
)

func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	inst, err := vk.CreateInstance()
	if err != nil {
		t.Skipf("no Vulkan: %v", err)
	}
	t.Cleanup(func() { vk.DestroyInstance(inst) })
	physDev, qFam, err := vk.ChoosePhysicalDevice(inst, 0)
	if err != nil {
		t.Skipf("no Vulkan device: %v", err)
	}
	dev, err := vk.CreateDeviceAndQueue(physDev, qFam)
	if err != nil {
		t.Skipf("no Vulkan device: %v", err)
	}
	t.Cleanup(func() { vk.DestroyDevice(dev) })
	d, err := newVulkanDevice(driver.Vulkan{
		PhysDevice:  unsafe.Pointer(physDev),
		Device:      unsafe.Pointer(dev),
		Format:      int(vk.FORMAT_R8G8B8A8_SRGB),
		QueueFamily: qFam,
	})
	if err != nil {
		t.Fatal(err)
	}
	b := d.(*Backend)
	t.Cleanup(b.Release)
	return b
}

// A texture made after another of its format and size was let go takes its
// image and memory, as the frames of an animation do, instead of making and
// freeing memory of the device for each; its sampler is shared. The images
// let go and not taken are destroyed some frames after.
func TestTexturesReuseImages(t *testing.T) {
	b := newTestBackend(t)
	newTex := func(w, h int) *Texture {
		t.Helper()
		tex, err := b.NewTexture(driver.TextureFormatSRGBA, w, h, driver.FilterLinear, driver.FilterLinear, driver.BufferBindingTexture)
		if err != nil {
			t.Fatal(err)
		}
		return tex.(*Texture)
	}
	a := newTex(240, 180)
	other := newTex(240, 180)
	if a.sampler != other.sampler {
		t.Error("two textures of the same filters made two samplers")
	}
	img := a.img
	a.Release()
	b.BeginFrame(nil, false, a.key.size())
	c := newTex(240, 180)
	if c.img != img {
		t.Fatal("a texture of the same size made a new image")
	}
	if c.layout != vk.IMAGE_LAYOUT_UNDEFINED {
		t.Errorf("the image taken is in layout %d", c.layout)
	}
	if d := newTex(120, 90); d.img == img {
		t.Fatal("a texture of another size took the image")
	}
	size := c.key.size()
	c.Release()
	// The first frame keeps it, as the frames before are done.
	for range freeImageFrames + 2 {
		b.BeginFrame(nil, false, size)
	}
	if len(b.freeImages) != 0 {
		t.Fatalf("%d images let go kept after %d frames", len(b.freeImages), freeImageFrames+1)
	}
}
