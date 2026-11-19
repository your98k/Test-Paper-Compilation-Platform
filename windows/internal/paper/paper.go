package paper

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"k12bank/internal/bank"
)

const MaxWrong = 500
const wrongLimit = 16 << 20
const wrongName = "wrongbook.json"

type wrongbook struct {
	Version   int             `json:"version"`
	Questions []bank.Question `json:"questions"`
}

func ReadSmall(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = errors.New("文件超出大小限制")
	}
	return b, err
}

func AtomicWrite(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".k12-save-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func LoadWrong(dir string) ([]bank.Question, error) {
	b, err := ReadSmall(filepath.Join(dir, wrongName), wrongLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d wrongbook
	if err = json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	if d.Version != 1 || len(d.Questions) > MaxWrong {
		return nil, errors.New("未知的错题本格式")
	}
	for _, q := range d.Questions {
		if strings.TrimSpace(q.Stem) == "" || strings.TrimSpace(q.Answer) == "" {
			return nil, errors.New("错题本中含不完整的题目")
		}
	}
	return d.Questions, nil
}
func SaveWrong(dir string, questions []bank.Question) error {
	if len(questions) > MaxWrong {
		return errors.New("错题本已满")
	}
	b, err := json.MarshalIndent(wrongbook{1, questions}, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > wrongLimit {
		return errors.New("错题本超过文件大小限制")
	}
	return AtomicWrite(filepath.Join(dir, wrongName), b)
}

var frac = regexp.MustCompile(`\\(?:d?frac)\{([^{}]{1,100})\}\{([^{}]{1,100})\}`)
var root = regexp.MustCompile(`\\sqrt\{([^{}]{1,100})\}`)
var known = regexp.MustCompile(`\\(?:times|div|cdot|pm|leq?|geq?|neq|pi|theta|alpha|beta|sin|cos|tan|rightarrow|Rightarrow|infty|approx|circ|left|right)\b\s?`)
var commands = map[string]string{`\times`: "×", `\div`: "÷", `\cdot`: "·", `\pm`: "±", `\le`: "≤", `\leq`: "≤", `\ge`: "≥", `\geq`: "≥", `\neq`: "≠", `\pi`: "π", `\theta`: "θ", `\alpha`: "α", `\beta`: "β", `\sin`: "sin ", `\cos`: "cos ", `\tan`: "tan ", `\rightarrow`: "→", `\Rightarrow`: "⇒", `\infty`: "∞", `\approx`: "≈", `\circ`: "°", `\left`: "", `\right`: ""}
var delims = strings.NewReplacer(`\(`, "", `\)`, "", `\[`, ``, `\]`, ``)
var inlineDollar = regexp.MustCompile(`\$([^$\n]{1,200})\$`)

func Readable(s string) string {
	s = delims.Replace(s)
	s = inlineDollar.ReplaceAllString(s, "$1")
	for i := 0; i < 5; i++ {
		old := s
		s = frac.ReplaceAllString(s, "($1)/($2)")
		s = root.ReplaceAllString(s, "√($1)")
		if old == s {
			break
		}
	}
	return known.ReplaceAllStringFunc(s, func(s string) string { return commands[strings.TrimSpace(s)] })
}
func CRLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"), "\n", "\r\n")
}
func QuestionBody(q bank.Question) string {
	var b strings.Builder
	b.WriteString(Readable(q.Stem))
	for i, option := range q.Options {
		option = strings.TrimSpace(option)
		prefix := string(rune('A' + i))
		for _, sep := range []string{".", "．", "、", ")", "）"} {
			if body, ok := strings.CutPrefix(option, prefix+sep); ok {
				option = strings.TrimSpace(body)
				break
			}
		}
		fmt.Fprintf(&b, "\n%s. %s", prefix, Readable(option))
	}
	return b.String()
}
func AnswerBody(q bank.Question) string {
	a := "答案：" + Readable(q.Answer)
	if q.Analysis != "" {
		a += "\n解析：" + Readable(q.Analysis)
	}
	return a
}
func Text(title string, questions []bank.Question, withAnswers bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n姓名：____________  日期：____________\n\n", title)
	for i, q := range questions {
		fmt.Fprintf(&b, "%d. %s\n\n", i+1, QuestionBody(q))
	}
	if withAnswers {
		b.WriteString("参考答案与解析\n\n")
		for i, q := range questions {
			fmt.Fprintf(&b, "%d. %s\n\n", i+1, AnswerBody(q))
		}
	}
	return b.String()
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`
const relationships = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`
const documentStart = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`
const documentEnd = `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="960" w:right="960" w:bottom="960" w:left="960" w:header="360" w:footer="360" w:gutter="0"/></w:sectPr></w:body></w:document>`
const fontProps = `<w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Microsoft YaHei"/>`

func SaveWord(path, title string, questions []bank.Question, answers bool) error {
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	add := func(name, value string) error {
		f, e := z.Create(name)
		if e != nil {
			return e
		}
		_, e = io.WriteString(f, value)
		return e
	}
	if err := add("[Content_Types].xml", contentTypes); err != nil {
		return err
	}
	if err := add("_rels/.rels", relationships); err != nil {
		return err
	}
	var doc strings.Builder
	doc.WriteString(documentStart)
	for i, line := range strings.Split(strings.TrimSuffix(Text(title, questions, answers), "\n\n"), "\n") {
		doc.WriteString(`<w:p><w:r><w:rPr>` + fontProps)
		size := 22
		if i == 0 || line == "参考答案与解析" {
			size = 32
			doc.WriteString(`<w:b/>`)
		}
		fmt.Fprintf(&doc, `<w:sz w:val="%d"/></w:rPr><w:t xml:space="preserve">`, size)
		if err := xml.EscapeText(&doc, []byte(line)); err != nil {
			return err
		}
		doc.WriteString(`</w:t></w:r></w:p>`)
	}
	doc.WriteString(documentEnd)
	if err := add("word/document.xml", doc.String()); err != nil {
		return err
	}
	if err := z.Close(); err != nil {
		return err
	}
	return AtomicWrite(path, out.Bytes())
}
