package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"
)

var genCmd = &cobra.Command{
	Use:   "gen <prompt-file>",
	Short: "Generate responses from multiple AI models",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		promptFile := args[0]
		prompt, err := os.ReadFile(promptFile)
		if err != nil {
			return fmt.Errorf("failed to read prompt file: %w", err)
		}

		var wg sync.WaitGroup
		results := make(chan struct {
			model string
			resp  string
			err   error
		}, 3)

		// OpenAI
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := openai.NewClient(os.Getenv("OPENAI_API_KEY"))
			resp, err := client.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{
				Model: openai.GPT4oLatest,
				Messages: []openai.ChatCompletionMessage{
					{Role: "user", Content: string(prompt)},
				},
			})
			results <- struct {
				model string
				resp  string
				err   error
			}{
				model: "OpenAI",
				resp:  resp.Choices[0].Message.Content,
				err:   err,
			}
		}()

		// Gemini
		// Gemini via OpenRouter
		wg.Add(1)
		go func() {
			defer wg.Done()

			apiKey := os.Getenv("OPENROUTER_API_KEY")
			if apiKey == "" {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Gemini (OpenRouter)", err: fmt.Errorf("OPENROUTER_API_KEY environment variable not set")}
				return
			}

			requestBody := map[string]interface{}{
				"model": "google/gemini-2.5-pro-preview-03-25", // OpenRouter model name
				"messages": []map[string]any{
					{
						"role": "user",
						"content": []map[string]any{
							{"type": "text", "text": string(prompt)},
						},
					},
				},
			}
			requestBodyBytes, err := json.Marshal(requestBody)
			if err != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Gemini (OpenRouter)", err: fmt.Errorf("failed to marshal request body: %w", err)}
				return
			}

			req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(requestBodyBytes))
			if err != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Gemini (OpenRouter)", err: fmt.Errorf("failed to create request: %w", err)}
				return
			}

			req.Header.Set("Authorization", "Bearer "+apiKey)
			req.Header.Set("Content-Type", "application/json")
			// Optional but recommended headers for OpenRouter
			// req.Header.Set("HTTP-Referer", "YOUR_SITE_URL") // Replace with your site URL
			// req.Header.Set("X-Title", "YOUR_APP_NAME") // Replace with your app name

			client := &http.Client{Timeout: 20 * time.Minute}
			resp, err := client.Do(req)
			if err != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Gemini (OpenRouter)", err: fmt.Errorf("failed to send request: %w", err)}
				return
			}
			defer resp.Body.Close()

			responseBodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Gemini (OpenRouter)", err: fmt.Errorf("failed to read response body: %w", err)}
				return
			}

			if resp.StatusCode != http.StatusOK {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Gemini (OpenRouter)", err: fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(responseBodyBytes))}
				return
			}

			// Parse the OpenRouter response structure
			var responseBody struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
				Error *struct { // Check for API errors in the response body
					Message string `json:"message"`
					Type    string `json:"type"`
					Code    int64  `json:"code"`
				} `json:"error"`
			}
			err = json.Unmarshal(responseBodyBytes, &responseBody)
			if err != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Gemini (OpenRouter)", err: fmt.Errorf("failed to unmarshal response body: %w", err)}
				return
			}

			// Check for errors returned in the JSON body
			if responseBody.Error != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Gemini (OpenRouter)", err: fmt.Errorf("OpenRouter API error (%d): %s", responseBody.Error.Code, responseBody.Error.Message)}
				return
			}

			if len(responseBody.Choices) == 0 || responseBody.Choices[0].Message.Content == "" {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Gemini (OpenRouter)", err: fmt.Errorf("no content found in response")}
				return
			}

			results <- struct {
				model string
				resp  string
				err   error
			}{
				model: "Gemini (OpenRouter)",
				resp:  responseBody.Choices[0].Message.Content,
				err:   nil,
			}
		}()

		// Claude
		wg.Add(1)
		go func() {
			defer wg.Done()

			apiKey := os.Getenv("ANTHROPIC_API_KEY")
			if apiKey == "" {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Claude", err: fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")}
				return
			}

			requestBody := map[string]interface{}{
				"model":      "claude-3-5-sonnet-20241022", // Or use the specific model from curl example if needed
				"max_tokens": 2048,
				"messages": []map[string]string{
					{"role": "user", "content": string(prompt)},
				},
			}
			requestBodyBytes, err := json.Marshal(requestBody)
			if err != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Claude", err: fmt.Errorf("failed to marshal request body: %w", err)}
				return
			}

			req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(requestBodyBytes))
			if err != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Claude", err: fmt.Errorf("failed to create request: %w", err)}
				return
			}

			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("anthropic-version", "2023-06-01")
			req.Header.Set("content-type", "application/json")

			client := &http.Client{Timeout: 20 * time.Minute}
			resp, err := client.Do(req)
			if err != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Claude", err: fmt.Errorf("failed to send request: %w", err)}
				return
			}
			defer resp.Body.Close()

			responseBodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Claude", err: fmt.Errorf("failed to read response body: %w", err)}
				return
			}

			if resp.StatusCode != http.StatusOK {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Claude", err: fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(responseBodyBytes))}
				return
			}

			var responseBody struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			err = json.Unmarshal(responseBodyBytes, &responseBody)
			if err != nil {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Claude", err: fmt.Errorf("failed to unmarshal response body: %w", err)}
				return
			}

			if len(responseBody.Content) == 0 {
				results <- struct {
					model string
					resp  string
					err   error
				}{model: "Claude", err: fmt.Errorf("no content found in response")}
				return
			}

			results <- struct {
				model string
				resp  string
				err   error
			}{
				model: "Claude",
				resp:  responseBody.Content[0].Text,
				err:   nil,
			}
		}()

		go func() {
			wg.Wait()
			close(results)
		}()

		var successfulResponses []struct {
			model string
			resp  string
		}

		for result := range results {
			if result.err != nil {
				fmt.Printf("%s error: %v\n", result.model, result.err)
				continue
			}
			fmt.Printf("\n=== %s Response ===\n%s\n", result.model, result.resp)
			successfulResponses = append(successfulResponses, struct {
				model string
				resp  string
			}{model: result.model, resp: result.resp})
		}

		if len(successfulResponses) > 0 {
			fmt.Println("\n=== Merging Responses ===")
			// Create a client specifically for merging, or reuse if appropriate
			// Note: Reusing the OpenAI client from the goroutine might be complex due to scope.
			// Creating a new one here is simpler for this example.
			mergeClient := openai.NewClient(os.Getenv("OPENAI_API_KEY"))
			mergedResponse, err := mergeResponses(mergeClient, successfulResponses)
			if err != nil {
				fmt.Printf("Error merging responses: %v\n", err)
			} else {
				fmt.Println("\n=== Merged Response ===\n", mergedResponse)
			}
		} else {
			fmt.Println("\nNo successful responses to merge.")
		}

		return nil
	},
}

func mergeResponses(client *openai.Client, responses []struct {
	model string
	resp  string
}) (string, error) {
	prompt := "Below are responses from different AI models to the same prompt. Please analyze these responses and provide either:\n" +
		"1. The best single response if one clearly stands out, or\n" +
		"2. A merged response that combines the unique insights and important points from all responses.\n\n"

	for _, resp := range responses {
		prompt += fmt.Sprintf("=== %s Response ===\n%s\n\n", resp.model, resp.resp)
	}

	resp, err := client.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{
		Model: openai.GPT4oLatest,
		Messages: []openai.ChatCompletionMessage{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to merge responses: %w", err)
	}

	return resp.Choices[0].Message.Content, nil
}

func init() {
	rootCmd.AddCommand(genCmd)
}
