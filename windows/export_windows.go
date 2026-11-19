//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"k12bank/internal/bank"
	"k12bank/internal/paper"
)

type openFileName struct {
	Size                      uint32
	Owner, Instance           uintptr
	Filter, CustomFilter      *uint16
	MaxCustom, FilterIndex    uint32
	File                      *uint16
	MaxFile                   uint32
	FileTitle                 *uint16
	MaxFileTitle              uint32
	InitialDir, Title         *uint16
	Flags                     uint32
	FileOffset, FileExtension uint16
	DefaultExt                *uint16
	Custom, Hook              uintptr
	Template                  *uint16
	Reserved                  uintptr
	ReservedFlags, FlagsEx    uint32
}

func saveWordDialog(h uintptr, name string) string {
	filter := utf16.Encode([]rune("Word 文档 (*.docx)\x00*.docx\x00所有文件 (*.*)\x00*.*\x00\x00"))
	file := make([]uint16, 4096)
	copy(file, syscall.StringToUTF16(name))
	d := openFileName{Owner: h, Filter: &filter[0], FilterIndex: 1, File: &file[0], MaxFile: uint32(len(file)), Title: wide("导出试卷为 Word"), DefaultExt: wide("docx"), Flags: 0x800 | 0x2 | 0x8 | 0x80000}
	d.Size = uint32(unsafe.Sizeof(d))
	r, _, _ := comdlg.NewProc("GetSaveFileNameW").Call(uintptr(unsafe.Pointer(&d)))
	runtime.KeepAlive(&d)
	if r == 0 {
		if e := proc(comdlg.NewProc("CommDlgExtendedError")); e != 0 {
			mb(h, fmt.Sprintf("打开保存对话框失败（0x%X）", e), 0x30)
		}
		return ""
	}
	return syscall.UTF16ToString(file)
}
func copyText(h uintptr, s string) error {
	b := syscall.StringToUTF16(strings.ReplaceAll(paper.CRLF(s), "\x00", ""))
	globalAlloc := kernel32.NewProc("GlobalAlloc")
	globalLock := kernel32.NewProc("GlobalLock")
	globalUnlock := kernel32.NewProc("GlobalUnlock")
	globalFree := kernel32.NewProc("GlobalFree")
	data := proc(globalAlloc, 2, uintptr(len(b)*2))
	if data == 0 {
		return errors.New("无法分配剪贴板数据")
	}
	owned := true
	defer func() {
		if owned {
			proc(globalFree, data)
		}
	}()
	addr := proc(globalLock, data)
	if addr == 0 {
		return errors.New("无法锁定剪贴板数据")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(addr)), len(b)), b)
	proc(globalUnlock, data)
	if proc(user32.NewProc("OpenClipboard"), h) == 0 {
		return errors.New("剪贴板正被其他程序占用；稍后重试")
	}
	defer proc(user32.NewProc("CloseClipboard"))
	if proc(user32.NewProc("EmptyClipboard")) == 0 {
		return errors.New("无法打开剪贴板")
	}
	if proc(user32.NewProc("SetClipboardData"), 13, data) == 0 {
		return errors.New("未能写入剪贴板")
	}
	owned = false
	return nil
}

type printDialog struct {
	Size                                           uint32
	Owner, DevMode, DevNames, DC                   uintptr
	Flags, Flags2, Exclusion, NumRanges, MaxRanges uint32
	Ranges                                         uintptr
	MinPage, MaxPage, Copies                       uint32
	Instance                                       uintptr
	Template                                       *uint16
	Callback                                       uintptr
	NumPropPages                                   uint32
	PropPages                                      uintptr
	StartPage, Action                              uint32
}
type docInfo struct {
	Size                   int32
	Name, Output, Datatype *uint16
	Type                   uint32
}

func printPaper(h uintptr, title string, questions []bank.Question, answers bool) (bool, error) {
	ole := syscall.NewLazyDLL("ole32.dll")
	if r := proc(ole.NewProc("OleInitialize"), 0); int32(r) >= 0 {
		defer proc(ole.NewProc("OleUninitialize"))
	}
	pd := printDialog{Owner: h, Flags: 0x100 | 0x8 | 0x4 | 0x40000, MinPage: 1, MaxPage: 999, Copies: 1, StartPage: 0xffffffff}
	pd.Size = uint32(unsafe.Sizeof(pd))
	v, _, _ := comdlg.NewProc("PrintDlgExW").Call(uintptr(unsafe.Pointer(&pd)))
	for _, mem := range []uintptr{pd.DevMode, pd.DevNames} {
		if mem != 0 {
			defer proc(kernel32.NewProc("GlobalFree"), mem)
		}
	}
	if pd.DC != 0 {
		defer proc(gdi32.NewProc("DeleteDC"), pd.DC)
	}
	if int32(v) < 0 {
		return false, fmt.Errorf("打开打印对话框失败（0x%08X）；检查 Windows 打印机设置", uint32(v))
	}
	if pd.Action != 1 {
		return false, nil
	}
	if pd.DC == 0 {
		return false, errors.New("打印机未返回可用的设备")
	}
	return runPrint(pd.DC, title, questions, answers)
}
func runPrint(dc uintptr, title string, questions []bank.Question, answers bool) (bool, error) {
	caps := gdi32.NewProc("GetDeviceCaps")
	w, hh, dpi := int(proc(caps, dc, 8)), int(proc(caps, dc, 10)), int(proc(caps, dc, 90))
	if w < 100 || hh < 100 || dpi < 30 {
		return false, errors.New("打印机返回了无效的纸张尺寸")
	}
	g := min(min(dpi/2, w/8), hh/8)
	font, _, _ := makeFont.Call(uintptr(-(dpi*11)/72), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(wide("Microsoft YaHei"))))
	if font == 0 {
		return false, errors.New("打印字体创建失败")
	}
	defer proc(deleteObject, font)
	selectObj := gdi32.NewProc("SelectObject")
	startPage := gdi32.NewProc("StartPage")
	endPage := gdi32.NewProc("EndPage")
	drawText := user32.NewProc("DrawTextW")
	d := docInfo{Size: int32(unsafe.Sizeof(docInfo{})), Name: wide(title)}
	v, _, _ := gdi32.NewProc("StartDocW").Call(dc, uintptr(unsafe.Pointer(&d)))
	if int32(v) <= 0 {
		return false, errors.New("打印任务未启动")
	}
	done := false
	defer func() {
		if !done {
			proc(gdi32.NewProc("AbortDoc"), dc)
		}
	}()
	y := g
	begin := func() error {
		y = g
		if int32(proc(startPage, dc)) <= 0 {
			return errors.New("启动新打印页失败")
		}
		proc(selectObj, dc, font)
		return nil
	}
	end := func() error {
		if int32(proc(endPage, dc)) <= 0 {
			return errors.New("写入打印页失败")
		}
		return nil
	}
	if err := begin(); err != nil {
		return false, err
	}
	size := func(line string, measure bool) int {
		bounds := rect{int32(g), int32(y), int32(w - g), int32(hh - g)}
		flags := uintptr(0x10 | 0x800 | 0x40)
		if measure {
			flags |= 0x400
		}
		r, _, _ := drawText.Call(dc, uintptr(unsafe.Pointer(wide(line))), ^uintptr(0), uintptr(unsafe.Pointer(&bounds)), flags)
		return int(int32(r))
	}
	for _, line := range strings.Split(paper.Text(title, questions, answers), "\n") {
		if line == "" {
			y += max(1, dpi/9)
			continue
		}
		if ht := size(line, true); ht <= 0 {
			return false, errors.New("打印文字排版失败")
		} else if ht > hh-2*g {
			if err := end(); err != nil {
				return false, err
			}
			if err := begin(); err != nil {
				return false, err
			}
			rs := []rune(line)
			for len(rs) > 0 {
				lo, hi := 1, len(rs)
				for lo < hi {
					mid := (lo + hi + 1) / 2
					if size(string(rs[:mid]), true) <= hh-2*g {
						lo = mid
					} else {
						hi = mid - 1
					}
				}
				if lo == 1 && size(string(rs[:1]), true) > hh-2*g {
					return false, errors.New("页面空间不足，无法打印")
				}
				y += size(string(rs[:lo]), false)
				rs = rs[lo:]
				if len(rs) > 0 {
					if err := end(); err != nil {
						return false, err
					}
					if err := begin(); err != nil {
						return false, err
					}
				}
			}
		} else {
			if y+ht > hh-g {
				if err := end(); err != nil {
					return false, err
				}
				if err := begin(); err != nil {
					return false, err
				}
			}
			if drawn := size(line, false); drawn > 0 {
				y += drawn
			} else {
				return false, errors.New("绘制打印文字失败")
			}
		}
	}
	if err := end(); err != nil {
		return false, err
	}
	if int32(proc(gdi32.NewProc("EndDoc"), dc)) <= 0 {
		return false, errors.New("未能提交打印任务")
	}
	done = true
	return true, nil
}
