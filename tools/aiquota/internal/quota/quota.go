// Package quota holds the data model shared by every provider and by the
// renderers. It mirrors QuotaList's `QuotaWindow` / `ProviderSnapshot` so both
// tools describe the same thing in the same words.
package quota

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

// localTimeLayout is RFC3339 without the fractional-second component, e.g.
// "2026-08-02T12:49:59+08:00". Every timestamp this tool emits (JSON or text)
// goes through FormatLocal so times read in the viewer's local zone instead
// of the raw UTC/whatever-zone the upstream API returned.
const localTimeLayout = "2006-01-02T15:04:05Z07:00"

// FormatLocal renders `t` in the local timezone, seconds precision.
func FormatLocal(t time.Time) string {
	return t.Local().Format(localTimeLayout)
}

// State is the fetch outcome of one provider.
type State string

const (
	StateOK     State = "ok"
	StateNoData State = "no_data" // Nothing to read (not logged in / not configured).
	StateError  State = "error"   // Fetch failed, `Error` says why.
)

// Window is a single quota window. 5h / weekly / balance all map onto this.
type Window struct {
	// Key is a stable machine id (five_hour, seven_day, ...). Prefer it over
	// Label when filtering: Label is human text and may be localized.
	Key   string `json:"key"`
	Label string `json:"label"`

	UsedPercent      float64 `json:"used_percent"`
	RemainingPercent float64 `json:"remaining_percent"`

	ResetAt        *time.Time `json:"reset_at,omitempty"`
	ResetInSeconds *int64     `json:"reset_in_seconds,omitempty"`

	// Detail is optional extra text for balance-style windows, e.g. "$8.4 / $20".
	Detail string `json:"detail,omitempty"`
}

// NewWindow builds a window with UsedPercent clamped to 0–100.
func NewWindow(key, label string, usedPercent float64, resetAt *time.Time) Window {
	w := Window{Key: key, Label: label, UsedPercent: clamp(usedPercent)}
	w.ResetAt = resetAt
	return w
}

// MarshalJSON emits ResetAt in the local timezone (see FormatLocal) instead
// of time.Time's default UTC/nanosecond rendering.
func (w Window) MarshalJSON() ([]byte, error) {
	type alias Window
	var resetAt *string
	if w.ResetAt != nil {
		s := FormatLocal(*w.ResetAt)
		resetAt = &s
	}
	return json.Marshal(struct {
		alias
		ResetAt *string `json:"reset_at,omitempty"`
	}{alias: alias(w), ResetAt: resetAt})
}

// Snapshot is one provider's result.
type Snapshot struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Plan  string `json:"plan,omitempty"`
	State State  `json:"state"`
	Error string `json:"error,omitempty"`

	// Source records where the numbers came from, which matters for freshness:
	// e.g. Codex "session_file" is a local cache that only updates when the
	// Codex CLI itself talks to the server.
	Source string `json:"source,omitempty"`

	Windows    []Window   `json:"windows"`
	ValidUntil *time.Time `json:"valid_until,omitempty"`

	FetchedAt time.Time `json:"fetched_at"`
}

// MarshalJSON emits ValidUntil/FetchedAt in the local timezone (see
// FormatLocal) instead of time.Time's default UTC/nanosecond rendering.
func (s Snapshot) MarshalJSON() ([]byte, error) {
	type alias Snapshot
	var validUntil *string
	if s.ValidUntil != nil {
		v := FormatLocal(*s.ValidUntil)
		validUntil = &v
	}
	return json.Marshal(struct {
		alias
		ValidUntil *string `json:"valid_until,omitempty"`
		FetchedAt  string  `json:"fetched_at"`
	}{alias: alias(s), ValidUntil: validUntil, FetchedAt: FormatLocal(s.FetchedAt)})
}

// Report is the top-level payload of `aiquota --json`.
type Report struct {
	OK        bool       `json:"ok"`
	Now       time.Time  `json:"now"`
	Providers []Snapshot `json:"providers"`
}

// MarshalJSON emits Now in the local timezone (see FormatLocal) instead of
// time.Time's default UTC/nanosecond rendering.
func (r Report) MarshalJSON() ([]byte, error) {
	type alias Report
	return json.Marshal(struct {
		alias
		Now string `json:"now"`
	}{alias: alias(r), Now: FormatLocal(r.Now)})
}

// Provider fetches one channel's quota. Implementations are strictly read-only.
type Provider interface {
	ID() string
	Name() string
	Fetch(ctx context.Context) Snapshot
}

// Fail builds a snapshot for a provider that produced no usable data.
func Fail(id, name string, state State, msg string) Snapshot {
	return Snapshot{ID: id, Name: name, State: state, Error: msg, FetchedAt: time.Now()}
}

// Normalize rounds percentages and derives each window's ResetInSeconds from
// `now`, so the JSON output carries a ready-to-use countdown alongside the
// absolute reset time.
func (s *Snapshot) Normalize(now time.Time) {
	if s.FetchedAt.IsZero() {
		s.FetchedAt = now
	}
	for i := range s.Windows {
		w := &s.Windows[i]
		w.UsedPercent = clamp(w.UsedPercent)
		w.RemainingPercent = round1(100 - w.UsedPercent)
		w.UsedPercent = round1(w.UsedPercent)
		w.ResetInSeconds = nil
		if w.ResetAt != nil {
			secs := int64(w.ResetAt.Sub(now).Seconds())
			if secs < 0 {
				secs = 0
			}
			w.ResetInSeconds = &secs
		}
	}
}

func clamp(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Min(math.Max(v, 0), 100)
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
