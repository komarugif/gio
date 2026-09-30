// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package windows

import (
	"fmt"
	"unsafe"

	syscall "golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	_DwmEnableBlurBehindWindow = dwmapi.NewProc("DwmEnableBlurBehindWindow")
	// SetWindowCompositionAttribute is not documented. It is what the
	// taskbar and the Start menu are blurred with, and what is there to
	// blur behind a window before Windows 11.
	_SetWindowCompositionAttribute = user32.NewProc("SetWindowCompositionAttribute")
	_CreateRectRgn                 = gdi32.NewProc("CreateRectRgn")
	_DeleteObject                  = gdi32.NewProc("DeleteObject")
)

// The accent states of SetWindowCompositionAttribute.
const (
	AccentDisabled = 0
	// AccentAcrylic blurs what is behind the window and tints it.
	AccentAcrylic = 4
)

const wcaAccentPolicy = 19

type accentPolicy struct {
	state, flags, color, animation uint32
}

type compositionAttribute struct {
	attribute uint32
	data      unsafe.Pointer
	size      uintptr
}

// SetWindowAccent sets the accent policy of a window: what the system draws
// behind its content. color is the tint, as 0xAABBGGRR; acrylic needs one
// that is not wholly transparent.
func SetWindowAccent(hwnd syscall.Handle, state, color uint32) error {
	if err := _SetWindowCompositionAttribute.Find(); err != nil {
		return err
	}
	policy := accentPolicy{state: state, color: color}
	data := compositionAttribute{attribute: wcaAccentPolicy, data: unsafe.Pointer(&policy), size: unsafe.Sizeof(policy)}
	r, _, err := _SetWindowCompositionAttribute.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&data)))
	if r == 0 {
		return fmt.Errorf("SetWindowCompositionAttribute: %v", err)
	}
	return nil
}

const (
	dwmBBEnable     = 1
	dwmBBBlurRegion = 2
)

type dwmBlurBehind struct {
	flags      uint32
	enable     int32
	region     syscall.Handle
	transition int32
}

// DwmEnableTransparency makes the system take the alpha of what the window
// draws, without blurring what shows through: DwmEnableBlurBehindWindow with
// an empty region, which has not blurred since Windows 8.
func DwmEnableTransparency(hwnd syscall.Handle, enable bool) error {
	bb := dwmBlurBehind{flags: dwmBBEnable}
	if enable {
		region, _, _ := _CreateRectRgn.Call(0, 0, ^uintptr(0), ^uintptr(0))
		defer _DeleteObject.Call(region)
		bb.flags, bb.enable, bb.region = dwmBBEnable|dwmBBBlurRegion, 1, syscall.Handle(region)
	}
	r, _, _ := _DwmEnableBlurBehindWindow.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&bb)))
	if r != 0 {
		return fmt.Errorf("DwmEnableBlurBehindWindow: %#x", r)
	}
	return nil
}

// TransparencyEffects reports whether the user has the transparency effects
// of the system on: with them off it draws its own acrylic surfaces solid.
func TransparencyEffects() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return true
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("EnableTransparency")
	return err != nil || v != 0
}
