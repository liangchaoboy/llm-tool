package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// This tool generates a JSON array where each element is a full chat.completions request body.
// It uses a configurable "generator LLM" to synthesize long random text (~50k tokens by repo heuristic),
// then writes bodies in the target format:
// {
//   "stream": true,
//   "model": "...",
//   "max_tokens": 1500,
//   "messages": [{ "role":"user", "content":[{"text":"...", "type":"text"}]}]
// }

type contentItem struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

type message struct {
	Role    string        `json:"role"`
	Content []contentItem `json:"content"`
}

type requestBody struct {
	Stream    bool      `json:"stream"`
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type genReq struct {
	Model    string      `json:"model"`
	Stream   bool        `json:"stream,omitempty"`
	Messages []genMsg    `json:"messages"`
	MaxTok   int         `json:"max_tokens,omitempty"`
	Extra    interface{} `json:"-"`
}

type genMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type genResp struct {
	Choices []struct {
		Message struct {
			Content interface{} `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func estimateTokenCount(text string) int {
	// Same heuristic used elsewhere in this repo: ~2.5 chars per token.
	return int(float64(len([]rune(text))) / 2.5)
}

func targetRunesForTokens(tokens int) int {
	return int(float64(tokens) * 2.5)
}

func extractContentString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case []interface{}:
		var b strings.Builder
		for _, it := range t {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}
			text, _ := m["text"].(string)
			b.WriteString(text)
		}
		return b.String()
	default:
		return ""
	}
}

func parseChatCompletionSSE(body io.Reader) (string, error) {
	type sseDelta struct {
		Content string `json:"content,omitempty"`
	}
	type sseChoice struct {
		Delta sseDelta `json:"delta"`
	}
	type sseChunk struct {
		Choices []sseChoice `json:"choices"`
	}

	var out strings.Builder
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				out.WriteString(c.Delta.Content)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func doGenCall(ctx context.Context, httpClient *http.Client, baseURL, apiKey string, req genReq) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", baseURL, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// If stream=true, many providers return SSE (text/event-stream).
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBody, _ := io.ReadAll(resp.Body)
			return "", fmt.Errorf("generator api status=%d body=%s", resp.StatusCode, string(respBody))
		}
		return parseChatCompletionSSE(resp.Body)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("generator api status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var gr genResp
	if err := json.Unmarshal(respBody, &gr); err != nil {
		return "", err
	}
	if len(gr.Choices) == 0 {
		return "", fmt.Errorf("generator returned no choices")
	}
	return extractContentString(gr.Choices[0].Message.Content), nil
}

func appendWithRuneLimit(b *strings.Builder, runeCount *int, s string, limit int) {
	if *runeCount >= limit {
		return
	}
	need := limit - *runeCount
	if need <= 0 {
		return
	}
	// Fast path: if within limit, append full string.
	if utf8.RuneCountInString(s) <= need {
		b.WriteString(s)
		*runeCount += utf8.RuneCountInString(s)
		return
	}
	// Slow path: truncate by runes.
	rs := []rune(s)
	if len(rs) > need {
		rs = rs[:need]
	}
	b.WriteString(string(rs))
	*runeCount += len(rs)
}

func buildRandomLongTextViaLLM(ctx context.Context, httpClient *http.Client, genBaseURL, genAPIKey, genModel string, targetTokens int, perCallMaxTokens int, genRequestMaxTokens int, seed int64) (string, error) {
	targetRunes := targetRunesForTokens(targetTokens)

	var b strings.Builder
	b.Grow(targetRunes*2 + 1024)
	runeCount := 0

	r := rand.New(rand.NewSource(seed))

	// A small starter to ensure diversity.
	starter := fmt.Sprintf("生成用于性能测试的随机长文本。seed=%d time=%s\n\n", seed, time.Now().UTC().Format(time.RFC3339))
	appendWithRuneLimit(&b, &runeCount, starter, targetRunes)

	for runeCount < targetRunes {
		remainRunes := targetRunes - runeCount
		remainTokensEst := int(float64(remainRunes) / 2.5)
		wantTokens := perCallMaxTokens
		if remainTokensEst < wantTokens {
			// Ask for a bit more than remaining to compensate for variability, but not too huge.
			wantTokens = remainTokensEst + 512
			if wantTokens < 512 {
				wantTokens = 512
			}
			if wantTokens > perCallMaxTokens {
				wantTokens = perCallMaxTokens
			}
		}

		nonce := fmt.Sprintf("%08x", r.Uint32())
		prompt := strings.Join([]string{
			"请输出一段纯文本（不要 markdown，不要代码块，不要 JSON），内容尽量随机、自然语言为主，中英混合也可以。",
			"不要包含任何解释或标题，直接输出正文。",
			fmt.Sprintf("本段落需要尽量长，目标约 %d tokens。", wantTokens),
			fmt.Sprintf("nonce=%s", nonce),
		}, "\n")

		req := genReq{
			Model:  genModel,
			Stream: true,
			Messages: []genMsg{
				{Role: "user", Content: prompt},
			},
			// Some platforms enforce their own limits; we still set max_tokens explicitly as requested.
			// By default this is set to -input-tokens (e.g. 50000).
			MaxTok: genRequestMaxTokens,
		}

		chunk, err := doGenCall(ctx, httpClient, genBaseURL, genAPIKey, req)
		if err != nil {
			return "", err
		}
		if chunk == "" {
			return "", fmt.Errorf("generator returned empty content")
		}

		// Separate chunks to avoid accidental merging.
		appendWithRuneLimit(&b, &runeCount, "\n\n", targetRunes)
		appendWithRuneLimit(&b, &runeCount, chunk, targetRunes)
	}

	return b.String(), nil
}

func main() {
	var (
		outPath          = flag.String("out", "sample.json", "输出案例文件路径（JSON数组）")
		count            = flag.Int("count", 2, "生成案例数量")
		targetInputTokens = flag.Int("input-tokens", 50000, "每条case的输入tokens（按项目估算口径）")
		targetModel      = flag.String("target-model", "moonshotai/kimi-k2.5", "输出request body里使用的 model")
		targetMaxTokens  = flag.Int("target-max-tokens", 1500, "输出request body里使用的 max_tokens")
		targetStream     = flag.Bool("target-stream", true, "输出request body里使用的 stream")
		concurrency      = flag.Int("concurrency", 1, "生成case的并发数（注意目标生成模型限流）")

		genBaseURL       = flag.String("gen-base-url", "https://api.qnaigc.com/v1/chat/completions", "生成用大模型的 baseUrl（chat completions）")
		genAPIKey        = flag.String("gen-api-key", "", "生成用大模型的 apiKey（Bearer）")
		genModel         = flag.String("gen-model", "", "生成用大模型的 model id（必填）")
		genTimeoutSec    = flag.Int("gen-timeout-sec", 600, "生成用大模型单次请求超时（秒）")
		genPerCallMaxTok = flag.Int("gen-per-call-max-tokens", 8000, "生成用大模型每次调用的 max_tokens（会分段拼到50k）")
		seed             = flag.Int64("seed", 0, "随机种子（0表示用当前时间）")
	)
	flag.Parse()

	if *count <= 0 {
		fmt.Println("invalid -count")
		os.Exit(2)
	}
	if *targetInputTokens <= 0 {
		fmt.Println("invalid -input-tokens")
		os.Exit(2)
	}
	if *concurrency <= 0 {
		fmt.Println("invalid -concurrency")
		os.Exit(2)
	}
	if strings.TrimSpace(*genModel) == "" {
		fmt.Println("missing -gen-model")
		flag.Usage()
		os.Exit(2)
	}

	actualSeed := *seed
	if actualSeed == 0 {
		actualSeed = time.Now().UnixNano()
	}

	httpClient := &http.Client{Timeout: time.Duration(*genTimeoutSec) * time.Second}
	ctx := context.Background()

	results := make([]requestBody, *count)
	jobs := make(chan int)
	errCh := make(chan error, 1)

	var printMu sync.Mutex
	worker := func() {
		for idx := range jobs {
			caseSeed := actualSeed + int64(idx)*1000003
			text, err := buildRandomLongTextViaLLM(ctx, httpClient, *genBaseURL, *genAPIKey, *genModel, *targetInputTokens, *genPerCallMaxTok, *targetInputTokens, caseSeed)
			if err != nil {
				select {
				case errCh <- fmt.Errorf("generate case %d failed: %w", idx+1, err):
				default:
				}
				return
			}

			results[idx] = requestBody{
				Stream:    *targetStream,
				Model:     *targetModel,
				MaxTokens: *targetMaxTokens,
				Messages: []message{
					{
						Role: "user",
						Content: []contentItem{
							{Text: text, Type: "text"},
						},
					},
				},
			}

			est := estimateTokenCount(text)
			printMu.Lock()
			fmt.Printf("case %d/%d generated: estimated_input_tokens=%d target=%d diff=%d\n", idx+1, *count, est, *targetInputTokens, est-*targetInputTokens)
			printMu.Unlock()
		}
	}

	var wg sync.WaitGroup
	workerCount := *concurrency
	if workerCount > *count {
		workerCount = *count
	}
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			worker()
		}()
	}

	go func() {
		defer close(jobs)
		for i := 0; i < *count; i++ {
			select {
			case err := <-errCh:
				_ = err
				return
			default:
			}
			jobs <- i
		}
	}()

	wg.Wait()
	select {
	case err := <-errCh:
		fmt.Println(err.Error())
		os.Exit(1)
	default:
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil && filepath.Dir(*outPath) != "." {
		fmt.Printf("mkdir failed: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(*outPath)
	if err != nil {
		fmt.Printf("create output failed: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		fmt.Printf("write output failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("wrote %d cases to %s\n", len(results), *outPath)
}

