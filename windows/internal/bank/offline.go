package bank

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
)

//go:generate node --jitless regenerate.cjs

//go:embed curriculum.json
var curriculumJSON []byte

//go:embed questions.jsonl.gz
var questionGZIP []byte

// Topic indicates whether the curriculum entry contains original offline questions.
type Topic struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Offline bool   `json:"offline"`
}

type course struct {
	Subject, Stage string
	Grade, Term    int
}

type courseRecord struct {
	course
	Topics []Topic
}

var curriculum, curriculumErr = readCurriculum()

func readCurriculum() (map[course][]Topic, error) {
	var rows []courseRecord
	if err := json.Unmarshal(curriculumJSON, &rows); err != nil {
		return nil, err
	}
	out := make(map[course][]Topic, len(rows))
	for _, row := range rows {
		out[row.course] = row.Topics
	}
	return out, nil
}

// Curriculum returns the original Registry topics for the grade and term.
// Term 0 is the union of both terms; unsupported entries remain visible.
func Curriculum(subject, stage string, grade, term int) []Topic {
	if term != 0 {
		return append([]Topic(nil), curriculum[course{subject, stage, grade, term}]...)
	}
	var topics []Topic
	seen := make(map[string]int)
	for t := 1; t <= 2; t++ {
		for _, topic := range curriculum[course{subject, stage, grade, t}] {
			if i, ok := seen[topic.ID]; ok {
				topics[i].Offline = topics[i].Offline || topic.Offline
			} else {
				seen[topic.ID] = len(topics)
				topics = append(topics, topic)
			}
		}
	}
	return topics
}

type poolKey struct {
	course
	difficulty int
}

var loadQuestions = sync.OnceValues(func() (map[poolKey][]Question, error) {
	reader, err := gzip.NewReader(bytes.NewReader(questionGZIP))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	pools := make(map[poolKey][]Question)
	for scanner.Scan() {
		var q Question
		if err := json.Unmarshal(scanner.Bytes(), &q); err != nil {
			return nil, err
		}
		k := poolKey{course{q.Subject, q.Stage, q.Grade, q.Term}, q.Difficulty}
		pools[k] = append(pools[k], q)
	}
	return pools, scanner.Err()
})

var blanks = regexp.MustCompile(`_{3,}|（\s*）`)
var processSuffix = regexp.MustCompile(`（写出(?:计算|解答)过程）$`)
var optionPrefix = regexp.MustCompile(`^[A-F][.．、]\s*`)

func compact(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// Using both keys prevents duplicates across option reordering, altered
// distractors, and equivalent fill/choice wrappers. The layout key also
// prevents repeating an identical prompt even if two source answers conflict.
func questionKeys(q Question) (content, layout string) {
	stem := processSuffix.ReplaceAllString(blanks.ReplaceAllString(compact(q.Stem), ""), "")
	opts := make([]string, len(q.Options))
	for i, o := range q.Options {
		opts[i] = compact(optionPrefix.ReplaceAllString(strings.TrimSpace(o), ""))
	}
	answer := compact(q.Answer)
	letter := strings.ToUpper(answer)
	if len(letter) == 1 && letter[0] >= 'A' && int(letter[0]-'A') < len(opts) {
		answer = opts[letter[0]-'A']
	}
	sort.Strings(opts)
	return stem + "\x1f" + answer, stem + "\x1f" + strings.Join(opts, "\x1e")
}

// Generate samples unique original questions without crossing the selected
// course, topic or difficulty. A shortage returns no paper plus an error.
// For a replacement, include the entire current paper in exclude.
func Generate(filter Filter, exclude []Question) ([]Question, error) {
	if err := filter.Validate(); err != nil {
		return nil, err
	}
	if curriculumErr != nil {
		return nil, fmt.Errorf("离线课程表无法读取：%w", curriculumErr)
	}
	topics := Curriculum(filter.Subject, filter.Stage, filter.Grade, filter.Term)
	if len(topics) == 0 {
		return nil, errors.New("所选科目、年级和学期暂无课程题目，请调整筛选条件")
	}
	if filter.TopicID != "" {
		found := false
		for _, topic := range topics {
			if topic.ID != filter.TopicID {
				continue
			}
			found = true
			if !topic.Offline {
				return nil, fmt.Errorf("知识点「%s」暂无离线题目，请选择其他知识点", topic.Name)
			}
			break
		}
		if !found {
			return nil, errors.New("所选知识点不属于当前年级和学期，请重新选择")
		}
	}
	pools, err := loadQuestions()
	if err != nil {
		return nil, fmt.Errorf("离线题库无法读取：%w", err)
	}
	var pool []*Question
	for term := 1; term <= 2; term++ {
		if filter.Term != 0 && filter.Term != term {
			continue
		}
		rows := pools[poolKey{course{filter.Subject, filter.Stage, filter.Grade, term}, filter.Difficulty}]
		for i := range rows {
			if filter.TopicID == "" || rows[i].TopicID == filter.TopicID {
				pool = append(pool, &rows[i])
			}
		}
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	ids := make(map[string]bool, len(exclude)+filter.Count)
	contents := make(map[string]bool, len(exclude)+filter.Count)
	layouts := make(map[string]bool, len(exclude)+filter.Count)
	for _, q := range exclude {
		if q.ID != "" {
			ids[q.ID] = true
		}
		if q.Stem != "" {
			content, layout := questionKeys(q)
			contents[content], layouts[layout] = true, true
		}
	}
	out := make([]Question, 0, filter.Count)
	for _, source := range pool {
		q := *source
		if ids[q.ID] {
			continue
		}
		content, layout := questionKeys(q)
		if contents[content] || layouts[layout] {
			continue
		}
		ids[q.ID], contents[content], layouts[layout] = true, true, true
		q.Options = append([]string(nil), q.Options...)
		out = append(out, q)
		if len(out) == filter.Count {
			return out, nil
		}
	}
	return nil, fmt.Errorf("当前条件下可选且不重复的离线题不足（需 %d 道，可选 %d 道），请减少题量或放宽筛选条件", filter.Count, len(out))
}
