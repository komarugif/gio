// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	"syscall"
	"unsafe"

	gowindows "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
	"gioui.org/io/clipboard"
)

const (
	cfHDROP = 15
	cfDIBV5 = 17
)

var dragQueryFile = syscall.NewLazyDLL("shell32.dll").NewProc("DragQueryFileW")

func hdropPaths(h uintptr) []string {
	count, _, _ := dragQueryFile.Call(h, 0xffffffff, 0, 0)
	var paths []string
	for i := uintptr(0); i < count; i++ {
		n, _, _ := dragQueryFile.Call(h, i, 0, 0)
		if n == 0 {
			continue
		}
		buf := make([]uint16, n+1)
		dragQueryFile.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), n+1)
		paths = append(paths, syscall.UTF16ToString(buf))
	}
	return paths
}

// clipboardContent reads the first of types the open clipboard has.
func clipboardContent(types []string) (string, []byte) {
	data := func(format uint32) ([]byte, bool) {
		mem, err := windows.GetClipboardData(format)
		if err != nil {
			return nil, false
		}
		ptr, err := windows.GlobalLock(mem)
		if err != nil {
			return nil, false
		}
		defer windows.GlobalUnlock(mem)
		size, _, _ := globalSize.Call(uintptr(mem))
		return append([]byte(nil), unsafe.Slice((*byte)(ptr), size)...), true
	}
	for _, t := range types {
		switch t {
		case clipboard.TypeText:
			mem, err := windows.GetClipboardData(windows.CF_UNICODETEXT)
			if err != nil {
				continue
			}
			ptr, err := windows.GlobalLock(mem)
			if err != nil {
				continue
			}
			text := gowindows.UTF16PtrToString((*uint16)(ptr))
			windows.GlobalUnlock(mem)
			return t, []byte(text)
		case clipboard.TypeURIList:
			if mem, err := windows.GetClipboardData(cfHDROP); err == nil {
				if paths := hdropPaths(uintptr(mem)); len(paths) > 0 {
					return t, []byte(uriList(paths))
				}
			}
		case clipboard.TypePNG:
			if format, err := windows.RegisterClipboardFormat("PNG"); err == nil {
				if b, ok := data(format); ok {
					return t, b
				}
			}
			for _, format := range []uint32{cfDIBV5, windows.CF_DIB} {
				if b, ok := data(format); ok {
					if b, err := dibToPNG(b); err == nil {
						return t, b
					}
				}
			}
		}
	}
	return "", nil
}

var globalSize = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalSize")
