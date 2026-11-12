// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd) && !nowayland && !novulkan

package app

import (
	"sync"
	"unsafe"

	"gioui.org/gpu"
	"gioui.org/internal/vk"
)

type wlVkContext struct {
	win  *window
	inst vk.Instance
	surf vk.Surface
	ctx  *vkContext
}

// waylandVulkan is the instance of Vulkan the windows on Wayland share,
// made by the first of them drawn with Vulkan and kept for the process:
// making an instance loads the drivers, which wakes their GPUs up.
var waylandVulkan struct {
	sync.Mutex
	inst vk.Instance
	// device is the device of Graphics.Vulkan when inst was made.
	device VulkanDevice
}

var waylandVulkanExtensions = []string{"VK_KHR_surface", "VK_KHR_wayland_surface"}

func init() {
	newWaylandVulkanContext = func(w *window) (context, error) {
		inst, device, err := waylandVulkanInstance()
		if err != nil {
			return nil, err
		}
		disp := w.display()
		wlSurf, _, _ := w.surface()
		surf, err := vk.CreateWaylandSurface(inst, unsafe.Pointer(disp), unsafe.Pointer(wlSurf))
		if err != nil {
			return nil, err
		}
		ctx, err := newPreferredVulkanContext(inst, surf, device.VendorID, device.DeviceID)
		if err != nil {
			vk.DestroySurface(inst, surf)
			return nil, err
		}
		c := &wlVkContext{
			win:  w,
			inst: inst,
			surf: surf,
			ctx:  ctx,
		}
		return c, nil
	}
	listVulkanDevices = waylandVulkanDevices
}

// waylandVulkanInstance returns the instance the windows share, making it
// if it is not made: with the driver of Graphics.Vulkan alone, if there is
// one and it loads, so that no other GPU wakes up; with every driver
// otherwise.
func waylandVulkanInstance() (vk.Instance, VulkanDevice, error) {
	waylandVulkan.Lock()
	defer waylandVulkan.Unlock()
	if waylandVulkan.inst != nil {
		return waylandVulkan.inst, waylandVulkan.device, nil
	}
	device := CurrentGraphics().Vulkan
	var inst vk.Instance
	if device.Driver != "" {
		for _, d := range vk.Drivers() {
			if d.Manifest != device.Driver {
				continue
			}
			if i, err := vk.CreateInstanceFor(d, waylandVulkanExtensions...); err == nil {
				inst = i
			}
			break
		}
	}
	if inst == nil {
		i, err := vk.CreateInstance(waylandVulkanExtensions...)
		if err != nil {
			return nil, VulkanDevice{}, err
		}
		inst = i
	}
	waylandVulkan.inst, waylandVulkan.device = inst, device
	return inst, device, nil
}

// waylandVulkanDevices lists the devices of each driver, with the driver
// loaded alone; of every driver together, with no driver named, where the
// loader cannot load one alone.
func waylandVulkanDevices() ([]VulkanDevice, error) {
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
	alone := false
	for _, d := range vk.Drivers() {
		inst, err := vk.CreateInstanceFor(d)
		if err != nil {
			// A manifest of another architecture, or a loader that cannot
			// load a driver alone.
			continue
		}
		alone = true
		infos, err := vk.PhysicalDevices(inst)
		vk.DestroyInstance(inst)
		if err != nil {
			return nil, err
		}
		add(d.Manifest, infos)
	}
	if alone {
		return devices, nil
	}
	inst, err := vk.CreateInstance()
	if err != nil {
		return nil, err
	}
	defer vk.DestroyInstance(inst)
	infos, err := vk.PhysicalDevices(inst)
	if err != nil {
		return nil, err
	}
	add("", infos)
	return devices, nil
}

func (c *wlVkContext) RenderTarget() (gpu.RenderTarget, error) {
	return c.ctx.RenderTarget()
}

func (c *wlVkContext) API() gpu.API {
	return c.ctx.api()
}

// Release keeps the instance, which the windows share.
func (c *wlVkContext) Release() {
	c.ctx.release()
	vk.DestroySurface(c.inst, c.surf)
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
