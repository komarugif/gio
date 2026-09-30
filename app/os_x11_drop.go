// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd || openbsd) && !nox11

package app

/*
#include <stdlib.h>
#include <X11/Xlib.h>
*/
import "C"

import (
	"unsafe"

	"gioui.org/f32"
)

// A window takes files dragged from other programs through XDND, version
// 5 (https://www.freedesktop.org/wiki/Specifications/XDND/), as a target:
// the source tells of its drag with client messages, the window answers
// each position with a status, and reads the files as the selection
// XdndSelection converted to text/uri-list. The files are asked for at the
// first position, with its time, so that the program can tell what is
// dragged before it is dropped; a drop that comes before them waits for
// them.

// xdndVersion is the version of XDND the window speaks.
const xdndVersion = 5

// x11Drag is the state of a drag over the window.
type x11Drag struct {
	atoms struct {
		aware, enter, position, status, leave, drop, finished C.Atom
		selection, typeList, actionCopy, uriList, property    C.Atom
	}
	// source is the window of the drag over the window, 0 when there is
	// none; version is the XDND version it speaks.
	source  C.Window
	version int
	// files is set when the drag offers text/uri-list.
	files bool
	// entered is set once DropEnter was told; asked once the files were
	// asked for.
	entered, asked bool
	// paths are the files, once read; read is set then.
	paths []string
	read  bool
	// dropped is set when the drag was let go before its files were read.
	dropped bool
	at      f32.Point
}

// setupDrop makes the window a target of XDND.
func (w *x11Window) setupDrop() {
	a := &w.dnd.atoms
	a.aware = w.atom("XdndAware", false)
	a.enter = w.atom("XdndEnter", false)
	a.position = w.atom("XdndPosition", false)
	a.status = w.atom("XdndStatus", false)
	a.leave = w.atom("XdndLeave", false)
	a.drop = w.atom("XdndDrop", false)
	a.finished = w.atom("XdndFinished", false)
	a.selection = w.atom("XdndSelection", false)
	a.typeList = w.atom("XdndTypeList", false)
	a.actionCopy = w.atom("XdndActionCopy", false)
	a.uriList = w.atom("text/uri-list", false)
	a.property = w.atom("GIO_XDND_FILES", false)
	version := C.long(xdndVersion)
	C.XChangeProperty(w.x, w.xw, a.aware, w.atoms.atom, 32, C.PropModeReplace,
		(*C.uchar)(unsafe.Pointer(&version)), 1)
}

// dropMessage handles a client message of XDND, and reports whether it
// was one.
func (w *x11Window) dropMessage(ev *C.XClientMessageEvent) bool {
	a := &w.dnd.atoms
	l := (*[5]C.long)(unsafe.Pointer(&ev.data))
	source := C.Window(l[0])
	switch ev.message_type {
	case a.enter:
		version := int(uint64(l[1]) >> 24)
		if version > xdndVersion {
			return true
		}
		w.dropEnded()
		w.dnd.source, w.dnd.version = source, version
		if l[1]&1 == 0 {
			for _, t := range l[2:5] {
				if C.Atom(t) == a.uriList {
					w.dnd.files = true
				}
			}
		} else {
			w.dnd.files = w.sourceOffersFiles(source)
		}
	case a.position:
		if source != w.dnd.source {
			return true
		}
		root := uint64(l[2])
		var x, y C.int
		var child C.Window
		C.XTranslateCoordinates(w.x, C.XDefaultRootWindow(w.x), w.xw,
			C.int(root>>16&0xffff), C.int(root&0xffff), &x, &y, &child)
		w.dnd.at = f32.Pt(float32(x), float32(y))
		w.dropStatus(w.dnd.files)
		if !w.dnd.files {
			return true
		}
		if !w.dnd.asked {
			w.dnd.asked = true
			time := C.Time(C.CurrentTime)
			if w.dnd.version >= 1 {
				time = C.Time(l[3])
			}
			C.XConvertSelection(w.x, a.selection, a.uriList, a.property, w.xw, time)
		}
		kind := DropMove
		if !w.dnd.entered {
			w.dnd.entered = true
			kind = DropEnter
		}
		w.ProcessEvent(DropEvent{Kind: kind, Position: w.dnd.at, Paths: w.dnd.paths})
	case a.leave:
		if source == w.dnd.source {
			w.dropEnded()
		}
	case a.drop:
		if source != w.dnd.source {
			return true
		}
		if !w.dnd.files {
			w.dropFinished(false)
			w.dropEnded()
			return true
		}
		if !w.dnd.asked {
			w.dnd.asked = true
			time := C.Time(C.CurrentTime)
			if w.dnd.version >= 1 {
				time = C.Time(l[2])
			}
			C.XConvertSelection(w.x, a.selection, a.uriList, a.property, w.xw, time)
		}
		if !w.dnd.read {
			w.dnd.dropped = true
			return true
		}
		w.dropDone()
	default:
		return false
	}
	return true
}

// dropSelection takes the files a conversion of XdndSelection brought.
func (w *x11Window) dropSelection(ev *C.XSelectionEvent) {
	a := &w.dnd.atoms
	if w.dnd.source == 0 || w.dnd.read || ev.target != a.uriList {
		return
	}
	w.dnd.read = true
	if ev.property != C.None {
		var typ C.Atom
		var format C.int
		var n, after C.ulong
		var data *C.uchar
		// The list is read whole, up to 16 MB, and the property deleted.
		if C.XGetWindowProperty(w.x, w.xw, ev.property, 0, 1<<22, C.True, C.AnyPropertyType,
			&typ, &format, &n, &after, &data) == C.Success && data != nil {
			if format == 8 {
				w.dnd.paths = uriListPaths(C.GoStringN((*C.char)(unsafe.Pointer(data)), C.int(n)))
			}
			C.XFree(unsafe.Pointer(data))
		}
	}
	if w.dnd.dropped {
		w.dropDone()
		return
	}
	if w.dnd.entered {
		w.ProcessEvent(DropEvent{Kind: DropMove, Position: w.dnd.at, Paths: w.dnd.paths})
	}
}

// dropDone tells of the drop of the files read, and ends the drag.
func (w *x11Window) dropDone() {
	paths := w.dnd.paths
	w.dropFinished(len(paths) > 0)
	if len(paths) > 0 {
		w.dnd.entered = false
		w.ProcessEvent(DropEvent{Kind: Drop, Position: w.dnd.at, Paths: paths})
	}
	w.dropEnded()
}

// dropEnded forgets the drag, telling that it left if it was told of.
func (w *x11Window) dropEnded() {
	entered := w.dnd.entered
	atoms := w.dnd.atoms
	w.dnd = x11Drag{atoms: atoms}
	if entered {
		w.ProcessEvent(DropEvent{Kind: DropLeave})
	}
}

// sourceOffersFiles reads the XdndTypeList of a source that offers more
// than three types.
func (w *x11Window) sourceOffersFiles(source C.Window) bool {
	var typ C.Atom
	var format C.int
	var n, after C.ulong
	var data *C.uchar
	if C.XGetWindowProperty(w.x, source, w.dnd.atoms.typeList, 0, 1024, C.False, w.atoms.atom,
		&typ, &format, &n, &after, &data) != C.Success || data == nil {
		return false
	}
	defer C.XFree(unsafe.Pointer(data))
	if format != 32 {
		return false
	}
	// Xlib gives 32-bit items as longs.
	for _, t := range unsafe.Slice((*C.long)(unsafe.Pointer(data)), int(n)) {
		if C.Atom(t) == w.dnd.atoms.uriList {
			return true
		}
	}
	return false
}

// dropStatus answers a position: whether the window takes the drag, as a
// copy, and that it wants every position.
func (w *x11Window) dropStatus(accept bool) {
	var l [5]C.long
	l[0] = C.long(w.xw)
	if accept {
		l[1] = 1 | 2
		l[4] = C.long(w.dnd.atoms.actionCopy)
	} else {
		l[1] = 2
	}
	w.dropSend(w.dnd.atoms.status, l)
}

// dropFinished tells the source the drop is done with.
func (w *x11Window) dropFinished(accepted bool) {
	if w.dnd.version < 2 {
		return
	}
	var l [5]C.long
	l[0] = C.long(w.xw)
	if accepted {
		l[1] = 1
		l[2] = C.long(w.dnd.atoms.actionCopy)
	}
	w.dropSend(w.dnd.atoms.finished, l)
}

func (w *x11Window) dropSend(message C.Atom, l [5]C.long) {
	var xev C.XEvent
	ev := (*C.XClientMessageEvent)(unsafe.Pointer(&xev))
	*ev = C.XClientMessageEvent{
		_type:        C.ClientMessage,
		display:      w.x,
		window:       w.dnd.source,
		message_type: message,
		format:       32,
	}
	*(*[5]C.long)(unsafe.Pointer(&ev.data)) = l
	C.XSendEvent(w.x, w.dnd.source, C.False, C.NoEventMask, &xev)
	C.XFlush(w.x)
}
