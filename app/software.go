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

func gpuDeviceName(g gpu.GPU) string { return gpu.Device(g) }

// gpuDevice is what the GPU made last for a window tells of its device.
var gpuDevice atomic.Pointer[string]

// GPUDevice names the device and the driver windows are drawn with, as
// the GPU made last for a window tells them through its API: GL_RENDERER
// and GL_VERSION of OpenGL, the adapter and its driver's version of
// Direct3D 11, the physical device of Vulkan, the device of Metal. It is
// empty until a window has drawn, or if the API tells nothing.
func GPUDevice() string {
	if d := gpuDevice.Load(); d != nil {
		return *d
	}
	return ""
}
