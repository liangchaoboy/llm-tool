package tester

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sync"
	"sync/atomic"
	"time"

	"github.com/user/llm-test-tool/internal/api"
)

// PerformanceTester 性能测试器
type PerformanceTester struct {
	client    *api.Client
	config    PerformanceConfig
	testCases []TestCase
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

// TestCase 测试用例结构
type TestCase struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"` // thinking or non_thinking
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Prompt       string  `json:"prompt"`
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
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
func (pt *PerformanceTester) loadTestCases() error {
	data, err := ioutil.ReadFile(pt.config.TestCaseFile)
	if err != nil {
		return fmt.Errorf("读取测试用例文件失败: %w", err)
	}

	var testCases []TestCase
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

	// 记录测试开始时间
	testStartTime := time.Now()

	var totalRequests int64
	var successfulRequests int64 // 新增成功请求数统计
	var totalTokens int64
	var totalOutputTokens int64
	var totalLatency int64
	var ttftSum int64
	var tpotSum int64
	var totalOutputTokenCount int64
	var totalGenerationRateSum float64 // 累计每个请求的生成速率（token/秒）
	var generationRateMutex sync.Mutex // 保护totalGenerationRateSum的并发访问

	var wg sync.WaitGroup

	// 启动并发测试
	for i := 0; i < pt.config.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < pt.config.RequestsPerWorker; j++ {
				// 选择测试用例 (遵循 thinking:non-thinking = 1:4 的比例)
				testCase := pt.selectTestCase(j)

				startTime := time.Now()

				// 构建请求
				req := pt.buildRequest(testCase)

				// 记录TTFT (Time To First Byte - 平均首字节耗时)
				// 注意：由于API是同步调用，这里实际测量的是整个API调用的耗时
				ttftStart := time.Now()
				resp, err := pt.client.RetryCall(ctx, req)
				ttft := time.Since(ttftStart)

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

				// 计算TPOT (Time Per Output Token)
				var tpot time.Duration
				if outputTokens > 0 {
					tpot = ttft / time.Duration(outputTokens)
				}

				// 计算输入token数 - 优先使用API返回的真实token数
				inputTokens := 0
				if resp.Usage.PromptTokens > 0 {
					inputTokens = resp.Usage.PromptTokens
				} else {
					// 降级方案：使用字符数估算
					inputTokens = len([]rune(testCase.Prompt))
				}

				// 计算该请求的生成速率（输出token数/该请求耗时）
				var requestGenerationRate float64
				if requestLatency.Seconds() > 0 && outputTokens > 0 {
					requestGenerationRate = float64(outputTokens) / requestLatency.Seconds()
				}

				// 更新统计
				atomic.AddInt64(&totalRequests, 1)
				atomic.AddInt64(&successfulRequests, 1) // 成功请求计数
				atomic.AddInt64(&totalTokens, int64(inputTokens+outputTokens))
				atomic.AddInt64(&totalOutputTokens, int64(outputTokens))
				atomic.AddInt64(&totalLatency, requestLatency.Nanoseconds())
				atomic.AddInt64(&ttftSum, ttft.Nanoseconds())
				atomic.AddInt64(&tpotSum, tpot.Nanoseconds())
				atomic.AddInt64(&totalOutputTokenCount, int64(outputTokens))
				
				// 累计每个请求的生成速率（使用mutex保护float64的并发访问）
				generationRateMutex.Lock()
				totalGenerationRateSum += requestGenerationRate
				generationRateMutex.Unlock()

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
			metrics.AvgLatency = time.Duration(atomic.LoadInt64(&totalLatency) / successfulRequestCount) // 只对成功请求计算平均时延
			metrics.TTFT = time.Duration(atomic.LoadInt64(&ttftSum) / successfulRequestCount)
			metrics.TPOT = time.Duration(atomic.LoadInt64(&tpotSum) / successfulRequestCount)
		}

		// 使用实际的测试运行时间计算速率指标
		testDurationSeconds := actualTestDuration.Seconds()
		if testDurationSeconds > 0 {
			// 这些计算基于实际的测试运行时间
			metrics.RPS = float64(successfulRequestCount) / testDurationSeconds // 使用成功请求数计算RPS
			metrics.RPM = metrics.RPS * 60

			totalTokens := atomic.LoadInt64(&totalTokens)
			totalOutputTokens := atomic.LoadInt64(&totalOutputTokens)
			metrics.TPS = float64(totalTokens) / testDurationSeconds
			metrics.TPM = metrics.TPS * 60
			metrics.OutputTPS = float64(totalOutputTokens) / testDurationSeconds
			metrics.OutputTPM = metrics.OutputTPS * 60

			if successfulRequestCount > 0 {
				metrics.AvgOutputTokens = float64(totalOutputTokens) / float64(successfulRequestCount) // 基于成功请求计算
			}
			// AvgGenerationRate 是每个请求的平均生成速率（每个请求的输出token数/该请求耗时，然后对所有请求求平均）
			generationRateMutex.Lock()
			if successfulRequestCount > 0 {
				metrics.AvgGenerationRate = totalGenerationRateSum / float64(successfulRequestCount)
			}
			generationRateMutex.Unlock()
		}

		// 计算成功率
		metrics.SuccessRate = float64(successfulRequestCount) / float64(totalRequestCount) * 100.0
	}

	return metrics
}

// selectTestCase 根据比例选择测试用例
func (pt *PerformanceTester) selectTestCase(requestIndex int) TestCase {
	// 实现 thinking:non-thinking = 1:4 的比例
	// 每5个请求中，1个是thinking，4个是非thinking

	// 统计thinking和non-thinking用例
	var thinkingCases []TestCase
	var nonThinkingCases []TestCase

	for _, tc := range pt.testCases {
		if tc.Type == "thinking" {
			thinkingCases = append(thinkingCases, tc)
		} else {
			nonThinkingCases = append(nonThinkingCases, tc)
		}
	}

	// 根据索引选择用例
	if (requestIndex%5) == 0 && len(thinkingCases) > 0 {
		// 选择thinking用例
		return thinkingCases[(requestIndex/5)%len(thinkingCases)]
	} else {
		// 选择non-thinking用例
		return nonThinkingCases[(requestIndex%len(nonThinkingCases))%len(nonThinkingCases)]
	}
}

// buildRequest 构建API请求，根据模型类型和测试用例类型自动处理thinking参数和消息格式
func (pt *PerformanceTester) buildRequest(testCase TestCase) *api.Request {
	req := &api.Request{
		Model:       pt.config.Model,
		MaxTokens:   testCase.MaxTokens,
		Temperature: testCase.Temperature,
	}

	// 检查是否是deepseek思考模型
	isDeepSeekThinkingModel := pt.isDeepSeekThinkingModel(pt.config.Model)
	isThinkingCase := testCase.Type == "thinking"

	// 如果是deepseek思考模型，使用新的消息格式和thinking参数
	if isDeepSeekThinkingModel {
		// 使用数组格式的消息
		req.Messages = []api.Message{
			{
				Role: "user",
				Content: api.ContentArray{
					{
						Text: testCase.Prompt,
						Type: "text",
					},
				},
			},
		}

		// 根据测试用例类型设置thinking参数
		if isThinkingCase {
			req.Thinking = &api.Thinking{Type: "enabled"}
		} else {
			req.Thinking = &api.Thinking{Type: "disabled"}
		}
	} else {
		// 使用传统的字符串格式消息
		req.Messages = []api.Message{
			{
				Role:    "user",
				Content: testCase.Prompt,
			},
		}
	}

	return req
}

// isDeepSeekThinkingModel 检查是否是deepseek思考模型
func (pt *PerformanceTester) isDeepSeekThinkingModel(model string) bool {
	// 检查模型名称是否包含 deepseek-v3.2 或 deepseek/deepseek-v3.2
	return model == "deepseek/deepseek-v3.2-251201" || 
		   model == "deepseek-v3.2-251201" ||
		   (len(model) > 8 && model[:9] == "deepseek/") ||
		   (len(model) > 7 && model[:8] == "deepseek")
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
