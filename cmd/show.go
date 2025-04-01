package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var showUnfiltered bool // Flag variable

// showCmd represents the show command
var showCmd = &cobra.Command{
	Use:   "show [directory]",
	Short: "Traverse and display files in the target directory",
	Long: `Traverses the specified directory recursively.
For each Go file found, it prints the absolute file path as a comment
followed by the file's content.

By default, it hides Go test files (files ending in '_test.go').
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
			fmt.Println("Filtering out test files ('_test.go'). Use -u to show all.")
		}
		fmt.Println("---") // Separator

		// Walk the directory
		err = filepath.WalkDir(absTargetDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Report errors during walk (e.g., permission issues) but continue if possible
				fmt.Fprintf(os.Stderr, "Error accessing path %q: %v\n", path, err)
				return err // Or return nil to try to continue
			}

			// Skip directories themselves
			if d.IsDir() {
				if d.Name() == ".git" || d.Name() == "vendor" || d.Name() == "__pycache__" || d.Name() == ".venv" || d.Name() == "venv" {
					return filepath.SkipDir
				}
				return nil // Continue walking
			}

			// --- Filtering Logic ---
			// Skip test files by default unless the -u flag is set
			if !showUnfiltered && strings.HasSuffix(d.Name(), "test") || strings.HasSuffix(d.Name(), "go.mod") || strings.HasSuffix(d.Name(), "go.sum") {
				return nil // Skip this file and continue walking
			}

			// --- Process File ---
			// Ensure we have the absolute path for display
			absPath, err := filepath.Abs(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not get absolute path for %s: %v\n", path, err)
				absPath = path // Use the relative path as fallback
			}

			// Print the header comment
			fmt.Printf("// %s\n\n", absPath)

			// Read file content
			content, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", path, err)
				// Decide if you want to stop the whole process or just skip this file
				return nil // Continue walking even if one file fails
			}

			// Print file content
			fmt.Println(string(content))
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
	showCmd.Flags().BoolVarP(&showUnfiltered, "unfiltered", "u", false, "Show all files, including test files")
}
