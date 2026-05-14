package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/user/llm-test-tool/config"
)

// Client LLM API客户端
type Client struct {
	httpClient *http.Client
	config     config.APIConfig
}

// Request API请求结构
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream,omitempty"`
	MaxTokens  int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Thinking    *Thinking `json:"thinking,omitempty"` // 思考模式参数
}

// Thinking 思考模式配置
type Thinking struct {
	Type string `json:"type"` // "enabled" 或 "disabled"
}

// Message 消息结构
type Message struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // 可以是字符串或ContentArray
}

// ContentItem 内容项（用于数组格式的消息）
type ContentItem struct {
	Text string `json:"text"`
	Type string `json:"type"` // "text"
}

// ContentArray 内容数组（用于支持新的消息格式）
type ContentArray []ContentItem

// Response API响应结构
type Response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice 响应选择项
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage 使用统计
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// NewClient 创建新的 API 客户端。
//
// maxConns 是「预期并发上限」。基准测试中通常 = worker 并发数；
// 传 0 会退化为 net/http 默认（DefaultMaxIdleConnsPerHost=2），
// 在高并发压测下会频繁重建 TCP/TLS，显著污染 TTFT 与延迟指标。
func NewClient(apiConfig config.APIConfig, maxConns int) *Client {
	if maxConns <= 0 {
		maxConns = 2
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          maxConns * 2,
		MaxIdleConnsPerHost:   maxConns,
		MaxConnsPerHost:       maxConns,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Timeout:   apiConfig.Timeout,
		Transport: transport,
	}

	return &Client{
		httpClient: client,
		config:     apiConfig,
	}
}

// Call 调用LLM API
func (c *Client) Call(ctx context.Context, req *Request) (*Response, error) {
	// 构建请求体
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	// 创建HTTP请求
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.config.BaseURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	// 设置请求头
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

	// 添加自定义头部
	for key, value := range c.config.Headers {
		httpReq.Header.Set(key, value)
	}

	// 发送请求
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 如果是流式响应（SSE），按流式解析
	if req.Stream || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		r, _, err := parseChatCompletionSSE(resp, time.Now())
		return r, err
	}

	// 读取响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API返回错误状态码 %d: %s", resp.StatusCode, string(respBody))
	}

	// 解析响应
	var apiResp Response
	err = json.Unmarshal(respBody, &apiResp)
	if err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &apiResp, nil
}

// parseChatCompletionSSE parses OpenAI-compatible chat.completions streaming responses (SSE).
// It aggregates delta.content into a single assistant message. Usage fields may be absent.
//
// sendStart is the timestamp at which the HTTP request was dispatched; the
// returned duration is the real TTFT — time from request send until the first
// SSE frame carrying generated content arrives.
func parseChatCompletionSSE(resp *http.Response, sendStart time.Time) (*Response, time.Duration, error) {
	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("API返回错误状态码 %d: %s", resp.StatusCode, string(respBody))
	}

	// sseDelta 同时识别常见的「思考/推理」字段；对于带推理能力的模型
	// （DeepSeek reasoning_content、Moonshot reasoning、Anthropic thinking 等），
	// TTFT 应该是模型产生的第一个 token（无论是推理 token 还是回答 token），
	// 否则 TTFT 会被推理阶段的耗时系统性高估。
	type sseDelta struct {
		Content          string `json:"content,omitempty"`
		ReasoningContent string `json:"reasoning_content,omitempty"`
		Reasoning        string `json:"reasoning,omitempty"`
		Thinking         string `json:"thinking,omitempty"`
	}
	type sseChoice struct {
		Index        int      `json:"index"`
		Delta        sseDelta `json:"delta"`
		FinishReason string   `json:"finish_reason"`
	}
	type sseChunk struct {
		ID      string      `json:"id"`
		Object  string      `json:"object"`
		Created int64       `json:"created"`
		Model   string      `json:"model"`
		Choices []sseChoice `json:"choices"`
		Usage   Usage       `json:"usage"`
	}

	var out Response
	out.Choices = []Choice{{Index: 0, Message: Message{Role: "assistant", Content: ""}}}

	var b strings.Builder
	var ttft time.Duration
	firstByteSeen := false
	// 用于识别"流是否正常结束"：三者任一为 true 即算正常收尾。
	// 都是 false 说明上游把连接切了（TCP 提前 EOF），数据可能不完整。
	gotDone := false
	gotFinishReason := false
	gotUsage := false

	scanner := bufio.NewScanner(resp.Body)
	// streaming chunks can be long; enlarge buffer
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// SSE "data:" lines
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			gotDone = true
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// ignore non-JSON frames
			continue
		}

		// 在第一个包含生成内容的 delta 到达时打点，跳过空的/role-only 前导帧。
		// 这里把 reasoning/thinking 流也视为「首 token」，因为这些确实是模型产生的 token，
		// 代表了 prefill 阶段完成。
		if !firstByteSeen {
			for _, c := range chunk.Choices {
				d := c.Delta
				if d.Content != "" || d.ReasoningContent != "" || d.Reasoning != "" || d.Thinking != "" {
					ttft = time.Since(sendStart)
					firstByteSeen = true
					break
				}
			}
		}

		if out.ID == "" && chunk.ID != "" {
			out.ID = chunk.ID
		}
		if out.Object == "" && chunk.Object != "" {
			out.Object = chunk.Object
		}
		if out.Model == "" && chunk.Model != "" {
			out.Model = chunk.Model
		}
		if out.Created == 0 && chunk.Created != 0 {
			out.Created = chunk.Created
		}

		// accumulate delta content
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				b.WriteString(c.Delta.Content)
			}
			if c.FinishReason != "" {
				out.Choices[0].FinishReason = c.FinishReason
				gotFinishReason = true
			}
		}

		// usage may appear in last chunk for some providers
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			out.Usage = chunk.Usage
			gotUsage = true
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("读取流式响应失败: %w", err)
	}

	// 识别上游提前断连：既没有 [DONE]、也没有 finish_reason、也没有 usage 的流视为失败。
	// 不这样判的话，被截断的响应会被静默计为成功，污染延迟 / token / 成功率指标。
	if !gotDone && !gotFinishReason && !gotUsage {
		return nil, 0, fmt.Errorf("流式响应提前结束：未收到 [DONE]/finish_reason/usage 任一收尾信号，可能被上游截断")
	}

	// 如果整个响应都没见到 content 帧：保持 ttft=0，由调用方识别为"无 TTFT"并从均值中剔除。
	// （不再退化为总耗时——把总耗时当 TTFT 会系统性污染 TTFT 均值。）

	out.Choices[0].Message.Content = b.String()
	return &out, ttft, nil
}

// CallStats carries per-call observability metadata that does not fit into
// Response itself. It is populated on both success and error paths so callers
// can correlate a failed request with upstream logs via RequestID.
type CallStats struct {
	RequestID string        // 上游响应头里的 reqid（X-Request-Id / X-Reqid / X-Trace-Id），拿不到则为空
	TTFT      time.Duration // 首 token 耗时；非流式响应里等于读到响应 body 的耗时；失败时为 0
}

// reqIDHeaders lists response headers that upstream services use to propagate
// a request identifier, in priority order. Go's http.Header canonicalizes
// keys via textproto.CanonicalMIMEHeaderKey, so casing like "X-Request-ID"
// and "x-request-id" collapse to the same canonical entry — no need to list
// every case variant here.
//
// Covers:
//   - Generic / nginx-style:           X-Request-Id, X-Reqid, X-Trace-Id
//   - OpenAI:                          X-Request-Id (already covered)
//   - Anthropic:                       Request-Id
//   - Azure OpenAI / APIM:             Apim-Request-Id, X-Ms-Request-Id
//   - AWS Bedrock:                     X-Amzn-Requestid, X-Amzn-Trace-Id
//   - Google Cloud:                    X-Cloud-Trace-Context
var reqIDHeaders = []string{
	"X-Request-Id",
	"X-Reqid",
	"X-Trace-Id",
	"Request-Id",
	"Apim-Request-Id",
	"X-Ms-Request-Id",
	"X-Amzn-Requestid",
	"X-Amzn-Trace-Id",
	"X-Cloud-Trace-Context",
}

// extractRequestID returns the first non-empty value from the known set of
// request-id headers, or "" if none is present. As a fallback when no known
// header matches, it scans the full header map for any key whose canonical
// form contains "reqid" or "request-id" (case-insensitive in spirit, but
// Go has already canonicalized the keys so we match against the canonical
// lowercase form).
func extractRequestID(h http.Header) string {
	for _, name := range reqIDHeaders {
		if v := h.Get(name); v != "" {
			return v
		}
	}
	// Fallback: tolerate upstreams using a header name we haven't enumerated.
	for k, v := range h {
		if len(v) == 0 || v[0] == "" {
			continue
		}
		lk := strings.ToLower(k)
		if strings.Contains(lk, "reqid") || strings.Contains(lk, "request-id") {
			return v[0]
		}
	}
	return ""
}

// debugHeadersOnce prints the full response header map once per process when
// LLM_TOOL_DEBUG_HEADERS=1 is set — useful for identifying the real request-id
// header name when the default list fails to match.
var debugHeadersOnce sync.Once

func maybeDumpHeadersForDebug(h http.Header) {
	if os.Getenv("LLM_TOOL_DEBUG_HEADERS") != "1" {
		return
	}
	debugHeadersOnce.Do(func() {
		fmt.Fprintln(os.Stderr, "[debug] response headers of first request:")
		for k, v := range h {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", k, strings.Join(v, ", "))
		}
	})
}

// CallRaw sends the given JSON body as-is. Used when test cases already contain
// the full request payload (model/messages/stream/etc).
func (c *Client) CallRaw(ctx context.Context, body []byte, streamHint bool) (*Response, error) {
	resp, _, err := c.CallRawWithStats(ctx, body, streamHint)
	return resp, err
}

// CallRawWithTTFT sends the given JSON body as-is and returns the measured TTFT.
// Thin wrapper over CallRawWithStats retained for backward compatibility.
func (c *Client) CallRawWithTTFT(ctx context.Context, body []byte, streamHint bool) (*Response, time.Duration, error) {
	resp, stats, err := c.CallRawWithStats(ctx, body, streamHint)
	return resp, stats.TTFT, err
}

// CallRawWithStats sends the given JSON body as-is and returns both the parsed
// response and per-call stats (request id, TTFT). The request id is captured
// from response headers as soon as they arrive, so callers can still correlate
// a failed request with upstream logs even when the body parse later errors.
//
// For SSE responses, TTFT is the time from request send until the first content
// frame. For non-streaming responses, TTFT coincides with reading the body.
func (c *Client) CallRawWithStats(ctx context.Context, body []byte, streamHint bool) (*Response, CallStats, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.config.BaseURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, CallStats{}, fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	for key, value := range c.config.Headers {
		httpReq.Header.Set(key, value)
	}

	sendStart := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// 连接未建立，没有响应头可读
		return nil, CallStats{}, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 响应头在状态行之后立刻到达，body 还没开始读也能取。捕获一次，后续无论走哪条
	// 解析路径、出不出错都带上它——失败请求日志没有 reqid 就无法定位到上游。
	maybeDumpHeadersForDebug(resp.Header)
	stats := CallStats{RequestID: extractRequestID(resp.Header)}

	if streamHint || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		apiResp, ttft, parseErr := parseChatCompletionSSE(resp, sendStart)
		stats.TTFT = ttft
		return apiResp, stats, parseErr
	}

	respBody, err := io.ReadAll(resp.Body)
	stats.TTFT = time.Since(sendStart)
	if err != nil {
		return nil, stats, fmt.Errorf("读取响应体失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, stats, fmt.Errorf("API返回错误状态码 %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp Response
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, stats, fmt.Errorf("解析响应失败: %w", err)
	}
	return &apiResp, stats, nil
}

// RetryCallRaw retries CallRaw with the client's retry policy.
func (c *Client) RetryCallRaw(ctx context.Context, body []byte, streamHint bool) (*Response, error) {
	resp, _, err := c.RetryCallRawWithStats(ctx, body, streamHint)
	return resp, err
}

// RetryCallRawWithTTFT retries CallRawWithTTFT. Thin wrapper over
// RetryCallRawWithStats retained for backward compatibility.
func (c *Client) RetryCallRawWithTTFT(ctx context.Context, body []byte, streamHint bool) (*Response, time.Duration, error) {
	resp, stats, err := c.RetryCallRawWithStats(ctx, body, streamHint)
	return resp, stats.TTFT, err
}

// RetryCallRawWithStats retries CallRawWithStats with the client's retry policy.
// On success, the returned stats reflect the winning attempt. On final failure,
// the stats reflect the LAST attempt — so a failed-request log still gets a
// usable request id when the upstream managed to emit one.
//
// For benchmark accuracy prefer RetryCount=0 so retries don't mask real slowness.
func (c *Client) RetryCallRawWithStats(ctx context.Context, body []byte, streamHint bool) (*Response, CallStats, error) {
	var lastErr error
	var lastStats CallStats
	for i := 0; i <= c.config.RetryCount; i++ {
		resp, stats, err := c.CallRawWithStats(ctx, body, streamHint)
		if err == nil {
			return resp, stats, nil
		}
		lastErr = err
		lastStats = stats
		if i < c.config.RetryCount {
			select {
			case <-ctx.Done():
				return nil, lastStats, ctx.Err()
			case <-time.After(c.config.RetryDelay):
			}
		}
	}
	return nil, lastStats, fmt.Errorf("重试%d次后仍然失败: %w", c.config.RetryCount, lastErr)
}

// HealthCheck 健康检查
func (c *Client) HealthCheck(ctx context.Context) error {
	// 发送一个简单的健康检查请求
	req := &Request{
		Model: c.config.Model,
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		MaxTokens: 10,
	}

	_, err := c.Call(ctx, req)
	return err
}

// GetModels 获取支持的模型列表
func (c *Client) GetModels(ctx context.Context) ([]string, error) {
	// 这里可以根据实际API实现获取模型列表的逻辑
	// 当前返回配置中的模型作为示例
	return []string{c.config.Model}, nil
}

// StreamCall 流式调用（如果API支持）
func (c *Client) StreamCall(ctx context.Context, req *Request) (<-chan string, <-chan error) {
	resultChan := make(chan string)
	errorChan := make(chan error)

	go func() {
		defer close(resultChan)
		defer close(errorChan)

		// 修改请求为流式模式
		req.Stream = true

		// 构建请求体
		body, err := json.Marshal(req)
		if err != nil {
			errorChan <- fmt.Errorf("序列化请求体失败: %w", err)
			return
		}

		// 创建HTTP请求
		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.config.BaseURL, bytes.NewBuffer(body))
		if err != nil {
			errorChan <- fmt.Errorf("创建HTTP请求失败: %w", err)
			return
		}

		// 设置请求头
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.config.APIKey)

		// 添加自定义头部
		for key, value := range c.config.Headers {
			httpReq.Header.Set(key, value)
		}

		// 发送请求
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			errorChan <- fmt.Errorf("发送请求失败: %w", err)
			return
		}
		defer resp.Body.Close()

		// 检查HTTP状态码
		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			errorChan <- fmt.Errorf("API返回错误状态码 %d: %s", resp.StatusCode, string(respBody))
			return
		}

		// 读取流式响应
		reader := resp.Body
		buffer := make([]byte, 1024)

		for {
			n, err := reader.Read(buffer)
			if err != nil {
				if err == io.EOF {
					break
				}
				errorChan <- fmt.Errorf("读取流式响应失败: %w", err)
				return
			}

			if n > 0 {
				resultChan <- string(buffer[:n])
			}
		}
	}()

	return resultChan, errorChan
}

// RetryCall 带重试机制的API调用
func (c *Client) RetryCall(ctx context.Context, req *Request) (*Response, error) {
	var lastErr error

	for i := 0; i <= c.config.RetryCount; i++ {
		resp, err := c.Call(ctx, req)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		// 如果不是最后一次重试，则等待后重试
		if i < c.config.RetryCount {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.config.RetryDelay):
				// 继续下一次重试
			}
		}
	}

	return nil, fmt.Errorf("重试%d次后仍然失败: %w", c.config.RetryCount, lastErr)
}

// GetModel 获取当前配置的模型名称
func (c *Client) GetModel() string {
	return c.config.Model
}

// GetConfig 获取API配置
func (c *Client) GetConfig() config.APIConfig {
	return c.config
}
