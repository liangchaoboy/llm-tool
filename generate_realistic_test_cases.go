package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"
)

// TestCase represents a single test case
type TestCase struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"` // thinking or non_thinking
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Prompt       string  `json:"prompt"`
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
}

// estimateTokenCount estimates the number of tokens in text
func estimateTokenCount(text string) int {
	// Simple estimation: average ~2.5 chars per token
	return int(float64(len([]rune(text))) / 2.5)
}

// generateRealisticThinkingPrompt generates realistic thinking scenario prompts
func generateRealisticThinkingPrompt() string {
	// Realistic thinking scenario prompts
	thinkings := []string{
		"在当前的全球供应链重构背景下，某制造企业面临着原材料价格上涨、物流成本增加、劳动力短缺等多重挑战。公司需要制定一套全面的战略来应对这些挑战，包括但不限于：供应商多元化策略、自动化升级计划、库存优化方案、质量管理改进措施、成本控制机制、人力资源配置优化、数字化转型路径、可持续发展目标等。请从战略规划、运营优化、财务分析、风险管理、技术创新等多个维度，深入分析该企业面临的挑战和机遇，制定一套切实可行的应对方案，并评估实施风险和预期收益。",

		"随着人工智能技术的快速发展，大型语言模型在医疗诊断领域的应用日益广泛。某医院计划引入AI辅助诊断系统来提高诊疗效率和准确性。然而，这一举措面临着技术可靠性、伦理合规性、医生接受度、患者信任度、法律责任界定、数据隐私保护、系统集成难度、成本效益分析等多方面的挑战。请详细分析AI在医疗诊断中应用的利弊，提出系统实施的技术路线图，制定相应的风险管控措施，并评估其对医患关系、医疗质量、成本控制的长远影响。",

		"一家金融科技公司正在开发基于区块链技术的跨境支付解决方案，旨在降低交易成本、提高结算效率、增强安全性。该项目需要考虑技术架构设计、合规监管要求、市场推广策略、合作伙伴关系、竞争对手分析、用户接受度、网络安全保障、国际法律框架等多方面因素。请从技术可行性、商业价值、法律合规、市场前景等角度对该方案进行全面评估，提出具体的实施方案和风险应对策略。",

		"某电商平台面临激烈的市场竞争，需要重新制定其用户增长和留存策略。公司拥有大量的用户行为数据、购买记录、偏好信息等，但如何有效利用这些数据来驱动业务增长仍是一个挑战。请基于用户画像分析、消费行为研究、市场细分策略、个性化推荐算法、营销渠道优化、客户生命周期管理等维度，设计一套完整的用户增长和留存方案，并制定数据驱动的决策机制。",

		"在碳中和目标下，某传统能源企业需要制定向清洁能源转型的战略规划。这涉及技术路线选择、投资布局调整、业务模式创新、人才结构优化、政策风险应对、市场竞争策略等多方面考量。请从行业发展趋势、技术演进路径、政策导向分析、财务影响评估、组织变革管理等角度，为企业制定一份详尽的转型战略规划。",
	}

	return thinkings[rand.Intn(len(thinkings))]
}

// generateRealisticNonThinkingPrompt generates realistic non-thinking scenario prompts
func generateRealisticNonThinkingPrompt() string {
	// Realistic non-thinking scenario prompts
	nonThinkings := []string{
		"将以下英文段落翻译成中文：The rapid advancement of artificial intelligence has revolutionized various industries, from healthcare and finance to transportation and entertainment. Machine learning algorithms now power recommendation systems, fraud detection mechanisms, autonomous vehicles, and personalized content delivery. Natural language processing enables seamless communication between humans and machines, while computer vision facilitates automated inspection and quality control. The integration of AI technologies continues to reshape business models and operational processes across sectors.",

		"请用简洁的语言概括以下文本的主要内容：Blockchain technology operates on a decentralized network of computers that maintain a shared ledger of transactions. Each transaction is verified by multiple participants in the network before being added to the chain. Cryptographic techniques ensure the integrity and immemence of the data. Smart contracts enable automated execution of agreements without intermediaries. The distributed nature of blockchain provides enhanced security, transparency, and resistance to tampering, making it suitable for applications ranging from cryptocurrency to supply chain management.",

		"请列出JavaScript中最重要的十个数组方法，并简要说明其功能：1. map() - 创建新数组，对每个元素执行函数；2. filter() - 创建满足条件的元素组成的新数组；3. reduce() - 将数组缩减为单一值；4. forEach() - 对每个元素执行函数；5. find() - 查找第一个满足条件的元素；6. findIndex() - 查找第一个满足条件的元素索引；7. slice() - 提取部分数组；8. splice() - 添加/删除数组元素；9. concat() - 合并数组；10. includes() - 检查元素是否存在。",

		"请用Python实现快速排序算法：def quicksort(arr):\n    if len(arr) <= 1:\n        return arr\n    pivot = arr[len(arr) // 2]\n    left = [x for x in arr if x < pivot]\n    middle = [x for x in arr if x == pivot]\n    right = [x for x in arr if x > pivot]\n    return quicksort(left) + middle + quicksort(right)\n\n# 示例\narr = [3, 6, 8, 10, 1, 2, 1]\nprint(quicksort(arr))",

		"将以下英文句子翻译成中文：The Internet of Things connects everyday devices to the internet, enabling them to collect and exchange data. Smart homes use IoT devices to automate lighting, heating, security, and appliances. Industrial IoT improves manufacturing efficiency through predictive maintenance and real-time monitoring. Healthcare IoT enables remote patient monitoring and wearable health devices. The widespread adoption of IoT raises concerns about privacy, security, and data management.",
	}

	return nonThinkings[rand.Intn(len(nonThinkings))]
}

// expandToTargetTokens expands text to reach target token count
func expandToTargetTokens(text string, targetTokens int) string {
	currentTokens := estimateTokenCount(text)

	if currentTokens >= targetTokens {
		// Truncate to target length if too long
		targetChars := int(float64(targetTokens) * 2.5)
		runes := []rune(text)
		if len(runes) > targetChars {
			return string(runes[:targetChars])
		}
		return text
	}

	// Expansion templates
	templates := []string{
		" 这一问题的重要性在于它直接影响到相关领域的未来发展。从历史经验来看，类似问题的解决往往需要综合考虑多个方面的因素。通过系统性分析，我们可以更好地理解问题的本质。",
		" 在实际应用中，该问题的解决方案需要充分考虑实施的可行性。不同的解决方案可能带来不同的成本效益比，需要进行综合评估。",
		" 从技术角度来看，实现这一目标需要克服诸多挑战。相关技术的发展历程为我们提供了宝贵的经验和启示。",
		" 市场环境的变化也为该问题的解决带来了新的机遇和挑战。深入了解市场需求是制定有效策略的前提。",
		" 监管政策的变化同样不容忽视。合规性要求是任何解决方案都必须满足的基本条件。",
		" 用户体验的优化也是关键考量因素。良好的用户体验是产品成功的重要保障。",
		" 安全性和可靠性是基础要求。任何解决方案都需要在安全性和功能性之间取得平衡。",
		" 未来的发展趋势也需要纳入考虑范围。前瞻性的规划有助于应对未来的挑战。",
		" 国际经验可以为我们提供有益的参考。通过比较分析，可以发现更适合本土情况的解决方案。",
		" 综合以上分析，我们可以得出以下结论。这些结论将为后续的实施提供指导。",
	}

	result := text
	rand.Seed(time.Now().UnixNano())

	// Expand until reaching target tokens
	for estimateTokenCount(result) < targetTokens {
		if len(templates) > 0 {
			// Randomly select a template
			templateIdx := rand.Intn(len(templates))
			newResult := result + templates[templateIdx]

			// Check if adding this template exceeds target
			if estimateTokenCount(newResult) <= targetTokens {
				result = newResult
				// Remove used template to avoid repetition
				templates = append(templates[:templateIdx], templates[templateIdx+1:]...)
			} else {
				// If adding template exceeds target, break and use filler
				break
			}
		} else {
			// If no templates left, use simple filler
			result += " 这是一段补充内容，用于达到所需的token数量。"
			if estimateTokenCount(result) >= targetTokens {
				break
			}
		}
	}

	return result
}

// generateTestCases creates test cases with realistic prompts
func generateTestCases(count int) []TestCase {
	var testCases []TestCase

	rand.Seed(time.Now().UnixNano())

	for i := 0; i < count; i++ {
		var testCase TestCase
		testCase.ID = fmt.Sprintf("case_%d", i+1)

		// Maintain 1:4 ratio (thinking:non-thinking)
		isThinking := (i%5 == 0)

		if isThinking {
			// Thinking case: 6000 input tokens, 1000 output tokens
			testCase.Type = "thinking"
			testCase.InputTokens = 6000
			testCase.OutputTokens = 1000
			testCase.MaxTokens = 1000
			testCase.Temperature = 0.7
			basicPrompt := generateRealisticThinkingPrompt()
			testCase.Prompt = expandToTargetTokens(basicPrompt, 6000)
		} else {
			// Non-thinking case: 5500 input tokens, 500 output tokens
			testCase.Type = "non_thinking"
			testCase.InputTokens = 5500
			testCase.OutputTokens = 500
			testCase.MaxTokens = 500
			testCase.Temperature = 0.3
			basicPrompt := generateRealisticNonThinkingPrompt()
			testCase.Prompt = expandToTargetTokens(basicPrompt, 5500)
		}

		testCases = append(testCases, testCase)
	}

	return testCases
}

func main() {
	fmt.Println("Generating realistic test cases...")

	// Generate test cases
	testCases := generateTestCases(50000) // Generate full set of 50,000 cases

	// Save to JSON file
	file, err := os.Create("test-data/large-realistic-test-cases.json")
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	err = encoder.Encode(testCases)
	if err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return
	}

	fmt.Printf("Successfully generated %d realistic test cases, saved to test-data/large-realistic-test-cases.json\n", len(testCases))

	// Verify token counts for a sample
	fmt.Println("\nVerifying token counts for sample cases:")
	for i, tc := range testCases[:5] {
		actualTokens := estimateTokenCount(tc.Prompt)
		difference := actualTokens - tc.InputTokens
		status := "✓"
		if abs(difference) > 200 { // Tolerate up to 200 token difference
			status = "⚠"
		}
		fmt.Printf("%s %s (%s): Target=%d, Actual=%d, Diff=%d\n",
			status, tc.ID, tc.Type, tc.InputTokens, actualTokens, difference)
	}

	// Show a sample of the content
	fmt.Println("\nSample content preview:")
	for i, tc := range testCases[:2] {
		contentPreview := tc.Prompt
		if len(contentPreview) > 200 {
			contentPreview = contentPreview[:200] + "..."
		}
		fmt.Printf("\n%s (%s):\n%s\n", tc.ID, tc.Type, contentPreview)
	}

	// Calculate and report ratio
	thinkingCount := 0
	nonThinkingCount := 0
	for _, tc := range testCases {
		if tc.Type == "thinking" {
			thinkingCount++
		} else {
			nonThinkingCount++
		}
	}
	fmt.Printf("\nFinal ratio: thinking=%d, non-thinking=%d, ratio=1:%.2f\n",
		thinkingCount, nonThinkingCount, float64(nonThinkingCount)/float64(thinkingCount))
}

// abs returns absolute value
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
