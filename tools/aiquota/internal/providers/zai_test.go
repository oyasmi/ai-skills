package providers

import "testing"

func TestZaiWindowKeyIsStableNotLocalized(t *testing.T) {
	cases := []struct {
		unit, number int
		want         string
	}{
		{5, 30, "zai_30m"},
		{3, 5, "zai_5h"},
		{1, 7, "zai_7d"},
		{1, 1, "zai_1d"},
		{6, 1, "zai_1w"},
		{6, 2, "zai_2w"},
		{9, 3, "zai_3u"}, // unknown unit still yields an ascii key
	}
	for _, c := range cases {
		got := zaiWindowKey(c.unit, c.number)
		if got != c.want {
			t.Errorf("zaiWindowKey(%d, %d) = %q, want %q", c.unit, c.number, got, c.want)
		}
		for _, r := range got {
			if r > 127 {
				t.Errorf("zaiWindowKey(%d, %d) = %q contains a non-ASCII rune, keys must be stable machine ids", c.unit, c.number, got)
			}
		}
	}
}

func TestZaiWindowsMapsFields(t *testing.T) {
	limits := []any{
		map[string]any{
			"type": "TOKENS_LIMIT", "unit": float64(3), "number": float64(5),
			"percentage": float64(42.5), "nextResetTime": float64(1735689600000),
		},
		map[string]any{
			"type": "TOKENS_LIMIT", "unit": float64(1), "number": float64(7),
			"percentage": float64(10),
		},
		map[string]any{"type": "OTHER_LIMIT", "unit": float64(1), "number": float64(1), "percentage": float64(99)},
	}

	windows := zaiWindows(limits)
	if len(windows) != 2 {
		t.Fatalf("expected non-TOKENS_LIMIT entries to be skipped, got %d windows", len(windows))
	}

	// Sorted by duration ascending: 5h before 7d.
	if windows[0].Key != "zai_5h" || windows[0].UsedPercent != 42.5 {
		t.Errorf("unexpected first window: %+v", windows[0])
	}
	if windows[0].ResetAt == nil {
		t.Error("expected nextResetTime to populate ResetAt")
	}
	if windows[1].Key != "zai_7d" || windows[1].Label != "一周" {
		t.Errorf("unexpected second window: %+v", windows[1])
	}
}

func TestZaiWindowsDedupesKeyNotLabel(t *testing.T) {
	limits := []any{
		map[string]any{"type": "TOKENS_LIMIT", "unit": float64(1), "number": float64(7), "percentage": float64(10)},
		map[string]any{"type": "TOKENS_LIMIT", "unit": float64(1), "number": float64(7), "percentage": float64(20)},
	}

	windows := zaiWindows(limits)
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(windows))
	}
	if windows[0].Key == windows[1].Key {
		t.Errorf("expected duplicate windows to get distinct keys, both are %q", windows[0].Key)
	}
	if windows[1].Label != "一周 2" {
		t.Errorf("expected the second duplicate's label to be disambiguated, got %q", windows[1].Label)
	}
}
