package productstate

import "time"

// Credits deduction for the fake points balance (需求 §5.4.4). The schema
// (Balance / MonthUsed / MonthKey) was fixed in P3; this file owns the rules
// that make the number move — every successfully-sent user message costs one
// unit, the monthly counter resets on the local calendar month, and balance
// never recovers across a month boundary.
//
// OCTO-FORK: P6 credits — see
// dev-docs-usdable/需求/2260906/技术方案/P6-入口隐藏与积分.md.

// rollMonth lazily resets MonthUsed when the local calendar month changes.
// Lazy, not timer-driven: the process may be unplugged across the boundary
// (便携 U 盘), so there is no reliable timer. Balance is deliberately
// untouched — 需求 §5.4.4「积分余额不因跨月恢复」.
func (c *Credits) rollMonth(now time.Time) {
	key := now.Format("2006-01")
	if c.MonthKey != key {
		c.MonthKey = key
		c.MonthUsed = 0
	}
}

// ConsumeCredit records one successfully-sent user message. MonthUsed always
// increments; Balance decrements only while it is above zero (0 分仍可发,
// 需求 §5.4.4). It returns the post-consume snapshot so the caller can hand
// it straight back to the frontend without a second state round-trip.
func (s *Store) ConsumeCredit(now time.Time) (Credits, error) {
	var out Credits
	err := s.Mutate(func(st *State) error {
		st.Credits.rollMonth(now)
		st.Credits.MonthUsed++
		if st.Credits.Balance > 0 {
			st.Credits.Balance--
		}
		out = st.Credits
		return nil
	})
	return out, err
}
