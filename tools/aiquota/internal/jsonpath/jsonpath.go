// Package jsonpath pokes at arbitrary decoded JSON (map[string]any /
// []any / scalars): dotted keypath lookup, loose numeric/date coercion, and
// the heuristic field detection used by the custom-provider auto-detect path.
// It mirrors QuotaList's JSONUtil.swift so the two tools agree on what a
// config's keypaths and auto-detect mean.
package jsonpath

import (
	"strconv"
	"strings"
	"time"
)

// Value resolves a keypath like "a.b[0].c" against decoded JSON.
func Value(path string, root any) any {
	cur := root
	for _, tok := range tokens(path) {
		if idx, err := strconv.Atoi(tok); err == nil {
			arr, ok := cur.([]any)
			if !ok || idx < 0 || idx >= len(arr) {
				return nil
			}
			cur = arr[idx]
			continue
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[tok]
		if !ok {
			return nil
		}
	}
	return cur
}

func tokens(path string) []string {
	path = strings.ReplaceAll(path, "[", ".")
	path = strings.ReplaceAll(path, "]", "")
	parts := strings.Split(path, ".")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Number coerces a decoded JSON value (float64 from encoding/json, or a
// numeric-looking string with optional "%") to a float64.
func Number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case string:
		s := strings.Trim(strings.TrimSpace(n), "% ")
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// Date parses an ISO-8601 string or a unix epoch (seconds or milliseconds).
func Date(v any) (time.Time, bool) {
	if v == nil {
		return time.Time{}, false
	}
	switch n := v.(type) {
	case float64:
		return epoch(n), true
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return time.Time{}, false
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return epoch(f), true
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

func epoch(raw float64) time.Time {
	if raw > 2_000_000_000 {
		return time.UnixMilli(int64(raw))
	}
	return time.Unix(int64(raw), 0)
}

// Detected is a best-effort guess of a usage percentage and reset time from an
// unknown JSON shape.
type Detected struct {
	UsedPercent float64
	ResetAt     *time.Time
}

// DetectUsage flattens `root` into dotted paths and scores each 0–100
// numeric leaf by how much its key looks like a usage field. Returns false
// when nothing plausible is found.
func DetectUsage(root any) (Detected, bool) {
	flat := flatten(root, "")

	type scored struct {
		score int
		value float64
	}
	var best *scored
	for _, kv := range flat {
		n, ok := Number(kv.value)
		if !ok || n < 0 || n > 100 {
			continue
		}
		key := strings.ToLower(kv.path)
		score := 0
		if containsAny(key, "percent", "usage", "used") {
			score += 3
		}
		if containsAny(key, "5h", "session", "primary") {
			score += 4
		}
		if containsAny(key, "rate", "limit", "quota") {
			score += 2
		}
		if containsAny(key, "weekly", "7d") {
			score += 1
		}
		if score == 0 {
			continue
		}
		if best == nil || score > best.score {
			best = &scored{score: score, value: n}
		}
	}
	if best == nil {
		return Detected{}, false
	}

	var reset *time.Time
	for _, kv := range flat {
		if !strings.Contains(strings.ToLower(kv.path), "reset") {
			continue
		}
		if t, ok := Date(kv.value); ok {
			reset = &t
			break
		}
	}
	return Detected{UsedPercent: best.value, ResetAt: reset}, true
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

type pathValue struct {
	path  string
	value any
}

func flatten(v any, prefix string) []pathValue {
	switch t := v.(type) {
	case map[string]any:
		out := make([]pathValue, 0, len(t))
		for k, val := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			out = append(out, flatten(val, p)...)
		}
		return out
	case []any:
		out := make([]pathValue, 0, len(t))
		for i, val := range t {
			p := prefix + "[" + strconv.Itoa(i) + "]"
			out = append(out, flatten(val, p)...)
		}
		return out
	default:
		return []pathValue{{path: prefix, value: v}}
	}
}
