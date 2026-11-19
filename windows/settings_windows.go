//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"k12bank/internal/ai"
	"k12bank/internal/paper"
)

type dataBlob struct {
	Size uint32
	Ptr  *byte
}

func dpapi(in []byte, protect bool) ([]byte, error) {
	if len(in) == 0 {
		return nil, nil
	}
	input := dataBlob{uint32(len(in)), &in[0]}
	var output dataBlob
	p := crypt32.NewProc("CryptUnprotectData")
	if protect {
		p = crypt32.NewProc("CryptProtectData")
	}
	r, _, e := p.Call(uintptr(unsafe.Pointer(&input)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&output)))
	runtime.KeepAlive(in)
	if r == 0 {
		return nil, e
	}
	if output.Ptr == nil {
		return nil, errors.New("Windows 凭据加密返回空数据")
	}
	defer kernel32.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(output.Ptr)))
	return append([]byte(nil), unsafe.Slice(output.Ptr, int(output.Size))...), nil
}
func dataDirectory() string {
	d, err := os.UserCacheDir()
	if err != nil || d == "" {
		return ""
	}
	return filepath.Join(d, "K12QuestionBank", "Data")
}

type configFile struct {
	BaseURL      string `json:"baseURL"`
	Model        string `json:"model"`
	ProtectedKey []byte `json:"protectedKey,omitempty"`
}

func (a *desktop) loadConfig() error {
	name := filepath.Join(a.dir, "api.json")
	b, err := paper.ReadSmall(name, 32<<10)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var c configFile
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}
	a.cfg = ai.Config{BaseURL: c.BaseURL, Model: c.Model}
	k, err := dpapi(c.ProtectedKey, false)
	if err != nil {
		return err
	}
	a.cfg.APIKey = string(k)
	clear(k)
	a.remember = len(c.ProtectedKey) > 0
	return nil
}
func (a *desktop) saveConfig(c ai.Config, remember bool) error {
	if a.dir == "" {
		a.cfg = c
		a.remember = false
		return nil
	}
	d := configFile{BaseURL: c.BaseURL, Model: c.Model}
	if remember && c.APIKey != "" {
		k := []byte(c.APIKey)
		encrypted, err := dpapi(k, true)
		clear(k)
		if err != nil {
			return err
		}
		d.ProtectedKey = encrypted
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if err = paper.AtomicWrite(filepath.Join(a.dir, "api.json"), b); err != nil {
		return err
	}
	a.cfg = c
	a.remember = len(d.ProtectedKey) > 0
	return nil
}

const (
	sIntro = 210 + iota
	sURLTitle
	sURL
	sModelTitle
	sModel
	sKeyTitle
	sKey
	sKeep
	sCheck
	sSave
	sCancel
	sStatus
)

var cfgUI settingsDialog

type settingsDialog struct {
	h        uintptr
	dp       int
	controls map[int]uintptr
	f, s     uintptr
	cancel   context.CancelFunc
	results  chan error
}

func (a *desktop) openSettings() {
	if cfgUI.h != 0 {
		proc(foreground, cfgUI.h)
		return
	}
	var owner rect
	getWindowRect.Call(a.h, uintptr(unsafe.Pointer(&owner)))
	ww, hh := scale(a.dp, 582), scale(a.dp, 447)
	x := int(owner.L) + (int(owner.R-owner.L)-ww)/2
	y := int(owner.T) + (int(owner.B-owner.T)-hh)/2
	h, _, e := createWindow.Call(0x1|wsExControlParent, uintptr(unsafe.Pointer(settingsName)), uintptr(unsafe.Pointer(wide("AI 接口设置"))), 0x00C80000|0x02000000, uintptr(x), uintptr(y), uintptr(ww), uintptr(hh), a.h, 0, instance, 0)
	if h == 0 {
		mb(a.h, "API 设置窗口创建失败："+e.Error(), 0x30)
		return
	}
	d := &cfgUI
	*d = settingsDialog{h: h, dp: dpi(h), controls: map[int]uintptr{}, results: make(chan error, 1)}
	lab := func(id int, label string) { d.controls[id] = child(h, "STATIC", label, id, 0x0080, 0) }
	edit := func(id int, value string, extra, limit uintptr) {
		d.controls[id] = child(h, "EDIT", value, id, wsTab|0x80|extra, wsExEdge)
		send(d.controls[id], 0xc5, limit, 0)
	}
	btn := func(id int, label string, extra uintptr) {
		d.controls[id] = child(h, "BUTTON", label, id, wsTab|extra, 0)
	}
	lab(sIntro, "OpenAI 兼容接口 · 请求由 Windows 直接发出")
	lab(sURLTitle, "API 地址（如 https://api.openai.com/v1）")
	edit(sURL, a.cfg.BaseURL, 0, 2048)
	lab(sModelTitle, "模型 ID")
	edit(sModel, a.cfg.Model, 0, 256)
	lab(sKeyTitle, "API Key")
	edit(sKey, a.cfg.APIKey, 0x20, 4096)
	btn(sKeep, "在此 Windows 帐户加密记住 Key", 3)
	toggle(d.controls[sKeep], a.remember)
	enable(d.controls[sKeep], a.dir != "")
	btn(sCheck, "测试连接", 0)
	btn(sSave, "保存", 0)
	btn(sCancel, "关闭", 0)
	lab(sStatus, "测试会发出一次模型请求，服务商可按量计费。")
	d.fonts()
	d.layout()
	enable(a.h, false)
	proc(showWindow, h, 5)
	proc(updateWindow, h)
	proc(setFocus, d.controls[sURL])
	send(d.controls[sURL], 0xb1, 0, ^uintptr(0))
}
func (d *settingsDialog) fonts() {
	f, s := createFont(d.dp, 15, 400), createFont(d.dp, 13, 400)
	for id, h := range d.controls {
		if id == sStatus {
			setFont(h, s)
		} else {
			setFont(h, f)
		}
	}
	if d.f != 0 {
		proc(deleteObject, d.f)
	}
	if d.s != 0 {
		proc(deleteObject, d.s)
	}
	d.f, d.s = f, s
}
func (d *settingsDialog) layout() {
	r := client(d.h)
	w := unscale(d.dp, int(r.R))
	h := unscale(d.dp, int(r.B))
	p := func(id, x, y, ww, hh int) { placement(d.controls[id], d.dp, x, y, ww, hh) }
	p(sIntro, 22, 22, w-44, 25)
	for i, v := range []int{sURLTitle, sModelTitle, sKeyTitle} {
		y := 62 + i*63
		p(v, 22, y, w-44, 20)
		p(v+1, 22, y+23, w-44, 29)
	}
	p(sKeep, 22, 253, w-44, 26)
	p(sStatus, 22, 291, w-44, 45)
	p(sCheck, 22, h-59, 113, 33)
	p(sSave, w-206, h-59, 85, 33)
	p(sCancel, w-108, h-59, 85, 33)
}
func (d *settingsDialog) form() ai.Config {
	return ai.Config{BaseURL: strings.TrimSpace(text(d.controls[sURL])), Model: strings.TrimSpace(text(d.controls[sModel])), APIKey: strings.TrimSpace(text(d.controls[sKey]))}
}
func (d *settingsDialog) setBusy(b bool) {
	for _, id := range []int{sURL, sModel, sKey, sCheck, sSave} {
		enable(d.controls[id], !b)
	}
	enable(d.controls[sKeep], !b && app.dir != "")
}
func closeSettings() {
	if cfgUI.h != 0 {
		enable(app.h, true)
		proc(destroyWindow, cfgUI.h)
		proc(foreground, app.h)
		proc(setFocus, app.ctl[cSettings])
	}
}
func settingsProc(h uintptr, m uint32, w, l uintptr) uintptr {
	d := &cfgUI
	if h != d.h {
		return proc(defWindow, h, uintptr(m), w, l)
	}
	switch m {
	case wmErase:
		return paintBG(h, w, d.dp, false)
	case wmCtlStatic:
		return labelColor(w, light.Text, bgBrush)
	case wmSize:
		d.layout()
		return 0
	case wmDPIChanged:
		d.dp = int(w & 0xffff)
		d.fonts()
		hitDPI(h, l)
		d.layout()
		return 0
	case wmClose:
		closeSettings()
		return 0
	case wmDestroy:
		if d.cancel != nil {
			d.cancel()
		}
		if d.f != 0 {
			proc(deleteObject, d.f)
		}
		if d.s != 0 {
			proc(deleteObject, d.s)
		}
		*d = settingsDialog{}
		return 0
	case wmCommand:
		if (w>>16)&0xffff != 0 {
			return 0
		}
		id := int(w & 0xffff)
		switch id {
		case sCancel:
			closeSettings()
		case sSave, sCheck:
			if d.cancel != nil {
				return 0
			}
			c := d.form()
			if id == sCheck || c != (ai.Config{}) {
				if err := c.Validate(); err != nil {
					set(d.controls[sStatus], err.Error())
					return 0
				}
			}
			if id == sSave {
				if err := app.saveConfig(c, checked(d.controls[sKeep])); err != nil {
					set(d.controls[sStatus], "保存失败："+err.Error())
					return 0
				}
				closeSettings()
				if app.dir == "" {
					app.status("API 设置已用于本次运行；本地数据目录不可用。")
				} else if c == (ai.Config{}) {
					app.status("API 设置已清空。")
				} else {
					app.status("API 设置已保存，可开始 AI 出题。")
				}
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
				d.cancel = cancel
				ch := d.results
				d.setBusy(true)
				set(d.controls[sStatus], "正在请求模型…")
				go func() { ch <- ai.Test(ctx, c); proc(postMessage, h, wmApp, 0, 0) }()
			}
		}
		return 0
	case wmApp:
		select {
		case err := <-d.results:
			d.cancel()
			d.cancel = nil
			d.setBusy(false)
			if err != nil {
				set(d.controls[sStatus], "测试失败："+err.Error())
			} else {
				set(d.controls[sStatus], "连接正常，模型已响应。点「保存」后使用。")
			}
		default:
		}
		return 0
	}
	return proc(defWindow, h, uintptr(m), w, l)
}

// MoveFileExW is also used by the data package for same-volume replacement.
var _ syscall.Errno
