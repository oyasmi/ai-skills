// Package render turns a quota.Report into human text or JSON.
package render

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/quota"
)

// ANSI color codes matching QuotaList's usage-percent thresholds:
// <50 green, 50-79 yellow, 80-94 orange(ish), >=95 red, no data gray.
const (
	colorGreen  = "\x1b[32m"
	colorYellow = "\x1b[33m"
	colorOrange = "\x1b[38;5;208m"
	colorRed    = "\x1b[31m"
	colorGray   = "\x1b[90m"
	colorReset  = "\x1b[0m"
	colorBold   = "\x1b[1m"
)

// ColorEnabled decides whether to emit ANSI codes: on by default for a real
// terminal, off when piped, when NO_COLOR is set, or when forced off.
func ColorEnabled(forceOff bool) bool {
	if forceOff || os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func usageColor(percent float64) string {
	switch {
	case percent >= 95:
		return colorRed
	case percent >= 80:
		return colorOrange
	case percent >= 50:
		return colorYellow
	default:
		return colorGreen
	}
}

func colorize(enabled bool, code, text string) string {
	if !enabled || code == "" {
		return text
	}
	return code + text + colorReset
}

// Text writes a compact table: one row per provider, one line per window.
func Text(w io.Writer, report quota.Report, color bool) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for i, s := range report.Providers {
		if i > 0 {
			fmt.Fprintln(tw)
		}
		writeProvider(tw, s, color)
	}
	tw.Flush()
}

func writeProvider(tw io.Writer, s quota.Snapshot, color bool) {
	header := colorize(color, colorBold, s.Name)
	if s.Plan != "" {
		header += "  " + s.Plan
	}
	fmt.Fprintln(tw, header)

	switch s.State {
	case quota.StateNoData:
		fmt.Fprintf(tw, "  %s\n", colorize(color, colorGray, "无数据 · "+s.Error))
		return
	case quota.StateError:
		fmt.Fprintf(tw, "  %s\n", colorize(color, colorRed, "错误 · "+s.Error))
		return
	}

	for _, win := range s.Windows {
		percent := colorize(color, usageColor(win.UsedPercent), fmt.Sprintf("%5.1f%%", win.UsedPercent))
		line := fmt.Sprintf("  %s\t%s\t%s", win.Label, percent, win.Detail)
		if win.ResetAt != nil {
			line += "\t↻ " + countdown(*win.ResetInSeconds)
		}
		fmt.Fprintln(tw, line)
	}
	if s.ValidUntil != nil {
		fmt.Fprintf(tw, "  %s\n", colorize(color, colorGray, "到期 "+expiryLabel(*s.ValidUntil)))
	}
}

// countdown renders seconds-until-reset as compact Chinese text, e.g.
// "3小时12分钟" / "2天15小时" / "即将刷新".
func countdown(seconds int64) string {
	if seconds <= 0 {
		return "即将刷新"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours() / 24)
	if days >= 1 {
		hours := int(d.Hours()) - days*24
		if hours > 0 {
			return fmt.Sprintf("%d天%d小时", days, hours)
		}
		return fmt.Sprintf("%d天", days)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) - hours*60
	if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%d小时%d分钟", hours, minutes)
		}
		return fmt.Sprintf("%d小时", hours)
	}
	if minutes > 0 {
		return fmt.Sprintf("%d分钟", minutes)
	}
	return "不到1分钟"
}

func expiryLabel(t time.Time) string {
	date := t.Format("2006-01-02")
	diff := time.Until(t)
	if diff <= 0 {
		return "已到期 · " + date
	}
	days := int((diff + 24*time.Hour - time.Nanosecond) / (24 * time.Hour))
	return fmt.Sprintf("%s · 剩 %d 天", date, days)
}
