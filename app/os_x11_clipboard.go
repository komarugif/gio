// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd || openbsd) && !nox11

package app

/*
#include <stdlib.h>
#include <X11/Xlib.h>
*/
import "C"

import (
	"bytes"
	"io"
	"slices"
	"unsafe"

	"gioui.org/io/clipboard"
	"gioui.org/io/transfer"
)

// Reads other than text ask for TARGETS first; large content comes in
// parts (INCR, ICCCM 2.7.2).
type x11ClipStage uint8

const (
	clipIdle x11ClipStage = iota
	clipTargets
	clipData
	clipIncr
)

type x11ClipRead struct {
	stage x11ClipStage
	types []string
	typ   string
	buf   []byte
	atoms struct {
		incr, uriList C.Atom
	}
}

func (w *x11Window) ReadClipboard(types []string) {
	r := &w.clipRead
	if r.atoms.incr == 0 {
		r.atoms.incr = w.atom("INCR", false)
		r.atoms.uriList = w.atom("text/uri-list", false)
	}
	r.types, r.buf = types, nil
	C.XDeleteProperty(w.x, w.xw, w.atoms.clipboardContent)
	if slices.Equal(types, []string{clipboard.TypeText}) {
		r.stage, r.typ = clipData, clipboard.TypeText
		C.XConvertSelection(w.x, w.atoms.clipboard, w.atoms.utf8string, w.atoms.clipboardContent, w.xw, C.CurrentTime)
		return
	}
	r.stage = clipTargets
	C.XConvertSelection(w.x, w.atoms.clipboard, w.atoms.targets, w.atoms.clipboardContent, w.xw, C.CurrentTime)
}

func (w *x11Window) clipTarget(typ string) C.Atom {
	switch typ {
	case clipboard.TypeText:
		return w.atoms.utf8string
	case clipboard.TypeURIList:
		return w.clipRead.atoms.uriList
	case clipboard.TypePNG:
		return w.atoms.imagePNG
	}
	return C.None
}

func (w *x11Window) clipboardNotify(ev *C.XSelectionEvent) {
	r := &w.clipRead
	switch {
	case r.stage == clipIdle || ev.selection != w.atoms.clipboard:
		return
	case ev.property == C.None:
		w.clipDone("", nil)
		return
	case ev.property != w.atoms.clipboardContent:
		return
	}
	typ, format, data, ok := w.readProperty(ev.property)
	switch r.stage {
	case clipTargets:
		if !ok || format != 32 {
			w.clipDone("", nil)
			return
		}
		atoms := make([]C.Atom, len(data)/int(unsafe.Sizeof(C.long(0))))
		for i := range atoms {
			atoms[i] = C.Atom(*(*C.long)(unsafe.Pointer(&data[i*int(unsafe.Sizeof(C.long(0)))])))
		}
		for _, t := range r.types {
			if target := w.clipTarget(t); target != C.None && slices.Contains(atoms, target) {
				r.stage, r.typ = clipData, t
				C.XConvertSelection(w.x, w.atoms.clipboard, target, w.atoms.clipboardContent, w.xw, C.CurrentTime)
				return
			}
		}
		w.clipDone("", nil)
	case clipData:
		switch {
		case !ok:
			w.clipDone("", nil)
		case typ == r.atoms.incr:
			r.stage, r.buf = clipIncr, nil
		default:
			w.clipDone(r.typ, data)
		}
	}
}

func (w *x11Window) clipboardProperty(ev *C.XPropertyEvent) {
	r := &w.clipRead
	if r.stage != clipIncr || ev.atom != w.atoms.clipboardContent || ev.state != C.PropertyNewValue {
		return
	}
	_, _, data, ok := w.readProperty(ev.atom)
	switch {
	case !ok:
		w.clipDone("", nil)
	case len(data) == 0:
		w.clipDone(r.typ, r.buf)
	default:
		r.buf = append(r.buf, data...)
	}
}

func (w *x11Window) clipDone(typ string, data []byte) {
	w.clipRead = x11ClipRead{atoms: w.clipRead.atoms}
	if typ == "" {
		data = nil
	}
	w.ProcessEvent(transfer.DataEvent{
		Type: typ,
		Open: func() io.ReadCloser {
			return io.NopCloser(bytes.NewReader(data))
		},
	})
}

// readProperty reads the property whole and deletes it. Items of format
// 32 are C.longs, as Xlib gives them.
func (w *x11Window) readProperty(prop C.Atom) (typ C.Atom, format int, data []byte, ok bool) {
	var offset C.long
	for {
		var cformat C.int
		var n, after C.ulong
		var p *C.uchar
		if C.XGetWindowProperty(w.x, w.xw, prop, offset, 1<<20, C.False, C.AnyPropertyType,
			&typ, &cformat, &n, &after, &p) != C.Success {
			return 0, 0, nil, false
		}
		format = int(cformat)
		if p != nil {
			size := int(n)
			switch format {
			case 16:
				size *= int(unsafe.Sizeof(C.short(0)))
			case 32:
				size *= int(unsafe.Sizeof(C.long(0)))
			}
			data = append(data, C.GoBytes(unsafe.Pointer(p), C.int(size))...)
			C.XFree(unsafe.Pointer(p))
		}
		if after == 0 || n == 0 {
			break
		}
		offset += C.long(int(n) * max(format, 8) / 32)
	}
	C.XDeleteProperty(w.x, w.xw, prop)
	return typ, format, data, typ != C.None
}
