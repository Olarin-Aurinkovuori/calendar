// Package rotation computes which duty group is responsible on a given date
// and the date ranges of its duty periods.
package rotation

import (
	"fmt"
	"time"

	"github.com/Olarin-Aurinkovuori/calendar/internal/config"
)

// Period describes one duty period: the group on duty and the inclusive
// date range [Start, End] it covers.
type Period struct {
	Group config.DutyGroup
	Index int // absolute period index since the rotation start date
	Start time.Time
	End   time.Time // inclusive
}

// Schedule wraps a loaded duty configuration for repeated period lookups.
type Schedule struct {
	groups     []config.DutyGroup
	start      time.Time
	periodDays int
}

// New parses the duty configuration and returns a Schedule, or an error if
// the configuration is incomplete or malformed.
func New(cfg config.DutyConfig) (*Schedule, error) {
	if len(cfg.Groups) == 0 {
		return nil, fmt.Errorf("no duty groups configured")
	}
	if cfg.PeriodDays <= 0 {
		return nil, fmt.Errorf("period_days must be positive")
	}
	start, err := time.ParseInLocation("2006-01-02", cfg.StartDate, time.UTC)
	if err != nil {
		return nil, fmt.Errorf("invalid duty start_date %q: %w", cfg.StartDate, err)
	}
	return &Schedule{groups: cfg.Groups, start: start, periodDays: cfg.PeriodDays}, nil
}

// floorDiv is integer division that rounds toward negative infinity, needed
// to correctly index periods that fall before the rotation start date.
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// dateOnly extracts the calendar date (year/month/day) of t -- as seen in
// t's own location, so "today" means today in the caller's timezone -- and
// reconstructs it at UTC midnight. Every date used in period arithmetic
// goes through this so that time.Sub always measures whole 24h days; doing
// the arithmetic in a timezone with DST would make days bordering a clock
// change 23 or 25 hours long and corrupt the day count.
func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// PeriodForDate returns the duty period covering the given date.
func (s *Schedule) PeriodForDate(date time.Time) Period {
	date = dateOnly(date)
	daysSince := int(date.Sub(s.start).Hours() / 24)
	periodIdx := floorDiv(daysSince, s.periodDays)
	groupIdx := ((periodIdx % len(s.groups)) + len(s.groups)) % len(s.groups)
	periodStart := s.start.AddDate(0, 0, periodIdx*s.periodDays)
	periodEnd := periodStart.AddDate(0, 0, s.periodDays-1)
	return Period{
		Group: s.groups[groupIdx],
		Index: periodIdx,
		Start: periodStart,
		End:   periodEnd,
	}
}

// PeriodsForGroupInRange returns every duty period belonging to groupID that
// overlaps the inclusive [from, to] date range, in chronological order.
func (s *Schedule) PeriodsForGroupInRange(groupID string, from, to time.Time) []Period {
	from = dateOnly(from)
	to = dateOnly(to)
	var out []Period
	if to.Before(from) {
		return out
	}
	// Walk period-by-period from the period containing `from` through `to`.
	p := s.PeriodForDate(from)
	for !p.Start.After(to) {
		if p.Group.ID == groupID {
			out = append(out, p)
		}
		nextStart := p.Start.AddDate(0, 0, s.periodDays)
		p = s.PeriodForDate(nextStart)
	}
	return out
}

// GroupByID returns the duty group with the given ID, if any.
func (s *Schedule) GroupByID(id string) (config.DutyGroup, bool) {
	for _, g := range s.groups {
		if g.ID == id {
			return g, true
		}
	}
	return config.DutyGroup{}, false
}

// Groups returns all configured duty groups in rotation order.
func (s *Schedule) Groups() []config.DutyGroup {
	return s.groups
}

// PeriodDays returns the configured length, in days, of each duty period.
func (s *Schedule) PeriodDays() int {
	return s.periodDays
}

// RecurrenceIntervalDays returns the fixed number of days between
// consecutive duty periods for the same group -- i.e. one full rotation
// through every group -- since the rotation repeats indefinitely at this
// constant interval, it also doubles as the day-interval of an equivalent
// calendar RRULE.
func (s *Schedule) RecurrenceIntervalDays() int {
	return s.periodDays * len(s.groups)
}

// FirstPeriodForGroupOnOrAfter returns the earliest duty period for groupID
// that starts on or after the period containing `from` -- i.e. groupID's
// current period if it is on duty as of `from`, otherwise its next
// upcoming one. Combined with RecurrenceIntervalDays, this gives the first
// occurrence of an indefinitely-repeating calendar series for that group.
func (s *Schedule) FirstPeriodForGroupOnOrAfter(groupID string, from time.Time) (Period, bool) {
	base := s.PeriodForDate(from)
	n := len(s.groups)
	baseGroupIdx := ((base.Index % n) + n) % n
	for i := 0; i < n; i++ {
		groupIdx := (baseGroupIdx + i) % n
		if s.groups[groupIdx].ID == groupID {
			start := base.Start.AddDate(0, 0, i*s.periodDays)
			return Period{
				Group: s.groups[groupIdx],
				Index: base.Index + i,
				Start: start,
				End:   start.AddDate(0, 0, s.periodDays-1),
			}, true
		}
	}
	return Period{}, false
}
