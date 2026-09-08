package productstate

import (
	"path/filepath"
	"testing"
	"time"
)

// P6 credits deduction rules: one unit per successful send, monthly reset,
// balance never recovers across months. See P6-入口隐藏与积分.md §3.4-3.5.

func TestConsumeCreditDecrements(t *testing.T) {
	s := openCredits(t, Credits{Balance: 1280, MonthKey: "2026-09"})

	got, err := s.ConsumeCredit(time.Date(2026, 9, 15, 10, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("ConsumeCredit = %v", err)
	}
	if got.Balance != 1279 {
		t.Fatalf("balance = %d, want 1279", got.Balance)
	}
	if got.MonthUsed != 1 {
		t.Fatalf("monthUsed = %d, want 1", got.MonthUsed)
	}
	// The snapshot must reflect the same values (persisted + published).
	if snap := s.Snapshot().Credits; snap.Balance != 1279 || snap.MonthUsed != 1 {
		t.Fatalf("snapshot credits = %+v, want balance 1279 monthUsed 1", snap)
	}
}

func TestConsumeCreditZeroBalanceKeepsSending(t *testing.T) {
	s := openCredits(t, Credits{Balance: 0, MonthUsed: 3, MonthKey: "2026-09"})

	got, err := s.ConsumeCredit(time.Date(2026, 9, 16, 10, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("ConsumeCredit = %v", err)
	}
	if got.Balance != 0 {
		t.Fatalf("balance = %d, want 0 (0 分仍可发)", got.Balance)
	}
	if got.MonthUsed != 4 {
		t.Fatalf("monthUsed = %d, want 4 (消耗继续累加)", got.MonthUsed)
	}
}

func TestConsumeCreditRollsMonth(t *testing.T) {
	s := openCredits(t, Credits{Balance: 1279, MonthUsed: 12, MonthKey: "2026-09"})

	got, err := s.ConsumeCredit(time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("ConsumeCredit = %v", err)
	}
	if got.MonthKey != "2026-10" {
		t.Fatalf("monthKey = %q, want 2026-10", got.MonthKey)
	}
	if got.MonthUsed != 1 {
		t.Fatalf("monthUsed = %d, want 1 (跨月归零后记本次)", got.MonthUsed)
	}
	if got.Balance != 1278 {
		t.Fatalf("balance = %d, want 1278 (余额不因跨月恢复)", got.Balance)
	}
}

func TestConsumeCreditRollsYear(t *testing.T) {
	s := openCredits(t, Credits{Balance: 5, MonthUsed: 7, MonthKey: "2026-12"})

	got, err := s.ConsumeCredit(time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("ConsumeCredit = %v", err)
	}
	if got.MonthKey != "2027-01" {
		t.Fatalf("monthKey = %q, want 2027-01", got.MonthKey)
	}
	if got.MonthUsed != 1 {
		t.Fatalf("monthUsed = %d, want 1", got.MonthUsed)
	}
	if got.Balance != 4 {
		t.Fatalf("balance = %d, want 4", got.Balance)
	}
}

// openCredits returns a Store over a fresh file with the given starting credits
// already persisted, so ConsumeCredit runs against a known state.
func openCredits(t *testing.T, c Credits) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "product-state.json"))
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	if err := s.Mutate(func(st *State) error { st.Credits = c; return nil }); err != nil {
		t.Fatalf("seed Mutate = %v", err)
	}
	return s
}
