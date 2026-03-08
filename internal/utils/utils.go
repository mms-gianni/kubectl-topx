package utils

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
)

// CreateProgressBar creates a visual progress bar representation of a percentage
func CreateProgressBar(percent float64, width int) string {
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

// GetColorForUsage returns an appropriate tcell.Color based on usage percentage
func GetColorForUsage(percent float64) tcell.Color {
	if percent >= 90 {
		return tcell.ColorRed
	} else if percent >= 75 {
		return tcell.ColorOrange
	} else if percent >= 50 {
		return tcell.ColorYellow
	}
	return tcell.ColorGreen
}

// GetColorNameForUsage returns a color name string based on usage percentage
func GetColorNameForUsage(percent float64) string {
	if percent >= 90 {
		return "red"
	} else if percent >= 75 {
		return "orange"
	} else if percent >= 50 {
		return "yellow"
	}
	return "green"
}
