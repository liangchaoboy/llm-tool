//go:build tools
// +build tools

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

// Scenario represents a single test case
type Scenario struct {
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

// Worker function that processes requests from the jobs channel
func worker(ctx context.Context, wg *sync.WaitGroup, jobs <-chan int, results chan<- Scenario, errors chan<- error, client *APIClient, maxWorkers int) {
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

			scenario := Scenario{
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

// bufferedWriteToFile writes scenarios to file in batches to reduce memory usage
func bufferedWriteToFile(outputFile string, scenarios <-chan Scenario, batchSize int) error {
	// Create output directory if it doesn't exist
	os.MkdirAll("../test-data", os.ModePerm)

	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer file.Close()

	// Write JSON array opening bracket
	if _, err := file.WriteString("[\n"); err != nil {
		return fmt.Errorf("failed to write opening bracket: %v", err)
	}

	buffer := make([]Scenario, 0, batchSize)
	totalWritten := 0
	firstItem := true

	for scenario := range scenarios {
		buffer = append(buffer, scenario)

		// When buffer is full, write the batch to file
		if len(buffer) >= batchSize {
			// Write batch to file
			for i, item := range buffer {
				if !firstItem || i > 0 {
					if _, err := file.WriteString(",\n"); err != nil {
						return fmt.Errorf("failed to write separator: %v", err)
					}
				}

				encoder := json.NewEncoder(file)
				if err := encoder.Encode(item); err != nil {
					return fmt.Errorf("failed to encode item: %v", err)
				}
				// Remove trailing newline that Encode adds by truncating and rewriting properly
				totalWritten++

				if firstItem {
					firstItem = false
				}
			}
			buffer = buffer[:0] // Clear buffer
			fmt.Printf("Flushed batch of %d items to file, total written: %d...\n", batchSize, totalWritten)
		}
	}

	// Write remaining items in buffer
	if len(buffer) > 0 {
		for i, item := range buffer {
			if !firstItem || i > 0 {
				if _, err := file.WriteString(",\n"); err != nil {
					return fmt.Errorf("failed to write separator: %v", err)
				}
			}

			encoder := json.NewEncoder(file)
			if err := encoder.Encode(item); err != nil {
				return fmt.Errorf("failed to encode item: %v", err)
			}
			totalWritten++

			if firstItem {
				firstItem = false
			}
		}
		fmt.Printf("Flushed final batch of %d items to file, total written: %d...\n", len(buffer), totalWritten)
	}

	// Write JSON array closing bracket
	if _, err := file.WriteString("\n]"); err != nil {
		return fmt.Errorf("failed to write closing bracket: %v", err)
	}

	return nil
}

// GenerateScenariosConcurrent generates test scenarios concurrently using API calls with buffered writing
func GenerateScenariosConcurrent(count int, maxWorkers int, client *APIClient, outputFile string) error {
	// Use a smaller channel size to limit memory usage
	jobs := make(chan int, maxWorkers*2) // Limit jobs channel size too
	results := make(chan Scenario, maxWorkers*2)
	errors := make(chan error, maxWorkers*2)

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

	// Process results with buffered writing
	err := bufferedWriteToFile(outputFile, results, 50) // Buffer 50 items at a time to save memory

	// Also handle any errors that occurred during the process
	go func() {
		var encounteredErrors []error
		for err := range errors {
			encounteredErrors = append(encounteredErrors, err)
		}

		if len(encounteredErrors) > 0 {
			fmt.Printf("Encountered %d errors during generation\n", len(encounteredErrors))
			for _, err := range encounteredErrors[:min(5, len(encounteredErrors))] { // Print first 5 errors
				fmt.Printf("Error: %v\n", err)
			}
		}
	}()

	return err
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
	outputFile := flag.String("output", "../test-data/buffered-api-test-cases.json", "Output file for test cases")
	count := flag.Int("count", 10, "Number of test cases to generate")
	concurrency := flag.Int("concurrency", 5, "Number of concurrent API requests")

	flag.Parse()

	// Validate required parameters
	if *apiKey == "" {
		fmt.Println("Error: API key is required. Use -api-key to specify it.")
		return
	}

	// Create API client
	client := NewAPIClient(*BaseURL, *apiKey, *modelName)

	fmt.Printf("Generating %d test scenarios using LLM API with concurrency=%d...\n", *count, *concurrency)
	fmt.Printf("Using model: %s, base URL: %s\n", *modelName, *BaseURL)

	startTime := time.Now()

	// Generate test scenarios concurrently with buffered writing
	err := GenerateScenariosConcurrent(*count, *concurrency, client, *outputFile)
	if err != nil {
		fmt.Printf("Error generating scenarios: %v\n", err)
		return
	}

	duration := time.Since(startTime)
	fmt.Printf("Completed generation of test scenarios in %v\n", duration)

	fmt.Printf("Successfully generated test scenarios, saved to %s\n", *outputFile)

	// Show completion message
	fmt.Println("Generation completed successfully!")
}
