//go:build tools
// +build tools

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// TestCase matches the existing test case JSON schema used by this repo.
type TestCase struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"` // thinking or non_thinking
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Prompt       string  `json:"prompt"`
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
}

// estimateTokenCount keeps consistent with other generators in this repo:
// average ~2.5 chars per token (rough heuristic).
func estimateTokenCount(text string) int {
	return int(float64(len([]rune(text))) / 2.5)
}

func targetCharLenFromTokens(tokens int) int {
	// Keep consistent with estimateTokenCount (tokens ~= runes/2.5)
	return int(float64(tokens) * 2.5)
}

func randomFrom[T any](r *rand.Rand, items []T) T {
	return items[r.Intn(len(items))]
}

func randomAlphaNum(r *rand.Rand, n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		b.WriteByte(alphabet[r.Intn(len(alphabet))])
	}
	return b.String()
}

func randomPrompt(r *rand.Rand, targetTokens int) string {
	// Mix of Chinese + English sentences to create variation.
	// The goal is size & randomness, not semantics.
	openers := []string{
		"你是一个严格的助手，请按要求输出结果。",
		"你将收到一段长输入，请在理解后完成任务。",
		"以下是背景信息与约束条件，请不要遗漏任何细节。",
		"请注意：这是一条用于性能测试的输入。",
	}

	sections := []string{
		"背景：我们在做一次端到端性能评估，包含网络、排队、推理与输出阶段。",
		"约束：请确保回答结构化；使用要点、小标题；避免重复。",
		"目标：给出一个可执行方案，包括里程碑、风险清单与验收标准。",
		"评估：分别从性能、成本、可靠性、安全、可维护性角度给出建议。",
		"注意事项：如果信息不足，请先列出需要澄清的问题，再提出假设继续。",
		"输出格式：先给 TL;DR，再给详细展开，最后给 checklist。",
		"English section: Provide a concise summary, then a detailed plan, then a risk register.",
		"More English: Include trade-offs, assumptions, and a simple estimation model.",
	}

	bullets := []string{
		"- 关键点：吞吐、延迟、错误率、重试、超时、限流与熔断。",
		"- 关键点：输入输出长度、并发、连接复用、队列、缓存与批处理。",
		"- 关键点：可观测性（日志、指标、追踪）、告警与回滚策略。",
		"- 关键点：安全与合规（脱敏、访问控制、审计、数据保留）。",
		"- 关键点：边界条件与失败模式（429/5xx/网络抖动/冷启动）。",
	}

	noiseTemplates := []string{
		"片段=%s；序号=%d；时间=%s；rand=%d。",
		"[chunk %d] id=%s ts=%s seed=%d",
		"data:%s/%s/%s",
	}

	targetRunes := targetCharLenFromTokens(targetTokens)
	var b strings.Builder
	b.Grow(targetRunes*2 + 512) // rough pre-alloc; bytes may be larger for CJK

	// Start with a short header to anchor variety.
	runeCount := 0
	appendChunk := func(s string) {
		b.WriteString(s)
		runeCount += utf8.RuneCountInString(s)
	}

	appendChunk(randomFrom(r, openers))
	appendChunk("\n\n")
	appendChunk(fmt.Sprintf("trace_id=%s\n", randomAlphaNum(r, 24)))
	appendChunk(fmt.Sprintf("session=%s\n", randomAlphaNum(r, 16)))
	appendChunk("\n")

	// Add sections until reaching target size.
	for runeCount < targetRunes {
		switch r.Intn(4) {
		case 0:
			appendChunk(randomFrom(r, sections))
			appendChunk("\n\n")
		case 1:
			// Bullet block
			n := 3 + r.Intn(6)
			for i := 0; i < n; i++ {
				appendChunk(randomFrom(r, bullets))
				appendChunk("\n")
			}
			appendChunk("\n")
		case 2:
			// Noisy line
			tpl := randomFrom(r, noiseTemplates)
			now := time.Unix(r.Int63n(time.Now().Unix()), 0).UTC().Format(time.RFC3339)
			line := fmt.Sprintf(tpl, randomAlphaNum(r, 12), r.Intn(1_000_000), now, r.Intn(1_000_000))
			// Some templates may not consume all verbs; keep safe by trimming
			line = strings.ReplaceAll(line, "%!s(MISSING)", randomAlphaNum(r, 8))
			line = strings.ReplaceAll(line, "%!d(MISSING)", fmt.Sprintf("%d", r.Intn(1_000_000)))
			appendChunk(line)
			appendChunk("\n")
		default:
			// Paragraph with repeated but varied tokens
			repeat := 2 + r.Intn(5)
			for i := 0; i < repeat; i++ {
				appendChunk("补充说明：")
				appendChunk(randomAlphaNum(r, 10))
				appendChunk(" / ")
				appendChunk(randomAlphaNum(r, 10))
				appendChunk(" / ")
				appendChunk(randomAlphaNum(r, 10))
				appendChunk("。")
			}
			appendChunk("\n\n")
		}
	}

	// Trim to exactly targetRunes to keep token estimate stable.
	runes := []rune(b.String())
	if len(runes) > targetRunes {
		runes = runes[:targetRunes]
	}
	return string(runes)
}

func main() {
	var (
		outPath      = flag.String("out", "test-data/random-50k-1p5k.json", "输出用例文件路径")
		count        = flag.Int("count", 600, "生成用例数量")
		inputTokens  = flag.Int("input-tokens", 50000, "每条用例输入tokens（按估算口径）")
		outputTokens = flag.Int("output-tokens", 1500, "每条用例输出tokens（也用于max_tokens）")
		seed         = flag.Int64("seed", 0, "随机种子（0表示用当前时间）")
		typeMode     = flag.String("type-mode", "random", "类型模式：random|thinking|non_thinking")
	)
	flag.Parse()

	if *count <= 0 {
		fmt.Printf("invalid -count: %d\n", *count)
		os.Exit(2)
	}
	if *inputTokens <= 0 || *outputTokens <= 0 {
		fmt.Printf("invalid token sizes: input=%d output=%d\n", *inputTokens, *outputTokens)
		os.Exit(2)
	}

	actualSeed := *seed
	if actualSeed == 0 {
		actualSeed = time.Now().UnixNano()
	}
	r := rand.New(rand.NewSource(actualSeed))

	cases := make([]TestCase, 0, *count)
	for i := 0; i < *count; i++ {
		var t string
		switch strings.TrimSpace(*typeMode) {
		case "thinking":
			t = "thinking"
		case "non_thinking":
			t = "non_thinking"
		default:
			if r.Intn(2) == 0 {
				t = "thinking"
			} else {
				t = "non_thinking"
			}
		}

		temp := 0.7
		if t == "non_thinking" {
			temp = 0.3
		}

		prompt := randomPrompt(r, *inputTokens)
		cases = append(cases, TestCase{
			ID:           fmt.Sprintf("rand_%s_%d", t, i+1),
			Type:         t,
			InputTokens:  *inputTokens,
			OutputTokens: *outputTokens,
			Prompt:       prompt,
			Temperature:  temp,
			MaxTokens:    *outputTokens,
		})
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
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
	if err := enc.Encode(cases); err != nil {
		fmt.Printf("write output failed: %v\n", err)
		os.Exit(1)
	}

	// Sanity output
	sampleTokens := estimateTokenCount(cases[0].Prompt)
	fmt.Printf("wrote %d test cases to %s (seed=%d)\n", len(cases), *outPath, actualSeed)
	fmt.Printf("sample[0] estimated input tokens: target=%d actual=%d diff=%d\n",
		*inputTokens, sampleTokens, sampleTokens-*inputTokens)
}

