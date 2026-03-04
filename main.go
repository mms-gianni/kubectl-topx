package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

var (
	namespace      = flag.String("namespace", "", "Kubernetes namespace to monitor (empty for all namespaces)")
	refreshSeconds = flag.Int("refresh", 5, "Refresh interval in seconds")
)

func main() {
	flag.Parse()

	app := &App{
		namespace:      *namespace,
		refreshSeconds: *refreshSeconds,
	}
	if err := app.Run(); err != nil {
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
	refreshSeconds int
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
		SetSelectable(false, false).
		SetFixed(1, 0)

	// Set up header
	headers := []string{"Namespace", "Pod", "CPU Request", "CPU Limit", "CPU Usage", "Memory Request", "Memory Limit", "Memory Usage"}
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
		SetText("[yellow]Kubernetes Resource Metrics Monitor[-]\nPress [green]'q'[-] to quit | Press [green]'r'[-] to refresh now").
		SetDynamicColors(true)

	// Create status bar
	a.statusBar = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)
	a.updateStatusBar()

	flex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(title, 3, 0, false).
		AddItem(a.table, 0, 1, false).
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
		}
		if event.Key() == tcell.KeyEscape {
			a.cancel()
			a.tviewApp.Stop()
			return nil
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
	namespaceInfo := "all namespaces"
	if a.namespace != "" {
		namespaceInfo = fmt.Sprintf("namespace: %s", a.namespace)
	}

	statusText := fmt.Sprintf("[gray]Monitoring: %s | Refresh: %ds | Last update: %s[-]",
		namespaceInfo,
		a.refreshSeconds,
		a.lastUpdate.Format("15:04:05"))

	a.statusBar.SetText(statusText)
}

func (a *App) updateMetrics() error {
	metrics, err := collectMetrics(a.kubeClient, a.metricsClient, a.namespace)
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
	// Namespace
	a.table.SetCell(row, 0, tview.NewTableCell(metric.Namespace).SetTextColor(tcell.ColorWhite))

	// Pod name
	a.table.SetCell(row, 1, tview.NewTableCell(metric.PodName).SetTextColor(tcell.ColorWhite))

	// CPU Request
	a.table.SetCell(row, 2, tview.NewTableCell(metric.CPURequest).SetTextColor(tcell.ColorWhite))

	// CPU Limit
	a.table.SetCell(row, 3, tview.NewTableCell(metric.CPULimit).SetTextColor(tcell.ColorWhite))

	// CPU Usage with bar
	cpuBar := createProgressBar(metric.CPUUsagePercent, 20)
	cpuText := fmt.Sprintf("%s %s", metric.CPUUsage, cpuBar)
	a.table.SetCell(row, 4, tview.NewTableCell(cpuText).SetTextColor(getColorForUsage(metric.CPUUsagePercent)))

	// Memory Request
	a.table.SetCell(row, 5, tview.NewTableCell(metric.MemoryRequest).SetTextColor(tcell.ColorWhite))

	// Memory Limit
	a.table.SetCell(row, 6, tview.NewTableCell(metric.MemoryLimit).SetTextColor(tcell.ColorWhite))

	// Memory Usage with bar
	memBar := createProgressBar(metric.MemoryUsagePercent, 20)
	memText := fmt.Sprintf("%s %s", metric.MemoryUsage, memBar)
	a.table.SetCell(row, 7, tview.NewTableCell(memText).SetTextColor(getColorForUsage(metric.MemoryUsagePercent)))
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
