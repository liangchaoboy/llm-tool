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
type PerformanceTestConfig struct {
	Enabled     bool          `yaml:"enabled"`
	Concurrency int           `yaml:"concurrency"`
	Duration    time.Duration `yaml:"duration"`
	WarmupTime  time.Duration `yaml:"warmup_time"`
	Requests    int           `yaml:"requests"`
	Timeout     time.Duration `yaml:"timeout"`
	Thresholds  Thresholds    `yaml:"thresholds"`
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
		config.API.Timeout = 30 * time.Second
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
	if config.Test.Performance.Duration == 0 {
		config.Test.Performance.Duration = 5 * time.Minute
	}
	if config.Test.Performance.WarmupTime == 0 {
		config.Test.Performance.WarmupTime = 30 * time.Second
	}
	if config.Test.Performance.Requests == 0 {
		config.Test.Performance.Requests = 1000
	}
	if config.Test.Performance.Timeout == 0 {
		config.Test.Performance.Timeout = 30 * time.Second
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

	// 安全测试默认值
	if !config.Test.Security.Enabled {
		config.Test.Security.Enabled = true
	}

	// 兼容性测试默认值
	if !config.Test.Compatibility.Enabled {
		config.Test.Compatibility.Enabled = true
	}
}
