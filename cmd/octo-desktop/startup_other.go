//go:build !darwin && !windows

package main

import "time"

func platformProcessStartedAt() (time.Time, error) { return time.Time{}, nil }

// Other platforms retain the existing HTML startup screen.
func platformStartupWindow(_, _ string, _ func()) (func(), func(string), error) {
	return nil, nil, nil
}
