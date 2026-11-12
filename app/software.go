// SPDX-License-Identifier: Unlicense OR MIT

package app

import (
	"sync/atomic"

	"gioui.org/gpu"
)

// softwareRendered is whether the GPU made last for a window draws on the CPU.
var softwareRendered atomic.Bool

// SoftwareRendered reports whether windows are drawn by a software
// rasterizer (llvmpipe and the other software renderers of OpenGL, WARP of
// Direct3D) rather than a GPU, as far as the GPU made last for a window
// tells: false until a window has drawn. A window that finds it changed is
// invalidated, so whatever it decides from it is decided again.
func SoftwareRendered() bool { return softwareRendered.Load() }

func gpuSoftware(g gpu.GPU) bool { return gpu.Software(g) }

func gpuDeviceInfo(g gpu.GPU) gpu.DeviceInfo { return gpu.Device(g) }

// gpuInfo is what the GPU made last for a window tells of itself.
var gpuInfo atomic.Pointer[gpu.DeviceInfo]

// GPUInfo tells what windows are drawn with, as the GPU made last for a
// window tells it through its API: GL_VERSION and GL_RENDERER of OpenGL,
// the adapter and its driver's version of Direct3D 11, the physical device
// of Vulkan, the device of Metal. It is empty until a window has drawn.
func GPUInfo() gpu.DeviceInfo {
	if d := gpuInfo.Load(); d != nil {
		return *d
	}
	return gpu.DeviceInfo{}
}
