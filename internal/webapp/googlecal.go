package webapp

import (
	"fmt"
	"net/url"
	"time"
)

// googleCalendarLink builds a Google Calendar "quick add" URL that opens
// Calendar with a prefilled all-day event ready to save with one click --
// as an alternative to downloading an .ics file and importing it by hand.
// start/end are inclusive calendar dates, matching internal/ical.Event's
// semantics; Google's "dates" parameter treats its end date as exclusive,
// so it is advanced by one day here. When recurIntervalDays > 0 the event
// recurs indefinitely every that many days (see
// rotation.Schedule.RecurrenceIntervalDays), expressed as FREQ=DAILY since
// Google's RRULE requires one of DAILY/WEEKLY/MONTHLY/YEARLY and the
// configured period rarely divides evenly into weeks.
func googleCalendarLink(summary, details string, start, end time.Time, recurIntervalDays int) string {
	v := url.Values{}
	v.Set("action", "TEMPLATE")
	v.Set("text", summary)
	if details != "" {
		v.Set("details", details)
	}
	v.Set("dates", start.Format("20060102")+"/"+end.AddDate(0, 0, 1).Format("20060102"))
	if recurIntervalDays > 0 {
		v.Set("recur", fmt.Sprintf("RRULE:FREQ=DAILY;INTERVAL=%d", recurIntervalDays))
	}
	return "https://calendar.google.com/calendar/render?" + v.Encode()
}
