package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// initTUI initializes the terminal user interface
func (a *App) initTUI() {
	a.historyViewWidth = 50 // default width
	a.tviewApp = tview.NewApplication()
	a.table = tview.NewTable().
		SetBorders(false).
		SetSelectable(true, false).
		SetFixed(1, 0)

	// Setup table headers
	a.setupTableHeaders()

	// Create title
	title := a.createTitle()

	// Create history views
	a.setupHistoryViews()

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

	// Create main layout
	a.mainFlex = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(title, 3, 0, false)

	if a.showHistory {
		a.mainFlex.AddItem(a.historyFlex, 8, 0, false)
	}

	a.mainFlex.AddItem(a.table, 0, 1, true).
		AddItem(a.statusBar, 1, 0, false)

	// Setup key bindings
	a.setupKeyBindings()

	a.tviewApp.SetRoot(a.mainFlex, true)
}

// setupTableHeaders creates and sets the table header row
func (a *App) setupTableHeaders() {
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
}

// createTitle creates the title text view
func (a *App) createTitle() *tview.TextView {
	return tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetText("[yellow]Kubernetes Resource Metrics Monitor[-]\nPress [green]'q'[-] to quit | Press [green]'r'[-] to refresh | Press [green]'w'[-] to toggle columns | Press [green]'t'[-] to toggle history | [green]Arrow keys/PgUp/PgDn[-] to scroll").
		SetDynamicColors(true)
}

// setupHistoryViews initializes the history view components
func (a *App) setupHistoryViews() {
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
}

// setupKeyBindings configures keyboard input handlers
func (a *App) setupKeyBindings() {
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
}

// toggleWide toggles the wide display mode
func (a *App) toggleWide() {
	a.wide = !a.wide
	a.rebuildTable()
}

// toggleHistory toggles the history display
func (a *App) toggleHistory() {
	a.showHistory = !a.showHistory
	a.rebuildUI()
}

// rebuildUI rebuilds the entire UI layout
func (a *App) rebuildUI() {
	// Clear the main flex
	a.mainFlex.Clear()

	// Create title
	title := a.createTitle()

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

// rebuildTable rebuilds the table with current headers and data
func (a *App) rebuildTable() {
	// Clear the table completely
	for i := a.table.GetRowCount() - 1; i >= 0; i-- {
		a.table.RemoveRow(i)
	}

	// Rebuild headers
	a.setupTableHeaders()

	// Refresh data
	a.updateMetrics()
}
