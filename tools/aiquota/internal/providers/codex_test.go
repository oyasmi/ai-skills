package providers

import (
	"testing"
	"time"
)

func TestCodexWindowUsedPercent(t *testing.T) {
	m := map[string]any{
		"primary_window": map[string]any{
			"used_percent": float64(1.2),
			"reset_at":     "2026-08-02T04:53:45Z",
		},
	}
	w := codexWindowUsedPercent(m, "primary_window", "5h", "5小时", "reset_at")
	if w == nil || w.UsedPercent != 1.2 || w.Label != "5小时" || w.ResetAt == nil {
		t.Fatalf("unexpected window: %+v", w)
	}

	if codexWindowUsedPercent(m, "secondary_window", "7d", "一周", "reset_at") != nil {
		t.Fatal("expected missing key to yield no window")
	}

	noPercent := map[string]any{"primary_window": map[string]any{"reset_at": "2026-08-02T04:53:45Z"}}
	if codexWindowUsedPercent(noPercent, "primary_window", "5h", "5小时", "reset_at") != nil {
		t.Fatal("expected missing used_percent to yield no window")
	}
}

func TestCodexNonExpired(t *testing.T) {
	future := time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	if got := codexNonExpired(future); got == nil {
		t.Error("expected a future date to be kept")
	}

	past := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	if got := codexNonExpired(past); got != nil {
		t.Errorf("expected a past date to be dropped as stale, got %v", got)
	}

	if got := codexNonExpired(nil); got != nil {
		t.Errorf("expected nil input to yield nil, got %v", got)
	}
}

func TestStringField(t *testing.T) {
	m := map[string]any{"a": "x", "b": "", "c": 1}
	if v, ok := stringField(m, "a"); !ok || v != "x" {
		t.Errorf("expected a=x, got %q ok=%v", v, ok)
	}
	if _, ok := stringField(m, "b"); ok {
		t.Error("expected empty string field to report not-ok")
	}
	if _, ok := stringField(m, "c"); ok {
		t.Error("expected non-string field to report not-ok")
	}
	if _, ok := stringField(nil, "a"); ok {
		t.Error("expected nil map to report not-ok")
	}
}
