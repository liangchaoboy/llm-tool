# LLM API 性能测试工具

这是一个专门用于测试 LLM API 性能的工具，能够测量各种性能指标，包括平均生成速率、RPM/RPS、TPM/TPS、TTFT、TPOT 等。

## 特性

- 高并发性能测试（支持100+并发）
- 支持 thinking 和 non-thinking 两种场景（比例 1:4）
- 测量多种性能指标：
  - 平均生成速率 (tokens/second)
  - RPM/RPS (每分钟/秒请求数)
  - TPM/TPS (每分钟/秒总Token数)
  - Output TPM/TPS (每分钟/秒输出Token数)
  - TTFT (平均首字节耗时)
  - TPOT (每输出Token时间)
  - 平均完整时延
  - 请求总数
  - 每个输出token的平均字数
  - 请求成功率
- 支持从本地JSON文件读取测试用例
- 生成HTML/JSON/Markdown格式的测试报告

## 使用方法

### 构建工具

```bash
go build ./cmd/main.go
```

### 运行性能测试

```bash
./main -concurrency 100 -requests-per-worker 500 -model gpt-3.5-turbo -api-key your-api-key -base-url https://api.openai.com/v1/chat/completions -test-case-file test-data/test-cases.json
```

### 命令行参数

- `-concurrency`: 并发数 (默认: 100)
- `-requests-per-worker`: 每个并发的请求数 (默认: 500)
- `-model`: 模型名称 (默认: gpt-3.5-turbo)
- `-api-key`: API密钥
- `-base-url`: API基础URL (默认: https://api.openai.com/v1/chat/completions)
- `-test-case-file`: 测试用例文件路径 (默认: test-data/test-cases.json)
- `-format`: 输出格式(json/html/markdown) (默认: html)
- `-config`: 配置文件路径 (默认: config/config.yaml)

### 生成测试用例

```bash
go run generate_test_cases.go
```

这将生成50000个测试用例，其中thinking和non-thinking场景的比例为1:4，满足以下要求：
- thinking场景：输入tokens 6k, 输出tokens 1k
- non-thinking场景：输入tokens 5.5k, 输出tokens 0.5k

## 测试用例文件格式

测试用例文件为JSON格式，每个用例包含以下字段：
- `id`: 用例ID
- `type`: 用例类型 (thinking/non_thinking)
- `input_tokens`: 输入token数
- `output_tokens`: 输出token数
- `prompt`: 提示文本
- `temperature`: 温度参数
- `max_tokens`: 最大输出token数

## 性能指标说明

- **平均生成速率**: 模型每秒生成的输出token数
- **RPM/RPS**: 每分钟/秒处理的请求数
- **TPM/TPS**: 每分钟/秒处理的总token数（输入+输出）
- **Output TPM/TPS**: 每分钟/秒生成的输出token数
- **TTFT**: 平均首字节耗时（从发送请求到收到第一个字节的平均时间）
- **TPOT**: 每个输出token的平均时间
- **平均完整时延**: 完整请求的平均响应时间
- **请求总数**: 测试期间发送的请求数
- **每个输出token的平均字数**: 输出内容的平均token效率

## 配置文件

工具支持配置文件，可以设置API连接信息和测试参数。参考 config/config.yaml 文件。

## 示例

运行一个基本的性能测试：

```bash
./main -concurrency 50 -requests-per-worker 100 -model gpt-3.5-turbo -api-key your-key -format html
```

## 注意事项

- 确保API密钥有效并具有足够的调用配额
- 高并发测试可能会产生大量API调用费用
- 建议在测试前先进行小规模测试确认配置正确