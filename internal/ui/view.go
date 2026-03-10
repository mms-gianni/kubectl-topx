package ui

import (
	"fmt"

	"github.com/carafagi/kubectl-topx/internal/metrics"
	"github.com/carafagi/kubectl-topx/internal/utils"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// View manages the terminal user interface
type View struct {
	App              *tview.Application
	Table            *tview.Table
	CPUHistoryView   *tview.TextView
	MemHistoryView   *tview.TextView
	HistoryFlex      *tview.Flex
	MainFlex         *tview.Flex
	StatusBar        *tview.TextView
	HistoryViewWidth int
	Wide             bool
	ShowHistory      bool
	AllNamespaces    bool

	// To resolve selections
	currentMetrics []*metrics.PodMetrics

	// Callbacks
	OnSelectionChanged func(namespace, podName string)
}

// NewView creates a new View instance
func NewView(wide, showHistory, allNamespaces bool) *View {
	v := &View{
		App:              tview.NewApplication(),
		HistoryViewWidth: 50,
		Wide:             wide,
		ShowHistory:      showHistory,
		AllNamespaces:    allNamespaces,
	}
	v.initComponents()
	return v
}

func (v *View) initComponents() {
	v.Table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	v.setupTableHeaders()

	// History components
	v.CPUHistoryView = tview.NewTextView().
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true).
		SetText("[gray]CPU History[-]")

	v.MemHistoryView = tview.NewTextView().
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true).
		SetText("[gray]Memory History[-]")

	// Set draw func for responsive width
	v.CPUHistoryView.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		newWidth := width - 8
		if newWidth < 10 {
			newWidth = 30
		}
		v.HistoryViewWidth = newWidth
		return x, y, width, height
	})

	v.MemHistoryView.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		return x, y, width, height
	})

	v.HistoryFlex = tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(v.CPUHistoryView, 0, 1, false).
		AddItem(v.MemHistoryView, 0, 1, false)

	v.StatusBar = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)

	v.Table.SetSelectionChangedFunc(func(row, col int) {
		if row > 0 && v.OnSelectionChanged != nil {
			if row-1 < len(v.currentMetrics) {
				m := v.currentMetrics[row-1]
				v.OnSelectionChanged(m.Namespace, m.PodName)
			}
		}
	})

	v.rebuildLayout()
}

func (v *View) setupTableHeaders() {
	var headers []string
	if v.AllNamespaces {
		headers = append(headers, "Namespace")
	}
	headers = append(headers, "Pod")
	if v.Wide {
		headers = append(headers, "CPU Request", "CPU Limit")
	}
	headers = append(headers, "CPU Usage")
	if v.Wide {
		headers = append(headers, "Memory Request", "Memory Limit")
	}
	headers = append(headers, "Memory Usage")

	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tcell.ColorYellow).
			SetAlign(tview.AlignLeft).
			SetSelectable(false).
			SetExpansion(1)
		v.Table.SetCell(0, col, cell)
	}
}

func (v *View) createTitle() *tview.TextView {
	return tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("[yellow]Kubernetes Resource Metrics Monitor[-]\nPress [green]'q'[-] to quit | Press [green]'r'[-] to refresh | Press [green]'w'[-] to toggle columns | Press [green]'t'[-] to toggle history | [green]Arrow keys/PgUp/PgDn[-] to scroll").
		SetDynamicColors(true)
}

func (v *View) rebuildLayout() {
	if v.MainFlex == nil {
		v.MainFlex = tview.NewFlex().SetDirection(tview.FlexRow)
	} else {
		v.MainFlex.Clear()
	}

	title := v.createTitle()
	v.MainFlex.AddItem(title, 3, 0, false)

	if v.ShowHistory {
		v.MainFlex.AddItem(v.HistoryFlex, 8, 0, false)
	}

	v.MainFlex.AddItem(v.Table, 0, 1, true).
		AddItem(v.StatusBar, 1, 0, false)

	v.App.SetRoot(v.MainFlex, true)
}

func (v *View) UpdateMetrics(metricsList []*metrics.PodMetrics) {
	v.currentMetrics = metricsList

	// Clear existing rows (keep header)
	for i := v.Table.GetRowCount() - 1; i > 0; i-- {
		v.Table.RemoveRow(i)
	}

	row := 1
	for _, metric := range metricsList {
		v.addMetricRow(row, metric)
		row++
	}
}

func (v *View) addMetricRow(row int, metric *metrics.PodMetrics) {
	col := 0

	if v.AllNamespaces {
		v.Table.SetCell(row, col, tview.NewTableCell(metric.Namespace).SetTextColor(tcell.ColorWhite))
		col++
	}

	v.Table.SetCell(row, col, tview.NewTableCell(metric.PodName).SetTextColor(tcell.ColorWhite))
	col++

	if v.Wide {
		v.Table.SetCell(row, col, tview.NewTableCell(metric.CPURequest).SetTextColor(tcell.ColorWhite))
		col++
		v.Table.SetCell(row, col, tview.NewTableCell(metric.CPULimit).SetTextColor(tcell.ColorWhite))
		col++
	}

	cpuBar := utils.CreateProgressBar(metric.CPUUsagePercent, 20)
	cpuText := fmt.Sprintf("%8s %s", metric.CPUUsage, cpuBar)
	v.Table.SetCell(row, col, tview.NewTableCell(cpuText).SetTextColor(utils.GetColorForUsage(metric.CPUUsagePercent)))
	col++

	if v.Wide {
		v.Table.SetCell(row, col, tview.NewTableCell(metric.MemoryRequest).SetTextColor(tcell.ColorWhite))
		col++
		v.Table.SetCell(row, col, tview.NewTableCell(metric.MemoryLimit).SetTextColor(tcell.ColorWhite))
		col++
	}

	memBar := utils.CreateProgressBar(metric.MemoryUsagePercent, 20)
	memText := fmt.Sprintf("%9s %s", metric.MemoryUsage, memBar)
	v.Table.SetCell(row, col, tview.NewTableCell(memText).SetTextColor(utils.GetColorForUsage(metric.MemoryUsagePercent)))
}

func (v *View) UpdateHistory(history []*metrics.HistoricalMetric) {
	if len(history) == 0 {
		noDataMsg := "[yellow]Select a pod to view history[-]"
		v.CPUHistoryView.SetText(noDataMsg)
		v.MemHistoryView.SetText(noDataMsg)
		return
	}

	maxDisplayCount := v.HistoryViewWidth
	if maxDisplayCount <= 0 {
		maxDisplayCount = 30
	}

	displayCount := maxDisplayCount
	if len(history) < displayCount {
		displayCount = len(history)
	}

	startIdx := len(history) - displayCount
	historySlice := history[startIdx:]

	cpuValues := make([]float64, len(historySlice))
	memValues := make([]float64, len(historySlice))
	for i, h := range historySlice {
		cpuValues[i] = h.CPUPercent
		memValues[i] = h.MemPercent
	}

	cpuTimeseries := createVerticalTimeseries(cpuValues, "CPU Usage", 6, maxDisplayCount)
	memTimeseries := createVerticalTimeseries(memValues, "Memory Usage", 6, maxDisplayCount)

	v.CPUHistoryView.SetText(cpuTimeseries)
	v.MemHistoryView.SetText(memTimeseries)
}

func (v *View) ToggleWide() {
	v.Wide = !v.Wide
	v.RebuildTable()
}

func (v *View) ToggleHistory() {
	v.ShowHistory = !v.ShowHistory
	v.rebuildLayout()
}

func (v *View) RebuildTable() {
	for i := v.Table.GetRowCount() - 1; i >= 0; i-- {
		v.Table.RemoveRow(i)
	}
	v.setupTableHeaders()
	if v.currentMetrics != nil {
		v.UpdateMetrics(v.currentMetrics)
	}
}

func (v *View) Run() error {
	return v.App.Run()
}

func (v *View) Stop() {
	v.App.Stop()
}
