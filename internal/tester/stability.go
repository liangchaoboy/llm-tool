package tester

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/user/llm-test-tool/internal/api"
	"github.com/user/llm-test-tool/internal/models"
)

// runLongRunningTest 长时间运行测试
func (tr *TestRunner) runLongRunningTest(ctx context.Context) []models.TestResult {
	var results []models.TestResult

	result := models.TestResult{
		TestCaseID: "STAB-001",
		Name:       "长时间运行测试",
		StartTime:  time.Now(),
	}

	duration := tr.config.Stability.Duration
	interval := tr.config.Stability.Interval
	maxErrors := tr.config.Stability.MaxErrors

	var errorCount int32
	var successCount int32
	var totalRequests int32

	// 创建停止信号
	stop := make(chan bool)

	// 启动定时测试
	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if time.Since(startTime) >= duration {
					stop <- true
					return
				}

				atomic.AddInt32(&totalRequests, 1)

				// 执行简单的健康检查请求
				req := &api.Request{
					Model: tr.client.GetModel(),
					Messages: []api.Message{
						{Role: "user", Content: "健康检查"},
					},
					MaxTokens: 10,
				}

				_, err := tr.client.Call(ctx, req)
				if err != nil {
					atomic.AddInt32(&errorCount, 1)
					fmt.Printf("长时间运行测试 - 请求失败: %v\n", err)
				} else {
					atomic.AddInt32(&successCount, 1)
				}

				// 检查错误数量是否超出限制
				if atomic.LoadInt32(&errorCount) > int32(maxErrors) {
					stop <- true
					return
				}
			case <-stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// 等待测试完成或出错
	select {
	case <-time.After(duration + time.Second):
		// 测试完成
	case <-stop:
		// 提前停止（可能是错误过多）
	}

	endTime := time.Now()

	total := int(atomic.LoadInt32(&totalRequests))
	errors := int(atomic.LoadInt32(&errorCount))
	successes := int(atomic.LoadInt32(&successCount))

	successRate := 0.0
	if total > 0 {
		successRate = float64(successes) / float64(total) * 100
	}

	// 检查是否通过测试
	if errors <= maxErrors && successRate >= 90.0 {
		result.Status = models.Pass
		result.Message = fmt.Sprintf("长时间运行测试通过 - 总请求: %d, 成功: %d, 失败: %d, 成功率: %.2f%%",
			total, successes, errors, successRate)
	} else {
		result.Status = models.Fail
		result.Error = fmt.Sprintf("长时间运行测试失败 - 总请求: %d, 成功: %d, 失败: %d, 成功率: %.2f%%",
			total, successes, errors, successRate)
	}

	result.EndTime = endTime
	result.Duration = endTime.Sub(startTime)
	result.Metrics = models.Metrics{
		SuccessRate:     successRate,
		ConcurrentUsers: 1, // 单线程测试
	}

	results = append(results, result)
	return results
}

// runErrorRecoveryTest 错误恢复测试
func (tr *TestRunner) runErrorRecoveryTest(ctx context.Context) []models.TestResult {
	var results []models.TestResult

	result := models.TestResult{
		TestCaseID: "STAB-002",
		Name:       "错误恢复测试",
		StartTime:  time.Now(),
	}

	// 首先执行一些正常的请求
	for i := 0; i < 5; i++ {
		req := &api.Request{
			Model: tr.client.GetModel(),
			Messages: []api.Message{
				{Role: "user", Content: fmt.Sprintf("正常请求 %d", i)},
			},
			MaxTokens: 20,
		}

		_, err := tr.client.Call(ctx, req)
		if err != nil {
			result.Status = models.Error
			result.Error = fmt.Sprintf("错误恢复测试初始化失败: %v", err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			results = append(results, result)
			return results
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 尝试执行可能导致错误的请求
	invalidReq := &api.Request{
		Model: tr.client.GetModel(),
		Messages: []api.Message{
			{Role: "user", Content: "正常请求"},
		},
		MaxTokens: -1, // 故意设置无效参数
	}

	_, err := tr.client.Call(ctx, invalidReq)

	// 无论上一步是否出错，尝试恢复正常操作
	time.Sleep(1 * time.Second)

	// 验证服务是否还能正常工作
	normalReq := &api.Request{
		Model: tr.client.GetModel(),
		Messages: []api.Message{
			{Role: "user", Content: "恢复测试请求"},
		},
		MaxTokens: 20,
	}

	resp, err := tr.client.Call(ctx, normalReq)

	if err == nil && len(resp.Choices) > 0 {
		result.Status = models.Pass
		result.Message = "错误恢复测试通过 - 服务在异常请求后仍能正常工作"
	} else {
		result.Status = models.Fail
		result.Error = fmt.Sprintf("错误恢复测试失败 - 服务无法从错误状态恢复: %v", err)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	results = append(results, result)
	return results
}

// executeStabilityTest 执行稳定性测试
func (tr *TestRunner) executeStabilityTest(ctx context.Context, testCase models.TestCase, result models.TestResult) models.TestResult {
	// 这里可以实现更详细的稳定性测试逻辑
	// 目前简化处理
	result.Status = models.Skip
	result.Message = "详细稳定性测试待实现"
	return result
}
