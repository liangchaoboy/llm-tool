# Token数值计算说明

## 一、Token数值来源

### 1.1 优先使用服务端返回的Token数值 ✅

**代码位置**: `internal/tester/performance.go:148-171`

#### 输出Token数（CompletionTokens）

```go
// 计算输出token数 - 优先使用API返回的真实token数
outputTokens := 0
if resp.Usage.CompletionTokens > 0 {
    // 使用API返回的真实输出token数
    outputTokens = resp.Usage.CompletionTokens
} else if len(resp.Choices) > 0 {
    // 降级方案：如果API没有返回token数，使用字符数估算
    outputTokens = len([]rune(resp.Choices[0].Message.Content))
}
```

**说明**:
- ✅ **优先使用**: `resp.Usage.CompletionTokens`（服务端返回的真实输出token数）
- ⚠️ **降级方案**: 如果服务端没有返回token数，才使用字符数（rune数）估算

#### 输入Token数（PromptTokens）

```go
// 计算输入token数 - 优先使用API返回的真实token数
inputTokens := 0
if resp.Usage.PromptTokens > 0 {
    inputTokens = resp.Usage.PromptTokens
} else {
    // 降级方案：使用字符数估算
    inputTokens = len([]rune(testCase.Prompt))
}
```

**说明**:
- ✅ **优先使用**: `resp.Usage.PromptTokens`（服务端返回的真实输入token数）
- ⚠️ **降级方案**: 如果服务端没有返回token数，才使用字符数（rune数）估算

### 1.2 API响应结构

**文件**: `internal/api/client.go:54-59`

```go
// Usage 使用统计
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`      // 输入token数
	CompletionTokens int `json:"completion_tokens"`   // 输出token数
	TotalTokens      int `json:"total_tokens"`        // 总token数
}
```

**说明**:
- 这些字段来自服务端API响应的 `usage` 对象
- 是服务端实际统计的token数，使用服务端的tokenizer计算

## 二、Token数值使用流程

### 2.1 完整流程

```
1. 发送API请求
   ↓
2. 接收API响应
   ↓
3. 检查 resp.Usage.CompletionTokens
   ├─ 如果 > 0 → 使用服务端返回的值 ✅
   └─ 如果 = 0 → 使用字符数估算 ⚠️
   ↓
4. 检查 resp.Usage.PromptTokens
   ├─ 如果 > 0 → 使用服务端返回的值 ✅
   └─ 如果 = 0 → 使用字符数估算 ⚠️
   ↓
5. 累计到统计变量
   - totalTokens = inputTokens + outputTokens
   - totalOutputTokens = outputTokens
```

### 2.2 使用服务端Token数的优势

1. **准确性高**: 使用服务端实际的tokenizer计算，与计费一致
2. **考虑编码**: 不同语言、不同tokenizer的编码方式不同
3. **真实反映**: 反映实际处理的token数，而不是估算值

### 2.3 降级方案的限制

**字符数估算的问题**:
- 中文字符：1个字符可能对应多个tokens
- 英文单词：1个单词可能对应1-2个tokens
- 标点符号：token数差异很大
- 编码差异：不同tokenizer的编码方式不同

**示例**:
```
文本: "你好，世界！"
字符数: 5个字符
实际token数: 可能是 8-10个tokens（取决于tokenizer）
```

## 三、代码验证

### 3.1 检查点

✅ **优先使用服务端返回的值**:
- `resp.Usage.CompletionTokens > 0` → 使用服务端值
- `resp.Usage.PromptTokens > 0` → 使用服务端值

✅ **降级方案**:
- 只有在服务端没有返回token数时才使用字符数估算
- 这是为了兼容性考虑

### 3.2 实际使用情况

**正常情况**（大多数API）:
- 服务端会返回 `usage` 对象
- 包含 `prompt_tokens`、`completion_tokens`、`total_tokens`
- 代码会使用这些真实值 ✅

**异常情况**（少数API）:
- 服务端没有返回 `usage` 对象
- 或返回的token数为0
- 代码会使用字符数估算作为降级方案 ⚠️

## 四、总结

### 4.1 回答您的问题

**是的，token数值计算优先使用服务端返回的tokens数值。**

1. ✅ **输出token数**: 优先使用 `resp.Usage.CompletionTokens`（服务端返回）
2. ✅ **输入token数**: 优先使用 `resp.Usage.PromptTokens`（服务端返回）
3. ⚠️ **降级方案**: 只有在服务端没有返回token数时，才使用字符数估算

### 4.2 优势

- **准确性**: 使用服务端实际统计的token数
- **一致性**: 与API计费使用的token数一致
- **可靠性**: 考虑不同tokenizer的编码差异

### 4.3 建议

1. **确保API返回usage信息**: 大多数现代LLM API都会返回token统计信息
2. **检查降级情况**: 如果发现使用了字符数估算，说明API没有返回token数
3. **验证准确性**: 对于重要的性能测试，建议验证API是否返回了token数
