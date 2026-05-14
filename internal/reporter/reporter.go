package reporter

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"time"

	"github.com/user/llm-test-tool/internal/models"
)

// GenerateReport 生成测试报告
func GenerateReport(results []models.TestResult, generatedAt time.Time) models.TestReport {
	// 计算摘要统计。
	// 注意：很多 PERF-XXX 行是"数值展示行"（RPS、RPM、token 数等），它们的 Duration 是 0
	// 而不是真实耗时。把这些行纳入 min/avg 计算会把摘要里的 MinTime 永远压成 0、
	// AverageTime 也被稀释。所以只对 Duration>0 的行参与耗时统计。
	var totalTests, passedTests, failedTests, skippedTests, errorTests int
	var totalTime time.Duration
	var minTime, maxTime time.Duration
	var durationSamples int

	for _, result := range results {
		totalTests++

		switch result.Status {
		case models.Pass:
			passedTests++
		case models.Fail:
			failedTests++
		case models.Skip:
			skippedTests++
		case models.Error:
			errorTests++
		}

		if result.Duration <= 0 {
			continue
		}
		totalTime += result.Duration
		durationSamples++
		if minTime == 0 || result.Duration < minTime {
			minTime = result.Duration
		}
		if result.Duration > maxTime {
			maxTime = result.Duration
		}
	}

	var avgTime time.Duration
	if durationSamples > 0 {
		avgTime = totalTime / time.Duration(durationSamples)
	}

	successRate := 0.0
	if totalTests > 0 {
		successRate = float64(passedTests) / float64(totalTests) * 100
	}

	summary := models.Summary{
		TotalTests:   totalTests,
		PassedTests:  passedTests,
		FailedTests:  failedTests,
		SkippedTests: skippedTests,
		ErrorTests:   errorTests,
		SuccessRate:  successRate,
		AverageTime:  avgTime,
		MinTime:      minTime,
		MaxTime:      maxTime,
	}

	return models.TestReport{
		GeneratedAt: generatedAt,
		Summary:     summary,
		Results:     results,
	}
}

// GenerateReportWithSummary 使用调用方已经算好的 Summary 生成报告。
//
// 适用场景：报告里的 results 包含多种"性质"的行（例如性能测试里既有每请求的
// PERF-REQ-XXXX 明细行，也有 PERF-001..014 的聚合指标展示行），直接用
// len(results) 统计总测试数会失真。这种情况下由调用方用真实数据源（如性能
// metrics）构造 Summary 传进来，本函数只负责组装 TestReport。
func GenerateReportWithSummary(results []models.TestResult, summary models.Summary, generatedAt time.Time) models.TestReport {
	return models.TestReport{
		GeneratedAt: generatedAt,
		Summary:     summary,
		Results:     results,
	}
}

// SaveReport 保存测试报告到文件
func SaveReport(report models.TestReport, filename, format string) error {
	switch format {
	case "json":
		return saveJSONReport(report, filename)
	case "html":
		return saveHTMLReport(report, filename)
	case "markdown":
		return saveMarkdownReport(report, filename)
	default:
		return fmt.Errorf("不支持的报告格式: %s", format)
	}
}

// saveJSONReport 保存JSON格式报告
func saveJSONReport(report models.TestReport, filename string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化JSON报告失败: %w", err)
	}

	return os.WriteFile(filename, data, 0644)
}

// saveHTMLReport 保存HTML格式报告
func saveHTMLReport(report models.TestReport, filename string) error {
	htmlTemplate := `
<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>LLM API 测试报告</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            margin: 20px;
            background-color: #f5f5f5;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        h1 {
            color: #333;
            text-align: center;
            border-bottom: 2px solid #007bff;
            padding-bottom: 10px;
        }
        .summary {
            background: #f8f9fa;
            padding: 15px;
            border-radius: 5px;
            margin: 20px 0;
        }
        .summary-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 15px;
        }
        .summary-item {
            text-align: center;
            padding: 10px;
        }
        .summary-value {
            font-size: 24px;
            font-weight: bold;
        }
        .pass { color: #28a745; }
        .fail { color: #dc3545; }
        .skip { color: #ffc107; }
        .error { color: #fd7e14; }
        .results-table {
            width: 100%;
            border-collapse: collapse;
            margin-top: 20px;
        }
        .results-table th, .results-table td {
            border: 1px solid #ddd;
            padding: 12px;
            text-align: left;
        }
        .results-table th {
            background-color: #f2f2f2;
            font-weight: bold;
        }
        .results-table tr:nth-child(even) {
            background-color: #f9f9f9;
        }
        .status-badge {
            padding: 4px 8px;
            border-radius: 4px;
            font-size: 12px;
            font-weight: bold;
            text-transform: uppercase;
        }
        .timestamp {
            color: #666;
            font-size: 14px;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>LLM API 测试报告</h1>
        <p class="timestamp">生成时间: {{.GeneratedAt}}</p>
        
        <div class="summary">
            <h2>测试摘要</h2>
            <div class="summary-grid">
                <div class="summary-item">
                    <div class="summary-value">{{.Summary.TotalTests}}</div>
                    <div>总测试数</div>
                </div>
                <div class="summary-item">
                    <div class="summary-value pass">{{.Summary.PassedTests}}</div>
                    <div>通过</div>
                </div>
                <div class="summary-item">
                    <div class="summary-value fail">{{.Summary.FailedTests}}</div>
                    <div>失败</div>
                </div>
                <div class="summary-item">
                    <div class="summary-value skip">{{.Summary.SkippedTests}}</div>
                    <div>跳过</div>
                </div>
                <div class="summary-item">
                    <div class="summary-value">{{printf "%.2f" .Summary.SuccessRate}}%</div>
                    <div>成功率</div>
                </div>
                <div class="summary-item">
                    <div class="summary-value">{{.Summary.AverageTime}}</div>
                    <div>平均耗时</div>
                </div>
            </div>
        </div>

        <h2>详细结果</h2>
        <table class="results-table">
            <thead>
                <tr>
                    <th>测试ID</th>
                    <th>测试名称</th>
                    <th>状态</th>
                    <th>开始时间</th>
                    <th>结束时间</th>
                    <th>耗时</th>
                    <th>消息</th>
                </tr>
            </thead>
            <tbody>
                {{range .Results}}
                <tr>
                    <td>{{.TestCaseID}}</td>
                    <td>{{.Name}}</td>
                    <td>
                        <span class="status-badge 
                            {{if eq .Status "PASS"}}pass
                            {{else if eq .Status "FAIL"}}fail
                            {{else if eq .Status "SKIP"}}skip
                            {{else if eq .Status "ERROR"}}error
                            {{end}}">
                            {{.Status}}
                        </span>
                    </td>
                    <td>{{.StartTime.Format "2006-01-02 15:04:05"}}</td>
                    <td>{{.EndTime.Format "2006-01-02 15:04:05"}}</td>
                    <td>{{.Duration}}</td>
                    <td>
                        {{if .Message}}{{.Message}}{{end}}
                        {{if .Error}}<br><strong>错误:</strong> {{.Error}}{{end}}
                    </td>
                </tr>
                {{end}}
            </tbody>
        </table>
    </div>
</body>
</html>`

	tmpl, err := template.New("report").Parse(htmlTemplate)
	if err != nil {
		return fmt.Errorf("解析HTML模板失败: %w", err)
	}

	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer file.Close()

	return tmpl.Execute(file, report)
}

// saveMarkdownReport 保存Markdown格式报告
func saveMarkdownReport(report models.TestReport, filename string) error {
	content := fmt.Sprintf(`# LLM API 测试报告

**生成时间:** %s

## 测试摘要

| 项目 | 数量 |
|------|------|
| 总测试数 | %d |
| 通过 | %d |
| 失败 | %d |
| 跳过 | %d |
| 错误 | %d |
| 成功率 | %.2f%% |

## 详细结果

| 测试ID | 测试名称 | 状态 | 开始时间 | 结束时间 | 耗时 | 消息 |
|--------|----------|------|----------|----------|------|------|
`,
		report.GeneratedAt.Format("2006-01-02 15:04:05"),
		report.Summary.TotalTests,
		report.Summary.PassedTests,
		report.Summary.FailedTests,
		report.Summary.SkippedTests,
		report.Summary.ErrorTests,
		report.Summary.SuccessRate,
	)

	for _, result := range report.Results {
		message := result.Message
		if result.Error != "" {
			message += " 错误: " + result.Error
		}
		content += fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s |\n",
			result.TestCaseID,
			result.Name,
			result.Status,
			result.StartTime.Format("15:04:05"),
			result.EndTime.Format("15:04:05"),
			result.Duration.String(),
			message,
		)
	}

	return os.WriteFile(filename, []byte(content), 0644)
}
