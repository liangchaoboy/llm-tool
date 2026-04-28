package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// NewClient 创建新的API客户端
func NewClient(apiConfig config.APIConfig) *Client {
	client := &http.Client{
		Timeout: apiConfig.Timeout,
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
		return parseChatCompletionSSE(resp)
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
func parseChatCompletionSSE(resp *http.Response) (*Response, error) {
	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API返回错误状态码 %d: %s", resp.StatusCode, string(respBody))
	}

	type sseDelta struct {
		Content string `json:"content,omitempty"`
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
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// ignore non-JSON frames
			continue
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
			}
		}

		// usage may appear in last chunk for some providers
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			out.Usage = chunk.Usage
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取流式响应失败: %w", err)
	}

	out.Choices[0].Message.Content = b.String()
	return &out, nil
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
