package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"k12bank/internal/bank"
)

const fakeKey = "fake-test-credential-42"

func config(base string) Config { return Config{base, "fake-model", fakeKey} }

func TestValidate(t *testing.T) {
	for in, want := range map[string]string{
		" https://example.test/ ":                          "https://example.test/v1/chat/completions",
		"https://example.test/v1//":                        "https://example.test/v1/chat/completions",
		"https://example.test/custom/v2/chat/completions/": "https://example.test/custom/v2/chat/completions",
		"http://localhost:8000/v1":                         "http://localhost:8000/v1/chat/completions",
		"http://[::1]:8000":                                "http://[::1]:8000/v1/chat/completions",
	} {
		e, err := normalize(config(in))
		if err != nil || e.url != want || config(in).Validate() != nil {
			t.Errorf("normalize(%q) endpoint = %q, err = %v; want %q", in, e.url, err, want)
		}
	}
	for _, c := range []Config{
		{}, config("http://example.test/v1"), config("http://localhost.example.test/v1"),
		config("https://user:" + fakeKey + "@example.test"), config("https://example.test?"), config("https://example.test/#"),
		config("https://example.test:65536/v1"), config("https://example.test/x/../v1"), config("https://example.test/%2f"),
		config("https://example.test/" + fakeKey), {"https://example.test", fakeKey, fakeKey},
		{"https://example.test", "model", fakeKey + "\r\n"}, {"https://example.test", "", fakeKey},
	} {
		if err := c.Validate(); err == nil || strings.Contains(err.Error(), fakeKey) {
			t.Errorf("expected a sanitized validation error; got %v", err)
		}
	}
}

func writeChoice(w http.ResponseWriter, reason, content string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{
		"finish_reason": reason, "message": message{"assistant", content},
	}}})
}

func TestProbe(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+fakeKey {
			t.Error("unexpected endpoint, method or authorization")
		}
		var p struct {
			Model     string    `json:"model"`
			Stream    *bool     `json:"stream"`
			MaxTokens int       `json:"max_tokens"`
			Messages  []message `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&p) != nil || p.Model != "fake-model" || p.Stream == nil || *p.Stream || p.MaxTokens != 8 || len(p.Messages) != 1 || p.Messages[0].Role != "user" {
			t.Error("unexpected probe payload")
		}
		writeChoice(w, "stop", "OK")
	}))
	defer s.Close()
	if err := Test(context.Background(), config(strings.Replace(s.URL, "127.0.0.1", "localhost", 1)+"/v1/")); err != nil || calls.Load() != 1 {
		t.Fatalf("probe: err=%v, calls=%d", err, calls.Load())
	}
}

func TestProbeFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"auth", 401, fakeKey, "HTTP 401"}, {"quota", 429, fakeKey, "HTTP 429"},
		{"redirect", 307, fakeKey, "重定向"}, {"server", 503, fakeKey, "HTTP 503"},
		{"empty choices", 200, `{"choices":[]}`, "Chat Completions JSON"},
		{"refusal", 200, `{"choices":[{"message":{"content":"OK","refusal":"yes"}}]}`, "拒绝"},
		{"truncated", 200, `{"choices":[{"finish_reason":"length","message":{"content":"OK"}}]}`, "截断"},
		{"echoed key", 200, `{"choices":[{"message":{"content":"` + fakeKey + `"}}]}`, "密钥"},
		{"oversized", 200, strings.Repeat("x", maxResponse+1), "过大"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/other" {
					t.Error("followed redirect")
					w.WriteHeader(200)
					return
				}
				w.Header().Set("Location", "/other")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer s.Close()
			err := Test(context.Background(), config(s.URL))
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), fakeKey) || calls.Load() != 1 {
				t.Fatalf("probe: err=%v, calls=%d", err, calls.Load())
			}
		})
	}
}

func TestCancelDuringBody(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(s.Close)
	defer close(release)
	result := make(chan error, 1)
	go func() { result <- Test(ctx, config(s.URL)) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("server did not start the response")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client did not return after cancellation")
	}
}

func filter(count int) bank.Filter {
	return bank.Filter{Subject: "math", Stage: "junior", Grade: 7, Term: 1, TopicID: "algebra", TopicName: "代数", Difficulty: 2, Count: count}
}

// A synthetic endpoint returns unique questions unless a scenario mutates the batch.
func generator(t *testing.T, mutate func(batchPrompt, []map[string]any)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var p struct{ Messages []message }
		if json.NewDecoder(r.Body).Decode(&p) != nil || len(p.Messages) != 2 {
			t.Error("invalid generation request")
			w.WriteHeader(400)
			return
		}
		var b batchPrompt
		if json.Unmarshal([]byte(p.Messages[1].Content), &b) != nil || b.Count < 1 || b.Count > 5 || strings.Contains(p.Messages[1].Content, fakeKey) {
			t.Error("invalid batch prompt")
			w.WriteHeader(400)
			return
		}
		items := make([]map[string]any, b.Count)
		for i := range items {
			items[i] = map[string]any{"kp": b.TopicID, "diff": b.Difficulty, "type": "choice",
				"stem": fmt.Sprintf("求 %d + 2 的值。", b.Offset+i), "opts": []string{"Ａ．正确选项", "错误选项"},
				"answer": "A", "analysis": "此处给出解析。"}
		}
		if mutate != nil {
			mutate(b, items)
		}
		data, _ := json.Marshal(map[string]any{"questions": items})
		writeChoice(w, "stop", "```json\n"+string(data)+"\n```")
	}))
	t.Cleanup(s.Close)
	return s, calls
}

func TestGenerate(t *testing.T) {
	f := filter(12)
	// Full school year and no selected topic are allowed by bank.Filter.
	f.Term, f.TopicID, f.TopicName = 0, "", ""
	s, calls := generator(t, func(b batchPrompt, _ []map[string]any) {
		if b.Subject != f.Subject || b.Stage != f.Stage || b.Grade != f.Grade || b.Term != f.Term || b.Difficulty != f.Difficulty || b.TopicID != f.TopicID {
			t.Errorf("invalid curriculum metadata: %+v", b)
		}
		if !slices.Contains(b.Avoid, "已有题干") || b.Offset == 5 && !slices.Contains(b.Avoid, "求 4 + 2 的值。") {
			t.Errorf("missing avoid stems: %v", b.Avoid)
		}
	})
	var progress []int
	qs, err := Generate(context.Background(), config(s.URL+"/v1/chat/completions"), f,
		[]bank.Question{{Stem: "已有题干"}, {Stem: fakeKey}}, func(done, total int) {
			if total != f.Count {
				t.Errorf("total=%d, want %d", total, f.Count)
			}
			progress = append(progress, done)
		})
	if err != nil || len(qs) != f.Count || calls.Load() != 3 || !slices.Equal(progress, []int{0, 5, 10, 12}) {
		t.Fatalf("returned %d, progress=%v, calls=%d, err=%v", len(qs), progress, calls.Load(), err)
	}
	ids := make(map[string]bool)
	for _, q := range qs {
		if !strings.HasPrefix(q.ID, "ai-") || ids[q.ID] || q.Subject != f.Subject || q.Stage != f.Stage || q.Grade != f.Grade || q.Term != f.Term || q.TopicID != f.TopicID || q.TopicName != f.TopicName || q.Difficulty != f.Difficulty || q.Answer != "A" || !slices.Equal(q.Options, []string{"A. 正确选项", "B. 错误选项"}) {
			t.Errorf("question not normalized: %+v", q)
		}
		ids[q.ID] = true
	}
}

func TestGenerateRejectsInvalidSecondBatch(t *testing.T) {
	for name, replace := range map[string]map[string]any{
		"duplicate": {"stem": "求 0+2 的值。", "answer": "B"},
		"excluded":  {"stem": "远处的旧题干"},
		"topic":     {"kp": "wrong"}, "difficulty": {"diff": 5}, "type": {"type": "invalid"},
		"blank": {"analysis": "  "}, "markup": {"analysis": "<b>粗体</b>"},
		"answer": {"answer": "C"}, "opts": {"opts": []string{"A. 同一个", "B. 同一个"}},
		"key echo":    {"analysis": fakeKey},
		"extra field": {"subject": "ignored"},
	} {
		t.Run(name, func(t *testing.T) {
			s, calls := generator(t, func(b batchPrompt, items []map[string]any) {
				if b.Offset == 5 {
					for field, v := range replace {
						items[0][field] = v
					}
				}
			})
			old := []bank.Question{{Stem: "远处的旧题干"}, {Stem: "甲"}, {Stem: "乙"}, {Stem: "丙"}, {Stem: "丁"}, {Stem: "戊"}}
			qs, err := Generate(context.Background(), config(s.URL), filter(6), old, nil)
			if qs != nil || err == nil || strings.Contains(err.Error(), fakeKey) || calls.Load() != 2 {
				t.Fatalf("expected atomic failure with no retries: count=%d, calls=%d, err=%v", len(qs), calls.Load(), err)
			}
		})
	}
}

func TestGenerateCountAndCancelBetweenBatches(t *testing.T) {
	s, calls := generator(t, nil)
	if qs, err := Generate(context.Background(), config(s.URL), filter(41), nil, nil); qs != nil || err == nil || calls.Load() != 0 {
		t.Fatalf("41 questions: count=%d, calls=%d, err=%v", len(qs), calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	qs, err := Generate(ctx, config(s.URL), filter(40), nil, func(done, _ int) {
		if done == 5 {
			cancel()
		}
	})
	if qs != nil || !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("canceled generation: count=%d, calls=%d, err=%v", len(qs), calls.Load(), err)
	}
}
