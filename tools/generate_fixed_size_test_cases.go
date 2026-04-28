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
)

type TestCase struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"` // thinking or non_thinking
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Prompt       string  `json:"prompt"`
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
}

// estimateTokenCount estimates tokens from rune length.
// NOTE: the rest of the repo uses the same rough heuristic in generators.
func estimateTokenCount(text string) int {
	return int(float64(len([]rune(text))) / 2.5)
}

func buildBasePrompt(caseType string) string {
	if caseType == "thinking" {
		return "请对以下主题进行系统性分析，给出结构化的思考过程、关键假设、风险点、权衡取舍与可执行建议：在多方约束条件下（预算、时间、人力、合规），为一个跨部门项目制定端到端实施方案。"
	}
	return "请根据要求完成任务：把下面的内容按要点列表总结，并给出一个简短示例。"
}

func expandToTargetTokens(seed string, targetTokens int) string {
	if targetTokens <= 0 {
		return seed
	}
	if estimateTokenCount(seed) >= targetTokens {
		targetChars := int(float64(targetTokens) * 2.5)
		runes := []rune(seed)
		if len(runes) > targetChars {
			return string(runes[:targetChars])
		}
		return seed
	}

	// Use a short, diverse filler and repeat; keep it deterministic-ish for stability.
	filler := strings.Join([]string{
		"请覆盖背景、目标、约束、方案、里程碑、风险、验证与回滚。",
		"输出尽量结构化，使用分点与小标题。",
		"同时给出必要的公式/估算与边界条件。",
		"避免空泛表述，给出可落地的步骤与检查清单。",
	}, " ")

	var b strings.Builder
	b.Grow(len(seed) + int(float64(targetTokens)*3))
	b.WriteString(seed)

	for estimateTokenCount(b.String()) < targetTokens {
		b.WriteString("\n\n")
		b.WriteString(filler)
	}

	// Final trim to close to target
	targetChars := int(float64(targetTokens) * 2.5)
	runes := []rune(b.String())
	if len(runes) > targetChars {
		return string(runes[:targetChars])
	}
	return b.String()
}

func main() {
	var (
		outPath      = flag.String("out", "test-data/fixed-size-test-cases.json", "输出文件路径")
		count        = flag.Int("count", 20, "生成用例数量（建议>=并发数）")
		caseType     = flag.String("type", "thinking", "用例类型：thinking 或 non_thinking")
		inputTokens  = flag.Int("input-tokens", 50000, "目标输入tokens（估算）")
		outputTokens = flag.Int("output-tokens", 1500, "目标输出tokens（作为max_tokens/期望输出）")
		temperature  = flag.Float64("temperature", 0.7, "temperature")
	)
	flag.Parse()

	t := strings.TrimSpace(*caseType)
	if t != "thinking" && t != "non_thinking" {
		fmt.Printf("invalid -type: %s (must be thinking or non_thinking)\n", t)
		os.Exit(2)
	}
	if *count <= 0 {
		fmt.Printf("invalid -count: %d\n", *count)
		os.Exit(2)
	}
	if *inputTokens <= 0 || *outputTokens <= 0 {
		fmt.Printf("invalid token sizes: input=%d output=%d\n", *inputTokens, *outputTokens)
		os.Exit(2)
	}

	rand.Seed(time.Now().UnixNano())
	base := buildBasePrompt(t)

	testCases := make([]TestCase, 0, *count)
	for i := 0; i < *count; i++ {
		seed := fmt.Sprintf("%s\n\n(用例编号=%d，随机因子=%d)", base, i+1, rand.Intn(1_000_000))
		prompt := expandToTargetTokens(seed, *inputTokens)

		testCases = append(testCases, TestCase{
			ID:           fmt.Sprintf("fixed_%s_%d", t, i+1),
			Type:         t,
			InputTokens:  *inputTokens,
			OutputTokens: *outputTokens,
			Prompt:       prompt,
			Temperature:  *temperature,
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
	if err := enc.Encode(testCases); err != nil {
		fmt.Printf("write output failed: %v\n", err)
		os.Exit(1)
	}

	// quick sanity
	actual := estimateTokenCount(testCases[0].Prompt)
	fmt.Printf("wrote %d test cases to %s\n", len(testCases), *outPath)
	fmt.Printf("sample[0] estimated input tokens: target=%d actual=%d (diff=%d)\n",
		*inputTokens, actual, actual-*inputTokens)
}

