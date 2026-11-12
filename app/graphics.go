// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"sync"
	"sync/atomic"
)

// GraphicsAPI is a graphics API windows can be drawn with where there is
// a choice of them: on Wayland, OpenGL through EGL or Vulkan.
type GraphicsAPI uint8

const (
	// GraphicsAuto leaves the API to Gio: OpenGL, Vulkan where OpenGL
	// fails.
	GraphicsAuto GraphicsAPI = iota
	// GraphicsOpenGL draws with OpenGL, Vulkan where OpenGL fails.
	GraphicsOpenGL
	// GraphicsVulkan draws with Vulkan, OpenGL where Vulkan fails.
	GraphicsVulkan
)

// VulkanDeviceKind is the kind of a Vulkan device.
type VulkanDeviceKind uint8

const (
	VulkanDeviceOther VulkanDeviceKind = iota
	VulkanDeviceIntegrated
	VulkanDeviceDiscrete
	VulkanDeviceVirtual
)

// VulkanDevice is a Vulkan device and the driver that has it, as
// VulkanDevices lists them.
type VulkanDevice struct {
	// Driver is the file name of the driver's manifest, such as
	// intel_icd.x86_64.json. A device with a driver is drawn with that
	// driver alone loaded, which wakes no other GPU up. Empty is every
	// driver, where the loader cannot load one alone.
	Driver             string
	VendorID, DeviceID uint32
	Name               string
	Kind               VulkanDeviceKind
	// API is the version of Vulkan the device has, Version its driver's.
	API, Version string
}

// Graphics is how windows are drawn where there is a choice.
type Graphics struct {
	API GraphicsAPI
	// Vulkan is the device windows are drawn with by Vulkan; the zero
	// value is the first device that can draw them, of any driver.
	Vulkan VulkanDevice
	// VulkanFailed is that Vulkan could not draw a window with any device
	// of any driver: windows are drawn with OpenGL without trying Vulkan,
	// which would load every driver again. Gio sets it when Vulkan fails
	// so; a program that keeps it across starts passes it back to
	// SetGraphics, and passes it false for Vulkan to be tried again.
	VulkanFailed bool
}

var graphics atomic.Pointer[Graphics]

// SetGraphics sets how windows are drawn, for the GPU each window makes
// from now on; ResetGPUs has the windows that have one make it anew. The
// windows drawn with Vulkan share an instance of it, made with the device
// chosen: windows that make their GPU after the device changed share a new
// one, and the old is destroyed once the last window on it has let it go.
func SetGraphics(g Graphics) {
	graphics.Store(&g)
}

// gpuWindows is the windows that have a GPU.
var gpuWindows windowSet

type windowSet struct {
	sync.Mutex
	set map[*Window]struct{}
}

func (s *windowSet) add(w *Window) {
	s.Lock()
	defer s.Unlock()
	if s.set == nil {
		s.set = make(map[*Window]struct{})
	}
	s.set[w] = struct{}{}
}

func (s *windowSet) remove(w *Window) {
	s.Lock()
	defer s.Unlock()
	delete(s.set, w)
}

// ResetGPUs has every window that has a GPU drop it, with its context, and
// make it anew in its next frame, as SetGraphics set: another graphics API
// or device, without making the window anew. A window drops its GPU on its
// own goroutine, as it does when the device is lost; a minimized one when
// it draws again. The content's textures and buffers are made anew.
func ResetGPUs() {
	gpuWindows.Lock()
	windows := make([]*Window, 0, len(gpuWindows.set))
	for w := range gpuWindows.set {
		windows = append(windows, w)
	}
	gpuWindows.Unlock()
	for _, w := range windows {
		w.resetGPU.Store(true)
		w.Invalidate()
	}
}

// failVulkan sets Graphics.VulkanFailed, keeping what SetGraphics set.
func failVulkan() {
	for {
		old := graphics.Load()
		g := Graphics{}
		if old != nil {
			g = *old
		}
		if g.VulkanFailed {
			return
		}
		g.VulkanFailed = true
		if graphics.CompareAndSwap(old, &g) {
			return
		}
	}
}

// CurrentGraphics is what SetGraphics set last, with VulkanFailed set
// where Vulkan has failed since.
func CurrentGraphics() Graphics {
	if g := graphics.Load(); g != nil {
		return *g
	}
	return Graphics{}
}

// graphicsChoice and listVulkanDevices are set where there is a choice.
var (
	graphicsChoice    func() bool
	listVulkanDevices func() ([]VulkanDevice, error)
)

// GraphicsChoice reports whether the API windows are drawn with can be
// chosen: they are drawn on Wayland, with OpenGL and Vulkan both built
// in. It is false until a window has been made.
func GraphicsChoice() bool {
	return graphicsChoice != nil && graphicsChoice()
}

// VulkanDevices lists the Vulkan devices windows can be drawn with, those
// of each driver with the driver loaded alone. It wakes up every GPU asleep,
// which a discrete one in a laptop takes seconds to: call it when the
// devices are to be shown. Devices that draw on the CPU are left out.
func VulkanDevices() ([]VulkanDevice, error) {
	if listVulkanDevices == nil {
		return nil, nil
	}
	return listVulkanDevices()
}
