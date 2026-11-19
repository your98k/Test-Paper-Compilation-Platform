//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unsafe"

	"k12bank/internal/ai"
	"k12bank/internal/bank"
	"k12bank/internal/paper"
)

const (
	cSource = 100 + iota
	cSubject
	cGrade
	cTerm
	cTopic
	cDiff
	cCount
	cGenerate
	cSettings
	cPaper
	cWrong
	cList
	cDetail
	cAnswer
	cReplace
	cMark
	cWord
	cPrint
	cCopy
	cExportAnswers
	cStatus
	cTitle
	cSubtitle
	cSection
	cViewTitle
	cHint
)

var app desktop
var subjects = []string{"math", "chinese", "english", "physics", "chemistry"}
var diffLabel = []string{"1 · 容易", "2 · 较易", "3 · 中等", "4 · 较难", "5 · 困难"}
var labelIDs = map[int]int{cSource: 181, cSubject: 182, cGrade: 183, cTerm: 184, cTopic: 185, cDiff: 186, cCount: 187}

type jobEvent struct {
	q           []bank.Question
	err         error
	done, total int
	final       bool
}
type desktop struct {
	h                               uintptr
	dp                              int
	ctl                             map[int]uintptr
	normal, large, bold, small      uintptr
	grades                          []int
	topics                          []bank.Topic
	viewWrong, answer               bool
	generated                       []bank.Question
	saved                           []bank.Question
	shown                           []bank.Question
	lastFilter, pending             bank.Filter
	lastAI, pendingAI, wrongBlocked bool
	cfg                             ai.Config
	remember                        bool
	dir                             string
	events                          chan jobEvent
	cancel                          context.CancelFunc
	replace                         int
}

func (a *desktop) init(h uintptr) {
	a.h = h
	a.dp = dpi(h)
	a.ctl = map[int]uintptr{}
	a.events = make(chan jobEvent, 48)
	t := func(id int, value string) { a.ctl[id] = child(h, "STATIC", value, id, 0x0080, 0) }
	b := func(id int, value string) { a.ctl[id] = child(h, "BUTTON", value, id, wsTab, 0) }
	t(cTitle, "组卷台")
	t(cSubtitle, "K12 练习工作台   /   Windows 桌面版")
	t(cSection, "01  组卷条件")
	t(cViewTitle, "02  试卷工作区")
	for _, v := range []struct {
		id    int
		title string
	}{{cSource, "出题方式"}, {cSubject, "科目"}, {cGrade, "年级"}, {cTerm, "学期"}, {cTopic, "知识点"}, {cDiff, "难度"}, {cCount, "题量"}} {
		t(labelIDs[v.id], v.title)
		a.ctl[v.id] = child(h, "COMBOBOX", "", v.id, wsTab|wsVScroll|3|0x200, 0)
	}
	b(cGenerate, "生成试卷")
	b(cSettings, "API 设置…")
	t(cHint, "离线出题不联网。")
	b(cPaper, "当前试卷")
	b(cWrong, "错题本")
	a.ctl[cList] = child(h, "LISTBOX", "", cList, wsTab|wsVScroll|0x101, wsExEdge)
	a.ctl[cDetail] = child(h, "EDIT", "", cDetail, wsTab|wsVScroll|0x0804|0x40, wsExEdge)
	b(cAnswer, "看答案")
	b(cReplace, "换此题")
	b(cMark, "记为错题")
	b(cWord, "Word…")
	b(cPrint, "打印 / PDF…")
	b(cCopy, "复制试卷")
	a.ctl[cExportAnswers] = child(h, "BUTTON", "含答案", cExportAnswers, wsTab|3, 0)
	t(cStatus, "选择条件后生成试卷。")
	a.setFonts()
	combo(a.ctl[cSource], []string{"本地离线题库", "AI 原创新题"}, 0)
	combo(a.ctl[cSubject], []string{"数学", "语文", "英语", "物理", "化学"}, 0)
	combo(a.ctl[cTerm], []string{"上学期", "下学期", "全学年"}, 0)
	combo(a.ctl[cDiff], diffLabel, 2)
	counts := make([]string, 40)
	for i := range counts {
		counts[i] = strconv.Itoa(i + 1)
	}
	combo(a.ctl[cCount], counts, 9)
	a.fillGrades(7)
	a.refreshTopics()
	a.dir = dataDirectory()
	if a.dir == "" {
		a.status("未找到本地数据目录，设置和错题将无法保存。")
	} else {
		if err := a.loadConfig(); err != nil {
			a.status("API 设置未能读取，打开设置可重新填写。")
		}
		if v, err := paper.LoadWrong(a.dir); err == nil {
			a.saved = v
		} else {
			a.status("错题本文件未能读取；原文件保留。")
			a.dirWrongReadFailed()
		}
	}
	a.render(-1)
	a.layout()
}

func (a *desktop) setFonts() {
	n, l, b, s := createFont(a.dp, 15, 400), createFont(a.dp, 31, 700), createFont(a.dp, 17, 700), createFont(a.dp, 13, 400)
	for id, h := range a.ctl {
		f := n
		switch id {
		case cTitle:
			f = l
		case cSection, cViewTitle:
			f = b
		case cSubtitle, cHint, cStatus:
			f = s
		}
		setFont(h, f)
	}
	for _, f := range []uintptr{a.normal, a.large, a.bold, a.small} {
		if f != 0 {
			proc(deleteObject, f)
		}
	}
	a.normal, a.large, a.bold, a.small = n, l, b, s
}
func (a *desktop) layout() {
	r := client(a.h)
	dp := a.dp
	w, h := unscale(dp, int(r.R)), unscale(dp, int(r.B))
	p := func(id, x, y, ww, hh int) { placement(a.ctl[id], dp, x, y, max(0, ww), max(0, hh)) }
	p(cTitle, 24, 13, 440, 42)
	p(cSubtitle, 26, 58, 530, 22)
	left, rx := 234, 290
	rw := max(0, w-rx-24)
	p(cSection, 24, 113, left, 24)
	p(cViewTitle, rx, 113, rw, 24)
	for i, id := range []int{cSource, cSubject, cGrade, cTerm, cTopic} {
		y := 149 + i*49
		p(labelIDs[id], 24, y, left, 18)
		p(id, 24, y+19, left, 216)
	}
	p(labelIDs[cDiff], 24, 394, 116, 18)
	p(cDiff, 24, 413, 120, 156)
	p(labelIDs[cCount], 158, 394, 100, 18)
	p(cCount, 158, 413, 100, 212)
	p(cGenerate, 24, 458, 234, 38)
	p(cSettings, 24, 506, 234, 33)
	p(cHint, 24, 553, 236, 38)
	p(cPaper, rx, 148, 105, 30)
	p(cWrong, rx+115, 148, 108, 30)
	listHeight := max(94, min(185, (h-198)*3/10))
	p(cList, rx, 191, rw, listHeight)
	y := 191 + listHeight + 8
	p(cAnswer, rx, y, 80, 29)
	p(cReplace, rx+90, y, 80, 29)
	p(cMark, rx+180, y, 104, 29)
	p(cDetail, rx, y+38, rw, h-y-122)
	p(cWord, rx, h-72, 83, 32)
	p(cPrint, rx+93, h-72, 110, 32)
	p(cCopy, rx+213, h-72, 100, 32)
	p(cExportAnswers, rx+325, h-70, 83, 27)
	p(cStatus, 24, h-30, w-48, 21)
}
func (a *desktop) status(s string) { set(a.ctl[cStatus], s) }
func (a *desktop) sel(id int) int  { return selection(a.ctl[id], cbCur) }
func (a *desktop) sourceAI() bool  { return a.sel(cSource) == 1 }
func (a *desktop) getSubject() string {
	s := a.sel(cSubject)
	if s < 0 || s >= len(subjects) {
		return subjects[0]
	}
	return subjects[s]
}
func stage(g int) string {
	if g <= 6 {
		return "primary"
	}
	if g <= 9 {
		return "junior"
	}
	return "senior"
}
func (a *desktop) getGrade() int {
	n := a.sel(cGrade)
	if n < 0 || n >= len(a.grades) {
		return 7
	}
	return a.grades[n]
}
func (a *desktop) getTerm() int {
	switch a.sel(cTerm) {
	case 0:
		return 1
	case 1:
		return 2
	}
	return 0
}
func (a *desktop) fillGrades(want int) {
	low := 1
	switch a.getSubject() {
	case "physics":
		low = 8
	case "chemistry":
		low = 9
	}
	var labels []string
	a.grades = nil
	index := 0
	for g := low; g <= 12; g++ {
		if g == want {
			index = g - low
		}
		a.grades = append(a.grades, g)
		labels = append(labels, bank.GradeLabel(g))
	}
	combo(a.ctl[cGrade], labels, index)
}
func (a *desktop) refreshTopics() {
	g := a.getGrade()
	all := bank.Curriculum(a.getSubject(), stage(g), g, a.getTerm())
	a.topics = nil
	labels := []string{"本册知识点（自动选取）"}
	if a.getTerm() == 0 {
		labels[0] = "全年知识点（自动选取）"
	}
	for _, t := range all {
		if a.sourceAI() || t.Offline {
			a.topics = append(a.topics, t)
			labels = append(labels, t.Name)
		}
	}
	combo(a.ctl[cTopic], labels, 0)
	if a.sourceAI() {
		set(a.ctl[cHint], "AI 请求直连你的 API；\r\n服务商可按量计费。")
	} else if len(a.topics) == 0 {
		set(a.ctl[cHint], "本册暂无离线题，可切换 AI。")
	} else {
		set(a.ctl[cHint], "离线题来自原有组卷台题库。")
	}
}
func (a *desktop) filter() bank.Filter {
	g := a.getGrade()
	f := bank.Filter{Subject: a.getSubject(), Stage: stage(g), Grade: g, Term: a.getTerm(), Difficulty: a.sel(cDiff) + 1, Count: a.sel(cCount) + 1}
	if i := a.sel(cTopic) - 1; i >= 0 && i < len(a.topics) {
		f.TopicID = a.topics[i].ID
		f.TopicName = a.topics[i].Name
	}
	return f
}

func (a *desktop) dirWrongReadFailed() { a.wrongBlocked = true }
func (a *desktop) hasSelection() bool {
	i := selection(a.ctl[cList], lbCur)
	return i >= 0 && i < len(a.shown)
}
func (a *desktop) refreshActions() {
	busy := a.cancel != nil
	sel := a.hasSelection()
	for _, id := range []int{cSource, cGrade, cSubject, cTerm, cTopic, cDiff, cCount, cSettings, cPaper, cWrong} {
		enable(a.ctl[id], !busy)
	}
	enable(a.ctl[cAnswer], sel)
	enable(a.ctl[cReplace], sel && !busy && !a.viewWrong)
	enable(a.ctl[cMark], sel && !busy && a.dir != "" && !a.wrongBlocked)
	for _, id := range []int{cWord, cPrint, cCopy, cExportAnswers} {
		enable(a.ctl[id], len(a.shown) > 0)
	}
	if a.answer {
		set(a.ctl[cAnswer], "隐藏答案")
	} else {
		set(a.ctl[cAnswer], "看答案")
	}
	if a.viewWrong {
		set(a.ctl[cMark], "已掌握")
	} else {
		set(a.ctl[cMark], "记为错题")
	}
	if busy {
		set(a.ctl[cGenerate], "取消出题")
	} else {
		set(a.ctl[cGenerate], "生成试卷")
		enable(a.ctl[cGenerate], true)
	}
}
func (a *desktop) render(i int) {
	a.answer = false
	a.shown = a.generated
	title := "02  试卷工作区"
	if a.viewWrong {
		a.shown = a.saved
		title = fmt.Sprintf("02  错题本  ·  %d 题", len(a.saved))
	} else if len(a.shown) > 0 {
		title = fmt.Sprintf("02  当前试卷  ·  %d 题", len(a.shown))
	}
	set(a.ctl[cViewTitle], title)
	set(a.ctl[cWrong], fmt.Sprintf("错题本  %d", len(a.saved)))
	h := a.ctl[cList]
	send(h, lbReset, 0, 0)
	for j, q := range a.shown {
		one := strings.Join(strings.Fields(q.Stem), " ")
		r := []rune(one)
		if len(r) > 58 {
			one = string(r[:58]) + "…"
		}
		addItem(h, lbAdd, fmt.Sprintf("%02d   %s  |  %s", j+1, bank.TypeLabel(q.Type), one))
	}
	if len(a.shown) > 0 {
		send(h, lbSel, uintptr(min(max(i, 0), len(a.shown)-1)), 0)
	}
	a.showDetail()
}
func (a *desktop) showDetail() {
	s := "当前没有试卷。\n\n在左侧选好科目、年级和难度，点击「生成试卷」。\nAI 出题前，在「API 设置」填入你的接口。"
	if a.viewWrong {
		s = "错题本是空的。\n\n在试卷中选中做错的题，点击「记为错题」。"
	}
	i := selection(a.ctl[cList], lbCur)
	if i >= 0 && i < len(a.shown) {
		q := a.shown[i]
		s = fmt.Sprintf("第 %d 题   %s · %s · 难度 %d\n%s\n\n%s", i+1, bank.SubjectLabel(q.Subject), bank.GradeLabel(q.Grade), q.Difficulty, q.TopicName, paper.QuestionBody(q))
		if a.answer {
			s += "\n\n──── 答案与解析 ────\n" + paper.AnswerBody(q)
		}
	}
	h := a.ctl[cDetail]
	set(h, paper.CRLF(s))
	send(h, 0xb1, 0, 0)
	send(h, 0xb7, 0, 0)
	a.refreshActions()
}
func (a *desktop) start(idx int) {
	f, useAI := a.filter(), a.sourceAI()
	var exclude []bank.Question
	if idx >= 0 {
		f, useAI = a.lastFilter, a.lastAI
		f.Count = 1
		exclude = append(exclude, a.generated...)
	}
	if err := f.Validate(); err != nil {
		a.status(err.Error())
		return
	}
	if useAI && (a.cfg.BaseURL == "" || a.cfg.Model == "" || a.cfg.APIKey == "") {
		a.status("请先填写 API 地址、模型和 Key，再启动 AI 出题。")
		a.openSettings()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.replace = idx
	a.refreshActions()
	if useAI {
		a.status(fmt.Sprintf("正在直连 AI 生成题目 · 0 / %d", f.Count))
	} else {
		a.status("正在从本地题库选题…")
	}
	notify := a.events
	h, c := a.h, a.cfg
	go func() {
		sendEvent := func(v jobEvent) { notify <- v; proc(postMessage, h, wmApp, 0, 0) }
		var q []bank.Question
		var err error
		if useAI {
			q, err = ai.Generate(ctx, c, f, exclude, func(done, total int) { sendEvent(jobEvent{done: done, total: total}) })
		} else {
			q, err = bank.Generate(f, exclude)
		}
		if ctx.Err() != nil {
			err = ctx.Err()
			q = nil
		}
		sendEvent(jobEvent{q: q, err: err, final: true})
	}()
	if idx < 0 {
		a.pending = f
		a.pendingAI = useAI
	}
}
func (a *desktop) receive() {
	for {
		select {
		case v := <-a.events:
			if !v.final {
				a.status(fmt.Sprintf("正在直连 AI 生成题目 · %d / %d", v.done, v.total))
				continue
			}
			a.cancel()
			a.cancel = nil
			a.refreshActions()
			if v.err != nil {
				if errors.Is(v.err, context.Canceled) {
					a.status("已取消；已发送的 AI 请求仍可能计费。")
				} else {
					a.status(v.err.Error())
					mb(a.h, v.err.Error(), 0x30)
				}
				continue
			}
			if len(v.q) == 0 {
				a.status("没有生成可用的题目。")
				continue
			}
			i := a.replace
			if i >= 0 {
				a.generated[i] = v.q[0]
				a.status(fmt.Sprintf("已更换第 %d 题。", i+1))
			} else {
				a.generated = v.q
				a.lastFilter = a.pending
				a.lastAI = a.pendingAI
				desc := fmt.Sprintf("已生成 %d 道%s。", len(v.q), map[bool]string{true: " AI 原创题", false: "离线练习题"}[a.lastAI])
				if a.lastAI {
					desc += " 使用前核对答案。"
				}
				a.status(desc)
			}
			a.viewWrong = false
			a.render(i)
		default:
			return
		}
	}
}
func (a *desktop) title() string {
	if a.viewWrong {
		return "组卷台 · 错题练习"
	}
	return fmt.Sprintf("%s%s · %s练习卷", bank.GradeLabel(a.lastFilter.Grade), bank.TermLabel(a.lastFilter.Term), bank.SubjectLabel(a.lastFilter.Subject))
}
func (a *desktop) onCommand(id, notify int) {
	if id == cList {
		if notify == 1 {
			a.answer = false
			a.showDetail()
		} else if notify == 2 && a.hasSelection() {
			a.answer = !a.answer
			a.showDetail()
		}
		return
	}
	if notify == 1 && (id == cSource || id == cSubject || id == cGrade || id == cTerm) {
		if id == cSubject {
			a.fillGrades(a.getGrade())
		}
		a.refreshTopics()
		return
	}
	if notify != 0 {
		return
	}
	if a.cancel != nil && id != cGenerate && id != cAnswer && id != cCopy && id != cWord && id != cPrint {
		return
	}
	switch id {
	case cGenerate:
		if a.cancel != nil {
			a.cancel()
			a.status("正在取消…")
			enable(a.ctl[cGenerate], false)
		} else {
			a.start(-1)
		}
	case cSettings:
		a.openSettings()
	case cPaper, cWrong:
		a.viewWrong = id == cWrong
		a.render(-1)
	case cAnswer:
		if a.hasSelection() {
			a.answer = !a.answer
			a.showDetail()
		}
	case cReplace:
		if a.hasSelection() && !a.viewWrong {
			a.start(selection(a.ctl[cList], lbCur))
		}
	case cMark:
		a.mark()
	case cWord, cCopy, cPrint:
		if len(a.shown) == 0 {
			return
		}
		answers := checked(a.ctl[cExportAnswers])
		title := a.title()
		var err error
		switch id {
		case cWord:
			name := saveWordDialog(a.h, title+".docx")
			if name == "" {
				return
			}
			err = paper.SaveWord(name, title, a.shown, answers)
			if err == nil {
				a.status("已导出 Word：" + name)
			}
		case cCopy:
			err = copyText(a.h, paper.Text(title, a.shown, answers))
			if err == nil {
				a.status("试卷已复制到 Windows 剪贴板。")
			}
		case cPrint:
			var printed bool
			printed, err = printPaper(a.h, title, a.shown, answers)
			if err == nil && printed {
				a.status("打印任务已发送。")
			}
		}
		if err != nil {
			mb(a.h, err.Error(), 0x30)
		}
	}
}
func (a *desktop) mark() {
	if !a.hasSelection() || a.dir == "" || a.wrongBlocked {
		return
	}
	i := selection(a.ctl[cList], lbCur)
	q := a.shown[i]
	items := append([]bank.Question(nil), a.saved...)
	if a.viewWrong {
		items = append(items[:i], items[i+1:]...)
	} else {
		key := bank.Fingerprint(q)
		for _, old := range items {
			if bank.Fingerprint(old) == key {
				a.status("这道题已在错题本内。")
				return
			}
		}
		items = append([]bank.Question{q}, items...)
		if len(items) > paper.MaxWrong {
			a.status("错题本已满，请先把已掌握的题移出。")
			return
		}
	}
	if err := paper.SaveWrong(a.dir, items); err != nil {
		mb(a.h, "写入错题本失败："+err.Error(), 0x30)
		return
	}
	a.saved = items
	set(a.ctl[cWrong], fmt.Sprintf("错题本  %d", len(a.saved)))
	if a.viewWrong {
		a.render(i)
		a.status("已标为掌握，并移出错题本。")
	} else {
		a.status("已加入错题本。")
	}
}
func mainProc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmSize:
		if h == app.h && w != 1 {
			app.layout()
		}
		return 0
	case wmGetMinMax:
		if l != 0 {
			dp := dpi(h)
			mm := (*minMax)(unsafe.Pointer(l))
			mm.MinTrack = point{int32(scale(dp, 744)), int32(scale(dp, 653))}
		}
		return 0
	case wmDPIChanged:
		if h == app.h {
			app.dp = int(w & 0xffff)
			app.setFonts()
			hitDPI(h, l)
			app.layout()
			proc(invalidate, h, 0, 1)
		}
		return 0
	case wmErase:
		if h == app.h {
			return paintBG(h, w, app.dp, true)
		}
	case wmCtlStatic:
		if h == app.h {
			switch l {
			case app.ctl[cTitle], app.ctl[cSubtitle]:
				return labelColor(w, light.White, bannerBrush)
			case app.ctl[cDetail]:
				return proc(defWindow, h, uintptr(m), w, l)
			case app.ctl[cSection], app.ctl[cViewTitle]:
				return labelColor(w, light.Accent, bgBrush)
			case app.ctl[cHint], app.ctl[cStatus]:
				return labelColor(w, light.Muted, bgBrush)
			}
			return labelColor(w, light.Text, bgBrush)
		}
	case wmCommand:
		if h == app.h {
			app.onCommand(int(w&0xffff), int((w>>16)&0xffff))
		}
		return 0
	case wmApp:
		if h == app.h {
			app.receive()
		}
		return 0
	case wmDestroy:
		if app.cancel != nil {
			app.cancel()
		}
		for _, f := range []uintptr{app.normal, app.large, app.bold, app.small} {
			if f != 0 {
				proc(deleteObject, f)
			}
		}
		proc(postQuit, 0)
		return 0
	}
	return proc(defWindow, h, uintptr(m), w, l)
}
