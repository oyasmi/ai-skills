package output

import (
	"testing"
	"time"
)

func TestLocalTimeUsesMachineLocalZone(t *testing.T) {
	previous := time.Local
	time.Local = time.FixedZone("CST", 8*60*60)
	t.Cleanup(func() { time.Local = previous })

	got := LocalTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	if got != "2026-01-01T08:00:00+08:00" {
		t.Fatalf("expected local RFC3339 timestamp, got %q", got)
	}
}

func TestLocalizeTimestampPreservesNonTimestampMetadata(t *testing.T) {
	previous := time.Local
	time.Local = time.FixedZone("CST", 8*60*60)
	t.Cleanup(func() { time.Local = previous })

	if got := LocalizeTimestamp("2026-01-01T00:00:00+0000"); got != "2026-01-01T08:00:00+08:00" {
		t.Fatalf("expected compact build timestamp to be localized, got %q", got)
	}
	if got := LocalizeTimestamp("unknown"); got != "unknown" {
		t.Fatalf("expected non-timestamp metadata to pass through, got %q", got)
	}
}
