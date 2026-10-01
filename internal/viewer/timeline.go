// SPDX-License-Identifier: GPL-3.0-only
package viewer

import (
	"strings"
	"time"
)

// Timeline maps an inclusive task interval onto a bounded calendar axis.
func Timeline(start, end, axisStart, axisEnd string, width int) string {
	s, e := time.Parse("2006-01-02", start)
	if e != nil {
		return "(unscheduled)"
	}
	f, e := time.Parse("2006-01-02", end)
	if e != nil || f.Before(s) {
		return "(invalid dates)"
	}
	a, e := time.Parse("2006-01-02", axisStart)
	if e != nil {
		return ""
	}
	b, e := time.Parse("2006-01-02", axisEnd)
	if e != nil || b.Before(a) {
		return ""
	}
	width = max(1, min(80, width))
	span := b.Sub(a).Hours()/24 + 1
	left := max(0, min(width-1, int(s.Sub(a).Hours()/24/span*float64(width))))
	right := max(left+1, min(width, int((f.Sub(a).Hours()/24+1)/span*float64(width))))
	return strings.Repeat(" ", left) + strings.Repeat("━", right-left) + strings.Repeat(" ", width-right)
}
