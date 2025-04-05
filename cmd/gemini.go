package cmd

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

// isRunningViaSSH checks for common SSH environment variables.
func isRunningViaSSH() bool {
	return os.Getenv("SSH_CLIENT") != "" || os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CONNECTION") != ""
}

// Function to generate the OSC 52 escape sequence for clipboard copy
func osc52Copy(content string) string {
	// Base64 encode the content
	encodedContent := base64.StdEncoding.EncodeToString([]byte(content))
	// Return the escape sequence. "c" is for the system clipboard.
	// \x1b is ESC, \x07 is BEL (terminator)
	// Some terminals might prefer \x1b\\ (ESC \) as a terminator ST. BEL is generally more compatible.
	return fmt.Sprintf("\x1b]52;c;%s\x07", encodedContent)
}

// geminiCmd represents the gemini command
var geminiCmd = &cobra.Command{
	Use:   "gemini [directory]",
	Short: "Gathers code context, attempts smart copy (OSC 52) over SSH or local copy, opens Gemini.",
	Long: `Traverses the specified directory recursively, gathering relevant source file content.

Default Behavior (Running Locally):
- Copies the collected context to the system clipboard.
- Attempts to open gemini.google.com/app in your default web browser.
- Requires manual pasting into the Gemini chat input.

Behavior when run via SSH:
- Detects the SSH environment.
- Attempts to copy context to your *local* clipboard via terminal escape sequence (OSC 52).
  This requires a compatible terminal emulator (e.g., iTerm2, Windows Terminal, Kitty).
  If your terminal is not compatible, this step may fail silently or print garbage characters.
- Prints the gathered context directly to standard output as a fallback for manual copying.
- Prints the Gemini URL and instructions to standard error.
- Skips direct remote clipboard/browser operations.

Filtering logic is the same as 'vibe show' default.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := args[0]
		inSSH := isRunningViaSSH()

		// --- 1. Validate Target Directory ---
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

		// --- User Feedback ---
		fmt.Fprintf(os.Stderr, "Gathering context from: %s\n", absTargetDir)
		if inSSH {
			fmt.Fprintln(os.Stderr, "(Running in SSH session, attempting OSC 52 copy to local clipboard...)")
		} else {
			fmt.Fprintln(os.Stderr, "Applying default filters (like 'vibe show')...")
		}

		// --- 2. Gather Context ---
		var contextBuilder strings.Builder
		filesCollected := 0
		skippedDirs := 0
		skipDirs := map[string]bool{".git": true, "node_modules": true, "vendor": true, "__pycache__": true, "venv": true, ".venv": true, "target": true, "build": true, "dist": true}

		walkErr := filepath.WalkDir(absTargetDir, func(path string, d fs.DirEntry, walkErr error) error {
			// Basic error handling
			if walkErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: Error accessing path %q: %v\n", path, walkErr)
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			// Skip directories
			if d.IsDir() {
				dirName := d.Name()
				if (strings.HasPrefix(dirName, ".") && dirName != ".") || skipDirs[dirName] {
					skippedDirs++
					return filepath.SkipDir
				}
				return nil
			}

			// File Filtering
			fileName := d.Name()
			isHidden := strings.HasPrefix(fileName, ".")
			isTestFile := strings.HasSuffix(fileName, "_test.go")
			isModFile := fileName == "go.mod"
			isSumFile := fileName == "go.sum"
			isLicense := fileName == "LICENSE"
			isMarkdown := strings.HasSuffix(strings.ToLower(fileName), ".md")
			if isTestFile || isModFile || isSumFile || isLicense || isMarkdown || isHidden {
				return nil
			}

			// Read/Append File Content
			absPath, pathErr := filepath.Abs(path)
			if pathErr != nil {
				absPath = path /* fallback */
			}
			fileInfo, statErr := d.Info()
			if statErr == nil && fileInfo.Size() > 5*1024*1024 { // Skip large files
				fmt.Fprintf(os.Stderr, "Warning: Skipping potentially large file %s (>5MB)\n", path)
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: Error reading file %s: %v\n", path, readErr)
				return nil
			}
			contextBuilder.WriteString(fmt.Sprintf("--- File: %s ---\n", absPath))
			contextBuilder.Write(content)
			contextBuilder.WriteString("\n\n")
			filesCollected++
			return nil
		}) // End WalkDir func
		contextBuilder.WriteString("Take on the persona of a distinguished software engineer.")

		if walkErr != nil {
			return fmt.Errorf("error during directory traversal of %q: %w", absTargetDir, walkErr)
		}
		if filesCollected == 0 {
			fmt.Fprintln(os.Stderr, "Warning: No relevant files found matching criteria.")
		} else {
			fmt.Fprintf(os.Stderr, "Collected context from %d file(s).\n", filesCollected)
		}

		collectedContent := contextBuilder.String()
		geminiURL := "https://gemini.google.com/app"

		// --- 3. Conditional Action: Local vs SSH ---
		if inSSH {
			// --- SSH Behavior ---
			fmt.Fprintln(os.Stderr, "\n---")
			fmt.Fprintf(os.Stderr, "Attempting copy to local clipboard via OSC 52 sequence...\n")
			fmt.Fprintf(os.Stderr, "(Requires a compatible terminal like iTerm2, Windows Terminal, Kitty)\n")

			// Print the OSC 52 sequence to stdout. The terminal *might* intercept this.
			// Don't print a newline after, as the sequence itself handles termination.
			if collectedContent != "" {
				fmt.Print(osc52Copy(collectedContent)) // <<< Attempt OSC 52 copy
			}

			// Provide instructions and fallback plan via stderr
			fmt.Fprintf(os.Stderr, "Check your local clipboard. If it worked, great!\n")
			fmt.Fprintf(os.Stderr, "If not, your terminal may not support OSC 52. Manually copy the context below.\n")
			fmt.Fprintln(os.Stderr, "--- Context for Manual Copy Starts Below ---")

			// Print the collected content to stdout *as a fallback* for manual copying.
			// This will appear in the terminal regardless of OSC 52 support.
			fmt.Println(collectedContent)
			fmt.Println("🌐 Gemini URL: ", geminiURL)

		} else {
			// --- Local Behavior (unchanged) ---
			if collectedContent != "" {
				err = clipboard.WriteAll(collectedContent)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: Failed to copy context to local clipboard: %v\n", err)
				} else {
					fmt.Fprintln(os.Stderr, "✅ Context copied to local clipboard!")
				}
			} else {
				fmt.Fprintln(os.Stderr, "No content gathered to copy to clipboard.")
			}

			fmt.Fprintf(os.Stderr, "Attempting to open %s in your local browser...\n", geminiURL)
			err = browser.OpenURL(geminiURL)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to open browser automatically: %v\n", err)
				fmt.Fprintf(os.Stderr, "Please open %s manually.\n", geminiURL)
			} else {
				fmt.Fprintln(os.Stderr, "✅ Browser opened (or attempted).")
			}

			fmt.Fprintln(os.Stderr, "\n➡️ Please MANUALLY PASTE the copied context into the Gemini chat input (Ctrl+V or Cmd+V).")
			fmt.Fprintln(os.Stderr, "---")
		}

		return nil
	},
}

// --- Init Function ---
func init() {
	rootCmd.AddCommand(geminiCmd)
}
