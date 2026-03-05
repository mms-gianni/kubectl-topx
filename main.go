package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

var (
	namespace      string
	allNamespaces  bool
	refreshSeconds int
	wide           bool
)

var rootCmd = &cobra.Command{
	Use:   "kubectl-topx",
	Short: "Kubernetes Resource Metrics Monitor",
	Long:  `A terminal UI for monitoring Kubernetes pod resource metrics including CPU and memory usage, requests, and limits.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		app := &App{
			namespace:      namespace,
			allNamespaces:  allNamespaces,
			refreshSeconds: refreshSeconds,
			wide:           wide,
		}
		return app.Run()
	},
}

func init() {
	rootCmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace to monitor")
	rootCmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Monitor all namespaces")
	rootCmd.Flags().IntVarP(&refreshSeconds, "refresh", "r", 5, "Refresh interval in seconds")
	rootCmd.Flags().BoolVarP(&wide, "wide", "w", false, "Show additional columns (requests and limits)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

type App struct {
	kubeClient     *kubernetes.Clientset
	metricsClient  *versioned.Clientset
	tviewApp       *tview.Application
	table          *tview.Table
	statusBar      *tview.TextView
	ctx            context.Context
	cancel         context.CancelFunc
	namespace      string
	allNamespaces  bool
	refreshSeconds int
	wide           bool
	lastUpdate     time.Time
}

func (a *App) Run() error {
	// Initialize Kubernetes clients
	if err := a.initKubeClients(); err != nil {
		return fmt.Errorf("failed to initialize Kubernetes clients: %w", err)
	}

	// Initialize TUI
	a.initTUI()

	// Start auto-refresh goroutine
	a.ctx, a.cancel = context.WithCancel(context.Background())
	defer a.cancel()

	go a.autoRefresh()

	// Initial data load
	if err := a.updateMetrics(); err != nil {
		return fmt.Errorf("failed to load initial metrics: %w", err)
	}

	// Run the application
	return a.tviewApp.Run()
}

func (a *App) initKubeClients() error {
	// Load kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Resolve namespace: use all namespaces if -A flag is set,
	// otherwise use specified namespace or current namespace from kubeconfig
	if a.allNamespaces {
		a.namespace = ""
	} else if a.namespace == "" {
		// Get current namespace from kubeconfig
		rawConfig, err := kubeConfig.RawConfig()
		if err != nil {
			return fmt.Errorf("failed to load kubeconfig: %w", err)
		}
		if rawConfig.Contexts[rawConfig.CurrentContext] != nil {
			if ns := rawConfig.Contexts[rawConfig.CurrentContext].Namespace; ns != "" {
				a.namespace = ns
			} else {
				a.namespace = "default"
			}
		} else {
			a.namespace = "default"
		}
	}

	// Create Kubernetes clientset
	a.kubeClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create metrics clientset
	a.metricsClient, err = versioned.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create metrics client: %w", err)
	}

	return nil
}

func (a *App) initTUI() {
	a.tviewApp = tview.NewApplication()
	a.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	// Set up header
	var headers []string
	// Only show namespace column when monitoring all namespaces
	if a.allNamespaces {
		headers = append(headers, "Namespace")
	}
	headers = append(headers, "Pod")
	if a.wide {
		headers = append(headers, "CPU Request", "CPU Limit")
	}
	headers = append(headers, "CPU Usage")
	if a.wide {
		headers = append(headers, "Memory Request", "Memory Limit")
	}
	headers = append(headers, "Memory Usage")
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignLeft).
			SetSelectable(false).
			SetExpansion(1)
		a.table.SetCell(0, col, cell)
	}

	// Create title
	title := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("[yellow]Kubernetes Resource Metrics Monitor[-]\nPress [green]'q'[-] to quit | Press [green]'r'[-] to refresh | Press [green]'w'[-] to toggle columns | [green]Arrow keys/PgUp/PgDn[-] to scroll").
		SetDynamicColors(true)

	// Create status bar
	a.statusBar = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	a.updateStatusBar()

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(title, 3, 0, false).
		AddItem(a.table, 0, 1, true).
		AddItem(a.statusBar, 1, 0, false)

	// Set up key bindings
	a.tviewApp.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q':
			a.cancel()
			a.tviewApp.Stop()
			return nil
		case 'r':
			go a.tviewApp.QueueUpdateDraw(func() {
				a.updateMetrics()
			})
			return nil
		case 'w':
			go a.tviewApp.QueueUpdateDraw(func() {
				a.toggleWide()
			})
			return nil
		}
		switch event.Key() {
		case tcell.KeyEscape:
			a.cancel()
			a.tviewApp.Stop()
			return nil
		case tcell.KeyUp, tcell.KeyDown, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyHome, tcell.KeyEnd:
			// Allow these keys to be handled by the table
			return event
		}
		return event
	})

	a.tviewApp.SetRoot(flex, true)
}

func (a *App) autoRefresh() {
	ticker := time.NewTicker(time.Duration(a.refreshSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.tviewApp.QueueUpdateDraw(func() {
				a.updateMetrics()
			})
		}
	}
}

func (a *App) updateStatusBar() {
	var namespaceInfo string
	if a.allNamespaces {
		namespaceInfo = "all namespaces"
	} else {
		namespaceInfo = fmt.Sprintf("namespace: %s", a.namespace)
	}

	statusText := fmt.Sprintf("[gray]Monitoring: %s | Refresh: %ds | Last update: %s[-]",
		namespaceInfo,
		a.refreshSeconds,
		a.lastUpdate.Format("15:04:05"))

	a.statusBar.SetText(statusText)
}

func (a *App) updateMetrics() error {
	// Use empty string for all namespaces, otherwise use the resolved namespace
	ns := a.namespace
	if a.allNamespaces {
		ns = ""
	}
	metrics, err := collectMetrics(a.kubeClient, a.metricsClient, ns)
	if err != nil {
		return err
	}

	a.lastUpdate = time.Now()
	a.updateStatusBar()

	// Clear existing rows (keep header)
	for i := a.table.GetRowCount() - 1; i > 0; i-- {
		a.table.RemoveRow(i)
	}

	// Add metrics to table
	row := 1
	for _, metric := range metrics {
		a.addMetricRow(row, metric)
		row++
	}

	return nil
}

func (a *App) addMetricRow(row int, metric *PodMetrics) {
	col := 0

	// Namespace (only when monitoring all namespaces)
	if a.allNamespaces {
		a.table.SetCell(row, col, tview.NewTableCell(metric.Namespace).SetTextColor(tcell.ColorWhite))
		col++
	}

	// Pod name
	a.table.SetCell(row, col, tview.NewTableCell(metric.PodName).SetTextColor(tcell.ColorWhite))
	col++

	// CPU Request (only in wide mode)
	if a.wide {
		a.table.SetCell(row, col, tview.NewTableCell(metric.CPURequest).SetTextColor(tcell.ColorWhite))
		col++
	}

	// CPU Limit (only in wide mode)
	if a.wide {
		a.table.SetCell(row, col, tview.NewTableCell(metric.CPULimit).SetTextColor(tcell.ColorWhite))
		col++
	}

	// CPU Usage with bar (right-aligned to 8 characters for consistent spacing)
	cpuBar := createProgressBar(metric.CPUUsagePercent, 20)
	cpuText := fmt.Sprintf("%8s %s", metric.CPUUsage, cpuBar)
	a.table.SetCell(row, col, tview.NewTableCell(cpuText).SetTextColor(getColorForUsage(metric.CPUUsagePercent)))
	col++

	// Memory Request (only in wide mode)
	if a.wide {
		a.table.SetCell(row, col, tview.NewTableCell(metric.MemoryRequest).SetTextColor(tcell.ColorWhite))
		col++
	}

	// Memory Limit (only in wide mode)
	if a.wide {
		a.table.SetCell(row, col, tview.NewTableCell(metric.MemoryLimit).SetTextColor(tcell.ColorWhite))
		col++
	}

	// Memory Usage with bar (right-aligned to 9 characters for consistent spacing)
	memBar := createProgressBar(metric.MemoryUsagePercent, 20)
	memText := fmt.Sprintf("%9s %s", metric.MemoryUsage, memBar)
	a.table.SetCell(row, col, tview.NewTableCell(memText).SetTextColor(getColorForUsage(metric.MemoryUsagePercent)))
}

func createProgressBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent * float64(width) / 100)
	empty := width - filled

	bar := "["
	for i := 0; i < filled; i++ {
		bar += "█"
	}
	for i := 0; i < empty; i++ {
		bar += "░"
	}
	bar += fmt.Sprintf("] %.1f%%", percent)

	return bar
}

func getColorForUsage(percent float64) tcell.Color {
	if percent >= 90 {
		return tcell.ColorRed
	} else if percent >= 75 {
		return tcell.ColorOrange
	} else if percent >= 50 {
		return tcell.ColorYellow
	}
	return tcell.ColorGreen
}

func (a *App) toggleWide() {
	// Toggle the wide flag
	a.wide = !a.wide

	// Rebuild the table with new headers and data
	a.rebuildTable()
}

func (a *App) rebuildTable() {
	// Clear the table completely
	for i := a.table.GetRowCount() - 1; i >= 0; i-- {
		a.table.RemoveRow(i)
	}

	// Rebuild headers
	var headers []string
	// Only show namespace column when monitoring all namespaces
	if a.allNamespaces {
		headers = append(headers, "Namespace")
	}
	headers = append(headers, "Pod")
	if a.wide {
		headers = append(headers, "CPU Request", "CPU Limit")
	}
	headers = append(headers, "CPU Usage")
	if a.wide {
		headers = append(headers, "Memory Request", "Memory Limit")
	}
	headers = append(headers, "Memory Usage")
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignLeft).
			SetSelectable(false).
			SetExpansion(1)
		a.table.SetCell(0, col, cell)
	}

	// Refresh data
	a.updateMetrics()
}
