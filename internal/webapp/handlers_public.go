package webapp

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Olarin-Aurinkovuori/calendar/internal/config"
	"github.com/Olarin-Aurinkovuori/calendar/internal/i18n"
	"github.com/Olarin-Aurinkovuori/calendar/internal/ical"
	"github.com/Olarin-Aurinkovuori/calendar/internal/rotation"
)

// IndexData is the template data for the home/info view.
type IndexData struct {
	baseData
	Today              string
	CurrentGroup       *GroupViewModel
	PeriodRange        string
	TodaySeasonInfo    string // extra info text for today's season (summer/winter), shown on the summary card
	TodayFlag          *FlagSummary
	NextFlag           *FlagSummary
	NextFlagWeeksAhead int // number of calendar weeks between this week and NextFlag's week
	WeekRows           []WeekRowView
	UpcomingSpecial    []UpcomingSpecialView  // active special dates within their (farther) summary window, shown as a row under the day summary
	SpecialDetail      []ProminentSpecialView // active special dates within their (closer) description window, shown larger alongside the day summary
	YearEndMonthsAhead int                    // months-ahead value covering the rest of the year (or into next year, if that's under a month)
	FlagSources        []FlagSourceView
	AssetOrigins       []AssetOriginView
	ShowMoreWeeks      int    // step size, in weeks, the "Show more" button loads per click
	ScheduleNextFrom   string // YYYY-MM-DD Monday right after the last rendered week row, for the "Show more" button's first request
}

// FlagSourceView describes where one calendar year's flag day data comes
// from, shown as a reference at the bottom of the info view.
type FlagSourceView struct {
	Year         string
	SourceURL    string
	ExternalFile string // resolved path, set only when flagging_days.year_file_template is configured
}

// AssetOriginView describes whether a UI template/static asset was loaded
// from the embedded binary or overridden from an external file on disk.
type AssetOriginView struct {
	Name     string
	External bool
	Dir      string
}

// WeekRowView is one row of the upcoming-schedule week list.
type WeekRowView struct {
	WeekLabel          string
	GroupLabel         string
	GroupID            string
	RangeLabel         string
	SeasonTooltip      string
	Summer             bool
	IsCurrent          bool
	FlagDays           []WeekFlagDayView
	HasFlag            bool
	MarkerIcons        []MarkerIconView
	GoogleCalendarLink string // quick-add link for this group's indefinitely-recurring duty series
}

// MarkerIconView is one de-duplicated icon shown in a week row's
// flag-marker column: the default 🇫🇮 for official flag days, plus one
// entry per distinct special-date icon observed that week.
type MarkerIconView struct {
	Icon    string
	IsImage bool
}

// WeekFlagDayView is one flag day or special date shown within a week row.
type WeekFlagDayView struct {
	Name        string
	Date        string
	Mandatory   bool
	Special     bool // true for an admin-configured special date, not an official flag day
	Icon        string
	IconIsImage bool
}

// UpcomingSpecialView is one special date shown, as a compact row entry
// alongside others, while within its (farther) SummaryLeadDays window but
// not yet within its (closer) DescriptionLeadDays window. Its icon/summary
// are repeated, larger, in a hover tooltip over the entry, alongside an
// "add to calendar" link for that single date.
type UpcomingSpecialView struct {
	Summary            string
	Date               string // formatted per the page's date format, for display
	DateISO            string // YYYY-MM-DD, for the calendar-download link
	DaysAhead          int    // number of days between today and Date, for an "in N day(s)" hint
	Icon               string
	IconIsImage        bool
	IconScale          float64
	GoogleCalendarLink string // quick-add link for this single date
}

// ProminentSpecialView is one special date shown, larger, alongside the day
// summary (to its right) while within its (closer) DescriptionLeadDays
// window -- including Title and Summary together with Description, plus
// DescriptionIcon.
type ProminentSpecialView struct {
	Title       string
	Summary     string
	Description string
	Icon        string
	IconIsImage bool
	IconScale   float64
}

// GroupViewModel is the display-ready version of a duty group.
type GroupViewModel struct {
	Label      string
	Apartments string
}

// ScheduleMoreData is the template data for a "Show more" button fragment
// response: just enough fields to satisfy the same "week_rows" named
// template used by the full info view.
type ScheduleMoreData struct {
	baseData
	YearEndMonthsAhead int
	TodayFlag          *FlagSummary
	WeekRows           []WeekRowView
}

// buildWeekRows renders one WeekRowView per calendar week overlapping
// [from, to], shared by the full info view and the "Show more" button's
// fragment endpoint so both produce identical row markup/links.
func buildWeekRows(sched *rotation.Schedule, season *rotation.Season, allFlags []flagDayParsed, allSpecial []specialDateParsed, from, to, todayMonday, now time.Time, tr *i18n.Translator, lang, dateFormat, weekNumberFormat string) []WeekRowView {
	recurIntervalDays := sched.RecurrenceIntervalDays()
	titlePrefix := tr.T(lang, "calendar_event_title_prefix")
	groupCalendarLinks := make(map[string]string)
	var rows []WeekRowView
	for _, week := range sched.Weeks(from, to, season) {
		row := WeekRowView{
			WeekLabel:  fmt.Sprintf(weekNumberFormat, week.Number),
			GroupLabel: week.Group.Label,
			GroupID:    week.Group.ID,
			RangeLabel: tr.T(lang, "duty_period_range", week.Monday.Format(dateFormat), week.Sunday.Format(dateFormat)),
			Summer:     week.Summer,
			IsCurrent:  week.Monday.Equal(todayMonday),
		}
		if week.Summer {
			row.SeasonTooltip = tr.T(lang, "schedule_season_summer_tooltip")
		} else {
			row.SeasonTooltip = tr.T(lang, "schedule_season_winter_tooltip")
		}
		row.FlagDays, row.MarkerIcons = mergedWeekFlagDays(allFlags, allSpecial, week.Monday, week.Sunday, dateFormat)
		row.HasFlag = len(row.FlagDays) > 0
		link, ok := groupCalendarLinks[row.GroupID]
		if !ok {
			if first, found := sched.FirstPeriodForGroupOnOrAfter(row.GroupID, now); found {
				link = googleCalendarLink(
					titlePrefix+fmt.Sprintf("%s: %s", first.Group.Label, strings.Join(first.Group.Apartments, ", ")),
					"", first.Start, first.End, recurIntervalDays,
				)
			}
			groupCalendarLinks[row.GroupID] = link
		}
		row.GoogleCalendarLink = link
		rows = append(rows, row)
	}
	return rows
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	flagging, ffErr := s.store.EffectiveFlaggingDays()
	if ffErr != nil {
		log.Printf("reading flag days: %v", ffErr)
	}
	configPath := s.store.Path()
	assetOrigins := publicAssetOrigins()

	s.store.View(func(cfg *config.Config) {
		tr := i18n.New(cfg.Languages, cfg.DefaultLanguage)
		lang := s.resolveLanguage(r, tr)
		base := s.newBaseData(r, cfg, tr, lang)
		dateFormat := tr.T(lang, "date_format")
		if dateFormat == "date_format" {
			dateFormat = "2006-01-02"
		}
		weekNumberFormat := tr.T(lang, "week_number_format")
		if weekNumberFormat == "week_number_format" {
			weekNumberFormat = "%d"
		}

		now := time.Now()
		data := IndexData{
			baseData:           base,
			Today:              now.Format(dateFormat),
			YearEndMonthsAhead: monthsUntilYearEnd(now),
		}
		for _, o := range assetOrigins {
			data.AssetOrigins = append(data.AssetOrigins, AssetOriginView{Name: o.Name, External: o.External, Dir: o.Dir})
		}

		allFlags := collectFlagDays(flagging.Years)
		allSpecial := collectSpecialDates(cfg.SpecialDates)
		data.SpecialDetail, data.UpcomingSpecial = activeSpecialDates(allSpecial, now, dateFormat)

		sched, err := rotation.New(cfg.Duty)
		if err == nil {
			period := sched.PeriodForDate(now)
			data.CurrentGroup = &GroupViewModel{
				Label:      period.Group.Label,
				Apartments: strings.Join(period.Group.Apartments, ", "),
			}
			data.PeriodRange = tr.T(lang, "duty_period_range", period.Start.Format(dateFormat), period.End.Format(dateFormat))

			season, serr := rotation.NewSeason(cfg.Schedule)
			if serr != nil {
				season = nil
			}
			todayMonday := rotation.MondayOf(now)
			if season != nil {
				if season.IsSummer(todayMonday) {
					data.TodaySeasonInfo = tr.T(lang, "today_season_summer_info")
				} else {
					data.TodaySeasonInfo = tr.T(lang, "today_season_winter_info")
				}
			}
			from := now.AddDate(0, -cfg.Schedule.MonthsBefore, 0)
			to := now.AddDate(0, cfg.Schedule.MonthsAfter, 0)
			data.WeekRows = buildWeekRows(sched, season, allFlags, allSpecial, from, to, todayMonday, now, tr, lang, dateFormat, weekNumberFormat)
			data.ShowMoreWeeks = cfg.Schedule.ShowMoreWeeks
			data.ScheduleNextFrom = rotation.MondayOf(to).AddDate(0, 0, 7).Format("2006-01-02")

			for y := from.Year(); y <= to.Year(); y++ {
				yearKey := strconv.Itoa(y)
				src := FlagSourceView{Year: yearKey}
				if fy := flagging.Years[yearKey]; fy != nil {
					src.SourceURL = fy.Source
				}
				if path, ok := flagging.YearFilePath(configPath, yearKey); ok {
					src.ExternalFile = path
				}
				data.FlagSources = append(data.FlagSources, src)
			}
		}

		data.TodayFlag = flagDayOn(allFlags, now)
		if next := nextFlagDayAfter(allFlags, now); next != nil {
			if t, perr := time.ParseInLocation("2006-01-02", next.Date, time.Local); perr == nil {
				data.NextFlagWeeksAhead = int(rotation.MondayOf(t).Sub(rotation.MondayOf(now)).Hours() / 24 / 7)
				next.Date = t.Format(dateFormat)
			}
			data.NextFlag = next
		}

		s.renderIndex(w, data)
	})
}

// handleScheduleMore renders additional upcoming-schedule week rows as a
// bare HTML fragment (a sequence of <tr> elements), for the info view's
// "Show more" button to append to the existing table via fetch(), without a
// full page reload. "from" (required) is the YYYY-MM-DD Monday to start
// from; "weeks" (required) is how many calendar weeks to render -- the
// client always passes the page's own Schedule.ShowMoreWeeks step size, so
// the next request's "from" can be computed client-side as from+weeks*7
// days without a server round-trip.
func (s *Server) handleScheduleMore(w http.ResponseWriter, r *http.Request) {
	flagging, ffErr := s.store.EffectiveFlaggingDays()
	if ffErr != nil {
		log.Printf("reading flag days: %v", ffErr)
	}

	from, ferr := time.ParseInLocation("2006-01-02", r.URL.Query().Get("from"), time.Local)
	if ferr != nil {
		http.Error(w, "invalid from date", http.StatusBadRequest)
		return
	}
	weeks, werr := strconv.Atoi(r.URL.Query().Get("weeks"))
	if werr != nil || weeks <= 0 || weeks > 520 {
		http.Error(w, "invalid weeks", http.StatusBadRequest)
		return
	}

	var errMsg string
	s.store.View(func(cfg *config.Config) {
		tr := i18n.New(cfg.Languages, cfg.DefaultLanguage)
		lang := s.resolveLanguage(r, tr)
		base := s.newBaseData(r, cfg, tr, lang)
		dateFormat := tr.T(lang, "date_format")
		if dateFormat == "date_format" {
			dateFormat = "2006-01-02"
		}
		weekNumberFormat := tr.T(lang, "week_number_format")
		if weekNumberFormat == "week_number_format" {
			weekNumberFormat = "%d"
		}

		now := time.Now()
		allFlags := collectFlagDays(flagging.Years)
		allSpecial := collectSpecialDates(cfg.SpecialDates)

		sched, err := rotation.New(cfg.Duty)
		if err != nil {
			errMsg = err.Error()
			return
		}
		season, serr := rotation.NewSeason(cfg.Schedule)
		if serr != nil {
			season = nil
		}
		todayMonday := rotation.MondayOf(now)
		to := from.AddDate(0, 0, weeks*7-1)

		data := ScheduleMoreData{
			baseData:           base,
			YearEndMonthsAhead: monthsUntilYearEnd(now),
			TodayFlag:          flagDayOn(allFlags, now),
			WeekRows:           buildWeekRows(sched, season, allFlags, allSpecial, from, to, todayMonday, now, tr, lang, dateFormat, weekNumberFormat),
		}
		s.renderScheduleMore(w, data)
	})

	if errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
	}
}

// handleCalendar produces an iCalendar (.ics) download for the selected
// apartment group's duty periods over the requested number of months ahead,
// including flag-day reminders that fall within those periods. If a
// "special" query parameter (an exact YYYY-MM-DD date) is given instead, it
// produces a single-event .ics for that one admin-configured special date.
func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	if specialDate := r.URL.Query().Get("special"); specialDate != "" {
		s.handleSpecialDateCalendar(w, r, specialDate)
		return
	}

	groupID := r.URL.Query().Get("group")
	months := 12
	if m, err := strconv.Atoi(r.URL.Query().Get("months")); err == nil && m > 0 && m <= 36 {
		months = m
	}

	flagging, ffErr := s.store.EffectiveFlaggingDays()
	if ffErr != nil {
		log.Printf("reading flag days: %v", ffErr)
	}

	var icsBytes []byte
	var errMsg string
	s.store.View(func(cfg *config.Config) {
		sched, err := rotation.New(cfg.Duty)
		if err != nil {
			errMsg = err.Error()
			return
		}
		group, ok := sched.GroupByID(groupID)
		if !ok {
			errMsg = "unknown apartment group"
			return
		}
		tr := i18n.New(cfg.Languages, cfg.DefaultLanguage)
		lang := s.resolveLanguage(r, tr)
		titlePrefix := tr.T(lang, "calendar_event_title_prefix")
		from := time.Now()
		to := from.AddDate(0, months, 0)
		periods := sched.PeriodsForGroupInRange(groupID, from, to)
		allFlags := collectFlagDays(flagging.Years)

		var events []ical.Event
		for _, p := range periods {
			events = append(events, ical.Event{
				UID:     ical.EventUID("duty", group.ID, p.Start.Format("20060102")),
				Summary: titlePrefix + fmt.Sprintf("%s: %s", group.Label, strings.Join(group.Apartments, ", ")),
				Start:   p.Start,
				End:     p.End,
			})
			for _, fd := range flagDaysInRange(allFlags, p.Start, p.End) {
				kind := "optional"
				if fd.Mandatory {
					kind = "mandatory"
				}
				events = append(events, ical.Event{
					UID:         ical.EventUID("flag", fd.Date.Format("20060102"), kind, slugify(fd.Name)),
					Summary:     "🏴 " + fd.Name,
					Description: fmt.Sprintf("%s (%s)", fd.Name, kind),
					Start:       fd.Date,
					End:         fd.Date,
				})
			}
		}
		icsBytes = ical.Calendar("-//calendar//EN", "Calendar: "+group.Label, events)
	})

	if errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.ics"`, groupID))
	w.Write(icsBytes)
}

// handleSpecialDateCalendar produces a single-event .ics download for one
// admin-configured special date, matched by its exact YYYY-MM-DD date. The
// event summary prefers Title, falling back to Summary/Description; its
// description combines whichever of Summary/Description are non-empty.
func (s *Server) handleSpecialDateCalendar(w http.ResponseWriter, r *http.Request, date string) {
	var icsBytes []byte
	var errMsg string
	s.store.View(func(cfg *config.Config) {
		t, err := time.ParseInLocation("2006-01-02", date, time.Local)
		if err != nil {
			errMsg = "invalid date"
			return
		}
		var found *config.SpecialDate
		for i := range cfg.SpecialDates {
			if cfg.SpecialDates[i].Date == date {
				found = &cfg.SpecialDates[i]
				break
			}
		}
		if found == nil {
			errMsg = "unknown special date"
			return
		}
		summary := found.Title
		if summary == "" {
			summary = found.Summary
		}
		if summary == "" {
			summary = found.Description
		}
		var descParts []string
		for _, p := range []string{found.Summary, found.Description} {
			if p != "" {
				descParts = append(descParts, p)
			}
		}
		event := ical.Event{
			UID:         ical.EventUID("special", date, slugify(summary)),
			Summary:     summary,
			Description: strings.Join(descParts, "\n\n"),
			Start:       t,
			End:         t,
		}
		icsBytes = ical.Calendar("-//calendar//EN", "Calendar: "+summary, []ical.Event{event})
	})

	if errMsg != "" {
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="special-%s.ics"`, date))
	w.Write(icsBytes)
}

// monthsUntilYearEnd returns the "months ahead" value (for /calendar.ics)
// that covers from now through the end of the current year. If less than a
// month of the current year remains, it covers through the end of next
// year instead, so the per-row "add to calendar" links on the schedule
// table never produce an almost-empty calendar near year-end.
func monthsUntilYearEnd(now time.Time) int {
	loc := now.Location()
	target := time.Date(now.Year(), time.December, 31, 23, 59, 59, 0, loc)
	if target.Sub(now) < 30*24*time.Hour {
		target = time.Date(now.Year()+1, time.December, 31, 23, 59, 59, 0, loc)
	}
	months := 1
	for months < 36 && now.AddDate(0, months, 0).Before(target) {
		months++
	}
	return months
}

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// mergedWeekFlagDays combines official flag days and admin-configured
// special dates falling within [from, to] into one chronologically sorted
// list of WeekFlagDayView entries (for the schedule table's flag-day-list
// column), plus the de-duplicated set of marker icons to show in that
// week's flag-marker column: the default 🇫🇮 if any official flag day is
// present, followed by one entry per distinct special-date icon.
func mergedWeekFlagDays(allFlags []flagDayParsed, allSpecial []specialDateParsed, from, to time.Time, dateFormat string) ([]WeekFlagDayView, []MarkerIconView) {
	type entry struct {
		date time.Time
		view WeekFlagDayView
	}
	var entries []entry
	for _, fd := range flagDaysInRange(allFlags, from, to) {
		entries = append(entries, entry{date: fd.Date, view: WeekFlagDayView{Name: fd.Name, Date: fd.Date.Format(dateFormat), Mandatory: fd.Mandatory}})
	}
	for _, sd := range specialDatesInRange(allSpecial, from, to) {
		entries = append(entries, entry{date: sd.Date, view: WeekFlagDayView{
			Name: sd.Title, Date: sd.Date.Format(dateFormat),
			Special: true, Icon: sd.Icon, IconIsImage: iconIsImage(sd.Icon),
		}})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].date.Before(entries[j].date) })

	var flagDays []WeekFlagDayView
	var markerIcons []MarkerIconView
	hasOfficial := false
	seenIcons := map[string]bool{}
	for _, e := range entries {
		flagDays = append(flagDays, e.view)
		if !e.view.Special {
			hasOfficial = true
			continue
		}
		if e.view.Icon != "" && !seenIcons[e.view.Icon] {
			seenIcons[e.view.Icon] = true
			markerIcons = append(markerIcons, MarkerIconView{Icon: e.view.Icon, IsImage: e.view.IconIsImage})
		}
	}
	if hasOfficial {
		markerIcons = append([]MarkerIconView{{Icon: "🇫🇮"}}, markerIcons...)
	}
	return flagDays, markerIcons
}

// activeSpecialDates computes, for each configured special date that has
// not yet passed, whether it is currently within its (closer) description
// lead-time window or its (farther) summary lead-time window (relative to
// "on", typically now), returning:
//   - prominent: one ProminentSpecialView per entry within its description
//     window (Title/Summary/Description shown together, larger, alongside
//     the day summary), and
//   - upcoming: one UpcomingSpecialView per entry within its summary
//     window but not yet its description window (shown as a compact row
//     entry under the day summary).
//
// Entries without any text for the applicable window are skipped. If
// several entries are active simultaneously, all are included (none are
// suppressed in favor of others). dateFormat formats each entry's date for
// UpcomingSpecialView.Date.
func activeSpecialDates(all []specialDateParsed, on time.Time, dateFormat string) (prominent []ProminentSpecialView, upcoming []UpcomingSpecialView) {
	today := dateOnly(on)
	for _, sd := range all {
		daysUntil := int(dateOnly(sd.Date).Sub(today).Hours() / 24)
		if daysUntil < 0 {
			continue
		}
		switch {
		case daysUntil <= sd.DescriptionLeadDays:
			if sd.Title != "" || sd.Summary != "" || sd.Description != "" {
				prominent = append(prominent, ProminentSpecialView{
					Title: sd.Title, Summary: sd.Summary, Description: sd.Description,
					Icon: sd.DescriptionIcon, IconIsImage: iconIsImage(sd.DescriptionIcon), IconScale: sd.DescriptionIconScale,
				})
			}
		case daysUntil <= sd.SummaryLeadDays:
			if sd.Summary != "" {
				upcoming = append(upcoming, UpcomingSpecialView{
					Summary: sd.Summary, Date: sd.Date.Format(dateFormat), DateISO: sd.Date.Format("2006-01-02"),
					DaysAhead: daysUntil,
					Icon:      sd.SummaryIcon, IconIsImage: iconIsImage(sd.SummaryIcon), IconScale: sd.SummaryIconScale,
					GoogleCalendarLink: googleCalendarLink(sd.Summary, "", sd.Date, sd.Date, 0),
				})
			}
		}
	}
	return prominent, upcoming
}
