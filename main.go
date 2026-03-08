package main

import (
"fmt"
"os"

"github.com/spf13/cobra"
)

var (
namespace      string
allNamespaces  bool
refreshSeconds int
wide           bool
showHistory    bool
)

var rootCmd = &cobra.Command{
	Use:   "kubectl-topx",
	Short: "Kubernetes Resource Metrics Monitor",
	Long:  `A terminal UI for monitoring Kubernetes pod resource metrics including CPU and memory usage, requests, and limits.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		app := NewApp(namespace, allNamespaces, refreshSeconds, wide, showHistory)
		return app.Run()
	},
}

func init() {
	rootCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace to monitor")
	rootCmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Monitor all namespaces")
	rootCmd.Flags().IntVarP(&refreshSeconds, "refresh", "r", 5, "Refresh interval in seconds")
	rootCmd.Flags().BoolVarP(&wide, "wide", "w", false, "Show additional columns (requests and limits)")
	rootCmd.Flags().BoolVarP(&showHistory, "history", "t", false, "Show historical metrics for selected pod")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
