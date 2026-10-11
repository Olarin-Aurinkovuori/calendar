// Package ical renders iCalendar (RFC 5545) VCALENDAR documents for
// all-day events, used to export duty periods and flag days.
package ical

import (
	"fmt"
	"strings"
	"time"
)

// Event is a single all-day iCalendar event.
type Event struct {
	UID         string
	Summary     string
	Description string
	Start       time.Time // inclusive, all-day
	End         time.Time // inclusive, all-day (will be exported as exclusive DTEND = End+1)
}

// Calendar renders a VCALENDAR document containing the given events.
func Calendar(prodID, calendarName string, events []Event) []byte {
	var b strings.Builder
	writeLine(&b, "BEGIN:VCALENDAR")
	writeLine(&b, "VERSION:2.0")
	writeLine(&b, "PRODID:"+escapeText(prodID))
	writeLine(&b, "CALSCALE:GREGORIAN")
	if calendarName != "" {
		writeLine(&b, "X-WR-CALNAME:"+escapeText(calendarName))
	}
	now := time.Now().UTC().Format("20060102T150405Z")
	for _, e := range events {
		writeLine(&b, "BEGIN:VEVENT")
		writeLine(&b, "UID:"+escapeText(e.UID))
		writeLine(&b, "DTSTAMP:"+now)
		writeLine(&b, "DTSTART;VALUE=DATE:"+e.Start.Format("20060102"))
		writeLine(&b, "DTEND;VALUE=DATE:"+e.End.AddDate(0, 0, 1).Format("20060102"))
		writeLine(&b, "SUMMARY:"+escapeText(e.Summary))
		if e.Description != "" {
			writeLine(&b, "DESCRIPTION:"+escapeText(e.Description))
		}
		writeLine(&b, "END:VEVENT")
	}
	writeLine(&b, "END:VCALENDAR")
	return []byte(b.String())
}

// writeLine appends a CRLF-terminated, folded iCalendar content line.
func writeLine(b *strings.Builder, line string) {
	b.WriteString(fold(line))
	b.WriteString("\r\n")
}

// fold wraps content lines longer than 75 octets per RFC 5545 section 3.1,
// continuing with a single leading space.
func fold(line string) string {
	const limit = 75
	if len(line) <= limit {
		return line
	}
	var b strings.Builder
	for len(line) > limit {
		b.WriteString(line[:limit])
		b.WriteString("\r\n ")
		line = line[limit:]
	}
	b.WriteString(line)
	return b.String()
}

// escapeText escapes special characters as required for TEXT values.
func escapeText(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`;`, `\;`,
		`,`, `\,`,
		"\n", `\n`,
	)
	return replacer.Replace(s)
}

// EventUID builds a deterministic, collision-resistant UID for an event.
func EventUID(parts ...string) string {
	return fmt.Sprintf("%s@calendar", strings.Join(parts, "-"))
}
