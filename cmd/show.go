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

var showUnfiltered bool // Flag variable

// showCmd represents the show command
var showCmd = &cobra.Command{
	Use:   "show [directory]",
	Short: "Traverse and display files in the target directory",
	Long: `Traverses the specified directory recursively.
For each file found, it prints the absolute file path as a comment
followed by the file's content rendered as Markdown.

By default, it filters out certain files (e.g., _test.go, go.mod, go.sum).
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
			fmt.Println("Filtering out test, mod, and sum files. Use -u to show all.")
		}
		fmt.Println("---") // Separator

		// --- Prepare Glamour Style ---
		// 1. Determine base style (like WithAutoStyle)
		var baseStyle ansi.StyleConfig
		// Use TrueColor profile for best results, fallback can be handled by termenv
		// Check if stdout is a terminal before checking background color
		if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode()&os.ModeCharDevice) != 0 && termenv.HasDarkBackground() {
			baseStyle = styles.DarkStyleConfig
		} else {
			// Default to LightStyle or NoTTY if not a terminal (though NoTTY might be better)
			// Let's stick to light for consistency if not dark.
			baseStyle = styles.LightStyleConfig
			// You could also explicitly use styles.NoTTYStyleConfig if os.Stdout isn't a TTY
			// if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) == 0 {
			//  baseStyle = styles.NoTTYStyleConfig
			// }
		}

		// 2. Modify the style to remove margins
		zeroMargin := uint(0)
		// Ensure key block elements have no margin for left-alignment
		// Note: Margin is *uint, so we need the address of zeroMargin
		baseStyle.Document.Margin = &zeroMargin
		baseStyle.Heading.Margin = &zeroMargin
		baseStyle.CodeBlock.Margin = &zeroMargin
		baseStyle.List.Margin = &zeroMargin
		baseStyle.Paragraph.Margin = &zeroMargin
		baseStyle.BlockQuote.Margin = &zeroMargin
		baseStyle.Table.Margin = &zeroMargin
		// --- End Prepare Glamour Style ---

		// Walk the directory
		err = filepath.WalkDir(absTargetDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error accessing path %q: %v\n", path, err)
				// Decide whether to stop or continue. Returning err stops. Returning nil continues.
				// Let's try to continue.
				return nil
			}

			// Skip directories themselves, and specific directories
			if d.IsDir() {
				// Skip common clutter/dependency directories
				dirName := d.Name()
				if dirName == ".git" || dirName == "vendor" || strings.HasPrefix(dirName, ".") ||
					dirName == "node_modules" || dirName == "__pycache__" || dirName == "target" ||
					dirName == "build" || dirName == "dist" {
					// fmt.Fprintf(os.Stderr, "Skipping directory: %s\n", path) // Optional debug info
					return filepath.SkipDir
				}
				return nil // Continue walking into other directories
			}

			// --- Filtering Logic ---
			fileName := d.Name()
			// Skip specific file types by default unless the -u flag is set
			if !showUnfiltered {
				// Add more file types/patterns to skip here if needed
				if strings.HasSuffix(fileName, "_test.go") ||
					fileName == "go.mod" || fileName == "go.sum" ||
					fileName == "LICENSE" || strings.HasSuffix(fileName, ".md") || // Skip READMEs etc.
					strings.HasPrefix(fileName, ".") { // Skip hidden files
					// fmt.Fprintf(os.Stderr, "Skipping filtered file: %s\n", path) // Optional debug info
					return nil // Skip this file and continue walking
				}
			}

			// --- Process File ---
			// Ensure we have the absolute path for display
			absPath, err := filepath.Abs(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not get absolute path for %s: %v\n", path, err)
				absPath = path // Use the relative path as fallback
			}

			// Read file content
			content, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", path, err)
				return nil // Continue walking even if one file fails
			}

			// Create markdown-formatted content with file header and code block
			fileExt := strings.ToLower(filepath.Ext(path))
			lang := ""
			if len(fileExt) > 1 {
				lang = fileExt[1:] // Get extension without the dot for ```lang
			}
			if lang == "" {
				lang = "text" // Default language for syntax highlighting
			}

			// Use filepath.Base to just show the filename in the header, keeps it cleaner
			// markdownContent := fmt.Sprintf("## %s\n\n```%s\n%s\n```\n",
			//  filepath.Base(absPath), lang, string(content))
			// Or keep the full path if preferred:
			markdownContent := fmt.Sprintf("## %s\n\n```%s\n%s\n```\n",
				absPath, lang, string(content))

			// Create renderer with the MODIFIED zero-margin style
			// Renderer creation is relatively cheap, but could be moved outside the loop
			// if performance becomes an issue on huge numbers of files.
			renderer, err := glamour.NewTermRenderer(
				glamour.WithStyles(baseStyle), // Use the modified style
				glamour.WithWordWrap(0),       // Keep word wrap disabled
				// glamour.WithEmoji(),        // Uncomment if you want emojis
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating markdown renderer: %v\nFalling back to raw output.\n", err)
				// Fallback to simple output
				fmt.Printf("// %s\n\n%s\n", absPath, string(content))
			} else {
				renderedOutput, err := renderer.Render(markdownContent)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error rendering markdown for %s: %v\nFalling back to raw output.\n", absPath, err)
					// Fallback to simple output
					fmt.Printf("// %s\n\n%s\n", absPath, string(content))
				} else {
					fmt.Print(renderedOutput)
				}
			}

			fmt.Println("---") // Separator between files

			return nil // Continue walking
		})

		if err != nil {
			// Handle error returned by WalkDir itself (e.g., root permission error)
			return fmt.Errorf("error walking the path %q: %w", absTargetDir, err)
		}

		return nil // Success
	},
}

func init() {
	rootCmd.AddCommand(showCmd)

	// Define the local flag for the show command
	showCmd.Flags().BoolVarP(&showUnfiltered, "unfiltered", "u", false, "Show all files, including normally filtered ones")
}
