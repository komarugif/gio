// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd) && !nowayland && !novulkan

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"

	"gioui.org/gpu"
	"gioui.org/internal/gpuchoice"
	"gioui.org/internal/vk"
)

type wlVkContext struct {
	win    *window
	shared *sharedVulkan
	inst   vk.Instance
	surf   vk.Surface
	ctx    *vkContext
}

// sharedVulkan is an instance of Vulkan that windows on Wayland share,
// made as they want it: making an instance loads drivers, which wakes their
// GPUs up.
type sharedVulkan struct {
	inst vk.Instance
	want vulkanWant
	// driver is the manifest of the driver the instance was made with
	// alone; empty where it was made with every driver.
	driver string
	// windows is how many contexts hold the instance.
	windows int
}

// vulkanWant is the instance windows want: with the driver of device
// alone where alone is set, of the compositor's device where no device is
// chosen; with every driver otherwise. Windows draw with device, the
// first that can draw where it is zero.
type vulkanWant struct {
	device VulkanDevice
	alone  bool
}

func (w vulkanWant) same(o vulkanWant) bool {
	return w.alone == o.alone && sameVulkanDevice(w.device, o.device)
}

// waylandVulkan is the instance that windows made now take: made by the
// first window drawn with Vulkan, kept while the device chosen stays, even
// with no window on it. Another device chosen makes another, and the old
// one goes with its last window.
var waylandVulkan struct {
	sync.Mutex
	current *sharedVulkan
	// failed is a device a window could not draw with: windows draw with
	// any device instead, of every driver, until another is chosen.
	failed *VulkanDevice
	// compositorFailed is that the driver of the compositor's device
	// alone could not draw a window: with no device chosen, windows draw
	// with every driver.
	compositorFailed bool
}

// sameVulkanDevice reports whether a and b are the same device of the same
// driver.
func sameVulkanDevice(a, b VulkanDevice) bool {
	return a.Driver == b.Driver && a.VendorID == b.VendorID && a.DeviceID == b.DeviceID
}

var waylandVulkanExtensions = []string{"VK_KHR_surface", "VK_KHR_wayland_surface"}

func init() {
	newWaylandVulkanContext = func(w *window) (context, error) {
		shared, want, err := acquireWaylandVulkan()
		if err != nil {
			// No driver loads, or none has a device.
			failVulkan()
			return nil, err
		}
		inst := shared.inst
		disp := w.display()
		wlSurf, _, _ := w.surface()
		surf, err := vk.CreateWaylandSurface(inst, unsafe.Pointer(disp), unsafe.Pointer(wlSurf))
		if err != nil {
			releaseWaylandVulkan(shared)
			return nil, err
		}
		ctx, err := newPreferredVulkanContext(inst, surf, want.device.VendorID, want.device.DeviceID)
		if err != nil {
			vk.DestroySurface(inst, surf)
			releaseWaylandVulkan(shared)
			if shared.driver != "" {
				// The driver loaded alone cannot draw the window: any
				// device of every driver may.
				failWaylandVulkan(want)
				return newWaylandVulkanContext(w)
			}
			failVulkan()
			return nil, err
		}
		c := &wlVkContext{
			win:    w,
			shared: shared,
			inst:   inst,
			surf:   surf,
			ctx:    ctx,
		}
		return c, nil
	}
	listVulkanDevices = waylandVulkanDevices
	gpuchoice.Prefer = waylandHeadlessAPI
	gpuchoice.VulkanInstance = waylandHeadlessVulkan
	currentVulkanDriver = func() string {
		waylandVulkan.Lock()
		defer waylandVulkan.Unlock()
		if cur := waylandVulkan.current; cur != nil {
			return cur.driver
		}
		return ""
	}
}

// waylandHeadlessAPI tells headless windows the API windows on Wayland
// are drawn with: that of the GPU made last for a window, or the one
// windows try first before any is made.
func waylandHeadlessAPI() gpuchoice.API {
	if !waylandWindows.Load() {
		return gpuchoice.Unknown
	}
	if api := GPUInfo().API; api != "" {
		if strings.HasPrefix(api, "Vulkan") {
			return gpuchoice.Vulkan
		}
		return gpuchoice.OpenGL
	}
	if waylandVulkanFirst(CurrentGraphics()) {
		return gpuchoice.Vulkan
	}
	return gpuchoice.OpenGL
}

// waylandHeadlessVulkan gives a headless window the instance windows on
// Wayland share (acquireWaylandVulkan), made with a driver alone where it
// can be, and the device they are drawn with.
func waylandHeadlessVulkan() (unsafe.Pointer, uint32, uint32, func(), error) {
	if !waylandWindows.Load() {
		return nil, 0, 0, nil, errors.New("app: no windows on Wayland")
	}
	shared, want, err := acquireWaylandVulkan()
	if err != nil {
		return nil, 0, 0, nil, err
	}
	vendorID, deviceID := want.device.VendorID, want.device.DeviceID
	if info := GPUInfo(); strings.HasPrefix(info.API, "Vulkan") {
		vendorID, deviceID = info.VendorID, info.DeviceID
	}
	return unsafe.Pointer(shared.inst), vendorID, deviceID, func() { releaseWaylandVulkan(shared) }, nil
}

// acquireWaylandVulkan returns the instance for a window to draw with,
// held for it until releaseWaylandVulkan, and what the window wants of it:
// the current one if it was made as windows want it now, or with the
// driver alone of the device they want, else a new one. A new one is made
// with a driver alone, the chosen device's or the compositor's, if it
// loads, so that no other GPU wakes up; with every driver otherwise.
func acquireWaylandVulkan() (*sharedVulkan, vulkanWant, error) {
	waylandVulkan.Lock()
	defer waylandVulkan.Unlock()
	want := wantedWaylandVulkan()
	if cur := waylandVulkan.current; cur != nil && cur.serves(want) {
		cur.windows++
		return cur, want, nil
	}
	inst, driver, err := makeWaylandVulkan(want)
	if err != nil {
		return nil, want, err
	}
	if old := waylandVulkan.current; old != nil && old.windows == 0 {
		destroyWaylandVulkan(old.inst)
	}
	waylandVulkan.current = &sharedVulkan{inst: inst, want: want, driver: driver, windows: 1}
	return waylandVulkan.current, want, nil
}

// serves reports whether windows that want want can draw with s: it was
// made so, or with the driver of the device wanted alone. The second is
// the compositor's driver, which a program remembers for the device it
// drew with: the windows that have the instance keep it.
func (s *sharedVulkan) serves(want vulkanWant) bool {
	return s.want.same(want) || want.alone && s.driver != "" && s.driver == want.device.Driver
}

// wantedWaylandVulkan is the device chosen with its driver alone; the
// compositor's driver alone where none is chosen; every driver where a
// window could not draw so. The caller holds waylandVulkan's lock.
func wantedWaylandVulkan() vulkanWant {
	device := CurrentGraphics().Vulkan
	if f := waylandVulkan.failed; f != nil && sameVulkanDevice(*f, device) {
		return vulkanWant{}
	}
	if sameVulkanDevice(device, VulkanDevice{}) {
		return vulkanWant{alone: !waylandVulkan.compositorFailed}
	}
	return vulkanWant{device: device, alone: true}
}

// failWaylandVulkan tells that a window could not draw with the driver
// loaded alone as want wanted.
func failWaylandVulkan(want vulkanWant) {
	waylandVulkan.Lock()
	defer waylandVulkan.Unlock()
	if sameVulkanDevice(want.device, VulkanDevice{}) {
		waylandVulkan.compositorFailed = true
		return
	}
	waylandVulkan.failed = &want.device
}

// releaseWaylandVulkan lets a window's hold of s go, destroying it with
// its last window if it is not the current one.
func releaseWaylandVulkan(s *sharedVulkan) {
	waylandVulkan.Lock()
	defer waylandVulkan.Unlock()
	s.windows--
	if s.windows == 0 && s != waylandVulkan.current {
		destroyWaylandVulkan(s.inst)
	}
}

// makeWaylandVulkan and destroyWaylandVulkan make and destroy the shared
// instances; tests replace them.
var (
	makeWaylandVulkan    = newWaylandVulkanInstance
	destroyWaylandVulkan = vk.DestroyInstance
)

// newWaylandVulkanInstance makes an instance as want wants it, and tells
// the manifest of the driver it was made with alone. Of the compositor's
// drivers, the first that loads and has a device that does not draw on
// the CPU is taken.
func newWaylandVulkanInstance(want vulkanWant) (vk.Instance, string, error) {
	if want.alone {
		var drivers []vk.Driver
		compositor := sameVulkanDevice(want.device, VulkanDevice{})
		if compositor {
			drivers = compositorVulkanDrivers(drmKernelDriver(waylandMainDevice.Load()), vk.Drivers(), runtime.GOARCH)
		} else if i := slices.IndexFunc(vk.Drivers(), func(d vk.Driver) bool { return d.Manifest == want.device.Driver }); i >= 0 {
			drivers = []vk.Driver{vk.Drivers()[i]}
		}
		for _, d := range drivers {
			inst, err := vk.CreateInstanceFor(d, waylandVulkanExtensions...)
			if err != nil {
				continue
			}
			if !compositor || hasGPU(inst) {
				return inst, d.Manifest, nil
			}
			vk.DestroyInstance(inst)
		}
	}
	inst, err := vk.CreateInstance(waylandVulkanExtensions...)
	return inst, "", err
}

// hasGPU reports whether inst has a device that does not draw on the CPU.
func hasGPU(inst vk.Instance) bool {
	infos, err := vk.PhysicalDevices(inst)
	return err == nil && slices.ContainsFunc(infos, func(i vk.PhysicalDeviceInfo) bool { return i.Type != vk.PhysicalDeviceCPU })
}

// drmKernelDriver is the name of the kernel's driver of the DRM device
// dev, a dev_t, from sysfs: i915, amdgpu, nvidia. Empty where it is not
// known.
func drmKernelDriver(dev uint64) string {
	if dev == 0 {
		return ""
	}
	link, err := os.Readlink(fmt.Sprintf("/sys/dev/char/%d:%d/device/driver", unix.Major(dev), unix.Minor(dev)))
	if err != nil {
		return ""
	}
	return filepath.Base(link)
}

// vulkanManifests are the names Mesa's and the vendors' Vulkan drivers
// give their manifests, before the architecture, by the kernel's driver of
// the devices they draw with.
var vulkanManifests = map[string][]string{
	"i915":       {"intel_icd", "intel_hasvk_icd"},
	"xe":         {"intel_icd"},
	"amdgpu":     {"radeon_icd", "amd_icd64", "amd_icd"},
	"radeon":     {"radeon_icd"},
	"nouveau":    {"nouveau_icd"},
	"nvidia":     {"nvidia_icd"},
	"virtio_gpu": {"virtio_icd"},
	"msm":        {"freedreno_icd"},
	"panfrost":   {"panfrost_icd"},
	"panthor":    {"panfrost_icd"},
	"v3d":        {"broadcom_icd"},
	"asahi":      {"asahi_icd"},
}

// vulkanArch is how manifests name the architecture of their library.
var vulkanArch = map[string]string{
	"amd64":   "x86_64",
	"386":     "i686",
	"arm64":   "aarch64",
	"arm":     "armv7",
	"riscv64": "riscv64",
	"ppc64le": "powerpc64le",
	"loong64": "loongarch64",
}

// compositorVulkanDrivers lists the Vulkan drivers that may draw with the
// device whose kernel driver is kernel, in the order to try them: those of
// goarch first, as manifests of the other architectures fail to load.
func compositorVulkanDrivers(kernel string, drivers []vk.Driver, goarch string) []vk.Driver {
	var found []vk.Driver
	for _, name := range vulkanManifests[kernel] {
		for _, d := range drivers {
			if d.Manifest == name+".json" || strings.HasPrefix(d.Manifest, name+".") {
				found = append(found, d)
			}
		}
	}
	if arch := vulkanArch[goarch]; arch != "" {
		slices.SortStableFunc(found, func(a, b vk.Driver) int {
			return boolInt(!strings.Contains(a.Manifest, arch)) - boolInt(!strings.Contains(b.Manifest, arch))
		})
	}
	return found
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// waylandVulkanDevices lists the devices of each driver, with the driver
// loaded alone; of every driver together, with no driver named, where the
// loader cannot load one alone.
func waylandVulkanDevices() ([]VulkanDevice, error) {
	return collectVulkanDevices(vk.Drivers(), func(d vk.Driver) ([]vk.PhysicalDeviceInfo, bool, error) {
		inst, err := vk.CreateInstanceFor(d)
		if err != nil {
			// A manifest of another architecture, or a loader that cannot
			// load a driver alone.
			return nil, false, nil
		}
		defer vk.DestroyInstance(inst)
		infos, err := vk.PhysicalDevices(inst)
		return infos, true, err
	}, func() ([]vk.PhysicalDeviceInfo, error) {
		inst, err := vk.CreateInstance()
		if err != nil {
			return nil, err
		}
		defer vk.DestroyInstance(inst)
		return vk.PhysicalDevices(inst)
	})
}

// collectVulkanDevices lists the devices of drivers, those of each from
// alone, which tells whether the driver loaded alone; those of every driver
// from together where none did. A driver that loads and fails to list its
// devices is left out, as the loader leaves it out of an instance of every
// driver: drivers of hardware that is not there fail so, as
// VK_ERROR_INITIALIZATION_FAILED. Its error is told only where no driver
// had a device. Devices that draw on the CPU are left out.
func collectVulkanDevices(drivers []vk.Driver, alone func(vk.Driver) ([]vk.PhysicalDeviceInfo, bool, error), together func() ([]vk.PhysicalDeviceInfo, error)) ([]VulkanDevice, error) {
	var devices []VulkanDevice
	add := func(driver string, infos []vk.PhysicalDeviceInfo) {
		for _, i := range infos {
			if i.Type == vk.PhysicalDeviceCPU {
				continue
			}
			devices = append(devices, VulkanDevice{
				Driver:   driver,
				VendorID: i.VendorID,
				DeviceID: i.DeviceID,
				Name:     i.Name,
				Kind:     VulkanDeviceKind(i.Type),
				API:      i.API(),
				Version:  i.Driver(),
			})
		}
	}
	loaded := false
	var firstErr error
	for _, d := range drivers {
		infos, ok, err := alone(d)
		if !ok {
			continue
		}
		loaded = true
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", d.Manifest, err)
			}
			continue
		}
		add(d.Manifest, infos)
	}
	if !loaded {
		infos, err := together()
		if err != nil {
			return nil, err
		}
		add("", infos)
	}
	if len(devices) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return devices, nil
}

func (c *wlVkContext) RenderTarget() (gpu.RenderTarget, error) {
	return c.ctx.RenderTarget()
}

func (c *wlVkContext) API() gpu.API {
	return c.ctx.api()
}

// Release lets the shared instance go, destroyed with its last window if
// another device has been chosen since.
func (c *wlVkContext) Release() {
	c.ctx.release()
	vk.DestroySurface(c.inst, c.surf)
	releaseWaylandVulkan(c.shared)
	*c = wlVkContext{}
}

func (c *wlVkContext) Present() error {
	return c.ctx.present()
}

func (c *wlVkContext) Lock() error {
	return nil
}

func (c *wlVkContext) Unlock() {}

func (c *wlVkContext) Refresh() error {
	_, w, h := c.win.surface()
	return c.ctx.refresh(c.surf, w, h)
}
