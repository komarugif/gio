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
	defer func(m func(VulkanDevice) (vk.Instance, error), d func(vk.Instance)) {
		makeWaylandVulkan, destroyWaylandVulkan = m, d
		waylandVulkan.current = nil
	}(makeWaylandVulkan, destroyWaylandVulkan)
	waylandVulkan.current = nil
	made := map[vk.Instance]VulkanDevice{}
	var destroyed []VulkanDevice
	// The garbage collector does not follow a vk.Instance, a pointer to
	// an incomplete C type: the fakes' memory is held here.
	var fakes []*byte
	makeWaylandVulkan = func(d VulkanDevice) (vk.Instance, error) {
		fakes = append(fakes, new(byte))
		inst := vk.Instance(unsafe.Pointer(fakes[len(fakes)-1]))
		made[inst] = d
		return inst, nil
	}
	destroyWaylandVulkan = func(inst vk.Instance) { destroyed = append(destroyed, made[inst]) }
	intel := VulkanDevice{Driver: "intel_icd.x86_64.json", VendorID: 0x8086, DeviceID: 0x4626}
	nvidia := VulkanDevice{Driver: "nvidia_icd.json", VendorID: 0x10de, DeviceID: 0x25a2}
	acquire := func() *sharedVulkan {
		t.Helper()
		s, err := acquireWaylandVulkan()
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
	if c == a || c.device != nvidia {
		t.Fatalf("another device kept the instance of %+v", c.device)
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
	if len(destroyed) != 2 || destroyed[1] != nvidia || e.device != intel {
		t.Fatalf("the old instance with no window was not destroyed at once: %+v", destroyed)
	}
	releaseWaylandVulkan(e)
}

// A device a window could not draw with gives way to every driver, until
// another device is chosen.
func TestWaylandVulkanFailedDevice(t *testing.T) {
	defer SetGraphics(Graphics{})
	defer func(m func(VulkanDevice) (vk.Instance, error), d func(vk.Instance)) {
		makeWaylandVulkan, destroyWaylandVulkan = m, d
		waylandVulkan.current, waylandVulkan.failed = nil, nil
	}(makeWaylandVulkan, destroyWaylandVulkan)
	waylandVulkan.current, waylandVulkan.failed = nil, nil
	var fakes []*byte
	var made []VulkanDevice
	makeWaylandVulkan = func(d VulkanDevice) (vk.Instance, error) {
		fakes = append(fakes, new(byte))
		made = append(made, d)
		return vk.Instance(unsafe.Pointer(fakes[len(fakes)-1])), nil
	}
	destroyWaylandVulkan = func(vk.Instance) {}
	intel := VulkanDevice{Driver: "intel_icd.x86_64.json", VendorID: 0x8086, DeviceID: 0x4626}
	nvidia := VulkanDevice{Driver: "nvidia_icd.json", VendorID: 0x10de, DeviceID: 0x25a2}

	SetGraphics(Graphics{Vulkan: intel})
	s, _ := acquireWaylandVulkan()
	releaseWaylandVulkan(s)
	failWaylandVulkan(intel)
	s, _ = acquireWaylandVulkan()
	if s.device != (VulkanDevice{}) {
		t.Fatalf("after %+v failed, drew with %+v", intel, s.device)
	}
	releaseWaylandVulkan(s)
	SetGraphics(Graphics{Vulkan: nvidia})
	s, _ = acquireWaylandVulkan()
	if s.device != nvidia {
		t.Fatalf("another device chosen, drew with %+v", s.device)
	}
	releaseWaylandVulkan(s)
	if len(made) != 3 || made[1] != (VulkanDevice{}) {
		t.Fatalf("instances made with %+v", made)
	}
}
