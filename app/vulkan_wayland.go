// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd) && !nowayland && !novulkan

package app

import (
	"fmt"
	"sync"
	"unsafe"

	"gioui.org/gpu"
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
// made with a device: making an instance loads drivers, which wakes their
// GPUs up.
type sharedVulkan struct {
	inst   vk.Instance
	device VulkanDevice
	// windows is how many contexts hold the instance.
	windows int
}

// waylandVulkan is the instance that windows made now take: made by the
// first window drawn with Vulkan, kept while the device chosen stays, even
// with no window on it. Another device chosen makes another, and the old
// one goes with its last window.
var waylandVulkan struct {
	sync.Mutex
	current *sharedVulkan
}

// sameVulkanDevice reports whether a and b are the same device of the same
// driver.
func sameVulkanDevice(a, b VulkanDevice) bool {
	return a.Driver == b.Driver && a.VendorID == b.VendorID && a.DeviceID == b.DeviceID
}

var waylandVulkanExtensions = []string{"VK_KHR_surface", "VK_KHR_wayland_surface"}

func init() {
	newWaylandVulkanContext = func(w *window) (context, error) {
		shared, err := acquireWaylandVulkan()
		if err != nil {
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
		ctx, err := newPreferredVulkanContext(inst, surf, shared.device.VendorID, shared.device.DeviceID)
		if err != nil {
			vk.DestroySurface(inst, surf)
			releaseWaylandVulkan(shared)
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
}

// acquireWaylandVulkan returns the instance for a window to draw with,
// held for it until releaseWaylandVulkan: the current one if it was made
// with the device chosen, else a new one. A new one is made with the
// chosen device's driver alone, if there is one and it loads, so that no
// other GPU wakes up; with every driver otherwise.
func acquireWaylandVulkan() (*sharedVulkan, error) {
	waylandVulkan.Lock()
	defer waylandVulkan.Unlock()
	device := CurrentGraphics().Vulkan
	if cur := waylandVulkan.current; cur != nil && sameVulkanDevice(cur.device, device) {
		cur.windows++
		return cur, nil
	}
	inst, err := makeWaylandVulkan(device)
	if err != nil {
		return nil, err
	}
	if old := waylandVulkan.current; old != nil && old.windows == 0 {
		destroyWaylandVulkan(old.inst)
	}
	waylandVulkan.current = &sharedVulkan{inst: inst, device: device, windows: 1}
	return waylandVulkan.current, nil
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

func newWaylandVulkanInstance(device VulkanDevice) (vk.Instance, error) {
	if device.Driver != "" {
		for _, d := range vk.Drivers() {
			if d.Manifest != device.Driver {
				continue
			}
			if inst, err := vk.CreateInstanceFor(d, waylandVulkanExtensions...); err == nil {
				return inst, nil
			}
			break
		}
	}
	return vk.CreateInstance(waylandVulkanExtensions...)
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
