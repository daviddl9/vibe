package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "vibe",
	Short: "A simple CLI tool to vibe with your Go files",
	Long: `Vibe is a utility designed by a distinguished engineer
to help you quickly browse through Go source files in a directory.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Whoops. There was an error while executing your command '%s'\n", err)
		os.Exit(1)
	}
}
