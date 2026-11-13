// SPDX-License-Identifier: Unlicense OR MIT

//go:build (linux || freebsd) && !novulkan

package vulkan

import (
	"image"
	"testing"
	"unsafe"

	"gioui.org/gpu/internal/driver"
	"gioui.org/internal/byteslice"
	"gioui.org/internal/vk"
	"gioui.org/shader"
	"gioui.org/shader/gio"
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
	if b.freeBytes != 0 || len(b.freeImages) != 0 {
		t.Fatalf("%d images let go kept after %d frames", len(b.freeImages), freeImageFrames+1)
	}
}

// A draw with the pipeline and textures of one before in the frame binds
// nothing anew and writes no other descriptor set: the gpu package binds
// them for each draw, and each call to C cost the Vulkan backend more CPU
// than OpenGL. A command buffer begins with nothing bound.
func TestDrawsBindOnce(t *testing.T) {
	b := newTestBackend(t)
	newTex := func(binding driver.BufferBinding) driver.Texture {
		t.Helper()
		tex, err := b.NewTexture(driver.TextureFormatSRGBA, 16, 16, driver.FilterNearest, driver.FilterNearest, binding)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(tex.Release)
		return tex
	}
	fbo := newTex(driver.BufferBindingFramebuffer)
	img1, img2 := newTex(driver.BufferBindingTexture), newTex(driver.BufferBindingTexture)
	vsh, err := b.NewVertexShader(gio.Shader_copy_vert)
	if err != nil {
		t.Fatal(err)
	}
	defer vsh.Release()
	fsh, err := b.NewFragmentShader(gio.Shader_copy_frag)
	if err != nil {
		t.Fatal(err)
	}
	defer fsh.Release()
	pipe, err := b.NewPipeline(driver.PipelineDesc{
		VertexShader:   vsh,
		FragmentShader: fsh,
		VertexLayout: driver.VertexLayout{
			Inputs: []driver.InputDesc{{Type: shader.DataTypeFloat, Size: 2, Offset: 0}, {Type: shader.DataTypeFloat, Size: 2, Offset: 8}},
			Stride: 16,
		},
		PixelFormat: driver.TextureFormatSRGBA,
		Topology:    driver.TopologyTriangleStrip,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Release()
	verts, err := b.NewImmutableBuffer(driver.BufferBindingVertices, byteslice.Slice([]float32{-1, -1, 0, 0, 1, -1, 1, 0, -1, 1, 0, 1, 1, 1, 1, 1}))
	if err != nil {
		t.Fatal(err)
	}
	defer verts.Release()
	uniforms, err := b.NewBuffer(driver.BufferBindingUniforms, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer uniforms.Release()
	p := pipe.(*Pipeline)

	b.BeginFrame(nil, false, fbo.(*Texture).key.size())
	b.BeginRenderPass(fbo, driver.LoadDesc{Action: driver.LoadActionClear})
	draw := func(img driver.Texture) {
		b.BindPipeline(pipe)
		b.BindTexture(0, img)
		b.BindVertexBuffer(verts, 0)
		b.BindUniforms(uniforms)
		b.Viewport(0, 0, 16, 16)
		b.DrawArrays(0, 4)
	}
	draw(img1)
	b.BindPipeline(pipe)
	if b.desc.dirty {
		t.Error("the pipeline bound was bound anew, with its descriptors")
	}
	draw(img1)
	if p.desc.size != 1 {
		t.Errorf("two draws of one texture wrote %d descriptor sets", p.desc.size)
	}
	draw(img2)
	draw(img1)
	if p.desc.size != 2 {
		t.Errorf("draws of two textures wrote %d descriptor sets", p.desc.size)
	}
	if b.bound.pipe != p || b.bound.vertBuf == 0 || b.bound.viewport.Empty() {
		t.Errorf("bound %+v", b.bound)
	}
	b.EndRenderPass()
	b.EndFrame()
	b.BeginFrame(nil, false, fbo.(*Texture).key.size())
	b.ensureCmdBuf()
	if b.bound != (boundState{}) {
		t.Errorf("a new command buffer has %+v bound", b.bound)
	}
	b.EndFrame()
}

// The images let go are kept by their memory, not their count: a list of
// GIFs lets two dozen frames go every frame, and each was made anew with
// a bound of sixteen.
func TestManyTexturesReuseImages(t *testing.T) {
	b := newTestBackend(t)
	const n = 32
	newTex := func() *Texture {
		t.Helper()
		tex, err := b.NewTexture(driver.TextureFormatSRGBA, 128, 96, driver.FilterLinear, driver.FilterLinear, driver.BufferBindingTexture)
		if err != nil {
			t.Fatal(err)
		}
		return tex.(*Texture)
	}
	var texs []*Texture
	for range n {
		texs = append(texs, newTex())
	}
	for _, tex := range texs {
		tex.Release()
	}
	b.BeginFrame(nil, false, image.Pt(128, 96))
	// A driver gives a new image the handle of one destroyed: the images
	// kept tell what is taken.
	if len(b.freeImages) != n {
		t.Fatalf("%d of %d images let go kept", len(b.freeImages), n)
	}
	for range n {
		newTex()
	}
	if b.freeBytes != 0 || len(b.freeImages) != 0 {
		t.Fatalf("%d images of %d bytes kept, all taken", len(b.freeImages), b.freeBytes)
	}
}
