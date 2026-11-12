// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd) && !nowayland && !novulkan

package app

import (
	"errors"
	"strings"
	"testing"

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
