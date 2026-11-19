package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Config contains user-supplied settings for a Chat Completions endpoint.
type Config struct{ BaseURL, Model, APIKey string }

// Validate checks the settings without making a network request. An empty
// configuration is not usable; callers may handle clearing settings separately.
func (c Config) Validate() error {
	_, err := normalize(c)
	return err
}

type endpoint struct{ url, model, key string }

const (
	chatPath    = "/chat/completions"
	maxResponse = 1 << 20
	maxContent  = 100 << 10
)

var plainPath = regexp.MustCompile(`^[a-zA-Z0-9/._~-]*$`)

func normalize(c Config) (endpoint, error) {
	base, model, key := strings.TrimSpace(c.BaseURL), strings.TrimSpace(c.Model), strings.TrimSpace(c.APIKey)
	badURL := errors.New("AI 接口地址无效：请填写 HTTPS 地址，不可包含凭据、查询参数或片段")
	if base == "" || len(base) > 2048 || strings.ContainsAny(base, `\?#%`) || strings.ContainsFunc(base, spaceOrControl) {
		return endpoint{}, badURL
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || u.User != nil || !plainPath.MatchString(u.Path) || strings.HasSuffix(u.Host, ":") {
		return endpoint{}, badURL
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return endpoint{}, badURL
		}
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return endpoint{}, badURL
		}
	}
	host := u.Hostname()
	local := strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
	if u.Scheme != "https" && (u.Scheme != "http" || !local) {
		return endpoint{}, errors.New("远程 AI 接口必须使用 HTTPS；HTTP 仅允许回环地址")
	}
	if model == "" || len(model) > 200 || !utf8.ValidString(model) || strings.ContainsFunc(c.Model, unicode.IsControl) {
		return endpoint{}, errors.New("请填写有效的 AI 模型名称")
	}
	if key == "" || len(key) > 8192 || strings.ContainsFunc(c.APIKey, unicode.IsControl) || strings.ContainsFunc(key, func(r rune) bool { return r < '!' || r > '~' }) {
		return endpoint{}, errors.New("请填写有效的 API Key，不可包含换行或非法请求头字符")
	}
	if strings.Contains(base, key) || strings.Contains(model, key) {
		return endpoint{}, errors.New("请仅在 API Key 栏填写密钥")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/v1"
	}
	if !strings.HasSuffix(u.Path, chatPath) {
		u.Path += chatPath
	}
	return endpoint{u.String(), model, key}, nil
}

func spaceOrControl(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }

// No proxy, no redirect handling, and no replay on a stale connection. The
// custom dialer keeps this transport on HTTP/1.1 with default TLS verification.
var direct = &http.Transport{
	DialContext:            dialDirect,
	DisableKeepAlives:      true,
	TLSHandshakeTimeout:    10 * time.Second,
	MaxResponseHeaderBytes: 64 << 10,
}

func dialDirect(ctx context.Context, network, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: 10 * time.Second}
	host, port, err := net.SplitHostPort(addr)
	if err == nil && strings.EqualFold(host, "localhost") {
		// Do not trust DNS or the hosts file for cleartext localhost requests.
		conn, err := d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		if err == nil || ctx.Err() != nil {
			return conn, err
		}
		return d.DialContext(ctx, network, net.JoinHostPort("::1", port))
	}
	return d.DialContext(ctx, network, addr)
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Test makes a real, potentially billable Chat Completions request; it sends
// no bank data and succeeds only on a usable assistant response.
func Test(ctx context.Context, c Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	api, err := normalize(c)
	if err != nil {
		return err
	}
	_, err = api.complete(ctx, 8, []message{{"user", "Reply only with OK."}})
	return err
}

func (e endpoint) complete(ctx context.Context, tokens int, messages []message) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	body, err := json.Marshal(struct {
		Model     string    `json:"model"`
		Messages  []message `json:"messages"`
		Stream    bool      `json:"stream"`
		MaxTokens int       `json:"max_tokens"`
	}{e.model, messages, false, tokens})
	if err != nil {
		return "", errors.New("无法构建 AI 请求")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, e.url, bytes.NewReader(body))
	if err != nil {
		return "", errors.New("无法构建 AI 请求")
	}
	req.GetBody = nil
	req.Header.Set("Authorization", "Bearer "+e.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := direct.RoundTrip(req)
	if err != nil {
		return "", requestError(ctx, requestCtx)
	}
	defer res.Body.Close()
	if err := requestCtx.Err(); err != nil {
		return "", requestError(ctx, requestCtx)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", statusError(res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil || requestCtx.Err() != nil {
		return "", requestError(ctx, requestCtx)
	}
	if len(raw) > maxResponse {
		return "", errors.New("AI 接口响应过大")
	}
	content, err := assistantContent(raw, e.key)
	if requestCtx.Err() != nil {
		return "", requestError(ctx, requestCtx)
	}
	return content, err
}

func requestError(ctx, requestCtx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if requestCtx.Err() != nil {
		return fmt.Errorf("AI 请求超时：%w", context.DeadlineExceeded)
	}
	return errors.New("无法连接 AI 接口或读取响应，请检查网络、接口地址和 HTTPS 证书")
}

func statusError(code int) error {
	why := "请检查接口配置和模型是否支持 Chat Completions"
	switch {
	case code >= 300 && code < 400:
		why = "已拒绝重定向，请填写最终接口地址"
	case code == 401 || code == 403:
		why = "鉴权失败或权限不足，请检查 API Key 和模型权限"
	case code == 404:
		why = "接口或模型不存在，请检查地址和模型名称"
	case code == 429:
		why = "限流或额度不足，请检查用量后手动重试"
	case code >= 500:
		why = "服务暂时不可用，请稍后手动重试"
	}
	return fmt.Errorf("AI 接口请求失败（HTTP %d）：%s", code, why)
}

func assistantContent(raw []byte, key string) (string, error) {
	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Finish  *string `json:"finish_reason"`
			Message struct {
				message
				Refusal  string            `json:"refusal"`
				Tools    []json.RawMessage `json:"tool_calls"`
				Function json.RawMessage   `json:"function_call"`
			} `json:"message"`
		} `json:"choices"`
	}
	if !utf8.Valid(raw) || json.Unmarshal(raw, &envelope) != nil || !nullOrAbsent(envelope.Error) || len(envelope.Choices) == 0 {
		return "", errors.New("AI 接口未返回有效的 Chat Completions JSON")
	}
	choice := envelope.Choices[0]
	m := choice.Message
	if choice.Finish != nil && *choice.Finish != "stop" || m.Refusal != "" || len(m.Tools) != 0 || !nullOrAbsent(m.Function) {
		return "", errors.New("AI 响应被截断、拒绝或未正常结束")
	}
	if m.Role != "" && m.Role != "assistant" || strings.TrimSpace(m.Content) == "" || len(m.Content) > maxContent {
		return "", errors.New("AI 消息为空、过长或格式无效")
	}
	if strings.Contains(m.Content, key) {
		return "", errors.New("AI 消息包含密钥，已拒绝结果")
	}
	return m.Content, nil
}

func nullOrAbsent(v json.RawMessage) bool { return len(v) == 0 || string(v) == "null" }
