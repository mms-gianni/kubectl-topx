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
	showHistory    bool
)

type HistoricalMetric struct {
	Timestamp  time.Time
	CPUUsage   string
	CPUPercent float64
	MemUsage   string
	MemPercent float64
}

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
			showHistory:    showHistory,
			metricsHistory: make(map[string][]*HistoricalMetric),
			currentMetrics: make(map[string]*PodMetrics),
		}
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

type App struct {
	kubeClient       *kubernetes.Clientset
	metricsClient    *versioned.Clientset
	tviewApp         *tview.Application
	table            *tview.Table
	cpuHistoryView   *tview.TextView
	memHistoryView   *tview.TextView
	historyFlex      *tview.Flex
	mainFlex         *tview.Flex
	statusBar        *tview.TextView
	ctx              context.Context
	cancel           context.CancelFunc
	namespace        string
	allNamespaces    bool
	refreshSeconds   int
	wide             bool
	showHistory      bool
	lastUpdate       time.Time
	selectedPodKey   string
	metricsHistory   map[string][]*HistoricalMetric
	currentMetrics   map[string]*PodMetrics
	maxHistorySize   int
	historyViewWidth int
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
	a.maxHistorySize = 200  // Store enough samples for even wide terminals
	a.historyViewWidth = 50 // default width
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
		SetText("[yellow]Kubernetes Resource Metrics Monitor[-]\nPress [green]'q'[-] to quit | Press [green]'r'[-] to refresh | Press [green]'w'[-] to toggle columns | Press [green]'t'[-] to toggle history | [green]Arrow keys/PgUp/PgDn[-] to scroll").
		SetDynamicColors(true)

	// Create history views for selected pod (side by side)
	a.cpuHistoryView = tview.NewTextView().
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true).
		SetText("[gray]CPU History[-]")

	a.memHistoryView = tview.NewTextView().
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true).
		SetText("[gray]Memory History[-]")

	// Set draw func to calculate dynamic width and trigger update
	a.cpuHistoryView.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		// Calculate available width (subtract scale column: 6 chars)
		newWidth := width - 8
		if newWidth < 10 {
			newWidth = 30 // fallback
		}
		// Always update the width
		a.historyViewWidth = newWidth
		return x, y, width, height
	})

	a.memHistoryView.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		return x, y, width, height
	})

	// Create horizontal flex for history views
	a.historyFlex = tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(a.cpuHistoryView, 0, 1, false).
		AddItem(a.memHistoryView, 0, 1, false)

	// Create status bar
	a.statusBar = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	a.updateStatusBar()

	// Add selection changed handler
	a.table.SetSelectionChangedFunc(func(row, col int) {
		if row > 0 { // Skip header row
			a.onPodSelectionChanged(row)
		}
	})

	a.mainFlex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(title, 3, 0, false)

	if a.showHistory {
		a.mainFlex.AddItem(a.historyFlex, 8, 0, false)
	}

	a.mainFlex.AddItem(a.table, 0, 1, true).
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
		case 't':
			go a.tviewApp.QueueUpdateDraw(func() {
				a.toggleHistory()
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

	a.tviewApp.SetRoot(a.mainFlex, true)
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

	// Store current metrics and update history
	for _, metric := range metrics {
		podKey := fmt.Sprintf("%s/%s", metric.Namespace, metric.PodName)
		a.currentMetrics[podKey] = metric

		// Add to history
		histEntry := &HistoricalMetric{
			Timestamp:  a.lastUpdate,
			CPUUsage:   metric.CPUUsage,
			CPUPercent: metric.CPUUsagePercent,
			MemUsage:   metric.MemoryUsage,
			MemPercent: metric.MemoryUsagePercent,
		}

		if _, exists := a.metricsHistory[podKey]; !exists {
			a.metricsHistory[podKey] = make([]*HistoricalMetric, 0, a.maxHistorySize)
		}

		a.metricsHistory[podKey] = append(a.metricsHistory[podKey], histEntry)

		// Limit history size
		if len(a.metricsHistory[podKey]) > a.maxHistorySize {
			a.metricsHistory[podKey] = a.metricsHistory[podKey][1:]
		}
	}

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

	// Update history view for selected pod if any
	if a.selectedPodKey != "" {
		a.updateHistoryView()
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

func (a *App) toggleHistory() {
	// Toggle the history flag
	a.showHistory = !a.showHistory

	// Rebuild the UI layout
	a.rebuildUI()
}

func (a *App) rebuildUI() {
	// Clear the main flex
	a.mainFlex.Clear()

	// Create title
	title := tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("[yellow]Kubernetes Resource Metrics Monitor[-]\nPress [green]'q'[-] to quit | Press [green]'r'[-] to refresh | Press [green]'w'[-] to toggle columns | Press [green]'t'[-] to toggle history | [green]Arrow keys/PgUp/PgDn[-] to scroll").
		SetDynamicColors(true)

	// Add title
	a.mainFlex.AddItem(title, 3, 0, false)

	// Add history view if enabled
	if a.showHistory {
		a.mainFlex.AddItem(a.historyFlex, 8, 0, false)
	}

	// Add table and status bar
	a.mainFlex.AddItem(a.table, 0, 1, true)
	a.mainFlex.AddItem(a.statusBar, 1, 0, false)
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

func (a *App) onPodSelectionChanged(row int) {
	// Get pod info from the selected row
	colOffset := 0
	if a.allNamespaces {
		colOffset = 1
	}

	var namespace, podName string
	if a.allNamespaces && a.table.GetCell(row, 0) != nil {
		namespace = a.table.GetCell(row, 0).Text
		if a.table.GetCell(row, 1) != nil {
			podName = a.table.GetCell(row, 1).Text
		}
	} else {
		namespace = a.namespace
		if a.table.GetCell(row, colOffset) != nil {
			podName = a.table.GetCell(row, colOffset).Text
		}
	}

	if podName == "" {
		return
	}

	a.selectedPodKey = fmt.Sprintf("%s/%s", namespace, podName)
	a.updateHistoryView()
}

func (a *App) updateHistoryView() {
	history, exists := a.metricsHistory[a.selectedPodKey]
	if !exists || len(history) == 0 {
		noDataMsg := "[yellow]Select a pod to view history[-]"
		a.cpuHistoryView.SetText(noDataMsg)
		a.memHistoryView.SetText(noDataMsg)
		return
	}

	// Calculate width based on view dimensions (use default if not yet calculated)
	maxDisplayCount := a.historyViewWidth
	if maxDisplayCount <= 0 {
		maxDisplayCount = 30 // fallback default
	}

	// Show last entries for timeseries
	displayCount := maxDisplayCount
	if len(history) < displayCount {
		displayCount = len(history)
	}

	startIdx := len(history) - displayCount
	historySlice := history[startIdx:]

	// Extract CPU and Memory percentages
	cpuValues := make([]float64, len(historySlice))
	memValues := make([]float64, len(historySlice))
	for i, h := range historySlice {
		cpuValues[i] = h.CPUPercent
		memValues[i] = h.MemPercent
	}

	// Create timeseries with fixed width for consistent baseline
	cpuTimeseries := createVerticalTimeseries(cpuValues, "CPU Usage", 6, maxDisplayCount)
	memTimeseries := createVerticalTimeseries(memValues, "Memory Usage", 6, maxDisplayCount)

	/*
		cpuText := fmt.Sprintf("[yellow]%s[-] (%d samples)\n%s", a.selectedPodKey, displayCount, cpuTimeseries)
		memText := fmt.Sprintf("[yellow]%s[-] (%d samples)\n%s", a.selectedPodKey, displayCount, memTimeseries)

		a.cpuHistoryView.SetText(cpuText)
		a.memHistoryView.SetText(memText)
	*/
	a.cpuHistoryView.SetText(cpuTimeseries)
	a.memHistoryView.SetText(memTimeseries)
}

func getColorNameForUsage(percent float64) string {
	if percent >= 90 {
		return "red"
	} else if percent >= 75 {
		return "orange"
	} else if percent >= 50 {
		return "yellow"
	}
	return "green"
}

func createVerticalTimeseries(values []float64, title string, height int, maxWidth int) string {
	if len(values) == 0 {
		// Still show baseline even with no data
		var result string
		result += fmt.Sprintf("[white]%s (0-100%%)[-]\n", title)
		for row := height; row > 0; row-- {
			if row == height {
				result += "[gray]100%[-]  "
			} else if row == height/2 {
				result += "[gray] 50%[-]  "
			} else if row == 1 {
				result += "[gray]  0%[-]  "
			} else {
				result += "      "
			}
			// Fill with empty space or baseline markers
			for i := 0; i < maxWidth; i++ {
				if row == 1 {
					result += "[gray]▁[-]"
				} else {
					result += " "
				}
			}
			result += "\n"
		}
		return result
	}

	// Find max value for scaling
	maxVal := 0.0
	for _, v := range values {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}
	if maxVal < 100 {
		maxVal = 100 // Scale to 100% max
	}

	var result string
	result += fmt.Sprintf("[white]%s (0-100%%)[-]\n", title)

	// Block characters for denser visualization (from full to empty)
	blocks := []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	// Draw timeseries from top to bottom (vertical bars)
	for row := height; row > 0; row-- {
		rowTop := (float64(row) / float64(height)) * maxVal
		rowBottom := (float64(row-1) / float64(height)) * maxVal
		rowHeight := rowTop - rowBottom

		// Add scale on the left (fixed width: 6 chars)
		if row == height {
			result += fmt.Sprintf("[gray]%3.0f%%[-]  ", maxVal)
		} else if row == height/2 {
			result += fmt.Sprintf("[gray]%3.0f%%[-]  ", maxVal/2)
		} else if row == 1 {
			result += "[gray]  0%[-]  "
		} else {
			result += "      "
		}

		// Fill empty columns on the left (timeline grows from right)
		emptyColumns := maxWidth - len(values)
		for col := 0; col < emptyColumns; col++ {
			if row == 1 {
				// Show baseline markers for empty columns on bottom row
				result += "[gray]▁[-]"
			} else {
				result += " "
			}
		}

		// Draw columns with actual data on the right
		for col := 0; col < len(values); col++ {
			val := values[col]
			color := getColorNameForUsage(val)

			// Determine which block character to use based on how much the value fills this row
			var blockChar rune
			if val >= rowTop {
				// Value is at or above the top of this row - full block
				blockChar = '█'
			} else if val <= rowBottom {
				// Value is below this row - empty (but show baseline if on bottom row and value exists)
				if row == 1 && val > 0 {
					blockChar = '▁'
				} else {
					blockChar = ' '
				}
			} else {
				// Value is partially within this row - calculate partial block
				fillRatio := (val - rowBottom) / rowHeight
				blockIndex := int(fillRatio * 8)
				if blockIndex < 0 {
					blockIndex = 0
				} else if blockIndex > 8 {
					blockIndex = 8
				}
				blockChar = blocks[blockIndex]
			}

			if blockChar == ' ' {
				result += " "
			} else {
				result += fmt.Sprintf("[%s]%c[-]", color, blockChar)
			}
		}

		result += "\n"
	}

	return result
}
