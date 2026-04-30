package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v2"
)

// Config 主配置结构
type Config struct {
	API  APIConfig  `yaml:"api"`
	Test TestConfig `yaml:"test"`
}

// APIConfig API配置
//
// Timeout 是单次请求的端到端超时（从发起到读完 Response Body）。
// 对流式响应，它约束的是整个 stream 读取完成的最长耗时，而不是单个 chunk 的间隔。
// 基准测试里建议设得比"最长合理响应时间"略大一点，例如 10m，以免真正慢的请求被
// 误判为超时；但过长会让被上游卡死的请求长时间占用 worker。
type APIConfig struct {
	BaseURL    string            `yaml:"base_url"`
	APIKey     string            `yaml:"api_key"`
	Model      string            `yaml:"model"`
	Timeout    time.Duration     `yaml:"timeout"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	RetryCount int               `yaml:"retry_count"`
	RetryDelay time.Duration     `yaml:"retry_delay"`
}

// TestConfig 测试配置
type TestConfig struct {
	Functional    FunctionalTestConfig    `yaml:"functional"`
	Performance   PerformanceTestConfig   `yaml:"performance"`
	Stability     StabilityTestConfig     `yaml:"stability"`
	Security      SecurityTestConfig      `yaml:"security"`
	Compatibility CompatibilityTestConfig `yaml:"compatibility"`
}

// FunctionalTestConfig 功能测试配置
type FunctionalTestConfig struct {
	Enabled       bool          `yaml:"enabled"`
	Timeout       time.Duration `yaml:"timeout"`
	TestCasesFile string        `yaml:"test_cases_file"`
	MaxRetries    int           `yaml:"max_retries"`
}

// PerformanceTestConfig 性能测试配置
//
// 说明：
//   - Concurrency:       并发 worker 数
//   - RequestsPerWorker: 每个 worker 串行发出的请求数
//   - TestCaseFile:      测试用例 JSON 文件路径（每条元素是完整的请求 body）
//
// 这些字段都可以被命令行 flag 覆盖（CLI 优先级高于 YAML）。
type PerformanceTestConfig struct {
	Concurrency       int    `yaml:"concurrency"`
	RequestsPerWorker int    `yaml:"requests_per_worker"`
	TestCaseFile      string `yaml:"test_case_file"`
}

// StabilityTestConfig 稳定性测试配置
type StabilityTestConfig struct {
	Enabled     bool          `yaml:"enabled"`
	Duration    time.Duration `yaml:"duration"`
	Interval    time.Duration `yaml:"interval"`
	MaxErrors   int           `yaml:"max_errors"`
	CheckHealth bool          `yaml:"check_health"`
}

// SecurityTestConfig 安全测试配置
type SecurityTestConfig struct {
	Enabled          bool     `yaml:"enabled"`
	TestSQLInjection bool     `yaml:"test_sql_injection"`
	TestXSS          bool     `yaml:"test_xss"`
	TestInjection    bool     `yaml:"test_injection"`
	BlockedInputs    []string `yaml:"blocked_inputs"`
	AllowedInputs    []string `yaml:"allowed_inputs"`
}

// CompatibilityTestConfig 兼容性测试配置
type CompatibilityTestConfig struct {
	Enabled    bool     `yaml:"enabled"`
	Models     []string `yaml:"models"`
	Formats    []string `yaml:"formats"`
	Parameters []string `yaml:"parameters"`
	MaxTokens  []int    `yaml:"max_tokens"`
}

// Thresholds 性能阈值
type Thresholds struct {
	MaxResponseTime time.Duration `yaml:"max_response_time"`
	MinSuccessRate  float64       `yaml:"min_success_rate"`
	MaxErrorRate    float64       `yaml:"max_error_rate"`
	MinThroughput   float64       `yaml:"min_throughput"`
}

// Load 加载配置文件
func Load(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 设置默认值
	setDefaults(&config)

	return &config, nil
}

// setDefaults 设置默认配置值
func setDefaults(config *Config) {
	// API 默认值
	if config.API.Timeout == 0 {
		// 对齐 config.yaml 的推荐值：LLM 长响应常常超过 1 分钟，30s 太激进。
		config.API.Timeout = 10 * time.Minute
	}
	// 注意：不把 RetryCount==0 当作"未设置"并替换为 3；
	// 基准测试常常希望显式关闭重试，0 必须被保留为合法值。
	if config.API.RetryCount < 0 {
		config.API.RetryCount = 0
	}
	if config.API.RetryDelay == 0 {
		config.API.RetryDelay = 1 * time.Second
	}

	// 功能测试默认值
	if config.Test.Functional.Timeout == 0 {
		config.Test.Functional.Timeout = 60 * time.Second
	}
	if config.Test.Functional.MaxRetries == 0 {
		config.Test.Functional.MaxRetries = 3
	}

	// 性能测试默认值
	if config.Test.Performance.Concurrency == 0 {
		config.Test.Performance.Concurrency = 10
	}
	if config.Test.Performance.RequestsPerWorker == 0 {
		config.Test.Performance.RequestsPerWorker = 100
	}

	// 稳定性测试默认值
	if config.Test.Stability.Duration == 0 {
		config.Test.Stability.Duration = 1 * time.Hour
	}
	if config.Test.Stability.Interval == 0 {
		config.Test.Stability.Interval = 10 * time.Second
	}
	if config.Test.Stability.MaxErrors == 0 {
		config.Test.Stability.MaxErrors = 10
	}
	// 注意：Security / Compatibility 的 Enabled 字段不在此处设默认值。
	// 之前的 `if !enabled { enabled = true }` 会让 YAML 里显式写的 false 被翻成 true，
	// 导致用户无法关闭这些测试；若需要默认开启，请在 YAML 中显式 enabled: true。
}
