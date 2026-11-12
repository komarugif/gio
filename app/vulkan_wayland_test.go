// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd) && !nowayland && !novulkan

package app

import (
	"errors"
	"strings"
	"testing"
	"unsafe"

	"gioui.org/internal/vk"
)

// A driver of hardware that is not there loads and fails to list its
// devices: the others' are listed still, as the loader lists them with
// every driver. Its error is told only where no driver has a device.
func TestCollectVulkanDevices(t *testing.T) {
	failed := errors.New("vulkan: vkEnumeratePhysicalDevices: error -3")
	intel := vk.PhysicalDeviceInfo{Name: "Intel(R) Graphics (ADL GT2)", VendorID: 0x8086, DeviceID: 0x4626, Type: vk.PhysicalDeviceIntegrated}
	lavapipe := vk.PhysicalDeviceInfo{Name: "llvmpipe", VendorID: 0x10005, Type: vk.PhysicalDeviceCPU}
	drivers := []vk.Driver{{Manifest: "intel_icd.x86_64.json"}, {Manifest: "intel_icd.i686.json"}, {Manifest: "nouveau_icd.x86_64.json"}, {Manifest: "lvp_icd.x86_64.json"}}
	alone := func(d vk.Driver) ([]vk.PhysicalDeviceInfo, bool, error) {
		switch d.Manifest {
		case "intel_icd.x86_64.json":
			return []vk.PhysicalDeviceInfo{intel}, true, nil
		case "nouveau_icd.x86_64.json":
			return nil, true, failed
		case "lvp_icd.x86_64.json":
			return []vk.PhysicalDeviceInfo{lavapipe}, true, nil
		}
		return nil, false, nil
	}
	together := func() ([]vk.PhysicalDeviceInfo, error) { t.Fatal("listed every driver together"); return nil, nil }
	devices, err := collectVulkanDevices(drivers, alone, together)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Driver != "intel_icd.x86_64.json" || devices[0].DeviceID != 0x4626 {
		t.Fatalf("devices %+v", devices)
	}
	_, err = collectVulkanDevices(drivers[2:3], alone, together)
	if err == nil || !strings.Contains(err.Error(), "nouveau_icd.x86_64.json") {
		t.Fatalf("with no device: error %v", err)
	}
	// Where no driver loads alone, every driver together, none named.
	devices, err = collectVulkanDevices(drivers[1:2], alone, func() ([]vk.PhysicalDeviceInfo, error) { return []vk.PhysicalDeviceInfo{intel, lavapipe}, nil })
	if err != nil || len(devices) != 1 || devices[0].Driver != "" {
		t.Fatalf("together: %+v, %v", devices, err)
	}
}

// Windows share the instance made with the device chosen; another device
// chosen makes another for the windows that make their GPU after, and the
// old one goes with its last window, or at once if it has none.
func TestWaylandVulkanInstances(t *testing.T) {
	defer SetGraphics(Graphics{})
	defer func(m func(vulkanWant) (vk.Instance, string, error), d func(vk.Instance)) {
		makeWaylandVulkan, destroyWaylandVulkan = m, d
		waylandVulkan.current = nil
	}(makeWaylandVulkan, destroyWaylandVulkan)
	waylandVulkan.current = nil
	made := map[vk.Instance]VulkanDevice{}
	var destroyed []VulkanDevice
	// The garbage collector does not follow a vk.Instance, a pointer to
	// an incomplete C type: the fakes' memory is held here.
	var fakes []*byte
	makeWaylandVulkan = func(w vulkanWant) (vk.Instance, string, error) {
		fakes = append(fakes, new(byte))
		inst := vk.Instance(unsafe.Pointer(fakes[len(fakes)-1]))
		made[inst] = w.device
		return inst, w.device.Driver, nil
	}
	destroyWaylandVulkan = func(inst vk.Instance) { destroyed = append(destroyed, made[inst]) }
	intel := VulkanDevice{Driver: "intel_icd.x86_64.json", VendorID: 0x8086, DeviceID: 0x4626}
	nvidia := VulkanDevice{Driver: "nvidia_icd.json", VendorID: 0x10de, DeviceID: 0x25a2}
	acquire := func() *sharedVulkan {
		t.Helper()
		s, _, err := acquireWaylandVulkan()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	SetGraphics(Graphics{Vulkan: intel})
	a, b := acquire(), acquire()
	if a != b || len(made) != 1 {
		t.Fatalf("two windows on one device made %d instances", len(made))
	}
	SetGraphics(Graphics{Vulkan: nvidia})
	c := acquire()
	if c == a || c.want.device != nvidia {
		t.Fatalf("another device kept the instance of %+v", c.want.device)
	}
	releaseWaylandVulkan(a)
	if len(destroyed) != 0 {
		t.Fatal("destroyed an instance a window still draws with")
	}
	releaseWaylandVulkan(b)
	if len(destroyed) != 1 || destroyed[0] != intel {
		t.Fatalf("with its last window gone, destroyed %+v", destroyed)
	}
	releaseWaylandVulkan(c)
	if len(destroyed) != 1 {
		t.Fatal("destroyed the current instance with no window on it")
	}
	if d := acquire(); d != c {
		t.Fatal("the current instance was not taken again")
	} else {
		releaseWaylandVulkan(d)
	}
	SetGraphics(Graphics{Vulkan: intel})
	e := acquire()
	if len(destroyed) != 2 || destroyed[1] != nvidia || e.want.device != intel {
		t.Fatalf("the old instance with no window was not destroyed at once: %+v", destroyed)
	}
	releaseWaylandVulkan(e)
}

// A device a window could not draw with gives way to every driver, until
// another device is chosen.
func TestWaylandVulkanFailedDevice(t *testing.T) {
	defer SetGraphics(Graphics{})
	defer func(m func(vulkanWant) (vk.Instance, string, error), d func(vk.Instance)) {
		makeWaylandVulkan, destroyWaylandVulkan = m, d
		waylandVulkan.current, waylandVulkan.failed = nil, nil
	}(makeWaylandVulkan, destroyWaylandVulkan)
	waylandVulkan.current, waylandVulkan.failed = nil, nil
	var fakes []*byte
	var made []VulkanDevice
	makeWaylandVulkan = func(w vulkanWant) (vk.Instance, string, error) {
		fakes = append(fakes, new(byte))
		made = append(made, w.device)
		return vk.Instance(unsafe.Pointer(fakes[len(fakes)-1])), w.device.Driver, nil
	}
	destroyWaylandVulkan = func(vk.Instance) {}
	intel := VulkanDevice{Driver: "intel_icd.x86_64.json", VendorID: 0x8086, DeviceID: 0x4626}
	nvidia := VulkanDevice{Driver: "nvidia_icd.json", VendorID: 0x10de, DeviceID: 0x25a2}

	SetGraphics(Graphics{Vulkan: intel})
	s, _, _ := acquireWaylandVulkan()
	releaseWaylandVulkan(s)
	failWaylandVulkan(vulkanWant{device: intel, alone: true})
	s, _, _ = acquireWaylandVulkan()
	if s.want.device != (VulkanDevice{}) {
		t.Fatalf("after %+v failed, drew with %+v", intel, s.want.device)
	}
	releaseWaylandVulkan(s)
	SetGraphics(Graphics{Vulkan: nvidia})
	s, _, _ = acquireWaylandVulkan()
	if s.want.device != nvidia {
		t.Fatalf("another device chosen, drew with %+v", s.want.device)
	}
	releaseWaylandVulkan(s)
	if len(made) != 3 || made[1] != (VulkanDevice{}) {
		t.Fatalf("instances made with %+v", made)
	}
}

// A Vulkan that cannot draw with any driver is not tried again by the
// windows after, which go to EGL at once, until SetGraphics tries it again.
func TestWaylandVulkanFailed(t *testing.T) {
	defer SetGraphics(Graphics{})
	defer func(m func(vulkanWant) (vk.Instance, string, error), egl func(*window) (context, error)) {
		makeWaylandVulkan, newWaylandEGLContext = m, egl
		waylandVulkan.current, waylandVulkan.failed = nil, nil
	}(makeWaylandVulkan, newWaylandEGLContext)
	waylandVulkan.current, waylandVulkan.failed = nil, nil
	made := 0
	makeWaylandVulkan = func(vulkanWant) (vk.Instance, string, error) {
		made++
		return nil, "", errors.New("vulkan: no driver")
	}
	type eglContext struct{ context }
	newWaylandEGLContext = func(*window) (context, error) { return eglContext{}, nil }

	SetGraphics(Graphics{API: GraphicsVulkan})
	for i := range 3 {
		c, err := (*window)(nil).NewContext()
		if _, ok := c.(eglContext); !ok || err != nil {
			t.Fatalf("window %d: %v, %v", i, c, err)
		}
	}
	if made != 1 {
		t.Fatalf("three windows tried Vulkan %d times", made)
	}
	if !CurrentGraphics().VulkanFailed {
		t.Fatal("the failure is not told")
	}
	SetGraphics(Graphics{API: GraphicsVulkan})
	(*window)(nil).NewContext()
	if made != 2 {
		t.Fatal("Vulkan chosen again was not tried")
	}
}

// The kernel's driver of the compositor's device names the Vulkan drivers
// that may draw with it, those of the architecture first.
func TestCompositorVulkanDrivers(t *testing.T) {
	drivers := []vk.Driver{
		{Manifest: "intel_hasvk_icd.x86_64.json"},
		{Manifest: "intel_icd.i686.json"},
		{Manifest: "intel_icd.x86_64.json"},
		{Manifest: "lvp_icd.x86_64.json"},
		{Manifest: "nouveau_icd.x86_64.json"},
		{Manifest: "nvidia_icd.json"},
		{Manifest: "radeon_icd.x86_64.json"},
	}
	names := func(ds []vk.Driver) string {
		var n []string
		for _, d := range ds {
			n = append(n, d.Manifest)
		}
		return strings.Join(n, " ")
	}
	for _, c := range []struct{ kernel, goarch, want string }{
		{"i915", "amd64", "intel_icd.x86_64.json intel_hasvk_icd.x86_64.json intel_icd.i686.json"},
		{"xe", "386", "intel_icd.i686.json intel_icd.x86_64.json"},
		{"amdgpu", "amd64", "radeon_icd.x86_64.json"},
		{"nvidia", "amd64", "nvidia_icd.json"},
		{"nouveau", "amd64", "nouveau_icd.x86_64.json"},
		{"vmwgfx", "amd64", ""},
		{"", "amd64", ""},
	} {
		if got := names(compositorVulkanDrivers(c.kernel, drivers, c.goarch)); got != c.want {
			t.Errorf("%s on %s: %q, want %q", c.kernel, c.goarch, got, c.want)
		}
	}
}

// With no device chosen, the instance is made with the compositor's driver
// alone, which wakes no other GPU; where it cannot draw a window, with
// every driver.
func TestWaylandVulkanCompositorDriver(t *testing.T) {
	defer SetGraphics(Graphics{})
	defer func(m func(vulkanWant) (vk.Instance, string, error), d func(vk.Instance)) {
		makeWaylandVulkan, destroyWaylandVulkan = m, d
		waylandVulkan.current, waylandVulkan.failed, waylandVulkan.compositorFailed = nil, nil, false
	}(makeWaylandVulkan, destroyWaylandVulkan)
	waylandVulkan.current, waylandVulkan.failed, waylandVulkan.compositorFailed = nil, nil, false
	var fakes []*byte
	var made []vulkanWant
	makeWaylandVulkan = func(w vulkanWant) (vk.Instance, string, error) {
		fakes = append(fakes, new(byte))
		made = append(made, w)
		driver := ""
		if w.alone {
			driver = "intel_icd.x86_64.json"
		}
		return vk.Instance(unsafe.Pointer(fakes[len(fakes)-1])), driver, nil
	}
	destroyWaylandVulkan = func(vk.Instance) {}

	SetGraphics(Graphics{})
	s, _, _ := acquireWaylandVulkan()
	if !s.want.alone || s.driver != "intel_icd.x86_64.json" {
		t.Fatalf("with no device chosen, made %+v with %q", s.want, s.driver)
	}
	if d := VulkanDriver(); d != "intel_icd.x86_64.json" {
		t.Fatalf("VulkanDriver is %q", d)
	}
	// The device it drew with, remembered with its driver, takes it.
	SetGraphics(Graphics{Vulkan: VulkanDevice{Driver: "intel_icd.x86_64.json", VendorID: 0x8086, DeviceID: 0x4626}})
	if r, want, _ := acquireWaylandVulkan(); r != s || want.device.DeviceID != 0x4626 {
		t.Fatalf("the device remembered made another instance, %+v", want)
	} else {
		releaseWaylandVulkan(r)
	}
	SetGraphics(Graphics{})
	releaseWaylandVulkan(s)
	failWaylandVulkan(s.want)
	s, _, _ = acquireWaylandVulkan()
	if s.want.alone || s.driver != "" || VulkanDriver() != "" {
		t.Fatalf("after the compositor's driver failed, made %+v with %q", s.want, s.driver)
	}
	releaseWaylandVulkan(s)
	if len(made) != 2 {
		t.Fatalf("instances made: %+v", made)
	}
}
