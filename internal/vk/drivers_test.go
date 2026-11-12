// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux || freebsd

package vk

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// A manifest found first hides those of its name after it, and a library
// path with a slash is relative to the manifest's directory.
func TestDriversIn(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	write := func(dir, name, lib string) {
		t.Helper()
		data := `{"file_format_version": "1.0.0", "ICD": {"library_path": "` + lib + `", "api_version": "1.4.0"}}`
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(first, "intel_icd.x86_64.json", "/usr/lib/libvulkan_intel.so")
	write(second, "intel_icd.x86_64.json", "/elsewhere/libvulkan_intel.so")
	write(second, "nvidia_icd.json", "libGLX_nvidia.so.0")
	write(second, "local.json", "./lib/libvulkan_local.so")
	if err := os.WriteFile(filepath.Join(second, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := driversIn([]string{first, filepath.Join(first, "missing"), second})
	want := []Driver{
		{Manifest: "intel_icd.x86_64.json", Library: "/usr/lib/libvulkan_intel.so"},
		{Manifest: "local.json", Library: filepath.Join(second, "lib", "libvulkan_local.so")},
		{Manifest: "nvidia_icd.json", Library: "libGLX_nvidia.so.0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("driversIn = %+v, want %+v", got, want)
	}
}

// The devices of the drivers, each loaded alone, are the devices of all of
// them loaded together.
func TestCreateInstanceFor(t *testing.T) {
	all, err := CreateInstance()
	if err != nil {
		t.Skip(err)
	}
	together, err := PhysicalDevices(all)
	DestroyInstance(all)
	if err != nil {
		t.Fatal(err)
	}
	if !instanceExtension(directDriverLoading) {
		t.Skipf("the loader lacks %s", directDriverLoading)
	}
	var alone []PhysicalDeviceInfo
	for _, d := range Drivers() {
		if _, err := driverEntry(d.Library); err != nil {
			// A manifest of another architecture, such as i686's.
			t.Logf("%s: %v", d.Manifest, err)
			continue
		}
		inst, err := CreateInstanceFor(d)
		if err != nil {
			t.Fatalf("%s: %v", d.Manifest, err)
		}
		devs, err := PhysicalDevices(inst)
		DestroyInstance(inst)
		if err != nil {
			t.Fatalf("%s: %v", d.Manifest, err)
		}
		for _, dev := range devs {
			t.Logf("%s: %s, %s, driver %s", d.Manifest, dev.Device(), dev.API(), dev.Driver())
		}
		alone = append(alone, devs...)
	}
	key := func(devs []PhysicalDeviceInfo) []PhysicalDeviceInfo {
		sort.Slice(devs, func(i, j int) bool { return devs[i].Device() < devs[j].Device() })
		return devs
	}
	if got, want := key(alone), key(together); !reflect.DeepEqual(got, want) {
		t.Errorf("drivers alone have %+v, together %+v", got, want)
	}
}

// A driver loaded alone is loaded from its manifest found here, not by the
// loader: with the loader finding none, it still has its devices.
func TestCreateInstanceForWithoutTheLoaders(t *testing.T) {
	if err := vkInit(); err != nil {
		t.Skip(err)
	}
	if !instanceExtension(directDriverLoading) {
		t.Skipf("the loader lacks %s", directDriverLoading)
	}
	var driver *Driver
	for _, d := range Drivers() {
		if _, err := driverEntry(d.Library); err == nil {
			driver = &d
			break
		}
	}
	if driver == nil {
		t.Skip("no driver loads")
	}
	t.Setenv("VK_DRIVER_FILES", filepath.Join(t.TempDir(), "none.json"))
	if inst, err := CreateInstance(); err == nil {
		devs, _ := PhysicalDevices(inst)
		DestroyInstance(inst)
		if len(devs) > 0 {
			t.Fatalf("the loader found %d devices without a manifest", len(devs))
		}
	}
	inst, err := CreateInstanceFor(*driver)
	if err != nil {
		t.Fatal(err)
	}
	defer DestroyInstance(inst)
	if devs, err := PhysicalDevices(inst); err != nil || len(devs) == 0 {
		t.Fatalf("%s alone: %d devices, %v", driver.Manifest, len(devs), err)
	}
}
