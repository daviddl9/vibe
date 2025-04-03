package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"          // Import ansi for StyleConfig
	styles "github.com/charmbracelet/glamour/styles" // Import default styles
	"github.com/muesli/termenv"                      // Import termenv for background detection
	"github.com/spf13/cobra"
)

var (
	showUnfiltered  bool // Flag variable for unfiltered listing
	outputPlainText bool // Flag variable for plain text output
)

// showCmd represents the show command
var showCmd = &cobra.Command{
	Use:   "show [directory]",
	Short: "Traverse and display files in the target directory",
	Long: `Traverses the specified directory recursively.
For each file found, it prints the absolute file path followed by the file's content.

By default, content is rendered as Markdown with syntax highlighting (if a TTY is detected).
Use the -o flag to output plain text without any Markdown rendering or color codes,
suitable for piping or redirection.

By default, it also filters out certain files (e.g., _test.go, go.mod, go.sum).
Use the -u flag to show all files unfiltered.`,
	Args: cobra.ExactArgs(1), // Requires exactly one argument: the directory
	RunE: func(cmd *cobra.Command, args []string) error {
		targetDir := args[0]

		// Get absolute path for consistent output and checking
		absTargetDir, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for %s: %w", targetDir, err)
		}

		// Check if the target directory exists and is a directory
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

		fmt.Printf("Traversing directory: %s\n", absTargetDir)
		if !showUnfiltered {
			fmt.Println("Filtering out test, mod, sum, LICENSE, hidden, and markdown files. Use -u to show all.")
		}
		if outputPlainText {
			fmt.Println("Outputting plain text format.")
		}
		fmt.Println("---") // Separator

		var renderer *glamour.TermRenderer // Declare renderer once outside the loop
		var errRenderer error

		// --- Prepare Glamour Renderer (Only if NOT outputting plain text) ---
		if !outputPlainText {
			// 1. Determine base style
			var baseStyle ansi.StyleConfig
			isTTY := false
			if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) != 0 {
				isTTY = true
			}

			// Only use color/styles if it's a TTY
			if isTTY && termenv.HasDarkBackground() {
				baseStyle = styles.DarkStyleConfig
			} else if isTTY {
				baseStyle = styles.LightStyleConfig
			} else {
				// If not a TTY, default to plain automatically unless -o is explicitly false?
				// Let's make it explicit: if not TTY, behave like -o was passed.
				// Update: User explicitly asks for -o for plain text. If it's not a TTY,
				// Glamour should handle it reasonably with NoTTYStyleConfig,
				// but we'll respect the -o flag primarily. Let's default to NoTTY if not TTY.
				baseStyle = styles.NoTTYStyleConfig // Use NoTTY style if not a terminal
				if termenv.HasDarkBackground() {
					// NoTTY doesn't have dark/light variants in the default styles package easily accessible
					// So we might just stick with the default NoTTY. Or manually create one if needed.
					// For simplicity, stick with default NoTTY.
				}

			}

			// 2. Modify the style to remove margins (if using a style that has them)
			if isTTY { // Only adjust margins if we're using a TTY style
				zeroMargin := uint(0)
				baseStyle.Document.Margin = &zeroMargin
				baseStyle.Heading.Margin = &zeroMargin
				baseStyle.CodeBlock.Margin = &zeroMargin
				baseStyle.List.Margin = &zeroMargin
				baseStyle.Paragraph.Margin = &zeroMargin
				baseStyle.BlockQuote.Margin = &zeroMargin
				baseStyle.Table.Margin = &zeroMargin
			}

			// 3. Create renderer (only once)
			renderer, errRenderer = glamour.NewTermRenderer(
				glamour.WithStyles(baseStyle),
				glamour.WithWordWrap(0), // Keep word wrap disabled for code
				// glamour.WithEmoji(), // Optional
			)
			if errRenderer != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to create markdown renderer: %v. Output may be simplified.\n", errRenderer)
				// Don't force plain text here, let the loop handle fallbacks.
			}
		}

		// Walk the directory
		walkErr := filepath.WalkDir(absTargetDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error accessing path %q: %v\n", path, err)
				return nil // Continue walking if possible
			}

			// Skip directories
			if d.IsDir() {
				dirName := d.Name()
				if dirName == ".git" || dirName == "vendor" || strings.HasPrefix(dirName, ".") ||
					dirName == "node_modules" || dirName == "__pycache__" || dirName == "target" ||
					dirName == "build" || dirName == "dist" {
					return filepath.SkipDir
				}
				return nil
			}

			// Filtering Logic
			fileName := d.Name()
			if !showUnfiltered {
				if strings.HasSuffix(fileName, "_test.go") ||
					fileName == "go.mod" || fileName == "go.sum" ||
					fileName == "LICENSE" || strings.HasSuffix(fileName, ".md") ||
					strings.HasPrefix(fileName, ".") {
					return nil // Skip filtered file
				}
			}

			// --- Process File ---
			absPath, err := filepath.Abs(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not get absolute path for %s: %v\n", path, err)
				absPath = path // Fallback
			}

			content, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", path, err)
				return nil // Continue walking
			}

			// --- Conditional Output ---
			if outputPlainText || errRenderer != nil || renderer == nil {
				// Output plain text if -o is set OR if renderer failed to initialize
				// Using a simple comment header format
				fmt.Printf("// File: %s\n\n%s\n", absPath, string(content))

			} else {
				// Render with Glamour
				fileExt := strings.ToLower(filepath.Ext(path))
				lang := ""
				if len(fileExt) > 1 {
					lang = fileExt[1:] // Get extension without the dot
				}
				if lang == "" {
					lang = "text" // Default language
				}

				// Use Header level 2 for filename, and a code block
				markdownContent := fmt.Sprintf("## %s\n\n```%s\n%s\n```\n",
					absPath, lang, string(content))

				renderedOutput, renderErr := renderer.Render(markdownContent)
				if renderErr != nil {
					fmt.Fprintf(os.Stderr, "Error rendering markdown for %s: %v\nFalling back to plain output.\n", absPath, renderErr)
					// Fallback to plain text format on rendering error
					fmt.Printf("// File: %s\n\n%s\n", absPath, string(content))
				} else {
					fmt.Print(renderedOutput)
				}
			}

			fmt.Println("---") // Separator between files (applies to both modes)

			return nil // Continue walking
		})

		if walkErr != nil {
			// Handle error returned by WalkDir itself
			return fmt.Errorf("error walking the path %q: %w", absTargetDir, walkErr)
		}

		return nil // Success
	},
}

func init() {
	rootCmd.AddCommand(showCmd)

	// Define flags for the show command
	showCmd.Flags().BoolVarP(&showUnfiltered, "unfiltered", "u", false, "Show all files, including normally filtered ones")
	showCmd.Flags().BoolVarP(&outputPlainText, "output-plain", "o", false, "Output plain text without markdown rendering or colors")
}
