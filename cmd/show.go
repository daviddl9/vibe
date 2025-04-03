package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	showUnfiltered bool // Flag variable for unfiltered listing
)

// showCmd represents the show command
var showCmd = &cobra.Command{
	Use:   "show [directory]",
	Short: "Traverse and display files in the target directory",
	Long: `Traverses the specified directory recursively.
For each file found, it prints the absolute file path followed by the file's content.

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
			fmt.Println("Filtering out test, mod, sum, LICENSE, hidden, and markdown files. Use -u to show all.")
		}
		fmt.Println("---") // Separator

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

			// Output plain text format
			fmt.Printf("File: %s\n\n%s\n", absPath, string(content))
			fmt.Println("---") // Separator between files

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
}
