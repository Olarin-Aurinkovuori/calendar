package rotation

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Olarin-Aurinkovuori/calendar/internal/config"
)

// Season classifies calendar weeks as "summer" or "winter" using two
// configured MM-DD anchor dates: summer runs from the first Monday on or
// after the start anchor (default "05-01", i.e. the first full week of May)
// through the Sunday of the week containing the end anchor (default
// "09-30", i.e. the week including the last day of September).
type Season struct {
	startMonth time.Month
	startDay   int
	endMonth   time.Month
	endDay     int
}

// NewSeason parses the configured anchors into a Season.
func NewSeason(cfg config.ScheduleConfig) (*Season, error) {
	startMonth, startDay, err := parseAnchor(cfg.SummerStartAnchor)
	if err != nil {
		return nil, fmt.Errorf("invalid summer_start_anchor %q: %w", cfg.SummerStartAnchor, err)
	}
	endMonth, endDay, err := parseAnchor(cfg.SummerEndAnchor)
	if err != nil {
		return nil, fmt.Errorf("invalid summer_end_anchor %q: %w", cfg.SummerEndAnchor, err)
	}
	return &Season{startMonth: startMonth, startDay: startDay, endMonth: endMonth, endDay: endDay}, nil
}

func parseAnchor(s string) (time.Month, int, error) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected MM-DD")
	}
	m, err := strconv.Atoi(parts[0])
	if err != nil || m < 1 || m > 12 {
		return 0, 0, fmt.Errorf("invalid month")
	}
	d, err := strconv.Atoi(parts[1])
	if err != nil || d < 1 || d > 31 {
		return 0, 0, fmt.Errorf("invalid day")
	}
	return time.Month(m), d, nil
}

// IsSummer reports whether the week starting on monday (which must be a
// Monday at UTC midnight, as produced by MondayOf) falls within the
// configured summer season for its calendar year.
func (se *Season) IsSummer(monday time.Time) bool {
	year := monday.Year()
	start := firstMondayOnOrAfter(time.Date(year, se.startMonth, se.startDay, 0, 0, 0, 0, time.UTC))
	endAnchor := time.Date(year, se.endMonth, se.endDay, 0, 0, 0, 0, time.UTC)
	end := MondayOf(endAnchor)
	return !monday.Before(start) && !monday.After(end)
}

// MondayOf returns the Monday (UTC midnight) of the ISO week containing t.
func MondayOf(t time.Time) time.Time {
	t = dateOnly(t)
	offset := (int(t.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
	return t.AddDate(0, 0, -offset)
}

// firstMondayOnOrAfter returns the earliest Monday that is not before t.
func firstMondayOnOrAfter(t time.Time) time.Time {
	offset := (8 - int(t.Weekday())) % 7
	return t.AddDate(0, 0, offset)
}

// Week describes one calendar week (Monday through Sunday) in the schedule
// view: its ISO week number, date range, the duty group on duty as of its
// Monday, and whether it falls within the configured summer season.
type Week struct {
	Number int // ISO 8601 week number
	Monday time.Time
	Sunday time.Time
	Group  config.DutyGroup
	Summer bool
}

// Weeks returns one Week per calendar week overlapping [from, to], covering
// full weeks (expanded outward to the enclosing Mondays/Sundays). The duty
// group shown for each week is whichever group is on duty on that week's
// Monday. If season is nil, Summer is always false.
func (s *Schedule) Weeks(from, to time.Time, season *Season) []Week {
	from = dateOnly(from)
	to = dateOnly(to)
	if to.Before(from) {
		return nil
	}
	var weeks []Week
	startMonday := MondayOf(from)
	endMonday := MondayOf(to)
	for m := startMonday; !m.After(endMonday); m = m.AddDate(0, 0, 7) {
		_, isoWeek := m.ISOWeek()
		period := s.PeriodForDate(m)
		summer := false
		if season != nil {
			summer = season.IsSummer(m)
		}
		weeks = append(weeks, Week{
			Number: isoWeek,
			Monday: m,
			Sunday: m.AddDate(0, 0, 6),
			Group:  period.Group,
			Summer: summer,
		})
	}
	return weeks
}
