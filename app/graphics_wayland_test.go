// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd) && !nowayland

package app

import (
	"errors"
	"testing"
)

// Windows on Wayland try the API chosen first, and the other where it
// fails; the automatic choice tries Vulkan first.
func TestWaylandContexts(t *testing.T) {
	egl, vulkan := newWaylandEGLContext, newWaylandVulkanContext
	defer func() { newWaylandEGLContext, newWaylandVulkanContext = egl, vulkan }()
	var tried []string
	newWaylandEGLContext = func(*window) (context, error) { tried = append(tried, "egl"); return nil, errors.New("egl") }
	newWaylandVulkanContext = func(*window) (context, error) { tried = append(tried, "vulkan"); return nil, errors.New("vulkan") }
	for api, want := range map[GraphicsAPI]string{
		GraphicsAuto:   "vulkan egl",
		GraphicsOpenGL: "egl vulkan",
		GraphicsVulkan: "vulkan egl",
	} {
		tried = nil
		for _, f := range waylandContexts(Graphics{API: api}) {
			f(nil)
		}
		if got := tried[0] + " " + tried[1]; got != want {
			t.Errorf("API %d tries %s, want %s", api, got, want)
		}
	}
}

// Where Vulkan has failed, windows are drawn with EGL without trying it.
func TestWaylandContextsVulkanFailed(t *testing.T) {
	for _, api := range []GraphicsAPI{GraphicsAuto, GraphicsOpenGL, GraphicsVulkan} {
		if c := waylandContexts(Graphics{API: api, VulkanFailed: true}); len(c) != 1 {
			t.Errorf("API %d tries %d APIs after Vulkan failed", api, len(c))
		}
	}
}
