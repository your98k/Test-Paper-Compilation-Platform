package bank

import (
	"errors"
	"strings"
	"unicode"
)

type Filter struct {
	Subject, Stage, TopicID, TopicName string
	Grade, Term, Difficulty, Count     int
}

type Question struct {
	ID         string   `json:"id"`
	Subject    string   `json:"subject"`
	Stage      string   `json:"stage"`
	Grade      int      `json:"grade"`
	Term       int      `json:"term"`
	TopicID    string   `json:"kp"`
	TopicName  string   `json:"kpName"`
	Type       string   `json:"type"`
	Difficulty int      `json:"diff"`
	Stem       string   `json:"stem"`
	Options    []string `json:"opts"`
	Answer     string   `json:"answer"`
	Analysis   string   `json:"analysis"`
}

func (f Filter) Validate() error {
	switch f.Subject {
	case "math", "english", "chinese", "physics", "chemistry":
	default:
		return errors.New("请选择有效的科目")
	}
	switch f.Stage {
	case "primary":
		if f.Grade < 1 || f.Grade > 6 || f.Subject == "physics" || f.Subject == "chemistry" {
			return errors.New("科目和年级不匹配")
		}
	case "junior":
		if f.Grade < 7 || f.Grade > 9 {
			return errors.New("科目和年级不匹配")
		}
	case "senior":
		if f.Grade < 10 || f.Grade > 12 {
			return errors.New("科目和年级不匹配")
		}
	default:
		return errors.New("请选择有效的学段")
	}
	if f.Term < 0 || f.Term > 2 {
		return errors.New("请选择有效的学期")
	}
	if f.Difficulty < 1 || f.Difficulty > 5 {
		return errors.New("请选择有效的难度")
	}
	if f.Count < 1 || f.Count > 40 {
		return errors.New("每卷题量为 1–40 道")
	}
	return nil
}

func Fingerprint(q Question) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, q.Stem+"\x1f"+q.Answer+"\x1f"+strings.Join(q.Options, "\x1e"))
}

func SubjectLabel(s string) string {
	switch s {
	case "math":
		return "数学"
	case "english":
		return "英语"
	case "chinese":
		return "语文"
	case "physics":
		return "物理"
	case "chemistry":
		return "化学"
	}
	return s
}

func GradeLabel(g int) string {
	labels := [...]string{"", "一年级", "二年级", "三年级", "四年级", "五年级", "六年级", "七年级", "八年级", "九年级", "高一", "高二", "高三"}
	if g < 1 || g >= len(labels) {
		return ""
	}
	return labels[g]
}

func TermLabel(t int) string {
	switch t {
	case 1:
		return "上学期"
	case 2:
		return "下学期"
	}
	return "全学年"
}

func TypeLabel(t string) string {
	switch t {
	case "oral":
		return "口算"
	case "vertical":
		return "竖式"
	case "estimate":
		return "估算"
	case "error":
		return "辨析"
	case "choice":
		return "单选"
	case "multi":
		return "多选"
	case "judge":
		return "判断"
	case "fill":
		return "填空"
	case "calc":
		return "计算"
	case "solve":
		return "解答"
	case "app":
		return "应用"
	case "proof":
		return "证明"
	case "experiment":
		return "实验"
	case "short":
		return "简答"
	case "cloze":
		return "完形"
	case "reading":
		return "阅读"
	case "translation":
		return "翻译"
	case "writing":
		return "写作"
	}
	return t
}
