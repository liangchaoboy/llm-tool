# LLM API 性能测试工具

这是一个专门用于测试 LLM API 性能的工具，完全符合准入测试标准，使用 Go 语言编写。该工具能够对模型提供商的接口进行全面的性能测试，并输出详细的测试报告。

## 项目概述

本项目是一个全面的LLM API性能测试解决方案，专门用于评估大语言模型API的性能表现。工具能够测量多种关键性能指标，并生成标准化的测试报告。

## 核心功能

### 1. 性能指标测量
- **平均生成速率**: 模型每秒生成的输出token数
- **RPM/RPS**: 每分钟/秒处理的请求数
- **TPM/TPS**: 每分钟/秒处理的总token数（输入+输出）
- **Output TPM/TPS**: 每分钟/秒生成的输出token数
- **TTFT (Time To First Byte)**: 平均首字节耗时（从发送请求到收到第一个字节的平均时间）
- **TPOT (Time Per Output Token)**: 每个输出token的平均时间
- **平均完整时延**: 完整请求的平均响应时间
- **请求总数**: 测试期间发送的请求数
- **每个输出token的平均字数**: 输出内容的平均token效率
- **请求成功率**: 成功请求占总请求的百分比

### 2. 高并发测试
- 支持100+并发连接
- 每个并发可配置请求数（默认500）
- 总测试量可达到50000+请求

### 3. 场景测试
- **Thinking场景**: 输入6k tokens，输出1k tokens，温度0.7
- **Non-thinking场景**: 输入5.5k tokens，输出0.5k tokens，温度0.3
- 严格遵循 1:4 的 thinking:non-thinking 比例

### 4. 测试用例管理
- 支持从JSON文件加载测试用例
- 自动按比例分配不同类型的测试场景
- 可扩展的测试用例格式

## 项目结构

```
llm-test-tool/
├── cmd/
│   └── main.go                 # 主程序入口
├── config/
│   └── config.yaml             # 配置文件
├── internal/
│   ├── api/                    # API客户端
│   │   └── client.go
│   ├── models/                 # 数据模型
│   │   └── models.go
│   └── tester/                 # 测试逻辑
│       ├── performance.go      # 性能测试
│       └── ...                 # 其他测试模块
├── test-data/
│   ├── test-cases.json         # 小规模测试用例
│   └── large-test-cases.json   # 大规模测试用例
├── generate_test_cases.go      # 测试用例生成器
├── USAGE.md                    # 使用说明
├── go.mod
└── go.sum
```

## 快速开始

### 1. 构建项目

```bash
# 确保已安装 Go 1.19+
go mod tidy
go build ./cmd/main.go
```

### 2. 准备测试用例

生成大规模测试用例（50000个，符合1:4比例）：

```bash
go run generate_test_cases.go
```

### 3. 运行性能测试

```bash
./main -concurrency 100 -requests-per-worker 500 -model gpt-3.5-turbo -api-key your-api-key -base-url https://api.openai.com/v1/chat/completions -test-case-file test-data/large-test-cases.json
```

## 命令行参数

- `-concurrency`: 并发数 (默认: 100)
- `-requests-per-worker`: 每个并发的请求数 (默认: 500)
- `-model`: 模型名称 (默认: gpt-3.5-turbo)
- `-api-key`: API密钥
- `-base-url`: API基础URL (默认: https://api.openai.com/v1/chat/completions)
- `-test-case-file`: 测试用例文件路径 (默认: test-data/test-cases.json)
- `-format`: 输出格式(json/html/markdown) (默认: html)
- `-config`: 配置文件路径 (默认: config/config.yaml)

## 测试用例格式

测试用例为JSON格式，包含以下字段：
- `id`: 用例唯一标识
- `type`: 用例类型 (thinking/non_thinking)
- `input_tokens`: 预期输入token数
- `output_tokens`: 预期输出token数
- `prompt`: 测试提示文本
- `temperature`: 温度参数
- `max_tokens`: 最大输出token数

## 输出报告

工具支持多种报告格式：
- **HTML**: 交互式网页报告，包含图表和详细数据
- **JSON**: 结构化数据，便于程序解析
- **Markdown**: 格式化文本，便于文档集成

报告包含所有性能指标的详细数据和统计信息。

## 配置文件

可通过 config/config.yaml 文件配置API连接信息和其他参数：

```yaml
api:
  base_url: "https://api.openai.com/v1/chat/completions"
  api_key: "your-api-key-here"
  model: "gpt-3.5-turbo"
  timeout: 120s
  retry_count: 3
  retry_delay: 1s
```

## 安全考虑

- API密钥通过命令行参数或配置文件提供
- 支持自定义请求头和认证方式
- 请求超时和重试机制防止资源泄露

## 性能优化

- 高效的并发控制
- 内存友好的数据结构
- 原子操作统计避免锁竞争
- 流式处理大量测试数据

## 扩展性

- 模块化设计便于功能扩展
- 支持自定义测试场景
- 可插拔的报告生成器
- 配置驱动的测试参数

## 注意事项

- 高并发测试会产生大量API调用，请确保API配额充足
- 建议在正式测试前先进行小规模测试验证配置
- 测试结果受网络状况影响，请在稳定的网络环境下运行
- 遵循API提供商的使用条款和限制

## 许可证

本项目为开源项目，详细信息请参阅 LICENSE 文件。