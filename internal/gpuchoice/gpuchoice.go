// SPDX-License-Identifier: Unlicense OR MIT

// Package gpuchoice tells headless windows (gpu/headless) what the
// program's windows are drawn with, for them to be drawn so too: package
// app sets it where there is a choice, on Wayland. A process that draws
// its windows with Vulkan would otherwise load OpenGL's drivers as well
// for a headless window, and a headless window drawn with Vulkan would
// load every Vulkan driver, waking every GPU.
package gpuchoice

import "unsafe"

// API is a graphics API.
type API uint8

const (
	// Unknown leaves the choice to the headless window.
	Unknown API = iota
	OpenGL
	Vulkan
)

var (
	// Prefer, where set, is the API the windows are drawn with.
	Prefer func() API
	// VulkanInstance, where set, is the VkInstance the windows are drawn
	// with, held for the caller until it calls release, and the ids of
	// the device the windows are drawn with, zero where any. It fails
	// where the windows have no instance to share.
	VulkanInstance func() (inst unsafe.Pointer, vendorID, deviceID uint32, release func(), err error)
)
