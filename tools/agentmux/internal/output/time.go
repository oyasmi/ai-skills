package output

import "time"

// LocalTime formats a user-visible timestamp in the machine's local timezone.
// Agentmux deliberately uses local time for both text and JSON output so a
// human can compare command output with the clock and other local tooling.
func LocalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format(time.RFC3339Nano)
}

func OptionalLocalTime(t time.Time) *string {
	value := LocalTime(t)
	if value == "" {
		return nil
	}
	return &value
}

// LocalizeTimestamp converts an RFC3339 build timestamp to local time. Values
// such as "unknown" are metadata, not timestamps, and pass through unchanged.
func LocalizeTimestamp(raw string) string {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		parsed, err = time.Parse("2006-01-02T15:04:05-0700", raw)
		if err != nil {
			return raw
		}
	}
	return LocalTime(parsed)
}
