// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows && !(amd64 || arm64)

package app

// dropTarget is not made on 32-bit Windows (see os_windows_drop.go).
type dropTarget struct{}

func (w *window) registerDropTarget() {}

func (w *window) revokeDropTarget() {}
