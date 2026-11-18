package http

import (
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Duration{
		"":                              0,
		"5":                             5 * time.Second,
		" 120 ":                         120 * time.Second,
		"0":                             0,
		"-3":                            0,
		"99999999":                      24 * time.Hour,
		"soon":                          0,
		"Wed, 18 Nov 2026 12:00:30 GMT": 30 * time.Second,
		"Wed, 18 Nov 2026 11:00:00 GMT": 0, // in the past
	} {
		if got := ParseRetryAfter([]byte(in), now); got != want {
			t.Errorf("ParseRetryAfter(%q) = %s, want %s", in, got, want)
		}
	}
}
