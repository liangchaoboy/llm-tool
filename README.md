# LLM API 性能测试工具

针对 OpenAI 兼容的 `chat/completions` 接口进行高并发压测，输出 HTML / JSON / Markdown 三种格式报告。Go 1.25.2 编写，模块名 `github.com/user/llm-test-tool`，唯一第三方依赖为 `gopkg.in/yaml.v2`（仅用于解析配置）。

> 项目入口 `cmd/main.go` **只**装配性能测试器（performance tester）。`internal/tester/` 下的 functional / stability / security / compatibility 文件能编译通过，但未接入 main，属于尚未上线的脚手架，不要假设它们功能可用。

---

## 一、核心能力

### 1. 性能指标

每次运行同时输出**单请求延迟**、**整体吞吐**与**派生速率**三类指标：

| 指标 | 含义 | 分母（取均值时） |
| --- | --- | --- |
| `AvgLatency` | 平均完整时延 | 仅成功请求 |
| `TTFT` | Time To First Token，首个含内容 SSE 帧到达时间 | 实际产生过 content 帧的请求（`ttftSamples`） |
| `TPOT` | 首 token 之后每个输出 token 的平均时间，公式 `(latency-ttft)/(outputTokens-1)` | `outputTokens >= 2` 的请求（`tpotSamples`） |
| `AvgGenerationRate` | 单请求 `outputTokens / latency` 后再求平均，单位 tokens/s | `outputTokens > 0` 的请求 |
| `RPS` / `RPM` | 每秒/每分钟成功请求数 | `actualTestDuration`（墙钟） |
| `TPS` / `TPM` | 每秒/每分钟总 token 数（输入+输出） | `actualTestDuration` |
| `OutputTPS` / `OutputTPM` | 每秒/每分钟输出 token 数 | `actualTestDuration` |
| `AvgOutputTokens` | 每个成功请求的平均输出 token | 成功请求 |
| `SuccessRate` | 成功率 % | 总请求 |

> 不同指标使用各自的有效样本数，避免“无 content 帧”或“output=0”的请求把均值拉低。

### 2. 高并发与无锁热路径

- 通过 `Concurrency × RequestsPerWorker` 决定总请求数（默认 10 × 100 = 1000）。
- 每个 worker 串行发出请求，使用全局 atomic 游标 `caseCursor` 轮询用例，多 worker 间负载均衡。
- 每个 worker 拥有私有 result slice，热路径完全无锁；`wg.Wait()` 后合并并按 `StartTime` 排序。
- HTTP 客户端的 `MaxIdleConnsPerHost`、`MaxConnsPerHost`、`MaxIdleConns` 与并发数对齐，避免高并发下反复 TLS 握手干扰 TTFT。

### 3. SSE / TTFT 解析

`internal/api/client.go` 的 `parseChatCompletionSSE` 把首个含以下任一字段的 data 帧视为“首 token”：
`delta.content`、`reasoning_content`、`reasoning`、`thinking`。新增 reasoning 风格字段时请同步加入这里，否则推理模型（DeepSeek / Moonshot 等）的 TTFT 会被推理阶段系统性拉长。

解析器同时跟踪流是否正常结束（`[DONE]` / finish_reason / usage），用于检测上游被截断的流。

### 4. 测试用例

用例文件是 **JSON 数组**，每个元素是**完整的 chat.completions 请求体**，工具会**原样透传**（不会改写 model / temperature / messages 等）。例：

```json
[
  {
    "stream": true,
    "model": "moonshotai/kimi-k2.5",
    "max_tokens": 1500,
    "messages": [
      {"role": "user", "content": [{"type": "text", "text": "hi"}]}
    ]
  }
]
```

`tools/` 目录下放有 LLM 驱动的用例生成脚本与语料；`tools/tools/` 与 `tools/corpus/` 已 gitignore，避免触发 GitHub 100MB 限制。

---

## 二、构建与运行

```bash
# 构建
go mod tidy
go build ./cmd/main.go            # 产出 ./main

# 运行（test-case-file 必填）
./main -test-case-file path/to/cases.json
```

仓库不含单测（无 `*_test.go`），可用的正确性检查只有：

```bash
go vet ./...
go build ./...
```

---

## 三、命令行参数

| Flag | 默认 | 说明 |
| --- | --- | --- |
| `-config` | `config/config.yaml` | YAML 配置路径；不存在则走内置回退 |
| `-concurrency` | 0（→ YAML / 兜底 10） | worker 并发数 |
| `-requests-per-worker` | 0（→ YAML / 兜底 100） | 每 worker 串行发出请求数 |
| `-api-key` | YAML | API 密钥 |
| `-base-url` | YAML | `chat/completions` 端点 URL |
| `-model` | YAML | 占位模型名（实际 model 取自每条用例） |
| `-test-case-file` | YAML | **必填**；用例 JSON 文件路径 |
| `-retry-count` | -1（→ YAML / 0） | 重试次数；**基准测试请保持 0**，重试会掩盖真实失败率与延迟 |
| `-format` | `html` | 报告格式：`html` / `json` / `markdown` |

### 配置优先级

`cmd/main.go` 严格执行 **CLI 显式指定 > YAML > 内置回退** 三级优先级，通过 `flag.Visit` 收集用户实际传过的 flag，避免“CLI 默认值恰好等于 0/空串”时静默覆盖 YAML。改动 flag 处理时请保留这一模式。

`config/setDefaults` 有两个**故意为之**的空缺：

1. 不把 `RetryCount==0` 视为未设置——基准场景需要显式关闭重试。
2. 不给 Security / Compatibility 的 `Enabled` 设默认 true，避免 YAML 里 `enabled: false` 被静默翻成 true。

---

## 四、配置文件

仓库内 `config/config.yaml` 仅放占位值；真实 API key 写到 `config/config.local.yaml`（已 gitignore）。

```yaml
api:
  base_url: "https://api.openai.com/v1/chat/completions"
  api_key: "your-api-key-here"
  model: "gpt-3.5-turbo"   # 占位；实际 model 来自用例
  timeout: 10m             # 单请求端到端超时（含流式读取完成）
  retry_count: 0           # 基准测试建议 0
  retry_delay: 1s
  headers:
    User-Agent: "LLM-Test-Tool/1.0"

test:
  performance:
    concurrency: 10
    requests_per_worker: 100
    test_case_file: ""     # 必填：可在此处或用 -test-case-file 指定
```

> `timeout` 对流式响应约束的是“整个 stream 读取完成”的最长耗时，不是单个 chunk 间隔。

---

## 五、报告结构

报告由两段拼成：

1. **每请求行**（`PERF-REQ-XXXX`）：一行对应一次真实 HTTP 请求，含 `RequestID`、起止时间、in/out tokens、TTFT、单请求 OutputTPS 等明细，按 `StartTime` 升序。
2. **14 条聚合指标行**（`PERF-001` … `PERF-014`）：对应平均生成速率、RPS/RPM、TPS/TPM、Output TPS/TPM、TTFT、TPOT、平均时延、请求总数、成功请求数、每输出 token 平均字数、成功率。

> `Summary` 直接由 `metrics` 构造而**不是** `len(allResults)` —— 否则 14 行聚合会被计入 `TotalTests`，让控制台数字与报告对不上。`MinTime` / `MaxTime` 只看成功请求（失败请求的 Duration 通常贴在超时上限）。

报告默认输出到当前目录，文件名形如 `performance-test-report-YYYYMMDD-HHMMSS.{html|json|md}`，已 gitignore。

---

## 六、控制台示例输出

```
开始LLM API性能测试
并发数: 10, 每并发请求数: 100, 总请求数: 1000

=== 性能测试结果 ===
总请求数: 1000  (成功: 998, 成功率: 99.80%)
测试耗时: 2m13.4s

-- 主指标：每请求平均 TPS --
平均生成速率 (每请求输出Token/秒，对所有请求求平均): 42.31 tokens/s

-- 单请求延迟 --
TTFT (首Token耗时):          612ms
TPOT (首Token后每Token时间): 23ms
平均完整时延:                12.4s
每个请求平均输出Token数:     510.20

-- 聚合吞吐（整体视角）--
RPS: 7.49  RPM: 449.4
Output TPS: 3820.5  Output TPM: 229230
Total TPS (含Prompt): 7980.2  Total TPM: 478812
```

每个请求另有一行 key=value 形式的实时日志，由 `logRequestLine` 单次 `Fprintln` 写入，避免高并发下多次 Printf 互相切片：

```
[req] id=chatcmpl-xxx status=Pass start=... end=... in=5512 out=482 out_tps=38.96 ttft=590ms latency=12.37s
```

---

## 七、目录结构

```
llm-tool/
├── cmd/main.go                # 入口，唯一接线性能测试
├── config/
│   ├── config.go              # YAML 解析 + setDefaults
│   ├── config.yaml            # 占位配置
│   └── config.local.yaml      # 真实密钥（gitignore）
├── internal/
│   ├── api/client.go          # HTTP 客户端 + SSE/TTFT 解析
│   ├── models/models.go       # TestResult / Metrics / Summary 等
│   ├── reporter/reporter.go   # HTML / JSON / Markdown 渲染
│   └── tester/
│       ├── performance.go     # 性能测试核心（先读这个）
│       ├── tester.go          # 调度器 / TestRunner 脚手架
│       ├── functional.go      # 未上线
│       ├── stability.go       # 未上线
│       ├── security.go        # 未上线
│       └── compatibility.go   # 未上线
├── docs/                      # 指标计算细节中文说明
├── tools/                     # 用例生成脚本 + 语料（部分 gitignore）
├── go.mod / go.sum
└── CLAUDE.md                  # 给 Claude Code 的项目指引
```

`docs/` 下的若干 Markdown（RPM_TPM 计算逻辑、TPOT 计算逻辑、Token 数值计算、平均生成速率、思考模型支持、测试用例选择逻辑、安全检查报告等）是对各项指标实现细节的更深入说明，遇到具体疑问可对应查阅。

---

## 八、注意事项

- 高并发会产生大量 API 调用与费用，请确认配额。
- 网络抖动会显著影响延迟指标，建议在稳定网络下进行基准测试。
- `performance-test-report-*` 与 `config/config.local.yaml` 已 gitignore，请勿提交。
- 注释和日志为中英文双语，编辑时请与所在文件已有风格保持一致。
