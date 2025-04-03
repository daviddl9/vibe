package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	openRouterAPIURL = "https://openrouter.ai/api/v1/chat/completions"
	// Model updated as per previous user code
	defaultModel   = "anthropic/claude-3.5-sonnet"
	apiKeyEnvVar   = "OPENROUTER_API_KEY"
	commandVersion = "vibe-code/0.1.1"                  // Incremented version slightly
	projectURL     = "https://github.com/daviddl9/vibe" // Project URL from previous user code
)

// --- Variables for flags ---
var (
	llmModel string
	noStream bool // Flag to DISABLE streaming (streaming is now default)
)

// --- Structs for API Interaction (Identical to previous version) ---

// openRouterRequest represents the base JSON payload for the OpenRouter API
type openRouterRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	// Stream field is handled dynamically before sending
}

// message represents a single message in the chat history
type message struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// openRouterResponse represents the expected JSON response for non-streaming requests
type openRouterResponse struct {
	ID      string   `json:"id"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
	Error   apiError `json:"error,omitempty"` // Capture potential API errors
}

type choice struct {
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// openRouterStreamResponse represents the structure of a streaming chunk
type openRouterStreamResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []streamChoice `json:"choices"`
	Error   apiError       `json:"error,omitempty"` // Capture potential API errors in stream
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason,omitempty"` // Pointer to handle potential null
}

type streamDelta struct {
	// Role string `json:"role"` // Sometimes present
	Content string `json:"content"`
}

// apiError represents error structure sometimes returned in the JSON body
type apiError struct {
	Code    *string `json:"code,omitempty"` // Using pointer to handle potential null
	Message string  `json:"message"`
	Param   *string `json:"param,omitempty"`
	Type    string  `json:"type"`
}

// --- Cobra Command Definition ---

// codeCmd represents the code command
var codeCmd = &cobra.Command{
	Use:   "code \"<prompt>\" [target_directory]",
	Short: "Uses an LLM to modify code based on project context and a prompt (streams by default)",
	Long: `Gathers relevant files from the specified directory (or current directory if none provided),
constructs a prompt including the file context and your request, and sends it
to an LLM via the OpenRouter API (requires OPENROUTER_API_KEY env var).

Output is streamed by default as it arrives from the LLM.
Use the --no-stream flag to wait for the full response before displaying.
Renders the final output as Markdown in the terminal.

Example:
  vibe code "add a function in lib/a.go to multiply the Answer by 2" .
  vibe code "refactor main.go to print the result" --no-stream
  vibe code "explain the main package" ./mygocode -m openai/gpt-4o`,
	Args: cobra.RangeArgs(1, 2), // Requires 1 (prompt) or 2 (prompt, directory) arguments
	RunE: func(cmd *cobra.Command, args []string) error {
		userPrompt := args[0]
		targetDir := "." // Default to current directory
		if len(args) == 2 {
			targetDir = args[1]
		}

		// Determine if streaming should be used (default is true unless --no-stream is present)
		streamOutput := !noStream // <--- Streaming is true if noStream is false

		// --- 1. Get API Key ---
		apiKey := os.Getenv(apiKeyEnvVar)
		if apiKey == "" {
			return fmt.Errorf("API key not found. Please set the %s environment variable", apiKeyEnvVar)
		}

		// --- 2. Validate Target Directory ---
		absTargetDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for %s: %w", targetDir, err)
		}
		info, err := os.Stat(absTargetDir)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("directory not found: %s", absTargetDir)
			}
			return fmt.Errorf("failed to stat %s: %w", absTargetDir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("path is not a directory: %s", absTargetDir)
		}

		// --- 3. Gather Context ---
		fmt.Fprintf(os.Stderr, "Gathering context from: %s\n", absTargetDir) // Use Stderr for progress
		var contextBuilder strings.Builder
		filesCollected := 0
		skippedDirs := 0

		// Define files/dirs to skip more explicitly
		skipDirs := map[string]bool{
			".git":         true,
			"node_modules": true,
			"vendor":       true,
			"__pycache__":  true,
			"venv":         true,
			".venv":        true,
			"target":       true, // Common for Rust/Java
			"build":        true, // Common build output dir
		}
		// Define relevant extensions
		extensionsToInclude := map[string]bool{
			".go":           true,
			".html":         true,
			".py":           true,
			".js":           true,
			".ts":           true,
			".jsx":          true,
			".tsx":          true,
			".rs":           true,
			".java":         true,
			".kt":           true,
			".c":            true,
			".h":            true,
			".cpp":          true,
			".cs":           true,
			".rb":           true,
			".php":          true,
			".md":           true,
			".yaml":         true,
			".yml":          true,
			".toml":         true,
			".json":         true,
			"dockerfile":    true, // Match Dockerfile exactly
			".dockerignore": true,
			".sh":           true,
			".sql":          true,
			".env":          true, ".env.example": true,
		}

		err = filepath.WalkDir(absTargetDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: Error accessing path %q: %v\n", path, walkErr)
				if d != nil && d.IsDir() {
					return filepath.SkipDir // Skip directory if error accessing it
				}
				return nil // Attempt to continue if it was a file error
			}

			// Skip directories, hidden files/dirs based on defined lists
			if d.IsDir() {
				dirName := d.Name()
				if skipDirs[dirName] || (strings.HasPrefix(dirName, ".") && dirName != ".") {
					skippedDirs++
					return filepath.SkipDir
				}
				return nil // Continue walking into non-skipped directories
			}

			// Skip hidden files (allow specific dotfiles like .env)
			if strings.HasPrefix(d.Name(), ".") && !extensionsToInclude[d.Name()] {
				return nil
			}

			// Include files based on extension map or exact name matches
			include := false
			fileNameLower := strings.ToLower(d.Name())
			fileExtLower := strings.ToLower(filepath.Ext(fileNameLower))

			if extensionsToInclude[fileExtLower] || extensionsToInclude[fileNameLower] {
				include = true
			}

			if !include {
				return nil // Skip files not matching criteria
			}

			// Get absolute path for consistency in context
			absPath, _ := filepath.Abs(path) // Ignore error here, fallback below if needed
			if absPath == "" {
				absPath = path // Fallback
			}

			// Avoid reading excessively large files (e.g., > 5MB)
			fileInfo, statErr := d.Info()
			if statErr == nil && fileInfo.Size() > 5*1024*1024 {
				fmt.Fprintf(os.Stderr, "Warning: Skipping large file %s (>5MB)\n", path)
				return nil
			}

			content, readErr := os.ReadFile(path)
			if readErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: Error reading file %s: %v\n", path, readErr)
				return nil // Skip file if unreadable, but continue walk
			}

			// Add file header and content to context
			contextBuilder.WriteString(fmt.Sprintf("// File: %s\n", absPath))
			contextBuilder.Write(content)
			contextBuilder.WriteString("\n\n---\n\n") // Separator
			filesCollected++
			return nil
		})

		if err != nil {
			// This error is from WalkDir itself (e.g., initial permission error)
			return fmt.Errorf("error walking the path %q: %w", absTargetDir, err)
		}

		if filesCollected == 0 {
			fmt.Fprintln(os.Stderr, "Warning: No relevant files found for context in the target directory.")
			// Proceeding without file context
		} else {
			fmt.Fprintf(os.Stderr, "Collected context from %d file(s). (Skipped %d directories)\n", filesCollected, skippedDirs)
		}

		// --- 4. Construct LLM Prompt ---
		// System prompt explaining the task
		systemContent := fmt.Sprintf(`You are an expert programming assistant integrated into a CLI tool called 'vibe'.
The user is working in the project context provided below (code files from their directory).
Analyze the user's request and the provided file context carefully.
Generate the necessary code modifications, additions, or provide explanations as requested.
Format your response clearly using Markdown. Use language-specific code blocks (e.g., `+"```"+`go ... `+"```"+`, `+"```"+`python ... `+"```"+`).
If modifying existing code, clearly indicate the file and the changes. If adding new code, suggest where it should go.
Focus on fulfilling the user's request accurately based *only* on the provided context and general programming best practices for the relevant language(s).
Do not add extraneous conversation or introductory/concluding remarks outside of the requested code/explanation.

--- FILE CONTEXT START ---
%s
--- FILE CONTEXT END ---`, contextBuilder.String())

		// User prompt combining context preamble and the actual request
		userContent := fmt.Sprintf(`Based on the file context provided in the system message, fulfill the following request:

"%s"`, userPrompt)

		// --- 5. Make API Call ---
		// Use the determined streamOutput value here
		fmt.Fprintf(os.Stderr, "Sending request to OpenRouter model: %s (Streaming: %v)...\n", llmModel, streamOutput)

		requestPayload := openRouterRequest{
			Model: llmModel,
			Messages: []message{
				{Role: "system", Content: systemContent},
				{Role: "user", Content: userContent},
			},
		}

		// Marshal base payload first
		payloadBytes, err := json.Marshal(requestPayload)
		if err != nil {
			return fmt.Errorf("failed to marshal base request payload: %w", err)
		}

		// Use a map to easily add the 'stream' field conditionally
		finalPayloadMap := map[string]interface{}{}
		if err := json.Unmarshal(payloadBytes, &finalPayloadMap); err != nil {
			return fmt.Errorf("failed to unmarshal payload to map: %w", err)
		}
		// Add stream field based on the streamOutput variable
		if streamOutput {
			finalPayloadMap["stream"] = true
		} // No need for 'else', default is false / field absent

		// Marshal the final map containing the stream field if needed
		requestBodyBytes, err := json.Marshal(finalPayloadMap)
		if err != nil {
			return fmt.Errorf("failed to marshal final request payload: %w", err)
		}

		req, err := http.NewRequest("POST", openRouterAPIURL, bytes.NewBuffer(requestBodyBytes))
		if err != nil {
			return fmt.Errorf("failed to create HTTP request: %w", err)
		}

		// Set Headers
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("HTTP-Referer", projectURL) // Optional but recommended
		req.Header.Set("X-Title", commandVersion)  // Optional but recommended

		client := &http.Client{Timeout: 180 * time.Second} // Reasonable timeout
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send request to OpenRouter: %w", err)
		}
		defer resp.Body.Close()

		// --- 6. Process Response ---
		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			var apiErrResp openRouterResponse
			json.Unmarshal(bodyBytes, &apiErrResp) // Ignore unmarshal error here
			errMsg := ""
			if apiErrResp.Error.Message != "" {
				errMsg = fmt.Sprintf("API Error: Type=%s, Message=%s", apiErrResp.Error.Type, apiErrResp.Error.Message)
			} else {
				errMsg = fmt.Sprintf("Body: %s", string(bodyBytes)) // Fallback to raw body
			}
			return fmt.Errorf("received non-OK status code from OpenRouter: %d - %s. %s", resp.StatusCode, resp.Status, errMsg)
		}

		// --- 7. Display Result ---
		fmt.Println("\n--- LLM Response ---") // Print header to Stdout
		if streamOutput {
			// == Streaming Logic ==
			scanner := bufio.NewScanner(resp.Body)
			streamErrorOccurred := false
			for scanner.Scan() {
				line := scanner.Text()
				if line == "" {
					continue // Skip empty lines
				}

				if strings.HasPrefix(line, "data: ") {
					data := strings.TrimPrefix(line, "data: ")
					if data == "[DONE]" {
						break // End of stream
					}

					var chunk openRouterStreamResponse
					if err := json.Unmarshal([]byte(data), &chunk); err != nil {
						fmt.Fprintf(os.Stderr, "\nWarning: Failed to decode stream chunk: %v\nData: %s\n", err, data)
						streamErrorOccurred = true
						continue
					}

					if chunk.Error.Message != "" {
						fmt.Fprintf(os.Stderr, "\nAPI Error during stream: Type=%s, Message=%s\n", chunk.Error.Type, chunk.Error.Message)
						streamErrorOccurred = true
						continue // Or break
					}

					if len(chunk.Choices) > 0 {
						contentDelta := chunk.Choices[0].Delta.Content
						fmt.Print(contentDelta) // Print raw delta to stdout immediately
					}
				} // End if "data: "
			} // End scanner loop

			if err := scanner.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "\nError reading stream: %v\n", err)
				streamErrorOccurred = true
			}
			fmt.Println() // Add a newline after streaming is done / before rendering

			if streamErrorOccurred {
				fmt.Fprintln(os.Stderr, "Note: Errors occurred during streaming. Output may be incomplete.")
			}

		} else {
			// == Non-Streaming Logic ==
			var openRouterResp openRouterResponse
			bodyBytes, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return fmt.Errorf("failed to read non-streaming response body: %w", readErr)
			}

			if err := json.Unmarshal(bodyBytes, &openRouterResp); err != nil {
				return fmt.Errorf("failed to decode non-streaming OpenRouter response: %w. Body: %s", err, string(bodyBytes))
			}

			if openRouterResp.Error.Message != "" {
				return fmt.Errorf("received API error: Type=%s, Message=%s", openRouterResp.Error.Type, openRouterResp.Error.Message)
			}

			if len(openRouterResp.Choices) == 0 || openRouterResp.Choices[0].Message.Content == "" {
				fmt.Fprintln(os.Stderr, "Warning: Received an empty non-streaming response from the LLM.")
			} else {
				content := openRouterResp.Choices[0].Message.Content
				fmt.Println(content) // Print raw content directly
			}
		}

		fmt.Println("--------------------") // Final separator on Stdout

		return nil // Success
	},
}

// --- Init Function ---

func init() {
	rootCmd.AddCommand(codeCmd)

	// Define flags for the code command
	codeCmd.Flags().StringVarP(&llmModel, "model", "m", defaultModel, "LLM model to use via OpenRouter")
	// Flag to DISABLE streaming (default is now streaming)
	codeCmd.Flags().BoolVar(&noStream, "no-stream", false, "Disable streaming output (stream is default)")
}
