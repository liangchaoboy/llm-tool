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
	concurrency := flag.Int("concurrency", 100, "并发数")
	requestsPerWorker := flag.Int("requests-per-worker", 500, "每个并发的请求数")
	model := flag.String("model", "gpt-3.5-turbo", "模型名称")
	apiKey := flag.String("api-key", "", "API密钥")
	baseURL := flag.String("base-url", "https://api.openai.com/v1/chat/completions", "Base URL")
	testCaseFile := flag.String("test-case-file", "test-data/test-cases.json", "测试用例文件路径")
	outputFormat := flag.String("format", "html", "输出格式: json|html|markdown")
	flag.Parse()

	fmt.Printf("开始LLM API性能测试\n")
	fmt.Printf("并发数: %d, 每并发请求数: %d, 总请求数: %d\n",
		*concurrency, *requestsPerWorker, *concurrency**requestsPerWorker)

	// 创建性能测试配置
	perfConfig := tester.PerformanceConfig{
		Concurrency:       *concurrency,
		RequestsPerWorker: *requestsPerWorker,
		Model:             *model,
		APIKey:            *apiKey,
		BaseURL:           *baseURL,
		TestCaseFile:      *testCaseFile,
	}

	// 如果提供了配置文件，尝试从中加载API信息
	var apiConfig config.APIConfig
	if _, err := os.Stat(*configFile); err == nil {
		cfg, err := config.Load(*configFile)
		if err == nil {
			apiConfig = cfg.API
			// 如果命令行参数未指定，则使用配置文件中的值
			if *apiKey == "" {
				apiConfig.APIKey = cfg.API.APIKey
			}
			if *model == "gpt-3.5-turbo" && cfg.API.Model != "" {
				apiConfig.Model = cfg.API.Model
			}
			if *baseURL == "https://api.openai.com/v1/chat/completions" && cfg.API.BaseURL != "" {
				apiConfig.BaseURL = cfg.API.BaseURL
			}
		}
	} else {
		// 如果配置文件不存在，使用命令行参数创建API配置
		apiConfig = config.APIConfig{
			BaseURL:    *baseURL,
			APIKey:     *apiKey,
			Model:      *model,
			Timeout:    120 * time.Second,
			RetryCount: 3,
			RetryDelay: 1 * time.Second,
		}
	}

	// 创建API客户端
	client := api.NewClient(apiConfig)

	// 创建性能测试器
	perfTester := tester.NewPerformanceTester(client, perfConfig)

	// 运行性能测试
	startTime := time.Now()
	metrics := perfTester.RunPerformanceTest(context.Background())
	testDuration := time.Since(startTime)

	// 注意：performance.go 内部已经使用实际测试时间计算所有指标
	// 这里记录测试耗时用于显示，所有速率指标已在 performance.go 中正确计算
	// 如果需要，可以使用 testDuration 重新验证计算的一致性

	// 输出性能指标
	fmt.Printf("\n=== 性能测试结果 ===\n")
	fmt.Printf("总请求数: %d\n", metrics.TotalRequests)
	fmt.Printf("测试耗时: %v\n", testDuration)
	fmt.Printf("平均响应时延: %v\n", metrics.AvgLatency)
	fmt.Printf("平均每输出Token字数: %.2f\n", metrics.AvgOutputTokens)
	fmt.Printf("RPS (每秒请求数): %.2f\n", metrics.RPS)
	fmt.Printf("RPM (每分钟请求数): %.2f\n", metrics.RPM)
	fmt.Printf("TPS (每秒总Token数): %.2f\n", metrics.TPS)
	fmt.Printf("TPM (每分钟总Token数): %.2f\n", metrics.TPM)
	fmt.Printf("Output TPS (每秒输出Token数): %.2f\n", metrics.OutputTPS)
	fmt.Printf("Output TPM (每分钟输出Token数): %.2f\n", metrics.OutputTPM)
	fmt.Printf("TTFT (平均首字节耗时): %v\n", metrics.TTFT)
	fmt.Printf("TPOT (每输出Token时间): %v\n", metrics.TPOT)
	fmt.Printf("平均生成速率 (输出Token/秒): %.2f\n", metrics.AvgGenerationRate)

	// 生成测试报告
	results := createPerformanceTestResults(metrics)
	report := reporter.GenerateReport(results, time.Now())

	filename := fmt.Sprintf("performance-test-report-%s.%s",
		time.Now().Format("20060102-150405"),
		getFileExtension(*outputFormat))

	err := reporter.SaveReport(report, filename, *outputFormat)
	if err != nil {
		log.Fatalf("保存报告失败: %v", err)
	}

	fmt.Printf("\n性能测试完成！报告已保存到: %s\n", filename)
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

func createPerformanceTestResults(metrics *tester.PerformanceMetrics) []models.TestResult {
	results := []models.TestResult{
		{
			TestCaseID: "PERF-001",
			Name:       "平均生成速率",
			Status:     models.Pass,
			StartTime:  time.Now(),
			EndTime:    time.Now(),
			Duration:   time.Duration(metrics.AvgGenerationRate) * time.Second,
			Message:    fmt.Sprintf("%.2f tokens/second", metrics.AvgGenerationRate),
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

	// 为每个结果设置适当的度量值
	for i := range results {
		results[i].Metrics = models.Metrics{
			ResponseTime:    metrics.AvgLatency,
			Latency:         metrics.AvgLatency,
			Throughput:      metrics.RPS,
			TokenCount:      int(metrics.AvgOutputTokens),
			ConcurrentUsers: int(metrics.RPS), // 临时设置
		}
	}

	return results
}
