package jsonpath

import (
	"encoding/json"
	"testing"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func TestValue(t *testing.T) {
	root := decode(t, `{"a":{"b":[{"c":42}]}}`)
	if v := Value("a.b[0].c", root); v != float64(42) {
		t.Fatalf("got %v", v)
	}
	if v := Value("a.missing", root); v != nil {
		t.Fatalf("expected nil, got %v", v)
	}
}

func TestNumber(t *testing.T) {
	cases := []struct {
		in   any
		want float64
		ok   bool
	}{
		{float64(12.5), 12.5, true},
		{"42%", 42, true},
		{" 3.5 ", 3.5, true},
		{"nope", 0, false},
		{nil, 0, false},
	}
	for _, c := range cases {
		got, ok := Number(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("Number(%v) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestDateEpochSecondsAndMillis(t *testing.T) {
	secTime, ok := Date(float64(1_700_000_000))
	if !ok || secTime.Unix() != 1_700_000_000 {
		t.Fatalf("seconds epoch parse failed: %v ok=%v", secTime, ok)
	}
	msTime, ok := Date(float64(1_700_000_000_000))
	if !ok || msTime.Unix() != 1_700_000_000 {
		t.Fatalf("millis epoch parse failed: %v ok=%v", msTime, ok)
	}
}

func TestDateISO(t *testing.T) {
	tm, ok := Date("2026-08-05T00:00:00Z")
	if !ok || tm.Year() != 2026 {
		t.Fatalf("iso parse failed: %v ok=%v", tm, ok)
	}
}

func TestDetectUsagePrefersPercentLikeKeys(t *testing.T) {
	root := decode(t, `{"five_hour":{"percent_used":42.5},"other":{"count":7},"meta":{"reset_at":"2026-08-05T00:00:00Z"}}`)
	d, ok := DetectUsage(root)
	if !ok {
		t.Fatal("expected detection")
	}
	if d.UsedPercent != 42.5 {
		t.Fatalf("got percent %v", d.UsedPercent)
	}
	if d.ResetAt == nil {
		t.Fatal("expected reset time")
	}
}

func TestDetectUsageNoneFound(t *testing.T) {
	root := decode(t, `{"unrelated":{"count":7}}`)
	if _, ok := DetectUsage(root); ok {
		t.Fatal("expected no detection")
	}
}
