// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd) && !nowayland

package app

/*
#include <stdlib.h>
#include <wayland-client.h>
*/
import "C"

import (
	"io"
	"os"
	"unsafe"

	"gioui.org/f32"
)

// A window takes files dragged from other programs through the seat's
// wl_data_device: the offer of a drag that comes over one of the windows
// is kept while it is there if it has text/uri-list, is accepted as a
// copy, and the files are read from it at once, in the background, so
// that the program can tell what is dragged before it is dropped. A drop
// that comes before them waits for them, and the offer is finished then.

// uriListMIME is the type of a list of files.
const uriListMIME = "text/uri-list"

// wlDrag is the drag of files over a window of a seat.
type wlDrag struct {
	offer  *C.struct_wl_data_offer
	window *window
	at     f32.Point
	// paths are the files, once read; read is set then.
	paths []string
	read  bool
	// dropped is set when the drag was let go before its files were read.
	dropped bool
	// reader is the end of the pipe the files are read from, closed when
	// the drag ends first.
	reader *os.File
}

// wlDropRead is the files read from an offer.
type wlDropRead struct {
	offer *C.struct_wl_data_offer
	paths []string
}

// dropEnter starts a drag over the surface surf, x and y from its top
// left, with the offer id; the offer is kept if it has files.
func (s *wlSeat) dropEnter(serial C.uint32_t, surf *C.struct_wl_surface, x, y C.wl_fixed_t, id *C.struct_wl_data_offer) {
	s.dropEnded()
	// A surface that is none of the windows, if any, takes no files.
	v, _ := callbackMap.Load(unsafe.Pointer(surf))
	w, _ := v.(*window)
	if id == nil {
		return
	}
	files := false
	for _, mime := range s.offers[id] {
		if mime == uriListMIME {
			files = true
		}
	}
	if w == nil || !files {
		C.wl_data_offer_accept(id, serial, nil)
		return
	}
	cmime := C.CString(uriListMIME)
	C.wl_data_offer_accept(id, serial, cmime)
	C.free(unsafe.Pointer(cmime))
	C.wl_data_offer_set_actions(id, C.WL_DATA_DEVICE_MANAGER_DND_ACTION_COPY, C.WL_DATA_DEVICE_MANAGER_DND_ACTION_COPY)
	s.drag = wlDrag{offer: id, window: w, at: w.dropPosition(x, y)}
	s.readDrop()
	w.ProcessEvent(DropEvent{Kind: DropEnter, Position: s.drag.at})
}

// dropMotion moves the drag to x and y of its surface.
func (s *wlSeat) dropMotion(x, y C.wl_fixed_t) {
	w := s.drag.window
	if w == nil || s.drag.dropped {
		return
	}
	s.drag.at = w.dropPosition(x, y)
	w.ProcessEvent(DropEvent{Kind: DropMove, Position: s.drag.at, Paths: s.drag.paths})
}

// dropLeave ends the drag, unless it was dropped and waits for its files.
func (s *wlSeat) dropLeave() {
	if !s.drag.dropped {
		s.dropEnded()
	}
}

// dropDrop is the drag let go.
func (s *wlSeat) dropDrop() {
	if s.drag.window == nil {
		return
	}
	if !s.drag.read {
		s.drag.dropped = true
		return
	}
	s.dropDone()
}

// dropRead takes the files read from an offer.
func (s *wlSeat) dropRead(r wlDropRead) {
	if r.offer != s.drag.offer || s.drag.offer == nil {
		return
	}
	s.drag.read, s.drag.paths, s.drag.reader = true, r.paths, nil
	if s.drag.dropped {
		s.dropDone()
		return
	}
	if w := s.drag.window; w != nil {
		w.ProcessEvent(DropEvent{Kind: DropMove, Position: s.drag.at, Paths: s.drag.paths})
	}
}

// dropDone tells of the drop of the files read, and ends the drag.
func (s *wlSeat) dropDone() {
	if len(s.drag.paths) == 0 {
		s.dropEnded()
		return
	}
	C.wl_data_offer_finish(s.drag.offer)
	// Taken before the drag is forgotten, which clears where it is.
	drop := DropEvent{Kind: Drop, Position: s.drag.at, Paths: s.drag.paths}
	w := s.drag.window
	s.drag.window = nil
	s.dropEnded()
	if w != nil {
		w.ProcessEvent(drop)
	}
}

// dropEnded forgets the drag and its offer, telling that it left if its
// window is still there.
func (s *wlSeat) dropEnded() {
	d := s.drag
	s.drag = wlDrag{}
	if d.reader != nil {
		d.reader.Close()
	}
	if d.offer != nil {
		delete(s.offers, d.offer)
		callbackDelete(unsafe.Pointer(d.offer))
		C.wl_data_offer_destroy(d.offer)
	}
	if d.window != nil {
		d.window.ProcessEvent(DropEvent{Kind: DropLeave})
	}
}

// dropWindowGone forgets the window w of a drag, which is being destroyed.
func (s *wlSeat) dropWindowGone(w *window) {
	if s.drag.window == w {
		s.drag.window = nil
		s.dropEnded()
	}
}

// readDrop asks the offer of the drag for its files, and reads them in the
// background; they come to the window's event loop.
func (s *wlSeat) readDrop() {
	w, offer := s.drag.window, s.drag.offer
	reads, disp := w.dropReads, w.disp
	r, wr, err := os.Pipe()
	if err != nil {
		s.drag.read = true
		return
	}
	cmime := C.CString(uriListMIME)
	C.wl_data_offer_receive(offer, cmime, C.int(wr.Fd()))
	C.free(unsafe.Pointer(cmime))
	// The request took a copy of the descriptor.
	wr.Close()
	s.drag.reader = r
	go func() {
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, 16<<20))
		if err != nil {
			// Closed when the drag ended.
			return
		}
		select {
		case reads <- wlDropRead{offer: offer, paths: uriListPaths(string(data))}:
			disp.wakeup()
		default:
			// Reads of drags long gone fill the queue; the window no
			// longer takes them.
		}
	}()
}

// dropPosition is where x and y of the window's surface are in pixels.
func (w *window) dropPosition(x, y C.wl_fixed_t) f32.Point {
	return f32.Point{X: fromFixed(x) * float32(w.scale), Y: fromFixed(y) * float32(w.scale)}
}
