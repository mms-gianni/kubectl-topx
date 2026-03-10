package ui

import (
	"fmt"

	"github.com/carafagi/kubectl-topx/internal/utils"
)

// createVerticalTimeseries creates a vertical bar chart visualization of historical metrics
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
			color := utils.GetColorNameForUsage(val)

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
