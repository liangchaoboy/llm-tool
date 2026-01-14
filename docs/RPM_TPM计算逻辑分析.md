# RPM 和 TPM 计算逻辑分析

## 一、当前实现

### 1.1 计算位置

**文件**: `internal/tester/performance.go`

**计算代码** (第226-234行):
```go
metrics.RPS = float64(successfulRequestCount) / testDurationSeconds
metrics.RPM = metrics.RPS * 60

metrics.TPS = float64(totalTokens) / testDurationSeconds
metrics.TPM = metrics.TPS * 60

metrics.OutputTPS = float64(totalOutputTokens) / testDurationSeconds
metrics.OutputTPM = metrics.OutputTPS * 60
```

### 1.2 计算公式

```
RPS = 成功请求数 / 测试耗时(秒)
RPM = RPS * 60 = (成功请求数 / 测试耗时(秒)) * 60

TPS = 总Token数 / 测试耗时(秒)
TPM = TPS * 60 = (总Token数 / 测试耗时(秒)) * 60

OutputTPS = 总输出Token数 / 测试耗时(秒)
OutputTPM = OutputTPS * 60 = (总输出Token数 / 测试耗时(秒)) * 60
```

## 二、计算逻辑验证

### 2.1 数学验证

假设：
- 测试耗时：30秒
- 成功请求数：100
- 总Token数：50000
- 总输出Token数：20000

**计算**:
```
RPS = 100 / 30 = 3.33 requests/second
RPM = 3.33 * 60 = 200 requests/minute

TPS = 50000 / 30 = 1666.67 tokens/second
TPM = 1666.67 * 60 = 100000 tokens/minute

OutputTPS = 20000 / 30 = 666.67 tokens/second
OutputTPM = 666.67 * 60 = 40000 tokens/minute
```

**验证**:
- RPM = 200 requests/minute 表示：如果以当前速率持续1分钟，会有200个请求 ✅
- TPM = 100000 tokens/minute 表示：如果以当前速率持续1分钟，会处理100000个tokens ✅

### 2.2 等价性验证

RPM也可以这样计算：
```
RPM = (成功请求数 / 测试耗时(秒)) * 60
    = 成功请求数 / (测试耗时(秒) / 60)
    = 成功请求数 / 测试耗时(分钟)
```

如果测试耗时是30秒 = 0.5分钟：
```
RPM = 100 / 0.5 = 200 requests/minute ✅
```

**结论**: 计算逻辑是正确的。

## 三、潜在问题分析

### 3.1 问题1: 测试时间较短时的准确性

**场景**: 如果测试只运行了很短时间（如1秒），RPM和TPM的值可能不够准确。

**示例**:
- 测试耗时：1秒
- 成功请求数：10
- RPS = 10 / 1 = 10 requests/second
- RPM = 10 * 60 = 600 requests/minute

**问题**: 
- 这个RPM值是基于1秒的数据推算的，可能不能代表长期稳定的性能
- 如果测试时间太短，统计波动会很大

**影响**: 
- 对于短时间测试，RPM/TPM的值可能不够稳定
- 但对于性能测试工具来说，这是合理的，因为它反映的是"在当前测试条件下的速率"

### 3.2 问题2: 并发测试的影响

**场景**: 在并发测试中，多个请求同时进行，实际的处理能力可能高于单线程的简单累加。

**当前计算**:
- RPM和TPM是基于总请求数和总耗时计算的
- 这反映了**整体吞吐量**，而不是单个请求的速率

**示例**:
- 并发数：10
- 每个并发发送100个请求
- 总请求数：1000
- 测试耗时：100秒（由于并发，实际可能更短）

**计算**:
```
RPS = 1000 / 100 = 10 requests/second
RPM = 10 * 60 = 600 requests/minute
```

**分析**:
- 这个值反映的是整个系统的吞吐量
- 如果并发度高，实际的处理能力可能更高
- 但当前的计算方式是正确的，因为它测量的是"实际完成的请求数 / 实际耗时"

### 3.3 问题3: 失败请求的处理

**当前实现**:
```go
metrics.RPS = float64(successfulRequestCount) / testDurationSeconds
```

**分析**:
- RPS只计算成功请求数，这是合理的 ✅
- 但总Token数（TPS/TPM）可能包含了失败请求的token（如果有的话）

**检查代码**:
- 第179行：`atomic.AddInt64(&totalRequests, 1)` - 失败请求也计入总数
- 第180行：`atomic.AddInt64(&successfulRequests, 1)` - 只有成功请求计入成功数
- 第181-182行：token统计只在成功时累计

**结论**: Token统计只包含成功请求的token，这是正确的 ✅

## 四、结论

### 4.1 计算逻辑正确性

✅ **RPM和TPM的计算公式是正确的**:
- RPM = RPS * 60
- TPM = TPS * 60
- OutputTPM = OutputTPS * 60

✅ **基础计算是正确的**:
- RPS = 成功请求数 / 测试耗时(秒)
- TPS = 总Token数 / 测试耗时(秒)
- OutputTPS = 总输出Token数 / 测试耗时(秒)

✅ **使用实际测试时间是正确的**:
- 基于实际测试耗时计算，而不是估算值
- 反映真实的性能表现

### 4.2 注意事项

1. **测试时间**: 测试时间越长，RPM/TPM的值越稳定和准确
2. **并发影响**: RPM/TPM反映的是整体吞吐量，考虑了并发效果
3. **失败处理**: 只统计成功请求，这是合理的

### 4.3 建议

当前实现是**正确的**，无需修改。但可以考虑：

1. **添加说明**: 在文档中说明RPM/TPM是基于实际测试时间计算的
2. **建议测试时长**: 建议测试时间至少30秒以上，以获得更稳定的指标
3. **区分指标**: 明确说明RPM/TPM反映的是"整体吞吐量"而不是"单请求速率"
