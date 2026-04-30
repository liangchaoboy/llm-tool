package tester

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/user/llm-test-tool/internal/api"
)

// PerformanceTester 性能测试器
type PerformanceTester struct {
	client    *api.Client
	config    PerformanceConfig
	testCases []json.RawMessage
}

// PerformanceConfig 性能测试配置
type PerformanceConfig struct {
	Concurrency       int    `json:"concurrency"`
	RequestsPerWorker int    `json:"requests_per_worker"`
	Model             string `json:"model"`
	APIKey            string `json:"api_key"`
	BaseURL           string `json:"base_url"`
	TestCaseFile      string `json:"test_case_file"`
}

// PerformanceMetrics 性能指标
type PerformanceMetrics struct {
	AvgGenerationRate  float64       `json:"avg_generation_rate"` // 平均生成速率 (tokens/second)
	RPM                float64       `json:"rpm"`                 // Requests Per Minute
	RPS                float64       `json:"rps"`                 // Requests Per Second
	TPM                float64       `json:"tpm"`                 // Total Tokens Per Minute
	TPS                float64       `json:"tps"`                 // Total Tokens Per Second
	OutputTPM          float64       `json:"output_tpm"`          // Output Tokens Per Minute
	OutputTPS          float64       `json:"output_tps"`          // Output Tokens Per Second
	TTFT               time.Duration `json:"ttft"`                // Time To First Byte (平均首字节耗时)
	TPOT               time.Duration `json:"tpot"`                // Time Per Output Token
	AvgLatency         time.Duration `json:"avg_latency"`         // 平均完整时延
	TotalRequests      int64         `json:"total_requests"`      // 请求总数
	SuccessfulRequests int64         `json:"successful_requests"` // 成功请求数
	AvgOutputTokens    float64       `json:"avg_output_tokens"`   // 每个输出token的平均字数
	SuccessRate        float64       `json:"success_rate"`        // 请求成功率
}

// NewPerformanceTester 创建性能测试器
func NewPerformanceTester(client *api.Client, config PerformanceConfig) *PerformanceTester {
	pt := &PerformanceTester{
		client: client,
		config: config,
	}

	// 加载测试用例
	if err := pt.loadTestCases(); err != nil {
		fmt.Printf("加载测试用例失败: %v\n", err)
	}

	return pt
}

// loadTestCases 加载测试用例
//
// 用例文件应为 JSON 数组，每个元素是"完整请求 body"，例如：
//
//	[
//	  {
//	    "stream": true,
//	    "model": "moonshotai/kimi-k2.5",
//	    "max_tokens": 1500,
//	    "messages": [ {"role":"user", "content":[{"text":"hi","type":"text"}]} ]
//	  }
//	]
//
// 请求体会原样透传给 API，不做任何字段改写。
func (pt *PerformanceTester) loadTestCases() error {
	data, err := os.ReadFile(pt.config.TestCaseFile)
	if err != nil {
		return fmt.Errorf("读取测试用例文件失败: %w", err)
	}

	var testCases []json.RawMessage
	if err := json.Unmarshal(data, &testCases); err != nil {
		return fmt.Errorf("解析测试用例文件失败: %w", err)
	}

	pt.testCases = testCases
	return nil
}

// RunPerformanceTest 运行性能测试
func (pt *PerformanceTester) RunPerformanceTest(ctx context.Context) *PerformanceMetrics {
	fmt.Printf("开始性能测试，使用并发数: %d，每并发请求数: %d\n",
		pt.config.Concurrency, pt.config.RequestsPerWorker)

	if cfg := pt.client.GetConfig(); cfg.RetryCount > 0 {
		fmt.Printf("⚠️  当前 retry_count=%d；基准测试时重试会掩盖真实延迟与失败率，建议设置为 0。\n",
			cfg.RetryCount)
	}

	// 记录测试开始时间
	testStartTime := time.Now()

	var totalRequests int64
	var successfulRequests int64 // 新增成功请求数统计
	var totalTokens int64
	var totalOutputTokens int64
	var totalLatency int64
	var ttftSum int64
	var ttftSamples int64 // 实际测到首 token 的请求数（用于 TTFT 均值分母）
	var tpotSum int64
	var tpotSamples int64 // outputTokens>=2 的请求数（用于 TPOT 均值分母）
	var genRateSum float64
	var genRateSamples int64 // outputTokens>0 的请求数（用于 AvgGenerationRate 分母）
	var genRateMutex sync.Mutex
	var caseCursor int64 // 全局游标，确保多 worker 间均匀轮询所有用例

	var wg sync.WaitGroup

	// 启动并发测试
	for i := 0; i < pt.config.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < pt.config.RequestsPerWorker; j++ {
				// 用全局 atomic 游标轮询，避免多 worker 重复抽同一批用例
				idx := atomic.AddInt64(&caseCursor, 1) - 1
				reqBody := pt.selectTestCase(int(idx))

				startTime := time.Now()

				// TTFT 由 SSE 解析器在收到第一个含内容的 data 帧时打点返回，
				// 这里拿到的是真正的 Time To First Token，而不是整个请求耗时。
				resp, ttft, err := pt.client.RetryCallRawWithTTFT(ctx, reqBody, true)

				requestLatency := time.Since(startTime)

				if err != nil {
					fmt.Printf("请求失败: %v\n", err)
					atomic.AddInt64(&totalRequests, 1) // 即使失败也计入总请求数
					continue
				}

				// 计算输出token数 - 优先使用API返回的真实token数
				outputTokens := 0
				if resp.Usage.CompletionTokens > 0 {
					// 使用API返回的真实输出token数
					outputTokens = resp.Usage.CompletionTokens
				} else if len(resp.Choices) > 0 {
					// 降级方案：如果API没有返回token数，使用字符数估算
					contentStr := pt.extractContentString(resp.Choices[0].Message.Content)
					outputTokens = len([]rune(contentStr))
				}

				// TPOT (Time Per Output Token) = 首字节之后生成每个输出 token 的平均时间
				// 公式：(总耗时 - TTFT) / (输出token数 - 1)
				// 用 outputTokens-1 是因为 TTFT 已经覆盖了"第一个 token"的生成。
				var tpot time.Duration
				if outputTokens > 1 && requestLatency > ttft {
					tpot = (requestLatency - ttft) / time.Duration(outputTokens-1)
				}

				// 计算输入token数 - 优先使用API返回的真实token数
				inputTokens := 0
				if resp.Usage.PromptTokens > 0 {
					inputTokens = resp.Usage.PromptTokens
				} else {
					// 降级方案：使用请求 body 字符数估算（粗略）
					inputTokens = len([]rune(string(reqBody)))
				}

				// 计算该请求的生成速率（输出token数/该请求耗时）
				var requestGenerationRate float64
				if requestLatency.Seconds() > 0 && outputTokens > 0 {
					requestGenerationRate = float64(outputTokens) / requestLatency.Seconds()
				}

				// 更新统计
				atomic.AddInt64(&totalRequests, 1)
				atomic.AddInt64(&successfulRequests, 1)
				atomic.AddInt64(&totalTokens, int64(inputTokens+outputTokens))
				atomic.AddInt64(&totalOutputTokens, int64(outputTokens))
				atomic.AddInt64(&totalLatency, requestLatency.Nanoseconds())

				// TTFT: 只在解析器真测到首 token 时才计入均值（ttft==0 表示该请求没产生 content 帧）
				if ttft > 0 {
					atomic.AddInt64(&ttftSum, ttft.Nanoseconds())
					atomic.AddInt64(&ttftSamples, 1)
				}
				// TPOT: 需要至少 2 个输出 token 才有意义
				if tpot > 0 {
					atomic.AddInt64(&tpotSum, tpot.Nanoseconds())
					atomic.AddInt64(&tpotSamples, 1)
				}
				// 单请求生成速率: 只在产生了输出 token 时才计入均值
				if requestGenerationRate > 0 {
					genRateMutex.Lock()
					genRateSum += requestGenerationRate
					genRateSamples++
					genRateMutex.Unlock()
				}

				// 显示进度
				currentTotal := atomic.LoadInt64(&totalRequests)
				if currentTotal%100 == 0 {
					fmt.Printf("已完成 %d 个请求\n", currentTotal)
				}
			}
		}(i)
	}

	wg.Wait()

	// 计算实际测试持续时间
	actualTestDuration := time.Since(testStartTime)

	// 计算性能指标
	metrics := &PerformanceMetrics{}

	totalRequestCount := atomic.LoadInt64(&totalRequests)
	successfulRequestCount := atomic.LoadInt64(&successfulRequests)
	if totalRequestCount > 0 {
		metrics.TotalRequests = totalRequestCount
		metrics.SuccessfulRequests = successfulRequestCount

		if successfulRequestCount > 0 {
			metrics.AvgLatency = time.Duration(atomic.LoadInt64(&totalLatency) / successfulRequestCount)
		}

		// TTFT / TPOT / AvgGenerationRate 各自用自己的有效样本数作分母，
		// 避免 outputTokens==0 或无 content 帧的请求把均值拉低。
		if n := atomic.LoadInt64(&ttftSamples); n > 0 {
			metrics.TTFT = time.Duration(atomic.LoadInt64(&ttftSum) / n)
		}
		if n := atomic.LoadInt64(&tpotSamples); n > 0 {
			metrics.TPOT = time.Duration(atomic.LoadInt64(&tpotSum) / n)
		}

		// 使用实际的测试运行时间计算速率指标
		testDurationSeconds := actualTestDuration.Seconds()
		if testDurationSeconds > 0 {
			metrics.RPS = float64(successfulRequestCount) / testDurationSeconds
			metrics.RPM = metrics.RPS * 60

			totalTokens := atomic.LoadInt64(&totalTokens)
			totalOutputTokens := atomic.LoadInt64(&totalOutputTokens)
			metrics.TPS = float64(totalTokens) / testDurationSeconds
			metrics.TPM = metrics.TPS * 60
			metrics.OutputTPS = float64(totalOutputTokens) / testDurationSeconds
			metrics.OutputTPM = metrics.OutputTPS * 60

			if successfulRequestCount > 0 {
				metrics.AvgOutputTokens = float64(totalOutputTokens) / float64(successfulRequestCount)
			}
		}

		genRateMutex.Lock()
		if genRateSamples > 0 {
			metrics.AvgGenerationRate = genRateSum / float64(genRateSamples)
		}
		genRateMutex.Unlock()

		metrics.SuccessRate = float64(successfulRequestCount) / float64(totalRequestCount) * 100.0
	}

	return metrics
}

// selectTestCase 按索引轮询选择一个请求 body（原样透传给 API）
func (pt *PerformanceTester) selectTestCase(requestIndex int) json.RawMessage {
	if len(pt.testCases) == 0 {
		return nil
	}
	idx := requestIndex % len(pt.testCases)
	if idx < 0 {
		idx += len(pt.testCases)
	}
	return pt.testCases[idx]
}

// extractContentString 从Message.Content中提取字符串内容
// 支持字符串格式和数组格式
func (pt *PerformanceTester) extractContentString(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		// 处理数组格式，提取所有text字段
		var result string
		for _, item := range v {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if text, ok := itemMap["text"].(string); ok {
					result += text
				}
			}
		}
		return result
	case api.ContentArray:
		// 处理ContentArray类型
		var result string
		for _, item := range v {
			result += item.Text
		}
		return result
	default:
		// 尝试转换为字符串
		if str, ok := v.(string); ok {
			return str
		}
		return ""
	}
}

// RunPerformanceTestWithDetailedMetrics 运行性能测试并返回详细指标
func (tr *TestRunner) RunPerformanceTestWithDetailedMetrics(ctx context.Context, perfConfig PerformanceConfig) *PerformanceMetrics {
	client := tr.client // 使用现有的客户端
	perfTester := NewPerformanceTester(client, perfConfig)

	return perfTester.RunPerformanceTest(ctx)
}
