// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux || freebsd

package vk

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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
// them loaded together, those that fail to list theirs left out of both.
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
			// A driver of hardware that is not there, which the loader
			// leaves out of an instance of every driver too.
			t.Logf("%s: %v", d.Manifest, err)
			continue
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

// otherDriverLibrary is a library of the system that a manifest of another
// driver names in TestCreateInstanceForLoadsNoOtherDriver: the loader loads
// it, and gives it up, when it loads that driver.
const otherDriverLibrary = "/usr/lib/libbz2.so.1"

// A driver loaded alone loads no other driver, not even to ask the loader
// what it has: a discrete GPU's driver wakes the GPU up when loaded. The
// test runs itself with another driver's manifest and LD_DEBUG telling the
// libraries loaded; with every driver, the other's library is loaded.
func TestCreateInstanceForLoadsNoOtherDriver(t *testing.T) {
	switch os.Getenv("GIO_VK_TEST_CHILD") {
	case "all":
		if inst, err := CreateInstance(); err == nil {
			DestroyInstance(inst)
		}
		return
	case "alone":
		for _, d := range Drivers() {
			if d.Library == otherDriverLibrary {
				continue
			}
			if _, err := driverEntry(d.Library); err != nil {
				continue
			}
			inst, err := CreateInstanceFor(d)
			if err != nil {
				t.Fatal(err)
			}
			DestroyInstance(inst)
			return
		}
		t.Skip("no driver loads")
	}
	if _, err := os.Stat(otherDriverLibrary); err != nil {
		t.Skip(err)
	}
	if err := vkInit(); err != nil {
		t.Skip(err)
	}
	data := t.TempDir()
	dir := filepath.Join(data, "vulkan", "icd.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"file_format_version": "1.0.0", "ICD": {"library_path": "` + otherDriverLibrary + `", "api_version": "1.4.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "zz_other_icd.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	loads := func(mode string) bool {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCreateInstanceForLoadsNoOtherDriver$", "-test.v")
		cmd.Env = append(os.Environ(), "GIO_VK_TEST_CHILD="+mode, "XDG_DATA_HOME="+data, "LD_DEBUG=files")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", mode, err, out)
		}
		if strings.Contains(string(out), "SKIP") {
			t.Skipf("%s: skipped", mode)
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.Contains(line, "file="+otherDriverLibrary) && strings.Contains(line, "dynamically loaded") {
				return true
			}
		}
		return false
	}
	if !loads("all") {
		t.Skip("the loader does not load the other driver with every driver: nothing to tell apart")
	}
	if loads("alone") {
		t.Errorf("a driver loaded alone loaded %s too", otherDriverLibrary)
	}
}
