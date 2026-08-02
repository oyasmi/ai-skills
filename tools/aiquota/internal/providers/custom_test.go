package providers

import (
	"testing"

	"github.com/oyasmi/ai-skills/tools/aiquota/internal/config"
)

func TestCustomWindowBalanceStyle(t *testing.T) {
	body := map[string]any{"remaining": float64(4), "total": float64(20), "reset_at": "2026-08-02T04:53:45Z"}
	m := config.WindowMapping{Label: "余额", RemainingPath: "remaining", TotalPath: "total", ResetPath: "reset_at", DetailUnit: "$"}

	w := customWindow(m, body)
	if w == nil {
		t.Fatal("expected a window")
	}
	if w.UsedPercent != 80 {
		t.Errorf("used_percent = %v, want 80 (1 - 4/20)", w.UsedPercent)
	}
	if w.Detail != "$4 / $20" {
		t.Errorf("detail = %q, want %q", w.Detail, "$4 / $20")
	}
	if w.ResetAt == nil {
		t.Error("expected resetPath to populate ResetAt")
	}
}

func TestCustomWindowBalanceStyleClampsOutOfRange(t *testing.T) {
	// remaining > total (e.g. a refund) must not produce a negative used_percent.
	body := map[string]any{"remaining": float64(25), "total": float64(20)}
	m := config.WindowMapping{Label: "余额", RemainingPath: "remaining", TotalPath: "total"}
	w := customWindow(m, body)
	if w == nil || w.UsedPercent != 0 {
		t.Fatalf("expected used_percent clamped to 0, got %+v", w)
	}
}

func TestCustomWindowPercentageStyle(t *testing.T) {
	body := map[string]any{"usage": map[string]any{"percent": float64(42)}}
	m := config.WindowMapping{Label: "用量", UsedPath: "usage.percent"}
	w := customWindow(m, body)
	if w == nil || w.UsedPercent != 42 || w.Key != "custom_用量" {
		t.Fatalf("unexpected window: %+v", w)
	}
}

func TestCustomWindowNoMatchingFields(t *testing.T) {
	if customWindow(config.WindowMapping{Label: "x"}, map[string]any{}) != nil {
		t.Error("expected nil when no path is configured")
	}
	if customWindow(config.WindowMapping{Label: "x", UsedPath: "missing"}, map[string]any{}) != nil {
		t.Error("expected nil when usedPath does not resolve")
	}
}

func TestWindowKeyForLowercasesAndReplacesSpaces(t *testing.T) {
	if got := windowKeyFor("My Window"); got != "custom_my_window" {
		t.Errorf("windowKeyFor = %q, want custom_my_window", got)
	}
}
