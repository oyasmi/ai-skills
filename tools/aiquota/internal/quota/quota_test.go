package quota

import (
	"testing"
	"time"
)

func TestNormalizeClampsAndRounds(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	reset := now.Add(90 * time.Second)
	s := Snapshot{
		Windows: []Window{
			{UsedPercent: 142.333, ResetAt: &reset},
			{UsedPercent: -5},
		},
	}
	s.Normalize(now)

	if s.Windows[0].UsedPercent != 100 {
		t.Fatalf("expected clamp to 100, got %v", s.Windows[0].UsedPercent)
	}
	if s.Windows[0].RemainingPercent != 0 {
		t.Fatalf("expected remaining 0, got %v", s.Windows[0].RemainingPercent)
	}
	if s.Windows[0].ResetInSeconds == nil || *s.Windows[0].ResetInSeconds != 90 {
		t.Fatalf("expected reset_in_seconds=90, got %v", s.Windows[0].ResetInSeconds)
	}
	if s.Windows[1].UsedPercent != 0 || s.Windows[1].RemainingPercent != 100 {
		t.Fatalf("expected clamp to 0/100, got %v/%v", s.Windows[1].UsedPercent, s.Windows[1].RemainingPercent)
	}
}

func TestNormalizePastResetIsZeroNotNegative(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	s := Snapshot{Windows: []Window{{UsedPercent: 10, ResetAt: &past}}}
	s.Normalize(now)
	if *s.Windows[0].ResetInSeconds != 0 {
		t.Fatalf("expected 0, got %v", *s.Windows[0].ResetInSeconds)
	}
}

func TestFailSetsFetchedAt(t *testing.T) {
	s := Fail("p", "Provider", StateNoData, "no creds")
	if s.State != StateNoData || s.Error != "no creds" {
		t.Fatalf("unexpected snapshot: %+v", s)
	}
	if s.FetchedAt.IsZero() {
		t.Fatal("expected FetchedAt to be set")
	}
}
