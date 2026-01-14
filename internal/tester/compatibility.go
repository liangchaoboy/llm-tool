package tester

import (
	"context"
	"fmt"
	"time"

	"github.com/user/llm-test-tool/internal/api"
	"github.com/user/llm-test-tool/internal/models"
)

// runModelCompatibilityTests 模型兼容性测试
func (tr *TestRunner) runModelCompatibilityTests(ctx context.Context) []models.TestResult {
	var results []models.TestResult

	// 获取配置中指定的模型列表（如果有）
	modelsToTest := tr.config.Compatibility.Models
	if len(modelsToTest) == 0 {
		// 如果没有指定，就测试当前配置的模型
		modelsToTest = []string{tr.client.GetModel()}
	}

	for i, modelName := range modelsToTest {
		result := models.TestResult{
			TestCaseID: fmt.Sprintf("COMP-00%d", i+1),
			Name:       fmt.Sprintf("模型 %s 兼容性测试", modelName),
			StartTime:  time.Now(),
		}

		// 尝试使用指定模型进行请求
		req := &api.Request{
			Model: modelName,
			Messages: []api.Message{
				{Role: "user", Content: "兼容性测试 - 请确认你能正常使用此模型"},
			},
			MaxTokens: 50,
		}

		_, err := tr.client.Call(ctx, req)

		if err != nil {
			result.Status = models.Fail
			result.Error = fmt.Sprintf("模型 %s 不兼容或不可用: %v", modelName, err)
		} else {
			result.Status = models.Pass
			result.Message = fmt.Sprintf("模型 %s 兼容性测试通过", modelName)
		}

		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		results = append(results, result)
	}

	return results
}

// runParameterCompatibilityTests 参数兼容性测试
func (tr *TestRunner) runParameterCompatibilityTests(ctx context.Context) []models.TestResult {
	var results []models.TestResult

	// 测试不同的参数组合
	testCases := []struct {
		id          string
		name        string
		maxTokens   int
		temperature float64
		topP        float64
	}{
		{
			id:          "COMP-011",
			name:        "默认参数测试",
			maxTokens:   100,
			temperature: 0.7,
			topP:        1.0,
		},
		{
			id:          "COMP-012",
			name:        "低温度参数测试",
			maxTokens:   100,
			temperature: 0.1,
			topP:        1.0,
		},
		{
			id:          "COMP-013",
			name:        "高温度参数测试",
			maxTokens:   100,
			temperature: 0.9,
			topP:        1.0,
		},
		{
			id:          "COMP-014",
			name:        "低top_p参数测试",
			maxTokens:   100,
			temperature: 0.7,
			topP:        0.5,
		},
		{
			id:          "COMP-015",
			name:        "不同max_tokens测试",
			maxTokens:   200,
			temperature: 0.7,
			topP:        1.0,
		},
	}

	for _, tc := range testCases {
		result := models.TestResult{
			TestCaseID: tc.id,
			Name:       tc.name,
			StartTime:  time.Now(),
		}

		req := &api.Request{
			Model: tr.client.GetModel(),
			Messages: []api.Message{
				{Role: "user", Content: "参数兼容性测试 - 请生成一段简短的回复"},
			},
			MaxTokens:   tc.maxTokens,
			Temperature: tc.temperature,
			TopP:        tc.topP,
		}

		_, err := tr.client.Call(ctx, req)

		if err != nil {
			result.Status = models.Fail
			result.Error = fmt.Sprintf("参数组合 %+v 不兼容: %v", tc, err)
		} else {
			result.Status = models.Pass
			result.Message = fmt.Sprintf("参数组合 %+v 兼容性测试通过", tc)
		}

		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		results = append(results, result)
	}

	return results
}

// runFormatCompatibilityTests 格式兼容性测试
func (tr *TestRunner) runFormatCompatibilityTests(ctx context.Context) []models.TestResult {
	var results []models.TestResult

	// 测试不同的输入格式
	formats := tr.config.Compatibility.Formats
	if len(formats) == 0 {
		// 默认测试几种常见格式
		formats = []string{"text", "json", "code"}
	}

	for i, format := range formats {
		result := models.TestResult{
			TestCaseID: fmt.Sprintf("COMP-0%d1", i+2),
			Name:       fmt.Sprintf("%s 格式兼容性测试", format),
			StartTime:  time.Now(),
		}

		var content string
		switch format {
		case "text":
			content = "请写一篇关于人工智能的小短文"
		case "json":
			content = "请以JSON格式返回一个人的基本信息，包含姓名、年龄、职业字段"
		case "code":
			content = "请用Python写一个快速排序算法的实现"
		default:
			content = fmt.Sprintf("格式测试 - %s", format)
		}

		req := &api.Request{
			Model: tr.client.GetModel(),
			Messages: []api.Message{
				{Role: "user", Content: content},
			},
			MaxTokens: 200,
		}

		_, err := tr.client.Call(ctx, req)

		if err != nil {
			result.Status = models.Fail
			result.Error = fmt.Sprintf("%s 格式不兼容: %v", format, err)
		} else {
			result.Status = models.Pass
			result.Message = fmt.Sprintf("%s 格式兼容性测试通过", format)
		}

		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		results = append(results, result)
	}

	return results
}

// executeCompatibilityTest 执行兼容性测试
func (tr *TestRunner) executeCompatibilityTest(ctx context.Context, testCase models.TestCase, result models.TestResult) models.TestResult {
	// 这里可以实现更详细的兼容性测试逻辑
	// 目前简化处理
	result.Status = models.Skip
	result.Message = "详细兼容性测试待实现"
	return result
}
