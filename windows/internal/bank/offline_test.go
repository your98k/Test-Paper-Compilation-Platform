package bank

import (
	"fmt"
	"strings"
	"testing"
)

func TestCurriculumExactTermsAndUnion(t *testing.T) {
	if curriculumErr != nil {
		t.Fatal(curriculumErr)
	}
	find := func(topics []Topic, id string) (Topic, bool) {
		for _, topic := range topics {
			if topic.ID == id {
				return topic, true
			}
		}
		return Topic{}, false
	}
	up := Curriculum("math", "primary", 4, 1)
	down := Curriculum("math", "primary", 4, 2)
	all := Curriculum("math", "primary", 4, 0)
	for _, topics := range [][]Topic{up, down, all} {
		topic, ok := find(topics, "angle")
		if !ok || !topic.Offline || topic.Name != "角与三角形" {
			t.Fatalf("repeated Registry topic missing: %+v", topics)
		}
	}
	if len(all) != len(up)+len(down)-1 {
		t.Fatalf("school-year union did not deduplicate: %+v", all)
	}
	if _, ok := find(up, "approx"); ok {
		t.Fatal("spring topic leaked into autumn")
	}
	up[0].ID = "tampered"
	if _, ok := find(Curriculum("math", "primary", 4, 1), "tampered"); ok {
		t.Fatal("caller mutated curriculum")
	}
	if topic, ok := find(Curriculum("english", "primary", 6, 2), "eng.p.read"); !ok || topic.Offline {
		t.Fatalf("pending Registry topic was not preserved: %+v", topic)
	}
	if topics := Curriculum("chemistry", "primary", 6, 0); len(topics) != 0 {
		t.Fatalf("invalid curriculum has topics: %+v", topics)
	}
}

func TestGenerateFiltersAndReplacement(t *testing.T) {
	filter := Filter{Subject: "math", Stage: "primary", Grade: 3, Term: 2, TopicID: "intDiv", Difficulty: 3, Count: 40}
	original, err := Generate(filter, nil)
	if err != nil || len(original) != filter.Count {
		t.Fatalf("full paper: %d questions, %v", len(original), err)
	}
	ids, keys, layouts := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, q := range original {
		if q.Subject != filter.Subject || q.Stage != filter.Stage || q.Grade != filter.Grade || q.Term != filter.Term || q.Difficulty != filter.Difficulty || q.TopicID != filter.TopicID || q.TopicName != "整数除法" {
			t.Fatalf("question outside filter: %+v", q)
		}
		content, layout := questionKeys(q)
		if ids[q.ID] || keys[content] || layouts[layout] {
			t.Fatalf("duplicate question: %+v", q)
		}
		ids[q.ID], keys[content], layouts[layout] = true, true, true
	}
	filter.Count = 1
	alt, err := Generate(filter, original)
	if err != nil || len(alt) != 1 {
		t.Fatalf("replacement: %d questions, %v", len(alt), err)
	}
	content, layout := questionKeys(alt[0])
	if ids[alt[0].ID] || keys[content] || layouts[layout] {
		t.Fatalf("replacement is not new: %+v", alt[0])
	}
}

func TestGenerateAllTermsAndChineseErrors(t *testing.T) {
	filter := Filter{Subject: "english", Stage: "primary", Grade: 6, Term: 0, Difficulty: 1, Count: 1}
	rows, err := Generate(filter, nil)
	if err != nil || len(rows) != 1 || rows[0].Term != 1 || rows[0].TopicID != "eng.p.past" {
		t.Fatalf("year selection = %+v, %v", rows, err)
	}
	cases := []struct {
		topic  string
		term   int
		count  int
		phrase string
	}{
		{"eng.p.past", 2, 1, "不属于"},
		{"eng.p.read", 2, 1, "暂无离线题目"},
		{"", 0, 41, "题量"},
	}
	for _, tc := range cases {
		filter.TopicID, filter.Term, filter.Count = tc.topic, tc.term, tc.count
		got, err := Generate(filter, nil)
		if got != nil || err == nil || !strings.Contains(err.Error(), tc.phrase) {
			t.Errorf("%+v: got %d questions and %v, want error containing %q", tc, len(got), err, tc.phrase)
		}
	}
}

func TestGenerateReportsShortageWhenPoolIsExcluded(t *testing.T) {
	pools, err := loadQuestions()
	if err != nil {
		t.Fatal(err)
	}
	f := Filter{Subject: "english", Stage: "primary", Grade: 6, Term: 1, TopicID: "eng.p.past", Difficulty: 1, Count: 1}
	excluded := pools[poolKey{course{f.Subject, f.Stage, f.Grade, f.Term}, f.Difficulty}]
	if len(excluded) == 0 {
		t.Fatal("the test pool must contain questions")
	}
	if got, err := Generate(f, excluded); got != nil || err == nil || !strings.Contains(err.Error(), "需 1 道，可选 0 道") {
		t.Fatalf("got %d questions and %v with exhausted pool", len(got), err)
	}
}

func TestContentIdentity(t *testing.T) {
	a := Question{Stem: "1＋1＝______", Options: []string{"A. 2", "B. 5"}, Answer: "A"}
	b := Question{Stem: "1 ＋ 1 ＝（　）", Options: []string{"A. 5", "B. 2"}, Answer: "B"}
	c := Question{Stem: "1＋1＝______", Answer: "2"}
	ka, la := questionKeys(a)
	kb, lb := questionKeys(b)
	kc, _ := questionKeys(c)
	if ka != kb || ka != kc || la != lb {
		t.Fatal("a change of blank, whitespace, labels or choice order created a duplicate")
	}
}

func TestCorpusIntegrityAndCoverage(t *testing.T) {
	pools, err := loadQuestions()
	if err != nil {
		t.Fatal(err)
	}
	courseTopics := make(map[course]map[string]Topic)
	var offline, topicTotal int
	for c, topics := range curriculum {
		courseTopics[c] = make(map[string]Topic, len(topics))
		topicTotal += len(topics)
		for _, topic := range topics {
			courseTopics[c][topic.ID] = topic
			if topic.Offline {
				offline++
			}
		}
	}
	seen := make(map[string]bool)
	supported := make(map[poolKey]map[string]int)
	subjectTotals := make(map[string]int)
	for k, rows := range pools {
		supported[k] = make(map[string]int)
		for _, q := range rows {
			topic := courseTopics[k.course][q.TopicID]
			if !topic.Offline || topic.Name != q.TopicName {
				t.Fatalf("corpus row %s is not correctly mapped to Registry: %+v", q.ID, q)
			}
			if q.ID == "" || seen[q.ID] || q.Stem == "" || q.Answer == "" || q.Type == "" || q.Difficulty < 1 || q.Difficulty > 5 {
				t.Fatalf("invalid corpus row: %+v", q)
			}
			if q.Type == "choice" && (len(q.Options) < 2 || len(q.Answer) != 1 || q.Answer[0] < 'A' || int(q.Answer[0]-'A') >= len(q.Options)) {
				t.Fatalf("invalid choice: %+v", q)
			}
			if q.Type == "judge" && q.Answer != "√" && q.Answer != "×" {
				t.Fatalf("invalid judge: %+v", q)
			}
			if q.Type == "error" && !strings.Contains(q.Stem, "\n") {
				t.Fatalf("missing displayed incorrect working: %+v", q)
			}
			seen[q.ID] = true
			supported[k][q.TopicID]++
			subjectTotals[q.Subject]++
		}
	}
	for c, topics := range curriculum {
		for _, topic := range topics {
			if topic.Offline {
				for d := 1; d <= 5; d++ {
					if supported[poolKey{c, d}][topic.ID] == 0 {
						t.Errorf("%+v %s has no questions in difficulty %d", c, topic.ID, d)
					}
				}
			}
		}
	}
	for _, subject := range []string{"math", "physics", "chemistry", "chinese", "english"} {
		if subjectTotals[subject] == 0 {
			t.Errorf("no questions for %s", subject)
		}
	}
	if len(seen) < 10_000 || offline < 130 {
		t.Fatalf("unexpectedly sparse corpus: %d questions, %d offline course-topic slots", len(seen), offline)
	}
	t.Log(fmt.Sprintf("%d course-term rows, %d/%d offline course-topic slots, %d questions: %v", len(curriculum), offline, topicTotal, len(seen), subjectTotals))
}
