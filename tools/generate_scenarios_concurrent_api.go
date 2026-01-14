package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// ConcurrentScenario represents a single test case for our scenario
type ConcurrentScenario struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"` // thinking or non_thinking
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Prompt       string  `json:"prompt"`
	Temperature  float64 `json:"temperature"`
	MaxTokens    int     `json:"max_tokens"`
}

// LLMAPIRequest represents the request to the LLM API
type LLMAPIRequest struct {
	Model       string       `json:"model"`
	Messages    []LLMMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens"`
}

type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMResponse struct {
	Choices []LLMChoice `json:"choices"`
	Usage   LLMUsage    `json:"usage"`
}

type LLMChoice struct {
	Message LLMMessage `json:"message"`
}

type LLMUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// LLMAPIClient represents an API client for LLM
type LLMAPIClient struct {
	BaseURL    string
	APIKey     string
	ModelName  string
	HTTPClient *http.Client
}

// NewLLMAPIClient creates a new API client
func NewLLMAPIClient(baseURL, apiKey, modelName string) *LLMAPIClient {
	return &LLMAPIClient{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		ModelName: modelName,
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// GeneratePrompt generates a prompt with the specified token count and thinking type
func (client *LLMAPIClient) GeneratePrompt(inputTokens int, isThinking bool) (string, error) {
	taskDescription := ""
	temp := 0.7
	if isThinking {
		taskDescription = fmt.Sprintf("生成一个用于测试LLM性能的深度思考类问题。这个问题应该是复杂的、需要深入分析的，比如涉及战略规划、技术分析、商业策略、科学研究等。要求生成的文本长度约为%d个tokens。问题应该具有现实性，能够测试模型的深层理解和推理能力。请确保内容详细、全面、具有挑战性。", inputTokens)
		temp = 0.7
	} else {
		taskDescription = fmt.Sprintf("生成一个用于测试LLM性能的具体任务指令。这应该是一个相对简单的任务，比如翻译、概括、列表生成、代码实现、计算等。要求生成的文本长度约为%d个tokens。指令应该明确具体，不需要复杂的推理过程。请确保内容实用、清晰、直接。", inputTokens)
		temp = 0.3
	}

	req := LLMAPIRequest{
		Model: client.ModelName,
		Messages: []LLMMessage{
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

	var apiResp LLMResponse
	err = json.Unmarshal(respBody, &apiResp)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return apiResp.Choices[0].Message.Content, nil
}

// Worker function that processes requests from the jobs channel
func worker(ctx context.Context, wg *sync.WaitGroup, jobs <-chan int, results chan<- ConcurrentScenario, errors chan<- error, client *LLMAPIClient, maxWorkers int) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case i, ok := <-jobs:
			if !ok {
				return // Channel closed
			}

			// Determine if this should be a thinking or non-thinking case (1:4 ratio)
			isThinking := (i%5 == 0)

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

			// Generate prompt via API call
			prompt, err := client.GeneratePrompt(inputTokens, isThinking)
			if err != nil {
				errors <- fmt.Errorf("error generating prompt for index %d: %v", i, err)
				continue
			}

			scenario := ConcurrentScenario{
				ID:           fmt.Sprintf("api_scenario_%d", i+1),
				Type:         map[bool]string{true: "thinking", false: "non_thinking"}[isThinking],
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				Prompt:       prompt,
				Temperature:  temperature,
				MaxTokens:    maxTokens,
			}

			results <- scenario
		}
	}
}

// GenerateScenariosConcurrent generates test scenarios concurrently using API calls
func GenerateScenariosConcurrent(count int, maxWorkers int, client *LLMAPIClient) ([]ConcurrentScenario, error) {
	jobs := make(chan int, count)
	results := make(chan ConcurrentScenario, count)
	errors := make(chan error, count)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Start workers
	for w := 0; w < maxWorkers; w++ {
		wg.Add(1)
		go worker(ctx, &wg, jobs, results, errors, client, maxWorkers)
	}

	// Send jobs
	go func() {
		for i := 0; i < count; i++ {
			jobs <- i
		}
		close(jobs)
	}()

	// Close results channel when all workers are done
	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	// Collect results
	var scenarios []ConcurrentScenario
	var encounteredErrors []error

	for result := range results {
		scenarios = append(scenarios, result)
		if len(scenarios)%100 == 0 {
			fmt.Printf("Generated %d/%d scenarios...\n", len(scenarios), count)
		}
	}

	for err := range errors {
		encounteredErrors = append(encounteredErrors, err)
	}

	if len(encounteredErrors) > 0 {
		fmt.Printf("Encountered %d errors during generation\n", len(encounteredErrors))
		for _, err := range encounteredErrors[:min(5, len(encounteredErrors))] { // Print first 5 errors
			fmt.Printf("Error: %v\n", err)
		}
	}

	return scenarios, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	// Define command-line flags
	apiKey := flag.String("api-key", "", "API key for the LLM service")
	BaseURL := flag.String("base-url", "https://api.openai.com/v1/chat/completions", "Base URL for the LLM API")
	modelName := flag.String("model", "gpt-3.5-turbo", "Model name to use")
	outputFile := flag.String("output", "../test-data/concurrent-api-test-cases.json", "Output file for test cases")
	count := flag.Int("count", 10, "Number of test cases to generate")
	concurrency := flag.Int("concurrency", 5, "Number of concurrent API requests")

	flag.Parse()

	// Validate required parameters
	if *apiKey == "" {
		fmt.Println("Error: API key is required. Use -api-key to specify it.")
		return
	}

	// Create API client
	client := NewLLMAPIClient(*BaseURL, *apiKey, *modelName)

	fmt.Printf("Generating %d test scenarios using LLM API with concurrency=%d...\n", *count, *concurrency)
	fmt.Printf("Using model: %s, base URL: %s\n", *modelName, *BaseURL)

	startTime := time.Now()

	// Generate test scenarios concurrently
	scenarios, err := GenerateScenariosConcurrent(*count, *concurrency, client)
	if err != nil {
		fmt.Printf("Error generating scenarios: %v\n", err)
		return
	}

	duration := time.Since(startTime)
	fmt.Printf("Generated %d scenarios in %v\n", len(scenarios), duration)

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

	err = encoder.Encode(scenarios)
	if err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return
	}

	fmt.Printf("Successfully generated %d test scenarios, saved to %s\n", len(scenarios), *outputFile)

	// Calculate and report ratio
	thinkingCount := 0
	nonThinkingCount := 0
	for _, sc := range scenarios {
		if sc.Type == "thinking" {
			thinkingCount++
		} else {
			nonThinkingCount++
		}
	}
	fmt.Printf("Final ratio: thinking=%d, non-thinking=%d, ratio=1:%.2f\n",
		thinkingCount, nonThinkingCount, float64(nonThinkingCount)/float64(thinkingCount))

	// Show a sample of the content
	fmt.Println("\nSample content preview:")
	for i, sc := range scenarios {
		if i >= 2 { // Show first 2 samples
			break
		}
		contentPreview := sc.Prompt
		if len(contentPreview) > 200 {
			contentPreview = contentPreview[:200] + "..."
		}
		fmt.Printf("\n%s (%s): %d tokens\n%s\n", sc.ID, sc.Type, len([]rune(sc.Prompt))/2, contentPreview)
	}
}
