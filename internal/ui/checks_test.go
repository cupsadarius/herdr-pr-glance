package ui

import (
	"testing"

	"github.com/cupsadarius/herdr-pr-glance/internal/model"
)

func TestBarRuns(t *testing.T) {
	for _, tc := range []struct {
		name  string
		c     model.CheckCounts
		width int
		want  [4]int
	}{
		{"screenshot PR", model.CheckCounts{Failed: 2, Passed: 33, Neutral: 1, Skipped: 22}, 40, [4]int{1, 0, 24, 15}},
		{"tiny failing share keeps a cell", model.CheckCounts{Failed: 1, Passed: 1000}, 40, [4]int{1, 0, 39, 0}},
		{"only passed fills the bar", model.CheckCounts{Passed: 5}, 30, [4]int{0, 0, 30, 0}},
		{"every group", model.CheckCounts{Failed: 1, Pending: 1, Passed: 1, Unknown: 1}, 23, [4]int{8, 5, 5, 5}},
		{"no checks", model.CheckCounts{}, 40, [4]int{}},
	} {
		got := barRuns(tc.c, tc.width)
		if got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
		sum := got[0] + got[1] + got[2] + got[3]
		if tc.c != (model.CheckCounts{}) && sum != tc.width {
			t.Errorf("%s: runs sum to %d, want %d", tc.name, sum, tc.width)
		}
	}
}
