# 使用手册（USAGE）

聚焦“跑一次基准测试需要做什么”。架构与指标定义见 [README.md](./README.md)；实现细节见 `docs/` 与 `CLAUDE.md`。

---

## 1. 一次最小可跑流程

```bash
# 1) 编译
go mod tidy
go build ./cmd/main.go

# 2) 准备 config/config.local.yaml（不要提交）
cat > config/config.local.yaml <<'YAML'
api:
  base_url: "https://your-endpoint/v1/chat/completions"
  api_key:  "sk-..."
  timeout:  10m
  retry_count: 0
test:
  performance:
    concurrency: 10
    requests_per_worker: 100
    test_case_file: "cases.json"
YAML

# 3) 准备用例文件 cases.json（JSON 数组，元素是完整请求体）
# 4) 运行（默认读 config/config.yaml；用本地配置则显式指定）
./main -config config/config.local.yaml
```

完成后会在当前目录生成 `performance-test-report-YYYYMMDD-HHMMSS.html`。

---

## 2. 用例文件

文件是 **JSON 数组**，每个元素就是要发给 `chat/completions` 的**完整请求体**（原样透传，不会改写任何字段）。

```json
[
  {
    "stream": true,
    "model": "moonshotai/kimi-k2.5",
    "max_tokens": 1500,
    "temperature": 0.7,
    "messages": [
      {"role": "user", "content": [{"type": "text", "text": "..."}]}
    ]
  }
]
```

要点：

- `stream` 决不要设为 `false`：TTFT/TPOT 依赖 SSE 的逐帧到达。
- 需要混合多种场景时，把不同 prompt 全部塞进同一个数组即可——worker 通过全局 atomic 游标轮询，分布天然均匀。
- `model` 写在每条用例里，不需要 CLI 再传 `-model`。CLI 的 `-model` 只是占位。

---

## 3. CLI 速查

| Flag | 含义 |
| --- | --- |
| `-config <path>` | YAML 配置文件，默认 `config/config.yaml` |
| `-concurrency <N>` | worker 并发数 |
| `-requests-per-worker <N>` | 每个 worker 串行请求数 |
| `-test-case-file <path>` | **必填**：用例 JSON 文件 |
| `-api-key`, `-base-url`, `-model` | 覆盖 YAML 中对应字段 |
| `-retry-count <N>` | 重试次数，**基准测试请保持 0** |
| `-format html\|json\|markdown` | 报告格式，默认 html |

优先级：CLI 显式传入 > YAML > 内置回退。CLI 里没传过的 flag 不会覆盖 YAML，即使 CLI 默认值与 YAML 值不同。

总请求数 = `concurrency × requests_per_worker`。

---

## 4. 常见运行模式

### 4.1 小流量冒烟（验证配置）

```bash
./main -config config/config.local.yaml \
  -concurrency 2 -requests-per-worker 5 \
  -test-case-file cases.json
```

10 个请求快速跑通端到端，验证 endpoint / key / 用例可用。

### 4.2 标准基准

```bash
./main -config config/config.local.yaml \
  -concurrency 50 -requests-per-worker 200 \
  -retry-count 0 \
  -test-case-file cases.json \
  -format html
```

5 万请求，HTML 报告。

### 4.3 输出 JSON 给下游解析

```bash
./main -config config/config.local.yaml -format json -test-case-file cases.json
```

JSON 报告同时包含每请求行和 14 条聚合指标行，下游可按 `TestCaseID` 前缀（`PERF-REQ-` vs `PERF-`）过滤。

---

## 5. 解读报告

控制台输出三段：
- **主指标** —— `AvgGenerationRate`（每请求 outputTokens/s 求平均），最贴近用户体感。
- **单请求延迟** —— `TTFT` / `TPOT` / `AvgLatency` / `AvgOutputTokens`。
- **聚合吞吐** —— `RPS/RPM`、`OutputTPS/TPM`、`TPS/TPM`，分母都是墙钟时间。

解读时常见坑：

- **TTFT 看起来异常偏大**：检查模型是否带推理，`parseChatCompletionSSE` 是否覆盖到对应 reasoning 字段（目前已支持 `delta.content`、`reasoning_content`、`reasoning`、`thinking`）。
- **TPOT 缺失或为 0**：用例 `outputTokens < 2` 或 stream 被截断，没进 TPOT 样本。
- **成功率 < 100%**：每请求行里看 `err=...` 字段，逐条定位。失败请求的 `Duration` 经常贴在 timeout 上限，不会污染 `MinTime/MaxTime`。

---

## 6. 排错

| 现象 | 排查 |
| --- | --- |
| 启动直接报 “未指定测试用例文件” 退出 | 检查 `-test-case-file` 或 YAML `test.performance.test_case_file` |
| 大量请求 timeout | 把 `api.timeout` 调到 ≥ 模型最长合理响应（默认 10m） |
| TTFT 系统性偏大 | 推理模型；确认 SSE 解析覆盖到推理字段；或目标侧首 token 真的慢 |
| RPS 远低于预期 | 检查 worker 数与连接池是否对齐（`api.NewClient(_, perfConfig.Concurrency)`） |
| 报告里 `TotalTests` 与控制台对不上 | 不要让 reporter 自动从 `len(results)` 推 `Summary`，必须用 `metrics` 显式构造 |

---

## 7. 注意事项

- 高并发 = 高费用，先冒烟再上量。
- `retry_count > 0` 会掩盖真实失败率与延迟分布，启动时工具会打印 ⚠️ 提示。
- `performance-test-report-*` 和 `config/config.local.yaml` 已 gitignore，不要提交。
- 编辑代码时注释与日志保持中英文双语，与所在文件风格一致。
