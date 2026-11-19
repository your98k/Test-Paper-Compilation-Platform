package paper_test

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"k12bank/internal/bank"
	"k12bank/internal/paper"
)

var sample = bank.Question{ID: "sample", Subject: "math", Stage: "junior", Grade: 7, Term: 1, Stem: `计算 \(\frac{1}{2}\times 4\)；对比 A & B < 3`, Options: []string{"2", "3"}, Answer: "A", Analysis: "先求乘积。"}

func TestWrongbookPersistsAndRemoves(t *testing.T) {
	dir := t.TempDir()
	if got, err := paper.LoadWrong(dir); err != nil || len(got) != 0 {
		t.Fatalf("new book: %v %v", got, err)
	}
	if err := paper.SaveWrong(dir, []bank.Question{sample}); err != nil {
		t.Fatal(err)
	}
	if got, err := paper.LoadWrong(dir); err != nil || !reflect.DeepEqual(got, []bank.Question{sample}) {
		t.Fatalf("saved book: %v %v", got, err)
	}
	if err := paper.SaveWrong(dir, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := paper.LoadWrong(dir); err != nil || len(got) != 0 {
		t.Fatalf("removed: %v %v", got, err)
	}
}
func TestBrokenBookIsNotTreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "wrongbook.json")
	if err := os.WriteFile(name, []byte(`{"version":7}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := paper.LoadWrong(dir); err == nil {
		t.Fatal("expected version error")
	}
}
func TestTextDefaultsToStudentCopy(t *testing.T) {
	got := paper.Text("练习卷", []bank.Question{sample}, false)
	if !strings.Contains(got, "(1)/(2)×4") || !strings.Contains(got, "A. 2") || strings.Contains(got, "先求乘积") {
		t.Fatalf("unexpected student copy %q", got)
	}
	if got := paper.Text("练习卷", []bank.Question{sample}, true); !strings.Contains(got, "1. 答案：A\n解析：先求乘积") {
		t.Fatalf("answer key missing: %q", got)
	}
}
func TestDocxContainsValidXMLAndEscapesContent(t *testing.T) {
	name := filepath.Join(t.TempDir(), "试卷.docx")
	if err := paper.SaveWord(name, "试卷", []bank.Question{sample}, true); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(name)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	want := map[string]bool{"[Content_Types].xml": true, "_rels/.rels": true, "word/document.xml": true}
	for _, file := range z.File {
		r, e := file.Open()
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		var element any
		if e := xml.Unmarshal(b, &element); e != nil {
			t.Fatalf("%s XML: %v", file.Name, e)
		}
		delete(want, file.Name)
		doc := string(b)
		if file.Name == "word/document.xml" && (!strings.Contains(doc, "A &amp; B &lt; 3") || !strings.Contains(doc, "参考答案与解析")) {
			t.Fatalf("content missing: %s", doc)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing parts: %v", want)
	}
}
