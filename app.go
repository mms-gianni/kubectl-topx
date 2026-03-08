package main

import (
	"context"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

// App represents the main application state
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

// NewApp creates a new App instance with the given configuration
func NewApp(namespace string, allNamespaces bool, refreshSeconds int, wide bool, showHistory bool) *App {
	return &App{
		namespace:      namespace,
		allNamespaces:  allNamespaces,
		refreshSeconds: refreshSeconds,
		wide:           wide,
		showHistory:    showHistory,
		metricsHistory: make(map[string][]*HistoricalMetric),
		currentMetrics: make(map[string]*PodMetrics),
		maxHistorySize: 200,
	}
}

// Run initializes and starts the application
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

// initKubeClients initializes the Kubernetes and metrics clients
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

// autoRefresh periodically updates metrics based on the refresh interval
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

// updateStatusBar updates the status bar with current information
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

// updateMetrics fetches and displays the latest metrics
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

// addMetricRow adds a single metric row to the table
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

// onPodSelectionChanged handles pod selection changes in the table
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
