package app

import (
	"context"
	"fmt"
	"time"

	"github.com/carafagi/kubectl-topx/internal/metrics"
	"github.com/carafagi/kubectl-topx/internal/ui"
	"github.com/gdamore/tcell/v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

// App represents the main application state
type App struct {
	Collector      *metrics.Collector
	View           *ui.View
	ctx            context.Context
	cancel         context.CancelFunc
	namespace      string
	allNamespaces  bool
	refreshSeconds int
	wide           bool
	showHistory    bool
	lastUpdate     time.Time
	selectedPodKey string
	metricsHistory map[string][]*metrics.HistoricalMetric
	maxHistorySize int
}

// NewApp creates a new App instance with the given configuration
func NewApp(namespace string, allNamespaces bool, refreshSeconds int, wide bool, showHistory bool) *App {
	return &App{
		namespace:      namespace,
		allNamespaces:  allNamespaces,
		refreshSeconds: refreshSeconds,
		wide:           wide,
		showHistory:    showHistory,
		metricsHistory: make(map[string][]*metrics.HistoricalMetric),
		maxHistorySize: 200,
	}
}

// Run initializes and starts the application
func (a *App) Run() error {
	// Initialize Kubernetes clients
	if err := a.initKubeClients(); err != nil {
		return fmt.Errorf("failed to initialize Kubernetes clients: %w", err)
	}

	// Initialize View
	a.View = ui.NewView(a.wide, a.showHistory, a.allNamespaces)

	// Setup callbacks
	a.View.OnSelectionChanged = func(namespace, podName string) {
		a.selectedPodKey = fmt.Sprintf("%s/%s", namespace, podName)
		a.updateHistoryView()
	}

	// Setup key bindings
	a.setupKeyBindings()

	// Start auto-refresh goroutine
	a.ctx, a.cancel = context.WithCancel(context.Background())
	defer a.cancel()

	go a.autoRefresh()

	// Initial data load
	if err := a.updateMetrics(); err != nil {
		return fmt.Errorf("failed to load initial metrics: %w", err)
	}

	// Run the application
	return a.View.Run()
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

	// Resolve namespace
	if a.allNamespaces {
		a.namespace = ""
	} else if a.namespace == "" {
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

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	metricsClient, err := versioned.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create metrics client: %w", err)
	}

	a.Collector = metrics.NewCollector(kubeClient, metricsClient)

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
			a.View.App.QueueUpdateDraw(func() {
				a.updateMetrics()
			})
		}
	}
}

// updateMetrics fetches and displays the latest metrics
func (a *App) updateMetrics() error {
	ns := a.namespace
	if a.allNamespaces {
		ns = ""
	}

	metricsList, err := a.Collector.CollectMetrics(ns)
	if err != nil {
		return err
	}

	a.lastUpdate = time.Now()
	a.updateStatusBar()

	// Store current metrics and update history
	for _, metric := range metricsList {
		podKey := fmt.Sprintf("%s/%s", metric.Namespace, metric.PodName)

		histEntry := &metrics.HistoricalMetric{
			Timestamp:  a.lastUpdate,
			CPUUsage:   metric.CPUUsage,
			CPUPercent: metric.CPUUsagePercent,
			MemUsage:   metric.MemoryUsage,
			MemPercent: metric.MemoryUsagePercent,
		}

		if _, exists := a.metricsHistory[podKey]; !exists {
			a.metricsHistory[podKey] = make([]*metrics.HistoricalMetric, 0, a.maxHistorySize)
		}

		a.metricsHistory[podKey] = append(a.metricsHistory[podKey], histEntry)

		if len(a.metricsHistory[podKey]) > a.maxHistorySize {
			a.metricsHistory[podKey] = a.metricsHistory[podKey][1:]
		}
	}

	a.View.UpdateMetrics(metricsList)

	if a.selectedPodKey != "" {
		a.updateHistoryView()
	}

	return nil
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

	a.View.StatusBar.SetText(statusText)
}

func (a *App) updateHistoryView() {
	if a.selectedPodKey == "" {
		return
	}
	history := a.metricsHistory[a.selectedPodKey]
	a.View.UpdateHistory(history)
}

func (a *App) setupKeyBindings() {
	a.View.App.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Rune() {
		case 'q':
			a.cancel()
			a.View.Stop()
			return nil
		case 'r':
			go a.View.App.QueueUpdateDraw(func() {
				a.updateMetrics()
			})
			return nil
		case 'w':
			go a.View.App.QueueUpdateDraw(func() {
				a.View.ToggleWide()
			})
			return nil
		case 't':
			go a.View.App.QueueUpdateDraw(func() {
				a.View.ToggleHistory()
			})
			return nil
		}
		switch event.Key() {
		case tcell.KeyEscape:
			a.cancel()
			a.View.Stop()
			return nil
		}
		return event
	})
}
