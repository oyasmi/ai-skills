package providers

import (
	"testing"
	"time"
)

func TestClaudeWindowMapsUtilizationAndResetAt(t *testing.T) {
	body := map[string]any{
		"five_hour": map[string]any{
			"utilization": float64(13.4),
			"resets_at":   "2026-08-02T04:10:00Z",
		},
		"seven_day": map[string]any{
			"percent": float64(63), // fallback field when utilization is absent
		},
	}

	w := claudeWindow(body, "five_hour", "5h", "5小时")
	if w == nil || w.UsedPercent != 13.4 || w.ResetAt == nil {
		t.Fatalf("unexpected five_hour window: %+v", w)
	}

	w2 := claudeWindow(body, "seven_day", "7d", "一周")
	if w2 == nil || w2.UsedPercent != 63 {
		t.Fatalf("expected `percent` fallback to be used when utilization is missing, got %+v", w2)
	}

	if claudeWindow(body, "seven_day_opus", "7d_opus", "Opus") != nil {
		t.Fatal("expected a missing key to yield no window")
	}
}

func TestClaudeValidUntilPrefersTopLevelMatch(t *testing.T) {
	body := map[string]any{
		"current_period_end": "2026-08-09T08:22:32Z",
		"nested": map[string]any{
			"expires_at": "2099-01-01T00:00:00Z",
		},
	}
	got := claudeValidUntil(body)
	if got == nil {
		t.Fatal("expected a match")
	}
	want, _ := time.Parse(time.RFC3339, "2026-08-09T08:22:32Z")
	if !got.Equal(want) {
		t.Errorf("expected the top-level key to win over a nested one, got %v want %v", got, want)
	}
}

func TestClaudeValidUntilFallsBackToNested(t *testing.T) {
	body := map[string]any{
		"unrelated": "field",
		"nested": map[string]any{
			"expires_at": "2026-08-09T08:22:32Z",
		},
	}
	got := claudeValidUntil(body)
	if got == nil {
		t.Fatal("expected the nested match to be found when there is no top-level match")
	}
}

// A deeply nested field of some unrelated sub-object (e.g. an org's own
// expires_at) must not be preferred over a shallower match living in a
// sibling branch, regardless of map/array iteration order.
func TestClaudeValidUntilShallowestMatchWinsAcrossBranches(t *testing.T) {
	shallowWant, _ := time.Parse(time.RFC3339, "2026-08-09T08:22:32Z")

	branchDeep := map[string]any{
		"inner": map[string]any{
			"expires_at": "2099-01-01T00:00:00Z", // 2 levels deeper than the real match
		},
	}
	branchShallow := map[string]any{
		"expires_at": "2026-08-09T08:22:32Z",
	}
	body := map[string]any{
		"wrapper": []any{branchDeep, branchShallow},
	}

	got := claudeValidUntil(body)
	if got == nil || !got.Equal(shallowWant) {
		t.Fatalf("expected the shallower sibling match %v, got %v", shallowWant, got)
	}
}

func TestClaudeValidUntilNoMatch(t *testing.T) {
	body := map[string]any{"five_hour": map[string]any{"utilization": float64(1)}}
	if got := claudeValidUntil(body); got != nil {
		t.Errorf("expected no match, got %v", got)
	}
}
