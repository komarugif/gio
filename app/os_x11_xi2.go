// SPDX-License-Identifier: Unlicense OR MIT

//go:build ((linux && !android) || freebsd || openbsd) && !nox11

package app

/*
#cgo freebsd openbsd CFLAGS: -I/usr/X11R6/include -I/usr/local/include
#cgo linux freebsd LDFLAGS: -ldl

#include <stdlib.h>
#include <pthread.h>
#include <dlfcn.h>
#include <X11/Xlib.h>

// The types of XInput2.h used here, declared rather than included so that
// building needs no libXi headers, as running needs no libXi: it is
// loaded when there. They are libXi's ABI, unchanged since XI 2.0 (2009)
// and the scroll class of XI 2.1 (2011).
typedef struct {
	int deviceid;
	int mask_len;
	unsigned char *mask;
} gio_XIEventMask;

typedef struct {
	int type;
	int sourceid;
} gio_XIAnyClassInfo;

typedef struct {
	int type;
	int sourceid;
	int number;
	int scroll_type;
	double increment;
	int flags;
} gio_XIScrollClassInfo;

typedef struct {
	int deviceid;
	char *name;
	int use;
	int attachment;
	Bool enabled;
	int num_classes;
	gio_XIAnyClassInfo **classes;
} gio_XIDeviceInfo;

typedef struct {
	int mask_len;
	unsigned char *mask;
} gio_XIButtonState;

typedef struct {
	int mask_len;
	unsigned char *mask;
	double *values;
} gio_XIValuatorState;

typedef struct {
	int base;
	int latched;
	int locked;
	int effective;
} gio_XIModifierState;

typedef struct {
	int type;
	unsigned long serial;
	Bool send_event;
	Display *display;
	int extension;
	int evtype;
	Time time;
	int deviceid;
	int sourceid;
	int detail;
	Window root;
	Window event;
	Window child;
	double root_x;
	double root_y;
	double event_x;
	double event_y;
	int flags;
	gio_XIButtonState buttons;
	gio_XIValuatorState valuators;
	gio_XIModifierState mods;
	gio_XIModifierState group;
} gio_XIDeviceEvent;

typedef struct {
	int type;
	unsigned long serial;
	Bool send_event;
	Display *display;
	int extension;
	int evtype;
	Time time;
	int deviceid;
	int sourceid;
	int reason;
	int num_classes;
	gio_XIAnyClassInfo **classes;
} gio_XIDeviceChangedEvent;

typedef Status (*gio_XIQueryVersion)(Display *, int *, int *);
typedef int (*gio_XISelectEvents)(Display *, Window, gio_XIEventMask *, int);
typedef gio_XIDeviceInfo *(*gio_XIQueryDevice)(Display *, int, int *);
typedef void (*gio_XIFreeDeviceInfo)(gio_XIDeviceInfo *);
typedef Status (*gio_XIGetProperty)(Display *, int, Atom, long, long, Bool, Atom, Atom *, int *, unsigned long *, unsigned long *, unsigned char **);

// An error trap: Xlib's default handler ends the process on any error,
// and a device may be gone by the time it is asked about. While a trap is
// set, the errors of its display are counted; those of other displays,
// other windows', go to the handler that was there.
static pthread_mutex_t gio_xi2_trap_lock = PTHREAD_MUTEX_INITIALIZER;
static Display *gio_xi2_trap_display;
static int gio_xi2_trap_errors;
static int (*gio_xi2_trap_previous)(Display *, XErrorEvent *);

static int gio_xi2_trap_handler(Display *d, XErrorEvent *e) {
	if (d == gio_xi2_trap_display) {
		gio_xi2_trap_errors++;
		return 0;
	}
	return gio_xi2_trap_previous != NULL ? gio_xi2_trap_previous(d, e) : 0;
}

static void gio_xi2_trap(Display *d) {
	pthread_mutex_lock(&gio_xi2_trap_lock);
	// The errors of earlier requests are not the trap's.
	XSync(d, False);
	gio_xi2_trap_display = d;
	gio_xi2_trap_errors = 0;
	gio_xi2_trap_previous = XSetErrorHandler(gio_xi2_trap_handler);
}

static int gio_xi2_untrap(Display *d) {
	XSync(d, False);
	XSetErrorHandler(gio_xi2_trap_previous);
	gio_xi2_trap_display = NULL;
	int errors = gio_xi2_trap_errors;
	pthread_mutex_unlock(&gio_xi2_trap_lock);
	return errors;
}

static void gio_xi2_set_mask(unsigned char *mask, int event) {
	mask[event >> 3] |= 1 << (event & 7);
}

// gio_xi2_init asks for XInput 2.1, the version of smooth scrolling, and
// selects the pointer events of the master devices on win, and the changes
// of devices on root. It returns 0 when that failed.
static int gio_xi2_init(void *queryVersion, void *selectEvents, Display *d, Window win, Window root) {
	int major = 2, minor = 1;
	if (((gio_XIQueryVersion)queryVersion)(d, &major, &minor) != Success || major < 2 || (major == 2 && minor < 1)) {
		return 0;
	}
	unsigned char mask[4] = {0};
	gio_XIEventMask m = {1, sizeof(mask), mask}; // XIAllMasterDevices
	gio_xi2_set_mask(mask, 4); // XI_ButtonPress
	gio_xi2_set_mask(mask, 5); // XI_ButtonRelease
	gio_xi2_set_mask(mask, 6); // XI_Motion
	gio_xi2_set_mask(mask, 7); // XI_Enter
	gio_xi2_trap(d);
	((gio_XISelectEvents)selectEvents)(d, win, &m, 1);
	if (gio_xi2_untrap(d) != 0) {
		return 0;
	}
	unsigned char rmask[4] = {0};
	gio_XIEventMask r = {0, sizeof(rmask), rmask}; // XIAllDevices
	gio_xi2_set_mask(rmask, 1); // XI_DeviceChanged
	gio_xi2_set_mask(rmask, 11); // XI_HierarchyChanged
	gio_xi2_trap(d);
	((gio_XISelectEvents)selectEvents)(d, root, &r, 1);
	// Without these, a device plugged in under the id of one gone keeps
	// what was known of that one; scrolling works still.
	gio_xi2_untrap(d);
	return 1;
}

// gio_xi2_query asks for device id and whether it is a touchpad, by the
// properties the drivers of touchpads set, as GTK tells them. It returns
// NULL when the device is gone.
static gio_XIDeviceInfo *gio_xi2_query(void *queryDevice, void *freeDeviceInfo, void *getProperty, Display *d, int id, int *touchpad) {
	static const char *props[] = {"libinput Tapping Enabled", "Synaptics Off"};
	*touchpad = 0;
	gio_xi2_trap(d);
	int n = 0;
	gio_XIDeviceInfo *info = ((gio_XIQueryDevice)queryDevice)(d, id, &n);
	for (int i = 0; info != NULL && i < 2 && !*touchpad; i++) {
		Atom prop = XInternAtom(d, props[i], True);
		if (prop == None) {
			continue;
		}
		Atom type = None;
		int format;
		unsigned long items, after;
		unsigned char *data = NULL;
		if (((gio_XIGetProperty)getProperty)(d, id, prop, 0, 1, False, AnyPropertyType, &type, &format, &items, &after, &data) == Success && type != None) {
			*touchpad = 1;
		}
		if (data != NULL) {
			XFree(data);
		}
	}
	if (gio_xi2_untrap(d) != 0 && info != NULL) {
		((gio_XIFreeDeviceInfo)freeDeviceInfo)(info);
		info = NULL;
	}
	return info;
}

static void gio_xi2_free(void *freeDeviceInfo, gio_XIDeviceInfo *info) {
	((gio_XIFreeDeviceInfo)freeDeviceInfo)(info);
}
*/
import "C"

import (
	"log"
	"os"
	"sync"
	"time"
	"unsafe"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
)

// Smooth scrolling on X11 comes through XInput 2.1: a touchpad scrolls
// valuators of its own, and the server makes the buttons of a wheel from
// them for the core protocol, marked as emulated for XInput 2. Selecting
// XInput 2 pointer events for a window replaces the core ones there, so
// the window takes its buttons and its moves through XInput 2 as well.
// libXi is loaded when there; without it, or with GIO_X11_NOXI2 set, the
// window takes the core events, and a touchpad scrolls by the notches of
// a wheel. GIO_X11_XI2_TRACE logs what is known of the devices.

const (
	xiDeviceChanged    = 1
	xiButtonPress      = 4
	xiButtonRelease    = 5
	xiMotion           = 6
	xiEnter            = 7
	xiHierarchyChanged = 11

	xiScrollClass          = 3
	xiScrollTypeHorizontal = 2
	xiSlaveSwitch          = 1
	xiPointerEmulated      = 1 << 16
)

var xi2Lib struct {
	once                                                              sync.Once
	queryVersion, selectEvents, queryDevice, freeDeviceInfo, property unsafe.Pointer
}

var xi2Trace = os.Getenv("GIO_X11_XI2_TRACE") != ""

func xi2Tracef(format string, args ...any) {
	if xi2Trace {
		log.Printf("gio: x11: xi2: "+format, args...)
	}
}

// loadXI2 loads libXi, once for the process, and tells whether it is
// there with all that is used of it.
func loadXI2() bool {
	l := &xi2Lib
	l.once.Do(func() {
		if os.Getenv("GIO_X11_NOXI2") != "" {
			xi2Tracef("off: GIO_X11_NOXI2 is set")
			return
		}
		var h unsafe.Pointer
		for _, name := range []string{"libXi.so.6", "libXi.so"} {
			cname := C.CString(name)
			h = C.dlopen(cname, C.RTLD_NOW|C.RTLD_LOCAL)
			C.free(unsafe.Pointer(cname))
			if h != nil {
				break
			}
		}
		if h == nil {
			xi2Tracef("off: libXi not found")
			return
		}
		sym := func(name string) unsafe.Pointer {
			cname := C.CString(name)
			defer C.free(unsafe.Pointer(cname))
			return C.dlsym(h, cname)
		}
		qv, se, qd, fd, gp := sym("XIQueryVersion"), sym("XISelectEvents"), sym("XIQueryDevice"), sym("XIFreeDeviceInfo"), sym("XIGetProperty")
		if qv == nil || se == nil || qd == nil || fd == nil || gp == nil {
			xi2Tracef("off: libXi lacks XInput 2 functions")
			return
		}
		// The handle stays open: libXi converts the events of every
		// display it has seen.
		l.queryVersion, l.selectEvents, l.queryDevice, l.freeDeviceInfo, l.property = qv, se, qd, fd, gp
	})
	return l.queryVersion != nil
}

// x11XI2 is the XInput 2 state of a window's display.
type x11XI2 struct {
	opcode C.int
	// devices is what is known of the slave devices, by id; an entry
	// with no axes is a device that does not scroll smoothly.
	devices map[C.int]*xi2Device
	fling   xi2Fling
}

// setupXI2 selects XInput 2 pointer events for the window, if it can.
func (w *x11Window) setupXI2() {
	if !loadXI2() {
		return
	}
	var opcode, event, errorBase C.int
	ext := C.CString("XInputExtension")
	defer C.free(unsafe.Pointer(ext))
	if C.XQueryExtension(w.x, ext, &opcode, &event, &errorBase) == C.False {
		xi2Tracef("off: the server has no XInputExtension")
		return
	}
	l := &xi2Lib
	if C.gio_xi2_init(l.queryVersion, l.selectEvents, w.x, w.xw, C.XDefaultRootWindow(w.x)) == 0 {
		xi2Tracef("off: the server has no XInput 2.1")
		return
	}
	w.xi2 = &x11XI2{opcode: opcode, devices: map[C.int]*xi2Device{}}
	xi2Tracef("on, window %d", w.xw)
}

// device returns what is known of slave device id, asking the server the
// first time.
func (w *x11Window) xi2Device(id C.int) *xi2Device {
	if d, ok := w.xi2.devices[id]; ok {
		return d
	}
	l := &xi2Lib
	var touchpad C.int
	d := new(xi2Device)
	if info := C.gio_xi2_query(l.queryDevice, l.freeDeviceInfo, l.property, w.x, id, &touchpad); info != nil {
		d.name = C.GoString(info.name)
		d.touchpad = touchpad != 0
		for _, c := range unsafe.Slice(info.classes, info.num_classes) {
			if c._type != xiScrollClass {
				continue
			}
			s := (*C.gio_XIScrollClassInfo)(unsafe.Pointer(c))
			d.axes = append(d.axes, xi2Axis{number: int(s.number), horizontal: s.scroll_type == xiScrollTypeHorizontal, increment: float64(s.increment)})
		}
		C.gio_xi2_free(l.freeDeviceInfo, info)
	}
	w.xi2.devices[id] = d
	xi2Tracef("device %d %q: touchpad %v, scroll axes %+v", id, d.name, d.touchpad, d.axes)
	return d
}

// xi2StopFling stops the kinetic scrolling after a touchpad's, as a press
// or other scrolling does.
func (w *x11Window) xi2StopFling() {
	if w.xi2 != nil {
		w.xi2.fling.stop()
	}
}

// xi2Flinging tells whether the kinetic scrolling goes on.
func (w *x11Window) xi2Flinging() bool {
	return w.xi2 != nil && w.xi2.fling.anim.Active()
}

// xi2FlingWait is how long, in milliseconds, the event loop waits for
// the end of a touchpad's scrolling; -1, without end, when none scrolls.
func (w *x11Window) xi2FlingWait() int {
	if w.xi2 == nil {
		return -1
	}
	return w.xi2.fling.wait(time.Now())
}

// xi2Fling starts and moves on the kinetic scrolling after a touchpad's;
// it tells whether it goes on.
func (w *x11Window) xi2Fling(now time.Time) bool {
	if w.xi2 == nil {
		return false
	}
	f := &w.xi2.fling
	was := f.anim.Active()
	on := f.step(now, w.metric, func(e pointer.Event) {
		e.Buttons = w.pointerBtns
		w.ProcessEvent(e)
	})
	if on && !was {
		xi2Tracef("touchpad fling at %.0f units a second", f.velocity)
	}
	return on
}

// handleXI2 handles an XInput 2 event; it tells whether it was one.
func (w *x11Window) handleXI2(xev *C.XEvent) bool {
	if w.xi2 == nil {
		return false
	}
	cookie := (*C.XGenericEventCookie)(unsafe.Pointer(xev))
	if cookie.extension != w.xi2.opcode || C.XGetEventData(w.x, cookie) == C.False {
		return false
	}
	defer C.XFreeEventData(w.x, cookie)
	switch cookie.evtype {
	case xiButtonPress, xiButtonRelease:
		ev := (*C.gio_XIDeviceEvent)(cookie.data)
		emulated := ev.flags&xiPointerEmulated != 0
		if b := ev.detail; b >= 4 && b <= 7 {
			// The buttons of a wheel made from smooth scrolling come as
			// the scrolling itself, in xi2Motion, unless that had nothing
			// to scroll from. Only those of scrolling are skipped: a
			// touchscreen's presses are emulated too.
			counts := !emulated || w.xi2Device(ev.sourceid).countsEmulated(uint(b))
			xi2Tracef("button %d from device %d, release %v, emulated %v, counted %v", b, ev.sourceid, cookie.evtype == xiButtonRelease, emulated, counts)
			if !counts {
				break
			}
		}
		if cookie.evtype == xiButtonPress {
			w.xi2StopFling()
		}
		w.pointerButton(cookie.evtype == xiButtonRelease, uint(ev.detail), f32.Pt(float32(ev.event_x), float32(ev.event_y)), C.Time(ev.time))
	case xiMotion:
		w.xi2Motion((*C.gio_XIDeviceEvent)(cookie.data))
	case xiEnter:
		// Scrolling went on elsewhere while the pointer was away.
		for _, d := range w.xi2.devices {
			d.reset()
		}
	case xiDeviceChanged:
		ev := (*C.gio_XIDeviceChangedEvent)(cookie.data)
		if ev.reason == xiSlaveSwitch {
			if d, ok := w.xi2.devices[ev.sourceid]; ok {
				d.reset()
			}
		} else {
			delete(w.xi2.devices, ev.deviceid)
		}
	case xiHierarchyChanged:
		// Devices came or went; ids may be taken again.
		clear(w.xi2.devices)
	}
	return true
}

func (w *x11Window) xi2Motion(ev *C.gio_XIDeviceEvent) {
	var vals []xi2Valuator
	if v := ev.valuators; v.mask_len > 0 && v.mask != nil {
		mask := unsafe.Slice((*byte)(unsafe.Pointer(v.mask)), v.mask_len)
		n := 0
		for _, b := range mask {
			for ; b != 0; b &= b - 1 {
				n++
			}
		}
		var values []float64
		if n > 0 && v.values != nil {
			values = unsafe.Slice((*float64)(unsafe.Pointer(v.values)), n)
		}
		vals = xi2Valuators(mask, values)
	}
	d := w.xi2Device(ev.sourceid)
	emulated := ev.flags&xiPointerEmulated != 0
	if xi2Trace {
		for _, v := range vals {
			if _, ok := d.axis(v.number); ok {
				xi2Tracef("scroll valuator %d of device %d = %g, emulated %v", v.number, ev.sourceid, v.value, emulated)
			}
		}
	}
	s, moves := d.scroll(vals, emulated)
	pos := f32.Pt(float32(ev.event_x), float32(ev.event_y))
	t := time.Duration(ev.time) * time.Millisecond
	mods := w.xkb.Modifiers()
	if moves || len(vals) == 0 {
		w.ProcessEvent(pointer.Event{
			Kind:      pointer.Move,
			Source:    pointer.Mouse,
			Buttons:   w.pointerBtns,
			Position:  pos,
			Time:      t,
			Modifiers: mods,
		})
	}
	if s == (f32.Point{}) {
		return
	}
	// Shift turns vertical scrolling horizontal, as for the buttons.
	if mods == key.ModShift && s.X == 0 {
		s.X, s.Y = s.Y, 0
	}
	e := pointer.Event{
		Kind:      pointer.Scroll,
		Source:    pointer.Mouse,
		Buttons:   w.pointerBtns,
		Position:  pos,
		Scroll:    s,
		Wheel:     !d.touchpad,
		Time:      t,
		Modifiers: mods,
	}
	if d.touchpad {
		w.xi2.fling.scrolled(e, time.Now())
	} else {
		w.xi2StopFling()
	}
	w.ProcessEvent(e)
}
