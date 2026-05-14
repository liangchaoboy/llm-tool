package models

import "time"

// TestStatus 测试状态枚举
type TestStatus string

const (
	Pass  TestStatus = "PASS"
	Fail  TestStatus = "FAIL"
	Skip  TestStatus = "SKIP"
	Error TestStatus = "ERROR"
)

// TestCase 测试用例定义
type TestCase struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Category    string                 `json:"category"` // functional, performance, stability, security, compatibility
	Tags        []string               `json:"tags"`
	Timeout     time.Duration          `json:"timeout"`
	Config      map[string]interface{} `json:"config,omitempty"`
}

// TestResult 测试结果
type TestResult struct {
	TestCaseID string        `json:"test_case_id"`
	Name       string        `json:"name"`
	Status     TestStatus    `json:"status"`
	StartTime  time.Time     `json:"start_time"`
	EndTime    time.Time     `json:"end_time"`
	Duration   time.Duration `json:"duration"`
	Message    string        `json:"message,omitempty"`
	Error      string        `json:"error,omitempty"`
	Metrics    Metrics       `json:"metrics,omitempty"`
}

// Metrics 性能指标
type Metrics struct {
	ResponseTime    time.Duration `json:"response_time,omitempty"`
	Latency         time.Duration `json:"latency,omitempty"`
	Throughput      float64       `json:"throughput,omitempty"` // 请求/秒
	SuccessRate     float64       `json:"success_rate,omitempty"`
	TokenCount      int           `json:"token_count,omitempty"`
	RequestSize     int           `json:"request_size,omitempty"`
	ResponseSize    int           `json:"response_size,omitempty"`
	ConcurrentUsers int           `json:"concurrent_users,omitempty"`

	// 以下字段用于单次请求级别的统计（报告里每一条 PERF-REQ-XXXX 行会填充）
	RequestID    string        `json:"request_id,omitempty"`    // 上游响应头里的 reqid，便于事后排障
	InputTokens  int           `json:"input_tokens,omitempty"`  // 本次请求的输入 token 数
	OutputTokens int           `json:"output_tokens,omitempty"` // 本次请求的输出 token 数
	OutputTPS    float64       `json:"output_tps,omitempty"`    // 本次请求的输出 token/秒
	TTFT         time.Duration `json:"ttft,omitempty"`          // 本次请求的首 token 耗时
}

// TestSuite 测试套件
type TestSuite struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	TestCases   []TestCase `json:"test_cases"`
}

// TestReport 测试报告
type TestReport struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Summary     Summary      `json:"summary"`
	Results     []TestResult `json:"results"`
}

// Summary 报告摘要
type Summary struct {
	TotalTests   int           `json:"total_tests"`
	PassedTests  int           `json:"passed_tests"`
	FailedTests  int           `json:"failed_tests"`
	SkippedTests int           `json:"skipped_tests"`
	ErrorTests   int           `json:"error_tests"`
	SuccessRate  float64       `json:"success_rate"`
	AverageTime  time.Duration `json:"average_time"`
	MinTime      time.Duration `json:"min_time"`
	MaxTime      time.Duration `json:"max_time"`
}
