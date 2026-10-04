// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows && (amd64 || arm64)

package app

import (
	"sync"
	"syscall"
	"unsafe"

	syswin "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
	"gioui.org/f32"
)

// A window takes files dragged from other programs through an IDropTarget
// of OLE, a COM object made here: a table of Go callbacks. On 64-bit
// Windows, the POINTL IDropTarget's methods take by value is one register,
// one argument of a callback; 32-bit Windows would split it in two, and
// has no drop target (os_windows_nodrop.go).

var (
	ole32              = syswin.NewLazySystemDLL("ole32.dll")
	_OleInitialize     = ole32.NewProc("OleInitialize")
	_OleUninitialize   = ole32.NewProc("OleUninitialize")
	_RegisterDragDrop  = ole32.NewProc("RegisterDragDrop")
	_RevokeDragDrop    = ole32.NewProc("RevokeDragDrop")
	_ReleaseStgMedium  = ole32.NewProc("ReleaseStgMedium")
	shell32            = syswin.NewLazySystemDLL("shell32.dll")
	dropTargetVtblOnce sync.Once
	dropTargetVtbl     *dropTargetMethods
	dropTargets        sync.Map // this pointer → *dropTarget
	iidIUnknown        = guid{Data1: 0x00000000, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIDropTarget     = guid{Data1: 0x00000122, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const (
	_S_OK          = 0
	_S_FALSE       = 1
	_E_NOINTERFACE = 0x80004002
	_E_UNEXPECTED  = 0x8000ffff

	_DROPEFFECT_NONE = 0
	_DROPEFFECT_COPY = 1

	_CF_HDROP          = 15
	_DVASPECT_CONTENT  = 1
	_TYMED_HGLOBAL     = 1
	idataObjectGetData = 3 // IUnknown's three methods, then GetData
)

// dropTargetMethods is IDropTarget's table of methods.
type dropTargetMethods struct {
	QueryInterface, AddRef, Release      uintptr
	DragEnter, DragOver, DragLeave, Drop uintptr
}

// dropTarget is the IDropTarget of a window. Its first field is what COM
// sees of it; the Go fields after it are the window's.
type dropTarget struct {
	vtbl *dropTargetMethods
	w    *window
	// paths are the files of the drag over the window, nil when it
	// carries none.
	paths []string
	// at is where the drag was last told to be: OLE asks again and again
	// while the pointer stays.
	at uintptr
}

type formatEtc struct {
	cfFormat uint16
	ptd      uintptr
	aspect   uint32
	index    int32
	tymed    uint32
}

type stgMedium struct {
	tymed          uint32
	handle         uintptr
	pUnkForRelease uintptr
}

// registerDropTarget makes the window take drags of files. OLE wants the
// thread of the window initialized for it; a failure leaves the window
// without drops.
func (w *window) registerDropTarget() {
	if r, _, _ := _OleInitialize.Call(0); r != _S_OK && r != _S_FALSE {
		return
	}
	dropTargetVtblOnce.Do(func() {
		dropTargetVtbl = &dropTargetMethods{
			QueryInterface: syscall.NewCallback(dropQueryInterface),
			AddRef:         syscall.NewCallback(dropAddRef),
			Release:        syscall.NewCallback(dropAddRef),
			DragEnter:      syscall.NewCallback(dropDragEnter),
			DragOver:       syscall.NewCallback(dropDragOver),
			DragLeave:      syscall.NewCallback(dropDragLeave),
			Drop:           syscall.NewCallback(dropDrop),
		}
	})
	t := &dropTarget{vtbl: dropTargetVtbl, w: w}
	this := uintptr(unsafe.Pointer(t))
	dropTargets.Store(this, t)
	if r, _, _ := _RegisterDragDrop.Call(uintptr(w.hwnd), this); r != _S_OK {
		dropTargets.Delete(this)
		_OleUninitialize.Call()
		return
	}
	w.drop = t
}

// revokeDropTarget undoes registerDropTarget, before the window goes.
func (w *window) revokeDropTarget() {
	if w.drop == nil {
		return
	}
	_RevokeDragDrop.Call(uintptr(w.hwnd))
	dropTargets.Delete(uintptr(unsafe.Pointer(w.drop)))
	w.drop = nil
	_OleUninitialize.Call()
}

func targetOf(this uintptr) *dropTarget {
	t, ok := dropTargets.Load(this)
	if !ok {
		return nil
	}
	return t.(*dropTarget)
}

func dropQueryInterface(this uintptr, riid *guid, ppv *uintptr) uintptr {
	if *riid != iidIUnknown && *riid != iidIDropTarget {
		*ppv = 0
		return _E_NOINTERFACE
	}
	*ppv = this
	return _S_OK
}

// dropAddRef counts nothing: the target lives as long as its window, which
// revokes it before it goes.
func dropAddRef(this uintptr) uintptr { return 1 }

func dropDragEnter(this uintptr, data *dataObject, keys, pt uintptr, effect *uint32) uintptr {
	t := targetOf(this)
	if t == nil {
		return _E_UNEXPECTED
	}
	t.paths = dataObjectPaths(data)
	t.at = pt
	if t.paths == nil {
		setEffect(effect, _DROPEFFECT_NONE)
		return _S_OK
	}
	setEffect(effect, _DROPEFFECT_COPY)
	t.w.ProcessEvent(DropEvent{Kind: DropEnter, Position: t.w.dropPosition(pt), Paths: t.paths})
	return _S_OK
}

func dropDragOver(this, keys, pt uintptr, effect *uint32) uintptr {
	t := targetOf(this)
	if t == nil {
		return _E_UNEXPECTED
	}
	if t.paths == nil {
		setEffect(effect, _DROPEFFECT_NONE)
		return _S_OK
	}
	setEffect(effect, _DROPEFFECT_COPY)
	if pt == t.at {
		return _S_OK
	}
	t.at = pt
	t.w.ProcessEvent(DropEvent{Kind: DropMove, Position: t.w.dropPosition(pt), Paths: t.paths})
	return _S_OK
}

func dropDragLeave(this uintptr) uintptr {
	t := targetOf(this)
	if t == nil {
		return _E_UNEXPECTED
	}
	if t.paths != nil {
		t.paths = nil
		t.w.ProcessEvent(DropEvent{Kind: DropLeave})
	}
	return _S_OK
}

func dropDrop(this uintptr, data *dataObject, keys, pt uintptr, effect *uint32) uintptr {
	t := targetOf(this)
	if t == nil {
		return _E_UNEXPECTED
	}
	paths := dataObjectPaths(data)
	t.paths = nil
	if paths == nil {
		setEffect(effect, _DROPEFFECT_NONE)
		return _S_OK
	}
	setEffect(effect, _DROPEFFECT_COPY)
	t.w.ProcessEvent(DropEvent{Kind: Drop, Position: t.w.dropPosition(pt), Paths: paths})
	return _S_OK
}

func setEffect(effect *uint32, v uint32) {
	if effect != nil {
		*effect = v
	}
}

// dropPosition is where a point of the screen, a POINTL in one register,
// is in the window's client area.
func (w *window) dropPosition(pt uintptr) f32.Point {
	p := windows.Point{X: int32(uint32(pt)), Y: int32(uint32(pt >> 32))}
	windows.ScreenToClient(w.hwnd, &p)
	return f32.Pt(float32(p.X), float32(p.Y))
}

// dataObjectPaths reads the files an IDataObject carries as CF_HDROP; nil
// when it carries none.
func dataObjectPaths(data *dataObject) []string {
	if data == nil {
		return nil
	}
	format := formatEtc{cfFormat: _CF_HDROP, aspect: _DVASPECT_CONTENT, index: -1, tymed: _TYMED_HGLOBAL}
	var medium stgMedium
	r, _, _ := syscall.SyscallN(data.vtbl[idataObjectGetData], uintptr(unsafe.Pointer(data)), uintptr(unsafe.Pointer(&format)), uintptr(unsafe.Pointer(&medium)))
	if r != _S_OK {
		return nil
	}
	defer _ReleaseStgMedium.Call(uintptr(unsafe.Pointer(&medium)))
	if medium.tymed != _TYMED_HGLOBAL || medium.handle == 0 {
		return nil
	}
	return hdropPaths(medium.handle)
}

// guid is a COM interface identifier.
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// dataObject is an IDataObject: its table of methods, of which GetData is
// the fourth.
type dataObject struct {
	vtbl *[4]uintptr
}
