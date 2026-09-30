package ui

import "github.com/cupsadarius/herdr-pr-glance/internal/model"

// barRuns splits width into the failing, running, passed and other runs of the
// CI bar. Each run is proportional to its count and any non-empty group keeps
// at least one cell; the largest run absorbs rounding and the cells the
// minimums borrowed, so the runs always fill the width exactly. Callers only
// draw the bar at widths well above four cells.
func barRuns(c model.CheckCounts, width int) [4]int {
	counts := [4]int{c.Failed, c.Pending, c.Passed, c.Neutral + c.Skipped + c.Unknown}
	total := counts[0] + counts[1] + counts[2] + counts[3]
	var runs [4]int
	if total == 0 || width <= 0 {
		return runs
	}
	sum, largest := 0, -1
	for i, n := range counts {
		if n == 0 {
			continue
		}
		runs[i] = max(1, n*width/total)
		sum += runs[i]
		if largest < 0 || runs[i] > runs[largest] {
			largest = i
		}
	}
	runs[largest] += width - sum
	return runs
}
