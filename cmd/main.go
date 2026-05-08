package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/user/llm-test-tool/config"
	"github.com/user/llm-test-tool/internal/api"
	"github.com/user/llm-test-tool/internal/models"
	"github.com/user/llm-test-tool/internal/reporter"
	"github.com/user/llm-test-tool/internal/tester"
)

func main() {
	// 命令行参数解析
	configFile := flag.String("config", "config/config.yaml", "配置文件路径")
	concurrency := flag.Int("concurrency", 0, "并发数（不传则取 config.yaml 的 test.performance.concurrency）")
	requestsPerWorker := flag.Int("requests-per-worker", 0, "每个并发的请求数（不传则取 config.yaml 的 test.performance.requests_per_worker）")
	model := flag.String("model", "", "模型名称（不传则取 config.yaml 的 api.model）")
	apiKey := flag.String("api-key", "", "API密钥（不传则取 config.yaml 的 api.api_key）")
	baseURL := flag.String("base-url", "", "Base URL（不传则取 config.yaml 的 api.base_url）")
	testCaseFile := flag.String("test-case-file", "", "测试用例文件路径（JSON 数组，每条元素为完整的 chat.completions 请求体；不传则取 config.yaml 的 test.performance.test_case_file）")
	outputFormat := flag.String("format", "html", "输出格式: json|html|markdown")
	// -1 表示未指定（沿用 config.yaml / 回退默认值 0）
	retryCount := flag.Int("retry-count", -1, "API 请求重试次数（基准测试建议 0；不传则沿用 config.yaml）")
	flag.Parse()

	// 记录用户显式设置过的 flag，避免 "CLI 默认值恰好等于用户想要的值" 时被 YAML 静默覆盖
	setFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	// 加载配置文件（不存在则用空 Config 走默认值）
	var cfg *config.Config
	if _, err := os.Stat(*configFile); err == nil {
		loaded, err := config.Load(*configFile)
		if err != nil {
			log.Fatalf("加载配置文件失败: %v", err)
		}
		cfg = loaded
	} else {
		cfg = &config.Config{}
	}

	// 合并 API 配置：CLI 显式指定 > YAML > 内置回退
	apiConfig := cfg.API
	if setFlags["api-key"] {
		apiConfig.APIKey = *apiKey
	}
	if setFlags["model"] {
		apiConfig.Model = *model
	}
	if setFlags["base-url"] {
		apiConfig.BaseURL = *baseURL
	}
	if setFlags["retry-count"] && *retryCount >= 0 {
		apiConfig.RetryCount = *retryCount
	}
	if apiConfig.Timeout == 0 {
		// 配置文件不存在时 Load 未被调用，setDefaults 不生效，这里兜底一次
		apiConfig.Timeout = 10 * time.Minute
	}
	if apiConfig.RetryDelay == 0 {
		apiConfig.RetryDelay = 1 * time.Second
	}

	// 合并性能测试配置：CLI 显式指定 > YAML > 兜底
	perfConfig := tester.PerformanceConfig{
		Concurrency:       cfg.Test.Performance.Concurrency,
		RequestsPerWorker: cfg.Test.Performance.RequestsPerWorker,
		TestCaseFile:      cfg.Test.Performance.TestCaseFile,
		Model:             apiConfig.Model,
		APIKey:            apiConfig.APIKey,
		BaseURL:           apiConfig.BaseURL,
	}
	if setFlags["concurrency"] {
		perfConfig.Concurrency = *concurrency
	}
	if setFlags["requests-per-worker"] {
		perfConfig.RequestsPerWorker = *requestsPerWorker
	}
	if setFlags["test-case-file"] {
		perfConfig.TestCaseFile = *testCaseFile
	}
	if perfConfig.Concurrency <= 0 {
		perfConfig.Concurrency = 10
	}
	if perfConfig.RequestsPerWorker <= 0 {
		perfConfig.RequestsPerWorker = 100
	}
	if perfConfig.TestCaseFile == "" {
		fmt.Fprintln(os.Stderr, "错误：未指定测试用例文件。请通过 -test-case-file 或 config.yaml 的 test.performance.test_case_file 提供。")
		flag.Usage()
		os.Exit(2)
	}

	fmt.Printf("开始LLM API性能测试\n")
	fmt.Printf("并发数: %d, 每并发请求数: %d, 总请求数: %d\n",
		perfConfig.Concurrency, perfConfig.RequestsPerWorker, perfConfig.Concurrency*perfConfig.RequestsPerWorker)

	// 创建API客户端：连接池上限与并发数对齐，避免大量短连接重建 TLS
	client := api.NewClient(apiConfig, perfConfig.Concurrency)

	// 创建性能测试器
	perfTester := tester.NewPerformanceTester(client, perfConfig)

	// 运行性能测试
	startTime := time.Now()
	metrics, perRequestResults := perfTester.RunPerformanceTest(context.Background())
	testDuration := time.Since(startTime)

	// 注意：performance.go 内部已经使用实际测试时间计算所有指标
	// 这里记录测试耗时用于显示，所有速率指标已在 performance.go 中正确计算
	// 如果需要，可以使用 testDuration 重新验证计算的一致性

	// 输出性能指标
	fmt.Printf("\n=== 性能测试结果 ===\n")
	fmt.Printf("总请求数: %d  (成功: %d, 成功率: %.2f%%)\n",
		metrics.TotalRequests, metrics.SuccessfulRequests, metrics.SuccessRate)
	fmt.Printf("测试耗时: %v\n", testDuration)

	fmt.Printf("\n-- 主指标：每请求平均 TPS --\n")
	fmt.Printf("平均生成速率 (每请求输出Token/秒，对所有请求求平均): %.2f tokens/s\n",
		metrics.AvgGenerationRate)

	fmt.Printf("\n-- 单请求延迟 --\n")
	fmt.Printf("TTFT (首Token耗时):          %v\n", metrics.TTFT)
	fmt.Printf("TPOT (首Token后每Token时间): %v\n", metrics.TPOT)
	fmt.Printf("平均完整时延:                %v\n", metrics.AvgLatency)
	fmt.Printf("每个请求平均输出Token数:     %.2f\n", metrics.AvgOutputTokens)

	fmt.Printf("\n-- 聚合吞吐（整体视角）--\n")
	fmt.Printf("RPS: %.2f  RPM: %.2f\n", metrics.RPS, metrics.RPM)
	fmt.Printf("Output TPS: %.2f  Output TPM: %.2f\n", metrics.OutputTPS, metrics.OutputTPM)
	fmt.Printf("Total TPS (含Prompt): %.2f  Total TPM: %.2f\n", metrics.TPS, metrics.TPM)

	// 生成测试报告
	//
	// 报告结构：
	//   - perRequestResults：每次真实 HTTP 请求一行（PERF-REQ-XXXX），含 reqid、
	//     起止时间、in/out tokens、TTFT、输出 TPS 等明细；
	//   - aggregate：14 条 PERF-001..014 聚合指标展示行（沿用原展示形态）。
	// 真实请求行放前面，表格读起来就是按时间顺序的用例明细；聚合指标行贴在
	// 后面作为总结。
	aggregate := createPerformanceTestResults(metrics, perfConfig.Concurrency)
	allResults := make([]models.TestResult, 0, len(perRequestResults)+len(aggregate))
	allResults = append(allResults, perRequestResults...)
	allResults = append(allResults, aggregate...)

	// Summary 必须从 metrics 构造，而不是基于 len(allResults) 让 reporter 自动统计——
	// 后者会把 14 行聚合指标也计入 TotalTests，让报告里的"总测试数""成功率"与
	// 控制台打印的真实数字对不上。
	summary := buildPerformanceSummary(metrics, perRequestResults)

	report := reporter.GenerateReportWithSummary(allResults, summary, time.Now())

	filename := fmt.Sprintf("performance-test-report-%s.%s",
		time.Now().Format("20060102-150405"),
		getFileExtension(*outputFormat))

	err := reporter.SaveReport(report, filename, *outputFormat)
	if err != nil {
		log.Fatalf("保存报告失败: %v", err)
	}

	fmt.Printf("\n性能测试完成！报告已保存到: %s\n", filename)
}

// buildPerformanceSummary 用真实请求量构造 Summary。
// MinTime / MaxTime 只看成功请求（失败请求的 Duration 经常是超时上限，会把最大值
// 拉到不真实的高点）；AverageTime 沿用 metrics.AvgLatency（已按成功请求数算过均值）。
func buildPerformanceSummary(metrics *tester.PerformanceMetrics, perRequestResults []models.TestResult) models.Summary {
	s := models.Summary{
		TotalTests:   int(metrics.TotalRequests),
		PassedTests:  int(metrics.SuccessfulRequests),
		FailedTests:  int(metrics.TotalRequests - metrics.SuccessfulRequests),
		SkippedTests: 0,
		ErrorTests:   0,
		SuccessRate:  metrics.SuccessRate,
		AverageTime:  metrics.AvgLatency,
	}

	var minD, maxD time.Duration
	for _, r := range perRequestResults {
		if r.Status != models.Pass || r.Duration <= 0 {
			continue
		}
		if minD == 0 || r.Duration < minD {
			minD = r.Duration
		}
		if r.Duration > maxD {
			maxD = r.Duration
		}
	}
	s.MinTime = minD
	s.MaxTime = maxD
	return s
}

func getFileExtension(format string) string {
	switch format {
	case "json":
		return "json"
	case "html":
		return "html"
	case "markdown":
		return "md"
	default:
		return "html"
	}
}

func createPerformanceTestResults(metrics *tester.PerformanceMetrics, concurrency int) []models.TestResult {
	results := []models.TestResult{
		{
			TestCaseID: "PERF-001",
			Name:       "平均生成速率",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			// AvgGenerationRate 单位是 tokens/second，不是时长，不能放进 Duration。
			Duration: 0,
			Message:  fmt.Sprintf("%.2f tokens/second", metrics.AvgGenerationRate),
		},
		{
			TestCaseID: "PERF-002",
			Name:       "RPS (每秒请求数)",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   0,
			Message:    fmt.Sprintf("%.2f requests/second", metrics.RPS),
		},
		{
			TestCaseID: "PERF-003",
			Name:       "RPM (每分钟请求数)",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   0,
			Message:    fmt.Sprintf("%.2f requests/minute", metrics.RPM),
		},
		{
			TestCaseID: "PERF-004",
			Name:       "TPM (每分钟总Token数)",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   0,
			Message:    fmt.Sprintf("%.2f tokens/minute", metrics.TPM),
		},
		{
			TestCaseID: "PERF-005",
			Name:       "TPS (每秒总Token数)",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   0,
			Message:    fmt.Sprintf("%.2f tokens/second", metrics.TPS),
		},
		{
			TestCaseID: "PERF-006",
			Name:       "Output TPM (每分钟输出Token数)",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   0,
			Message:    fmt.Sprintf("%.2f output tokens/minute", metrics.OutputTPM),
		},
		{
			TestCaseID: "PERF-007",
			Name:       "Output TPS (每秒输出Token数)",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   0,
			Message:    fmt.Sprintf("%.2f output tokens/second", metrics.OutputTPS),
		},
		{
			TestCaseID: "PERF-008",
			Name:       "TTFT (平均首字节耗时)",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   metrics.TTFT,
			Message:    fmt.Sprintf("%v", metrics.TTFT),
		},
		{
			TestCaseID: "PERF-009",
			Name:       "TPOT (每输出Token时间)",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   metrics.TPOT,
			Message:    fmt.Sprintf("%v", metrics.TPOT),
		},
		{
			TestCaseID: "PERF-010",
			Name:       "平均完整时延",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   metrics.AvgLatency,
			Message:    fmt.Sprintf("%v", metrics.AvgLatency),
		},
		{
			TestCaseID: "PERF-011",
			Name:       "请求总数",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   0,
			Message:    fmt.Sprintf("%d", metrics.TotalRequests),
		},
		{
			TestCaseID: "PERF-012",
			Name:       "成功请求数",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   0,
			Message:    fmt.Sprintf("%d", metrics.SuccessfulRequests),
		},
		{
			TestCaseID: "PERF-013",
			Name:       "每输出Token平均字数",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   0,
			Message:    fmt.Sprintf("%.2f", metrics.AvgOutputTokens),
		},
		{
			TestCaseID: "PERF-014",
			Name:       "请求成功率",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   0,
			Message:    fmt.Sprintf("%.2f%%", metrics.SuccessRate),
		},
	}

	// 整体 Metrics 仅挂到主结果（PERF-001）上，避免每一行都写同一份指标。
	// ConcurrentUsers 应该是本次压测的 worker 并发数，不是 RPS。
	if len(results) > 0 {
		results[0].Metrics = models.Metrics{
			ResponseTime:    metrics.AvgLatency,
			Latency:         metrics.AvgLatency,
			Throughput:      metrics.RPS,
			SuccessRate:     metrics.SuccessRate,
			TokenCount:      int(metrics.AvgOutputTokens),
			ConcurrentUsers: concurrency,
		}
	}

	return results
}
