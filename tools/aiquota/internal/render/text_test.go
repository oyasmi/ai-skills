package render

import (
	"testing"
	"time"
)

func TestCountdown(t *testing.T) {
	cases := []struct {
		seconds int64
		want    string
	}{
		{0, "即将刷新"},
		{-5, "即将刷新"},
		{30, "不到1分钟"},
		{45 * 60, "45分钟"},
		{2 * 3600, "2小时"},
		{3*3600 + 12*60, "3小时12分钟"},
		{3 * 86400, "3天"},
		{2*86400 + 15*3600, "2天15小时"},
	}
	for _, c := range cases {
		if got := countdown(c.seconds); got != c.want {
			t.Errorf("countdown(%d) = %q, want %q", c.seconds, got, c.want)
		}
	}
}

func TestExpiryLabelAlreadyExpired(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	got := expiryLabel(past)
	want := "已到期 · " + past.Format("2006-01-02")
	if got != want {
		t.Errorf("expiryLabel(past) = %q, want %q", got, want)
	}
}

func TestExpiryLabelDaysRemaining(t *testing.T) {
	// Kept well away from a 24h boundary so a few ms of test execution time
	// can't flip the rounding and make this flaky.
	future := time.Now().Add(50 * time.Hour)
	got := expiryLabel(future)
	want := future.Format("2006-01-02") + " · 剩 3 天"
	if got != want {
		t.Errorf("expiryLabel(+50h) = %q, want %q", got, want)
	}
}

func TestLocalClockSameYearOmitsYear(t *testing.T) {
	now := time.Now()
	t2 := time.Date(now.Year(), 6, 15, 9, 5, 3, 0, now.Location())
	got := localClock(t2)
	want := t2.Format("01-02 15:04:05")
	if got != want {
		t.Errorf("localClock same-year = %q, want %q", got, want)
	}
}

func TestColorEnabled(t *testing.T) {
	// go test's stdout is not a char device, so the terminal check alone is
	// always false here — CLICOLOR_FORCE is what lets us exercise the "on" path.
	if ColorEnabled(true) {
		t.Error("--no-color must always win")
	}

	t.Setenv("NO_COLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "1")
	if ColorEnabled(false) {
		t.Error("NO_COLOR must win over CLICOLOR_FORCE")
	}
	t.Setenv("NO_COLOR", "")

	if !ColorEnabled(false) {
		t.Error("CLICOLOR_FORCE=1 should force color on even off a non-tty stdout")
	}

	t.Setenv("CLICOLOR_FORCE", "0")
	if ColorEnabled(false) {
		t.Error("CLICOLOR_FORCE=0 must not force color on")
	}
}

func TestLocalClockDifferentYearIncludesYear(t *testing.T) {
	now := time.Now()
	t2 := time.Date(now.Year()+1, 6, 15, 9, 5, 3, 0, now.Location())
	got := localClock(t2)
	want := t2.Format("2006-01-02 15:04:05")
	if got != want {
		t.Errorf("localClock different-year = %q, want %q", got, want)
	}
}
