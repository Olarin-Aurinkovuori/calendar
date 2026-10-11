package webapp

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Olarin-Aurinkovuori/calendar/internal/config"
	"github.com/kyokomi/emoji/v2"
)

// flagDayParsed pairs a FlagDay with its parsed date for sorting/comparison.
type flagDayParsed struct {
	Date time.Time
	config.FlagDay
}

// collectFlagDays parses and sorts every configured flag day across all
// years into one chronological slice. Entries with unparsable dates are
// skipped.
func collectFlagDays(years map[string]*config.FlagYear) []flagDayParsed {
	var out []flagDayParsed
	for _, year := range years {
		if year == nil {
			continue
		}
		for _, d := range year.Days {
			t, err := time.ParseInLocation("2006-01-02", d.Date, time.Local)
			if err != nil {
				continue
			}
			out = append(out, flagDayParsed{Date: t, FlagDay: d})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

// FlagSummary is the aggregated info about flag day(s) on a particular date
// (there can be more than one observance on the same day).
type FlagSummary struct {
	Date      string
	Name      string
	Mandatory bool
}

func aggregate(entries []flagDayParsed) *FlagSummary {
	if len(entries) == 0 {
		return nil
	}
	names := make([]string, 0, len(entries))
	mandatory := false
	for _, e := range entries {
		names = append(names, e.Name)
		if e.Mandatory {
			mandatory = true
		}
	}
	return &FlagSummary{
		Date:      entries[0].Date.Format("2006-01-02"),
		Name:      strings.Join(names, "; "),
		Mandatory: mandatory,
	}
}

// flagDayOn returns the (possibly combined) flag day summary for exactly
// the given date, or nil if there is none.
func flagDayOn(all []flagDayParsed, date time.Time) *FlagSummary {
	date = dateOnly(date)
	var match []flagDayParsed
	for _, e := range all {
		if dateOnly(e.Date).Equal(date) {
			match = append(match, e)
		}
	}
	return aggregate(match)
}

// nextFlagDayAfter returns the (possibly combined) flag day summary for the
// earliest date strictly after the given date, or nil if none are known.
func nextFlagDayAfter(all []flagDayParsed, after time.Time) *FlagSummary {
	after = dateOnly(after)
	for i, e := range all {
		d := dateOnly(e.Date)
		if d.After(after) {
			var match []flagDayParsed
			for _, e2 := range all[i:] {
				if dateOnly(e2.Date).Equal(d) {
					match = append(match, e2)
				} else {
					break
				}
			}
			return aggregate(match)
		}
	}
	return nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// flagDaysInRange returns all flag days whose date falls within [from, to]
// inclusive.
func flagDaysInRange(all []flagDayParsed, from, to time.Time) []flagDayParsed {
	from = dateOnly(from)
	to = dateOnly(to)
	var out []flagDayParsed
	for _, e := range all {
		d := dateOnly(e.Date)
		if !d.Before(from) && !d.After(to) {
			out = append(out, e)
		}
	}
	return out
}

// specialDateParsed pairs a config.SpecialDate with its parsed date for
// sorting/comparison.
type specialDateParsed struct {
	Date time.Time
	config.SpecialDate
}

// collectSpecialDates parses and sorts every configured special date into
// one chronological slice, resolving any :emoji-name: shortcode icon
// fields (see resolveIconShortcode) to their actual emoji for display.
// Entries with unparsable dates are skipped. The original config is left
// untouched -- the config-edit form reads config.SpecialDate directly, so
// admins still see/edit the shortcode text they typed.
func collectSpecialDates(entries []config.SpecialDate) []specialDateParsed {
	var out []specialDateParsed
	for _, sd := range entries {
		t, err := time.ParseInLocation("2006-01-02", sd.Date, time.Local)
		if err != nil {
			continue
		}
		sd.Icon = resolveIconShortcode(sd.Icon)
		sd.SummaryIcon = resolveIconShortcode(sd.SummaryIcon)
		sd.DescriptionIcon = resolveIconShortcode(sd.DescriptionIcon)
		out = append(out, specialDateParsed{Date: t, SpecialDate: sd})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

// specialDatesInRange returns all special dates whose date falls within
// [from, to] inclusive.
func specialDatesInRange(all []specialDateParsed, from, to time.Time) []specialDateParsed {
	from = dateOnly(from)
	to = dateOnly(to)
	var out []specialDateParsed
	for _, e := range all {
		d := dateOnly(e.Date)
		if !d.Before(from) && !d.After(to) {
			out = append(out, e)
		}
	}
	return out
}

// imageIconExtensions lists the file extensions (lowercase, with the dot)
// recognized as images by iconIsImage.
var imageIconExtensions = []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp"}

// iconIsImage reports whether an icon field value should be rendered as an
// <img> (a remote URL, an already-local path, or a recognized image file
// extension) rather than as plain text/emoji. It mirrors the heuristic
// implemented in JavaScript for the config UI's live preview
// (static/special-dates.js) and must be kept in sync with it.
func iconIsImage(icon string) bool {
	if icon == "" {
		return false
	}
	lower := strings.ToLower(icon)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(icon, "/") {
		return true
	}
	for _, ext := range imageIconExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// builtinWeatherIcons maps a config.WeatherCondition.Icon shorthand name to
// a default emoji, so admins don't need to look up/paste emoji themselves
// for common condition kinds. Any Icon value not matching one of these
// names (case-insensitively) -- including any other emoji, text, or image
// URL/path -- is used as-is.
var builtinWeatherIcons = map[string]string{
	"slippery": "⛸️",
	"snow":     "❄️",
	"rain":     "🌧️",
}

// resolveWeatherIcon expands a WeatherCondition.Icon shorthand name (see
// builtinWeatherIcons) to its built-in emoji, falling back to :emoji-name:
// shortcode resolution (see resolveIconShortcode), or returns icon
// unchanged if neither matches.
func resolveWeatherIcon(icon string) string {
	if resolved, ok := builtinWeatherIcons[strings.ToLower(icon)]; ok {
		return resolved
	}
	return resolveIconShortcode(icon)
}

// emojiShortcodePattern matches a whole icon field value consisting of a
// single :shortcode: (e.g. ":snowflake:", ":cloud_with_rain:"), the same
// GitHub/Slack-style notation recognized by the github.com/kyokomi/emoji
// package's code map.
var emojiShortcodePattern = regexp.MustCompile(`^:[a-zA-Z0-9_+-]+:$`)

// resolveIconShortcode expands an icon field value given as a single
// :emoji-name: shortcode (see emojiShortcodePattern) to the actual emoji
// character(s), so admins can type e.g. ":snowflake:" instead of having to
// paste an emoji glyph. Any value that isn't a recognized shortcode --
// including plain emoji, text, image URLs/paths, or an unrecognized
// ":something:" -- is returned unchanged.
func resolveIconShortcode(icon string) string {
	trimmed := strings.TrimSpace(icon)
	if !emojiShortcodePattern.MatchString(trimmed) {
		return icon
	}
	if resolved, ok := emoji.CodeMap()[trimmed]; ok {
		return resolved
	}
	return icon
}
