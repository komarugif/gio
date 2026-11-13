// SPDX-License-Identifier: Unlicense OR MIT

//go:build (linux || freebsd) && !novulkan

package headless

import (
	"unsafe"

	"gioui.org/gpu"
	"gioui.org/internal/gpuchoice"
	"gioui.org/internal/vk"
)

type vkContext struct {
	physDev  vk.PhysicalDevice
	inst     vk.Instance
	dev      vk.Device
	queueFam int
	// release lets the instance of the program's windows go; the
	// context's own instance is destroyed where it is nil.
	release func()
}

func init() {
	newContextFallback = newVulkanContext
	newContextVulkan = newVulkanContext
}

// newVulkanContext makes a context of the instance and the device the
// program's windows are drawn with, where they tell them
// (gpuchoice.VulkanInstance); of an instance of its own, with every
// driver, otherwise.
func newVulkanContext() (context, error) {
	var (
		inst               vk.Instance
		vendorID, deviceID uint32
		release            func()
	)
	if gpuchoice.VulkanInstance != nil {
		if p, v, d, r, err := gpuchoice.VulkanInstance(); err == nil {
			inst, vendorID, deviceID, release = vk.Instance(p), v, d, r
		}
	}
	if release == nil {
		var err error
		inst, err = vk.CreateInstance()
		if err != nil {
			return nil, err
		}
		release = func() { vk.DestroyInstance(inst) }
	}
	physDev, qFam, err := vk.ChoosePreferredPhysicalDevice(inst, 0, vendorID, deviceID)
	if err != nil {
		release()
		return nil, err
	}
	dev, err := vk.CreateDeviceAndQueue(physDev, qFam)
	if err != nil {
		release()
		return nil, err
	}
	ctx := &vkContext{
		physDev:  physDev,
		inst:     inst,
		dev:      dev,
		queueFam: qFam,
		release:  release,
	}
	return ctx, nil
}

func (c *vkContext) API() gpu.API {
	return gpu.Vulkan{
		PhysDevice:  unsafe.Pointer(c.physDev),
		Device:      unsafe.Pointer(c.dev),
		Format:      int(vk.FORMAT_R8G8B8A8_SRGB),
		QueueFamily: c.queueFam,
		QueueIndex:  0,
	}
}

func (c *vkContext) MakeCurrent() error {
	return nil
}

func (c *vkContext) ReleaseCurrent() {
}

func (c *vkContext) Release() {
	vk.DeviceWaitIdle(c.dev)

	vk.DestroyDevice(c.dev)
	c.release()
	*c = vkContext{}
}
