// SPDX-License-Identifier: Unlicense OR MIT

package opengl

import "testing"

func TestSplitGLVersion(t *testing.T) {
	for version, want := range map[string][2]string{
		"OpenGL ES 3.2 Mesa 26.1.6-arch3.1": {"OpenGL ES 3.2", "Mesa 26.1.6-arch3.1"},
		"OpenGL ES 3.2 NVIDIA 610.43.03":    {"OpenGL ES 3.2", "NVIDIA 610.43.03"},
		"4.6.0 NVIDIA 610.43.03":            {"OpenGL 4.6.0", "NVIDIA 610.43.03"},
		"4.1 ATI-4.8.101":                   {"OpenGL 4.1", "ATI-4.8.101"},
		"OpenGL ES 3.0":                     {"OpenGL ES 3.0", ""},
		"":                                  {"", ""},
	} {
		if api, driver := splitGLVersion(version); api != want[0] || driver != want[1] {
			t.Errorf("splitGLVersion(%q) = %q, %q, want %q, %q", version, api, driver, want[0], want[1])
		}
	}
}
