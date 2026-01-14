package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
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

// APIRequest represents the request to the LLM API
type APIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type APIResponse struct {
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Message Message `json:"message"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// APIClient represents an API client for LLM
type APIClient struct {
	BaseURL    string
	APIKey     string
	ModelName  string
	HTTPClient *http.Client
}

// NewAPIClient creates a new API client
func NewAPIClient(baseURL, apiKey, modelName string) *APIClient {
	return &APIClient{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		ModelName: modelName,
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// GeneratePrompt generates a prompt with the specified token count and thinking type
func (client *APIClient) GeneratePrompt(inputTokens int, isThinking bool) (string, error) {
	taskDescription := ""
	temp := 0.7
	if isThinking {
		taskDescription = fmt.Sprintf("生成一个用于测试LLM性能的深度思考类问题。这个问题应该是复杂的、需要深入分析的，比如涉及战略规划、技术分析、商业策略、科学研究等。要求生成的文本长度约为%d个tokens。问题应该具有现实性，能够测试模型的深层理解和推理能力。请确保内容详细、全面、具有挑战性。", inputTokens)
		temp = 0.7
	} else {
		taskDescription = fmt.Sprintf("生成一个用于测试LLM性能的具体任务指令。这应该是一个相对简单的任务，比如翻译、概括、列表生成、代码实现、计算等。要求生成的文本长度约为%d个tokens。指令应该明确具体，不需要复杂的推理过程。请确保内容实用、清晰、直接。", inputTokens)
		temp = 0.3
	}

	req := APIRequest{
		Model: client.ModelName,
		Messages: []Message{
			{
				Role:    "user",
				Content: taskDescription,
			},
		},
		Temperature: temp,
		MaxTokens:   inputTokens * 2, // Request more tokens to ensure we get enough content
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", client.BaseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+client.APIKey)

	resp, err := client.HTTPClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp APIResponse
	err = json.Unmarshal(respBody, &apiResp)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return apiResp.Choices[0].Message.Content, nil
}

func main() {
	// Define command-line flags
	apiKey := flag.String("api-key", "", "API key for the LLM service")
	BaseURL := flag.String("base-url", "https://api.openai.com/v1/chat/completions", "Base URL for the LLM API")
	modelName := flag.String("model", "gpt-3.5-turbo", "Model name to use")
	outputFile := flag.String("output", "../test-data/generated-test-cases.json", "Output file for test cases")
	count := flag.Int("count", 10, "Number of test cases to generate")

	flag.Parse()

	// Validate required parameters
	if *apiKey == "" {
		fmt.Println("Error: API key is required. Use -api-key to specify it.")
		return
	}

	// Create API client
	client := NewAPIClient(*BaseURL, *apiKey, *modelName)

	fmt.Printf("Generating %d test cases using LLM API...\n", *count)
	fmt.Printf("Using model: %s, base URL: %s\n", *modelName, *BaseURL)

	// Generate test cases
	testCases := make([]TestCase, 0, *count)

	for i := 0; i < *count; i++ {
		// Alternate between thinking and non-thinking with 1:4 ratio
		isThinking := (i%5 == 0) // Every 5th item is thinking (1 out of 5)

		var inputTokens int
		var outputTokens int
		var temperature float64
		var maxTokens int

		if isThinking {
			inputTokens = 6000
			outputTokens = 1000
			temperature = 0.7
			maxTokens = 1000
		} else {
			inputTokens = 5500
			outputTokens = 500
			temperature = 0.3
			maxTokens = 500
		}

		fmt.Printf("Generating test case %d/%d (%s)... ", i+1, *count,
			map[bool]string{true: "thinking", false: "non-thinking"}[isThinking])

		prompt, err := client.GeneratePrompt(inputTokens, isThinking)
		if err != nil {
			fmt.Printf("Error generating prompt: %v\n", err)
			continue
		}

		testCase := TestCase{
			ID:           fmt.Sprintf("llm_generated_%d", i+1),
			Type:         map[bool]string{true: "thinking", false: "non-thinking"}[isThinking],
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			Prompt:       prompt,
			Temperature:  temperature,
			MaxTokens:    maxTokens,
		}

		testCases = append(testCases, testCase)
		fmt.Println("Done")
	}

	if len(testCases) == 0 {
		fmt.Println("No test cases were generated successfully.")
		return
	}

	// Create output directory if it doesn't exist
	os.MkdirAll("../test-data", os.ModePerm)

	// Save to JSON file
	file, err := os.Create(*outputFile)
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

	// Count thinking vs non-thinking cases
	thinkingCount := 0
	nonThinkingCount := 0
	for _, tc := range testCases {
		if tc.Type == "thinking" {
			thinkingCount++
		} else {
			nonThinkingCount++
		}
	}

	fmt.Printf("\nSuccessfully generated %d test cases, saved to %s\n", len(testCases), *outputFile)
	fmt.Printf("Thinking cases: %d, Non-thinking cases: %d\n", thinkingCount, nonThinkingCount)
	fmt.Printf("Ratio: 1:%.1f (should be 1:4)\n", float64(nonThinkingCount)/float64(thinkingCount))

	// Show a sample
	fmt.Println("\nSample of generated test cases:")
	for i, tc := range testCases {
		if i >= 2 { // Show first 2 samples
			break
		}
		contentPreview := tc.Prompt
		if len(contentPreview) > 200 {
			contentPreview = contentPreview[:200] + "..."
		}
		fmt.Printf("[%d] %s (%s): %d chars\n%s\n\n",
			i+1, tc.ID, tc.Type, len(tc.Prompt), contentPreview)
	}
}
