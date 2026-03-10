package main

import (
	"fmt"
	"os"

	"github.com/carafagi/kubectl-topx/internal/app"
	"github.com/spf13/cobra"
)

var (
	// Version information (set via ldflags during build)
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"

	// Command flags
	namespace      string
	allNamespaces  bool
	refreshSeconds int
	wide           bool
	showHistory    bool
	showVersion    bool
)

var rootCmd = &cobra.Command{
	Use:   "kubectl-topx",
	Short: "Kubernetes Resource Metrics Monitor",
	Long:  `A terminal UI for monitoring Kubernetes pod resource metrics including CPU and memory usage, requests, and limits.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if showVersion {
			fmt.Printf("kubectl-topx version %s\n", version)
			fmt.Printf("  commit: %s\n", commit)
			fmt.Printf("  built: %s\n", buildDate)
			return nil
		}
		application := app.NewApp(namespace, allNamespaces, refreshSeconds, wide, showHistory)
		return application.Run()
	},
}

func init() {
	rootCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace to monitor")
	rootCmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Monitor all namespaces")
	rootCmd.Flags().IntVarP(&refreshSeconds, "refresh", "r", 5, "Refresh interval in seconds")
	rootCmd.Flags().BoolVarP(&wide, "wide", "w", false, "Show additional columns (requests and limits)")
	rootCmd.Flags().BoolVarP(&showHistory, "history", "t", false, "Show historical metrics for selected pod")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Display version information")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
