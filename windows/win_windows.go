//go:build windows

package main

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	wsChild           = 0x40000000
	wsVisible         = 0x10000000
	wsTab             = 0x10000
	wsVScroll         = 0x200000
	wsExEdge          = 0x200
	wsExControlParent = 0x10000
	wmDestroy         = 2
	wmSize            = 5
	wmClose           = 0x10
	wmGetMinMax       = 0x24
	wmSetFont         = 0x30
	wmCommand         = 0x111
	wmDPIChanged      = 0x02e0
	wmCtlStatic       = 0x138
	wmErase           = 0x14
	wmKeyDown         = 0x0100
	wmApp             = 0x8001
	cbAdd             = 0x143
	cbCur             = 0x147
	cbReset           = 0x14b
	cbSel             = 0x14e
	lbAdd             = 0x180
	lbReset           = 0x184
	lbSel             = 0x186
	lbCur             = 0x188
	bmGetCheck        = 0xf0
	bmSetCheck        = 0xf1
	stockApp          = 32512
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	gdi32            = syscall.NewLazyDLL("gdi32.dll")
	comdlg           = syscall.NewLazyDLL("comdlg32.dll")
	crypt32          = syscall.NewLazyDLL("crypt32.dll")
	shcore           = syscall.NewLazyDLL("shcore.dll")
	createWindow     = user32.NewProc("CreateWindowExW")
	defWindow        = user32.NewProc("DefWindowProcW")
	regClass         = user32.NewProc("RegisterClassExW")
	sendMessage      = user32.NewProc("SendMessageW")
	postMessage      = user32.NewProc("PostMessageW")
	getMsg           = user32.NewProc("GetMessageW")
	dialogMessage    = user32.NewProc("IsDialogMessageW")
	translate        = user32.NewProc("TranslateMessage")
	dispatch         = user32.NewProc("DispatchMessageW")
	moveWindow       = user32.NewProc("MoveWindow")
	showWindow       = user32.NewProc("ShowWindow")
	updateWindow     = user32.NewProc("UpdateWindow")
	getClientRect    = user32.NewProc("GetClientRect")
	getWindowRect    = user32.NewProc("GetWindowRect")
	adjustRect       = user32.NewProc("AdjustWindowRectExForDpi")
	destroyWindow    = user32.NewProc("DestroyWindow")
	invalidate       = user32.NewProc("InvalidateRect")
	enableWindow     = user32.NewProc("EnableWindow")
	getTextLen       = user32.NewProc("GetWindowTextLengthW")
	getText          = user32.NewProc("GetWindowTextW")
	setText          = user32.NewProc("SetWindowTextW")
	getDPI           = user32.NewProc("GetDpiForWindow")
	dpiSystem        = user32.NewProc("GetDpiForSystem")
	dpiAware         = user32.NewProc("SetProcessDpiAwarenessContext")
	setFocus         = user32.NewProc("SetFocus")
	foreground       = user32.NewProc("SetForegroundWindow")
	messageBox       = user32.NewProc("MessageBoxW")
	getModule        = kernel32.NewProc("GetModuleHandleW")
	loadCursor       = user32.NewProc("LoadCursorW")
	loadImage        = user32.NewProc("LoadImageW")
	loadIcon         = user32.NewProc("LoadIconW")
	postQuit         = user32.NewProc("PostQuitMessage")
	makeFont         = gdi32.NewProc("CreateFontW")
	deleteObject     = gdi32.NewProc("DeleteObject")
	solidBrush       = gdi32.NewProc("CreateSolidBrush")
	fillRect         = user32.NewProc("FillRect")
	setTextColor     = gdi32.NewProc("SetTextColor")
	setBkMode        = gdi32.NewProc("SetBkMode")
	sysColor         = user32.NewProc("GetSysColor")
	systemParameters = user32.NewProc("SystemParametersInfoW")
)

type point struct{ X, Y int32 }
type rect struct{ L, T, R, B int32 }
type msg struct {
	H       uintptr
	M       uint32
	W, L    uintptr
	Time    uint32
	P       point
	Private uint32
}
type winClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	IconSmall                          uintptr
}
type minMax struct{ Reserved, MaxSize, MaxPosition, MinTrack, MaxTrack point }
type highContrast struct {
	Size, Flags uint32
	Scheme      *uint16
}

var instance uintptr
var bgBrush, bannerBrush uintptr
var light fgPalette
var mainName = wide("K12QuestionBank.Main")
var settingsName = wide("K12QuestionBank.Settings")

type fgPalette struct {
	Text, Muted, Accent, White uintptr
	High                       bool
}

func wide(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	return p
}
func rgb(r, g, b uintptr) uintptr                    { return r | g<<8 | b<<16 }
func proc(p *syscall.LazyProc, a ...uintptr) uintptr { v, _, _ := p.Call(a...); return v }
func send(h uintptr, m uint32, w, l uintptr) uintptr { return proc(sendMessage, h, uintptr(m), w, l) }
func addItem(h uintptr, m uint32, value string) {
	p := wide(value)
	send(h, m, 0, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}
func set(h uintptr, s string) { setText.Call(h, uintptr(unsafe.Pointer(wide(s)))) }
func text(h uintptr) string {
	n := int(proc(getTextLen, h))
	if n < 0 || n > 200000 {
		return ""
	}
	b := make([]uint16, n+1)
	getText.Call(h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b)
}
func mb(h uintptr, s string, flags uintptr) uintptr {
	v, _, _ := messageBox.Call(h, uintptr(unsafe.Pointer(wide(s))), uintptr(unsafe.Pointer(wide("组卷台"))), flags)
	return v
}
func client(h uintptr) rect { var r rect; getClientRect.Call(h, uintptr(unsafe.Pointer(&r))); return r }
func dpi(h uintptr) int {
	if getDPI.Find() == nil {
		if n := int(proc(getDPI, h)); n != 0 {
			return n
		}
	}
	return 96
}
func scale(dp, n int) int   { return (dp*n + 48) / 96 }
func unscale(dp, n int) int { return (n*96 + dp/2) / dp }
func placement(h uintptr, dp, x, y, w, hh int) {
	proc(moveWindow, h, uintptr(scale(dp, x)), uintptr(scale(dp, y)), uintptr(scale(dp, w)), uintptr(scale(dp, hh)), 1)
}
func enable(h uintptr, b bool) {
	if b {
		proc(enableWindow, h, 1)
	} else {
		proc(enableWindow, h, 0)
	}
}
func child(parent uintptr, cls, title string, id int, style, ex uintptr) uintptr {
	h, _, _ := createWindow.Call(ex, uintptr(unsafe.Pointer(wide(cls))), uintptr(unsafe.Pointer(wide(title))), wsChild|wsVisible|style, 0, 0, 0, 0, parent, uintptr(id), instance, 0)
	if h == 0 {
		panic("Win32 控件创建失败: " + cls)
	}
	return h
}
func combo(h uintptr, values []string, sel int) {
	send(h, cbReset, 0, 0)
	for _, v := range values {
		addItem(h, cbAdd, v)
	}
	if len(values) > 0 {
		send(h, cbSel, uintptr(sel), 0)
	}
}
func selection(h uintptr, m uint32) int { return int(int32(send(h, m, 0, 0))) }
func checked(h uintptr) bool            { return send(h, bmGetCheck, 0, 0) == 1 }
func toggle(h uintptr, b bool) {
	var v uintptr
	if b {
		v = 1
	}
	send(h, bmSetCheck, v, 0)
}
func createFont(dp, size, weight int) uintptr {
	h, _, _ := makeFont.Call(uintptr(-scale(dp, size)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wide("Microsoft YaHei UI"))))
	return h
}
func setFont(h, f uintptr) { send(h, wmSetFont, f, 1) }
func hitDPI(h, l uintptr) {
	r := (*rect)(unsafe.Pointer(l))
	proc(user32.NewProc("SetWindowPos"), h, 0, uintptr(r.L), uintptr(r.T), uintptr(r.R-r.L), uintptr(r.B-r.T), 0x0014)
}
func palette() {
	hc := highContrast{Size: uint32(unsafe.Sizeof(highContrast{}))}
	systemParameters.Call(0x42, uintptr(hc.Size), uintptr(unsafe.Pointer(&hc)), 0)
	light = fgPalette{rgb(27, 43, 57), rgb(77, 95, 108), rgb(0, 101, 101), rgb(255, 255, 255), false}
	bg, banner := rgb(243, 247, 249), rgb(8, 67, 68)
	if hc.Flags&1 != 0 {
		light.High = true
		light.Text = proc(sysColor, 8)
		light.Muted = light.Text
		light.Accent = light.Text
		light.White = light.Text
		bg = proc(sysColor, 5)
		banner = bg
	}
	bgBrush = proc(solidBrush, bg)
	bannerBrush = proc(solidBrush, banner)
}
func paintBG(h, dc uintptr, dp int, banner bool) uintptr {
	r := client(h)
	fillRect.Call(dc, uintptr(unsafe.Pointer(&r)), bgBrush)
	if banner {
		r.B = int32(scale(dp, 93))
		fillRect.Call(dc, uintptr(unsafe.Pointer(&r)), bannerBrush)
	}
	return 1
}
func labelColor(dc, color, brush uintptr) uintptr {
	proc(setBkMode, dc, 1)
	proc(setTextColor, dc, color)
	return brush
}
func register(name *uint16, cb uintptr, icons bool) error {
	c := winClass{Style: 3, Proc: cb, Instance: instance, Name: name, Size: uint32(unsafe.Sizeof(winClass{}))}
	c.Cursor = proc(loadCursor, 0, stockApp)
	if icons {
		for _, id := range []uintptr{101, 1, 32512} {
			i := instance
			if id == 32512 {
				i = 0
			}
			c.Icon = proc(loadIcon, i, id)
			if c.Icon != 0 {
				c.IconSmall = proc(loadImage, i, id, 1, 16, 16, 0x8000)
				break
			}
		}
	}
	res, _, err := regClass.Call(uintptr(unsafe.Pointer(&c)))
	if res == 0 {
		return err
	}
	return nil
}
func run() error {
	runtime.LockOSThread()
	if dpiAware.Find() == nil {
		proc(dpiAware, ^uintptr(3))
	} else if p := shcore.NewProc("SetProcessDpiAwareness"); p.Find() == nil {
		proc(p, 2)
	}
	instance = proc(getModule, 0)
	palette()
	defer proc(deleteObject, bgBrush)
	defer proc(deleteObject, bannerBrush)
	if err := register(mainName, syscall.NewCallback(mainProc), true); err != nil {
		return err
	}
	if err := register(settingsName, syscall.NewCallback(settingsProc), false); err != nil {
		return err
	}
	dp := 96
	if dpiSystem.Find() == nil {
		if d := int(proc(dpiSystem)); d > 0 {
			dp = d
		}
	}
	sw, sh := int(proc(user32.NewProc("GetSystemMetrics"), 0)), int(proc(user32.NewProc("GetSystemMetrics"), 1))
	ww, hh := min(scale(dp, 1120), sw-40), min(scale(dp, 790), sh-50)
	if ww <= 0 || hh <= 0 {
		ww, hh = 1080, 740
	}
	style := uintptr(0x00CF0000 | 0x02000000)
	h, _, e := createWindow.Call(wsExControlParent, uintptr(unsafe.Pointer(mainName)), uintptr(unsafe.Pointer(wide("组卷台 · Windows 桌面版"))), style, 0x80000000, 0x80000000, uintptr(ww), uintptr(hh), 0, 0, instance, 0)
	if h == 0 {
		return fmt.Errorf("主窗口创建失败：%w", e)
	}
	app.init(h)
	proc(showWindow, h, 5)
	proc(updateWindow, h)
	proc(setFocus, app.ctl[cSource])
	var m msg
	for {
		r, _, e := getMsg.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) < 0 {
			return e
		}
		if r == 0 {
			return nil
		}
		target := h
		if cfgUI.h != 0 {
			target = cfgUI.h
		}
		if m.M == wmKeyDown && m.W == 27 && cfgUI.h != 0 {
			closeSettings()
			continue
		}
		if m.M == wmKeyDown && m.W == 13 && m.H == app.ctl[cList] && app.hasSelection() {
			app.onCommand(cAnswer, 0)
			continue
		}
		if r, _, _ := dialogMessage.Call(target, uintptr(unsafe.Pointer(&m))); r != 0 {
			continue
		}
		translate.Call(uintptr(unsafe.Pointer(&m)))
		dispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
}
func main() {
	if err := run(); err != nil {
		mb(0, err.Error(), 0x10)
	}
}
