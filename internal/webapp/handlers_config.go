package webapp

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Olarin-Aurinkovuori/calendar/internal/config"
	"github.com/Olarin-Aurinkovuori/calendar/internal/flagdays"
	"github.com/Olarin-Aurinkovuori/calendar/internal/i18n"
	"github.com/Olarin-Aurinkovuori/calendar/internal/icons"
	"github.com/Olarin-Aurinkovuori/calendar/internal/weather"
)

// GroupRow is one editable row in the duty-group table.
type GroupRow struct {
	ID         string
	Label      string
	Apartments string
}

// FlagDayRow is one editable row in a flag-year table.
type FlagDayRow struct {
	Date      string
	Name      string
	Mandatory bool
}

// FlagYearView is one year's editable flag day table.
type FlagYearView struct {
	Year   string
	Source string
	Rows   []FlagDayRow
}

// SpecialDateRow is one editable special-date entry.
type SpecialDateRow struct {
	Date                 string
	Icon                 string
	Title                string
	Summary              string
	SummaryIcon          string
	SummaryIconScale     float64
	SummaryLeadDays      int
	Description          string
	DescriptionIcon      string
	DescriptionIconScale float64
	DescriptionLeadDays  int
}

// WeatherConditionRow is one editable weather threshold-condition entry.
type WeatherConditionRow struct {
	Condition string
	Text      string
	Icon      string
}

// LanguageBlockView is one language's editable translation block.
type LanguageBlockView struct {
	Code        string
	Name        string
	StringsText string
}

// ConfigData is the template data for the settings page.
type ConfigData struct {
	baseData
	PeriodDays        int
	StartDate         string
	GroupRows         []GroupRow
	MonthsBefore      int
	MonthsAfter       int
	SummerStartAnchor string
	SummerEndAnchor   string
	SourceURLTemplate string
	YearFileTemplate  string
	FlagYears         []FlagYearView
	LanguageBlocks    []LanguageBlockView
	Users             []string
	UsersFile         string
	SpecialDateRows   []SpecialDateRow
	SpecialDateCount  int // number of SpecialDateRows backed by an actually-saved entry (excludes trailing blank rows), for per-row delete buttons

	ForecastURLTemplate  string
	Place                string
	LookaheadHours       int
	RefreshIntervalHours int
	WeatherConditionRows []WeatherConditionRow
}

const blankExtraGroupRows = 3
const blankExtraFlagRows = 5
const blankExtraSpecialDateRows = 2
const blankExtraWeatherConditionRows = 2

func buildConfigData(r *http.Request, s *Server, cfg *config.Config, users []config.User, flagYears map[string]*config.FlagYear, flash string) ConfigData {
	tr := i18n.New(cfg.Languages, cfg.DefaultLanguage)
	lang := s.resolveLanguage(r, tr)
	base := s.newBaseData(r, cfg, tr, lang)
	base.Flash = flash

	data := ConfigData{
		baseData:          base,
		PeriodDays:        cfg.Duty.PeriodDays,
		StartDate:         cfg.Duty.StartDate,
		MonthsBefore:      cfg.Schedule.MonthsBefore,
		MonthsAfter:       cfg.Schedule.MonthsAfter,
		SummerStartAnchor: cfg.Schedule.SummerStartAnchor,
		SummerEndAnchor:   cfg.Schedule.SummerEndAnchor,
		SourceURLTemplate: cfg.FlaggingDays.SourceURLTemplate,
		YearFileTemplate:  cfg.FlaggingDays.YearFileTemplate,
	}
	for _, g := range cfg.Duty.Groups {
		data.GroupRows = append(data.GroupRows, GroupRow{ID: g.ID, Label: g.Label, Apartments: strings.Join(g.Apartments, ", ")})
	}
	for i := 0; i < blankExtraGroupRows; i++ {
		data.GroupRows = append(data.GroupRows, GroupRow{})
	}

	years := (config.FlaggingDaysConfig{Years: flagYears}).SortedYears()
	for _, y := range years {
		fy := flagYears[y]
		view := FlagYearView{Year: y}
		if fy != nil {
			view.Source = fy.Source
			for _, d := range fy.Days {
				view.Rows = append(view.Rows, FlagDayRow{Date: d.Date, Name: d.Name, Mandatory: d.Mandatory})
			}
		}
		for i := 0; i < blankExtraFlagRows; i++ {
			view.Rows = append(view.Rows, FlagDayRow{})
		}
		data.FlagYears = append(data.FlagYears, view)
	}

	langCodes := make([]string, 0, len(cfg.Languages))
	for code := range cfg.Languages {
		langCodes = append(langCodes, code)
	}
	sort.Strings(langCodes)
	for _, code := range langCodes {
		l := cfg.Languages[code]
		var sb strings.Builder
		keys := make([]string, 0, len(l.Strings))
		for k := range l.Strings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&sb, "%s=%s\n", k, l.Strings[k])
		}
		data.LanguageBlocks = append(data.LanguageBlocks, LanguageBlockView{Code: code, Name: l.Name, StringsText: sb.String()})
	}

	for _, u := range users {
		data.Users = append(data.Users, u.Username)
	}
	sort.Strings(data.Users)
	data.UsersFile = cfg.Auth.UsersFile

	for _, sd := range cfg.SpecialDates {
		data.SpecialDateRows = append(data.SpecialDateRows, SpecialDateRow{
			Date:                 sd.Date,
			Icon:                 sd.Icon,
			Title:                sd.Title,
			Summary:              sd.Summary,
			SummaryIcon:          sd.SummaryIcon,
			SummaryIconScale:     sd.SummaryIconScale,
			SummaryLeadDays:      sd.SummaryLeadDays,
			Description:          sd.Description,
			DescriptionIcon:      sd.DescriptionIcon,
			DescriptionIconScale: sd.DescriptionIconScale,
			DescriptionLeadDays:  sd.DescriptionLeadDays,
		})
	}
	data.SpecialDateCount = len(cfg.SpecialDates)
	for i := 0; i < blankExtraSpecialDateRows; i++ {
		data.SpecialDateRows = append(data.SpecialDateRows, SpecialDateRow{SummaryIconScale: 1, DescriptionIconScale: 1})
	}

	weather := cfg.Weather
	if weather.ForecastURLTemplate == "" {
		// Weather hasn't been enabled (or this config.yaml predates the
		// feature): pre-fill the form with the suggested defaults from
		// the embedded default config so the admin sees a ready-to-use
		// URL instead of a blank field, without silently writing
		// anything to config.yaml -- it's only saved if they submit
		// this form, leaving untouched configs genuinely untouched.
		weather = config.Default().Weather
	}
	data.ForecastURLTemplate = weather.ForecastURLTemplate
	data.Place = weather.Place
	data.LookaheadHours = weather.LookaheadHours
	data.RefreshIntervalHours = weather.RefreshIntervalHours
	for _, c := range cfg.Weather.Conditions {
		data.WeatherConditionRows = append(data.WeatherConditionRows, WeatherConditionRow{
			Condition: c.Condition,
			Text:      c.Text,
			Icon:      c.Icon,
		})
	}
	for i := 0; i < blankExtraWeatherConditionRows; i++ {
		data.WeatherConditionRows = append(data.WeatherConditionRows, WeatherConditionRow{})
	}

	return data
}

func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.EffectiveUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	flagYears, err := s.store.EffectiveFlagYears()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var data ConfigData
	s.store.View(func(cfg *config.Config) {
		data = buildConfigData(r, s, cfg, users, flagYears, r.URL.Query().Get("flash"))
	})
	s.renderConfig(w, data)
}

func (s *Server) redirectToConfig(w http.ResponseWriter, r *http.Request, flash string) {
	target := s.basePath + "/config"
	if flash != "" {
		target += "?flash=" + flash
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleConfigDuty replaces the duty roster (period length, start date, and
// the list of apartment-pair groups) from the submitted indexed rows,
// dropping any row whose ID is blank.
func (s *Server) handleConfigDuty(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	periodDays, _ := strconv.Atoi(r.FormValue("period_days"))
	startDate := r.FormValue("start_date")
	rowCount, _ := strconv.Atoi(r.FormValue("row_count"))

	var groups []config.DutyGroup
	for i := 0; i < rowCount; i++ {
		id := strings.TrimSpace(r.FormValue(fmt.Sprintf("group_id_%d", i)))
		if id == "" {
			continue
		}
		label := strings.TrimSpace(r.FormValue(fmt.Sprintf("group_label_%d", i)))
		apartmentsRaw := r.FormValue(fmt.Sprintf("group_apartments_%d", i))
		var apartments []string
		for _, a := range strings.Split(apartmentsRaw, ",") {
			a = strings.TrimSpace(a)
			if a != "" {
				apartments = append(apartments, a)
			}
		}
		groups = append(groups, config.DutyGroup{ID: id, Label: label, Apartments: apartments})
	}

	err := s.store.Update(func(cfg *config.Config) error {
		if periodDays > 0 {
			cfg.Duty.PeriodDays = periodDays
		}
		if startDate != "" {
			cfg.Duty.StartDate = startDate
		}
		cfg.Duty.Groups = groups
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigSchedule updates the upcoming-schedule window (how many
// months back/ahead to show) and the summer/winter season anchor dates.
func (s *Server) handleConfigSchedule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	monthsBefore, _ := strconv.Atoi(r.FormValue("months_before"))
	monthsAfter, _ := strconv.Atoi(r.FormValue("months_after"))
	summerStart := strings.TrimSpace(r.FormValue("summer_start_anchor"))
	summerEnd := strings.TrimSpace(r.FormValue("summer_end_anchor"))

	if _, _, err := parseMonthDay(summerStart); err != nil {
		s.redirectToConfig(w, r, "invalid_season_anchor")
		return
	}
	if _, _, err := parseMonthDay(summerEnd); err != nil {
		s.redirectToConfig(w, r, "invalid_season_anchor")
		return
	}

	err := s.store.Update(func(cfg *config.Config) error {
		if monthsBefore > 0 {
			cfg.Schedule.MonthsBefore = monthsBefore
		}
		if monthsAfter > 0 {
			cfg.Schedule.MonthsAfter = monthsAfter
		}
		cfg.Schedule.SummerStartAnchor = summerStart
		cfg.Schedule.SummerEndAnchor = summerEnd
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// parseMonthDay validates a "MM-DD" anchor string.
func parseMonthDay(s string) (month, day int, err error) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected MM-DD")
	}
	month, err = strconv.Atoi(parts[0])
	if err != nil || month < 1 || month > 12 {
		return 0, 0, fmt.Errorf("invalid month")
	}
	day, err = strconv.Atoi(parts[1])
	if err != nil || day < 1 || day > 31 {
		return 0, 0, fmt.Errorf("invalid day")
	}
	return month, day, nil
}

func (s *Server) handleConfigFlagTemplate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	tmpl := r.FormValue("source_url_template")
	err := s.store.Update(func(cfg *config.Config) error {
		cfg.FlaggingDays.SourceURLTemplate = tmpl
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigFlagYearFileTemplate updates the path template used to store
// each calendar year's flag day data in its own external YAML file instead
// of inline in the main configuration file. An empty value switches back
// to inline storage. Switching preserves whichever data is currently
// active for every known year.
func (s *Server) handleConfigFlagYearFileTemplate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	tmpl := strings.TrimSpace(r.FormValue("year_file_template"))
	if err := s.store.SetYearFileTemplate(tmpl); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigFlagAddYear fetches and stores flag days for a new year,
// always fetching from the (possibly just-updated) source URL template.
func (s *Server) handleConfigFlagAddYear(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	year, err := strconv.Atoi(r.FormValue("year"))
	if err != nil {
		http.Error(w, "invalid year", http.StatusBadRequest)
		return
	}
	yearKey := strconv.Itoa(year)
	var template string
	s.store.View(func(cfg *config.Config) { template = cfg.FlaggingDays.SourceURLTemplate })
	sourceURL := strings.ReplaceAll(template, "{year}", yearKey)
	// Fetch outside the config lock so a slow/unreachable remote source does
	// not stall other requests (e.g. the public info view).
	days, fetchErr := flagdays.FetchYear(r.Context(), sourceURL, year)
	var updErr error
	if fetchErr == nil {
		updErr = s.store.UpdateFlagYear(yearKey, func(_ *config.FlagYear) *config.FlagYear {
			return &config.FlagYear{Source: sourceURL, Days: days}
		})
	}
	if updErr != nil {
		http.Error(w, updErr.Error(), http.StatusInternalServerError)
		return
	}
	if fetchErr != nil {
		s.redirectToConfig(w, r, "fetch_failed")
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigFlagYear replaces a given year's resolved flag day entries
// with the manually edited table, and its source/origin description.
func (s *Server) handleConfigFlagYear(w http.ResponseWriter, r *http.Request) {
	year := r.PathValue("year")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	source := strings.TrimSpace(r.FormValue("source"))
	rowCount, _ := strconv.Atoi(r.FormValue("row_count"))
	var days []config.FlagDay
	for i := 0; i < rowCount; i++ {
		date := strings.TrimSpace(r.FormValue(fmt.Sprintf("date_%d", i)))
		name := strings.TrimSpace(r.FormValue(fmt.Sprintf("name_%d", i)))
		if date == "" || name == "" {
			continue
		}
		mandatory := r.FormValue(fmt.Sprintf("mandatory_%d", i)) != ""
		days = append(days, config.FlagDay{Date: date, Name: name, Mandatory: mandatory})
	}
	err := s.store.UpdateFlagYear(year, func(existing *config.FlagYear) *config.FlagYear {
		fetchedAt := ""
		if existing != nil {
			fetchedAt = existing.FetchedAt
		}
		return &config.FlagYear{Source: source, FetchedAt: fetchedAt, Days: days}
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigFlagRefetch re-downloads a year's flag days from its
// configured source, overwriting whatever is currently stored.
func (s *Server) handleConfigFlagRefetch(w http.ResponseWriter, r *http.Request) {
	year := r.PathValue("year")
	yearNum, err := strconv.Atoi(year)
	if err != nil {
		http.Error(w, "invalid year", http.StatusBadRequest)
		return
	}
	flagYears, err := s.store.EffectiveFlagYears()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var sourceURL string
	if existing := flagYears[year]; existing != nil && existing.Source != "" {
		sourceURL = existing.Source
	} else {
		s.store.View(func(cfg *config.Config) {
			sourceURL = strings.ReplaceAll(cfg.FlaggingDays.SourceURLTemplate, "{year}", year)
		})
	}
	days, fetchErr := flagdays.FetchYear(r.Context(), sourceURL, yearNum)
	var updErr error
	if fetchErr == nil {
		updErr = s.store.UpdateFlagYear(year, func(_ *config.FlagYear) *config.FlagYear {
			return &config.FlagYear{Source: sourceURL, Days: days}
		})
	}
	if updErr != nil {
		http.Error(w, updErr.Error(), http.StatusInternalServerError)
		return
	}
	if fetchErr != nil {
		s.redirectToConfig(w, r, "fetch_failed")
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigLanguageAdd creates a new language, optionally seeded with a
// copy of the default language's strings so the admin has something to
// translate from.
func (s *Server) handleConfigLanguageAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	name := strings.TrimSpace(r.FormValue("name"))
	if code == "" {
		s.redirectToConfig(w, r, "invalid_language")
		return
	}
	err := s.store.Update(func(cfg *config.Config) error {
		if cfg.Languages == nil {
			cfg.Languages = map[string]config.Language{}
		}
		if _, exists := cfg.Languages[code]; exists {
			return nil
		}
		strs := map[string]string{}
		if def, ok := cfg.Languages[cfg.DefaultLanguage]; ok {
			for k, v := range def.Strings {
				strs[k] = v
			}
		}
		if name == "" {
			name = code
		}
		cfg.Languages[code] = config.Language{Name: name, Strings: strs}
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigLanguageUpdate replaces a language's display name and
// translated strings from the "key=value" lines in the submitted textarea.
func (s *Server) handleConfigLanguageUpdate(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	raw := r.FormValue("strings")
	strs := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if key != "" {
			strs[key] = val
		}
	}
	err := s.store.Update(func(cfg *config.Config) error {
		if cfg.Languages == nil {
			cfg.Languages = map[string]config.Language{}
		}
		if name == "" {
			if existing, ok := cfg.Languages[code]; ok {
				name = existing.Name
			} else {
				name = code
			}
		}
		cfg.Languages[code] = config.Language{Name: name, Strings: strs}
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

func (s *Server) handleConfigUserAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		s.redirectToConfig(w, r, "invalid_user")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = s.store.UpdateUsers(func(users []config.User) []config.User {
		for i, u := range users {
			if u.Username == username {
				users[i].PasswordHash = string(hash)
				return users
			}
		}
		return append(users, config.User{Username: username, PasswordHash: string(hash)})
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigUserUpdate sets a new password for, or deletes, an existing
// administrator account. The last remaining account cannot be deleted, to
// avoid locking everyone out of the configuration UI.
func (s *Server) handleConfigUserUpdate(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	action := r.FormValue("action")
	password := r.FormValue("password")

	var hash string
	if action != "delete" && password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		hash = string(h)
	}

	var flash string
	err := s.store.UpdateUsers(func(users []config.User) []config.User {
		if action == "delete" {
			if len(users) <= 1 {
				flash = "cannot_delete_last_user"
				return users
			}
			var kept []config.User
			for _, u := range users {
				if u.Username != username {
					kept = append(kept, u)
				}
			}
			flash = "saved"
			return kept
		}
		if hash == "" {
			flash = "saved"
			return users
		}
		for i, u := range users {
			if u.Username == username {
				users[i].PasswordHash = hash
				break
			}
		}
		flash = "saved"
		return users
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, flash)
}

// handleConfigUsersFile updates the path to an external YAML file used to
// store administrator accounts instead of the inline auth.users list. An
// empty value switches back to inline storage. Switching preserves
// whichever set of accounts is currently active so nobody gets locked out.
func (s *Server) handleConfigUsersFile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	usersFile := strings.TrimSpace(r.FormValue("users_file"))
	if err := s.store.SetUsersFile(usersFile); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigSpecialDates replaces the admin-configured special dates
// (custom flagged dates shown in the schedule table and today's-summary)
// from the submitted indexed rows, dropping any row whose date is blank.
// Icon fields that are remote URLs are downloaded and cached locally
// (internal/icons.Fetch) before being persisted; if any icon fails to
// download, the whole save is rejected (nothing is persisted) and the
// admin is redirected back with an "icon_fetch_failed" flash, consistent
// with this handler's other all-or-nothing validation.
func (s *Server) handleConfigSpecialDates(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	rowCount, _ := strconv.Atoi(r.FormValue("row_count"))

	var entries []config.SpecialDate
	for i := 0; i < rowCount; i++ {
		date := strings.TrimSpace(r.FormValue(fmt.Sprintf("special_date_%d", i)))
		if date == "" {
			continue
		}
		icon, err := resolveSpecialIcon(r.Context(), strings.TrimSpace(r.FormValue(fmt.Sprintf("special_icon_%d", i))))
		if err != nil {
			s.redirectToConfig(w, r, "icon_fetch_failed")
			return
		}
		summaryIcon, err := resolveSpecialIcon(r.Context(), strings.TrimSpace(r.FormValue(fmt.Sprintf("special_summary_icon_%d", i))))
		if err != nil {
			s.redirectToConfig(w, r, "icon_fetch_failed")
			return
		}
		descriptionIcon, err := resolveSpecialIcon(r.Context(), strings.TrimSpace(r.FormValue(fmt.Sprintf("special_description_icon_%d", i))))
		if err != nil {
			s.redirectToConfig(w, r, "icon_fetch_failed")
			return
		}
		summaryLeadDays, _ := strconv.Atoi(r.FormValue(fmt.Sprintf("special_summary_lead_days_%d", i)))
		descriptionLeadDays, _ := strconv.Atoi(r.FormValue(fmt.Sprintf("special_description_lead_days_%d", i)))
		summaryIconScale, _ := strconv.ParseFloat(r.FormValue(fmt.Sprintf("special_summary_icon_scale_%d", i)), 64)
		descriptionIconScale, _ := strconv.ParseFloat(r.FormValue(fmt.Sprintf("special_description_icon_scale_%d", i)), 64)
		entries = append(entries, config.SpecialDate{
			Date:                 date,
			Icon:                 icon,
			Title:                strings.TrimSpace(r.FormValue(fmt.Sprintf("special_title_%d", i))),
			Summary:              strings.TrimSpace(r.FormValue(fmt.Sprintf("special_summary_%d", i))),
			SummaryIcon:          summaryIcon,
			SummaryIconScale:     summaryIconScale,
			SummaryLeadDays:      summaryLeadDays,
			Description:          strings.TrimSpace(r.FormValue(fmt.Sprintf("special_description_%d", i))),
			DescriptionIcon:      descriptionIcon,
			DescriptionIconScale: descriptionIconScale,
			DescriptionLeadDays:  descriptionLeadDays,
		})
	}

	err := s.store.Update(func(cfg *config.Config) error {
		cfg.SpecialDates = entries
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// resolveSpecialIcon returns icon unchanged unless it is a remote URL, in
// which case it is downloaded and cached locally via internal/icons.Fetch,
// and the resulting local path is returned instead.
func resolveSpecialIcon(ctx context.Context, icon string) (string, error) {
	if icon == "" || !icons.IsRemote(icon) {
		return icon, nil
	}
	return icons.Fetch(ctx, icon)
}

// handleConfigSpecialDateDelete removes one special date, identified by its
// index into cfg.SpecialDates (matching the order of the existing-entry
// rows rendered by buildConfigData), and redirects back to the config page.
func (s *Server) handleConfigSpecialDateDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	index, err := strconv.Atoi(r.FormValue("index"))
	if err != nil {
		s.redirectToConfig(w, r, "invalid_special_date")
		return
	}
	err = s.store.Update(func(cfg *config.Config) error {
		if index < 0 || index >= len(cfg.SpecialDates) {
			return nil
		}
		cfg.SpecialDates = append(cfg.SpecialDates[:index], cfg.SpecialDates[index+1:]...)
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigSpecialDatesCleanup removes every special date whose date is
// more than a week in the past, and redirects back to the config page.
func (s *Server) handleConfigSpecialDatesCleanup(w http.ResponseWriter, r *http.Request) {
	cutoff := dateOnly(time.Now()).AddDate(0, 0, -7)
	err := s.store.Update(func(cfg *config.Config) error {
		var kept []config.SpecialDate
		for _, sd := range cfg.SpecialDates {
			t, perr := time.ParseInLocation("2006-01-02", sd.Date, time.Local)
			if perr != nil || !t.Before(cutoff) {
				kept = append(kept, sd)
			}
		}
		cfg.SpecialDates = kept
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}

// handleConfigWeather replaces the forecast settings (source URL template,
// place, lookahead/refresh-interval hours) and the full list of threshold
// conditions from the submitted indexed rows, dropping any row whose
// condition expression is blank. Each remaining expression is validated via
// weather.Evaluate (with a lookup that treats every $VARIABLE as present,
// so only the expression's syntax -- not live forecast data -- is
// checked) before saving, so a typo can't silently disable the feature.
func (s *Server) handleConfigWeather(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	forecastURLTemplate := strings.TrimSpace(r.FormValue("forecast_url_template"))
	place := strings.TrimSpace(r.FormValue("place"))
	lookaheadHours, _ := strconv.Atoi(r.FormValue("lookahead_hours"))
	refreshIntervalHours, _ := strconv.Atoi(r.FormValue("refresh_interval_hours"))
	rowCount, _ := strconv.Atoi(r.FormValue("condition_row_count"))

	var conditions []config.WeatherCondition
	for i := 0; i < rowCount; i++ {
		expr := strings.TrimSpace(r.FormValue(fmt.Sprintf("weather_condition_%d", i)))
		if expr == "" {
			continue
		}
		if _, err := weather.Evaluate(expr, func(string) (float64, bool) { return 0, true }); err != nil {
			s.redirectToConfig(w, r, "invalid_weather_condition")
			return
		}
		icon, err := resolveSpecialIcon(r.Context(), strings.TrimSpace(r.FormValue(fmt.Sprintf("weather_icon_%d", i))))
		if err != nil {
			s.redirectToConfig(w, r, "icon_fetch_failed")
			return
		}
		conditions = append(conditions, config.WeatherCondition{
			Condition: expr,
			Text:      strings.TrimSpace(r.FormValue(fmt.Sprintf("weather_text_%d", i))),
			Icon:      icon,
		})
	}

	err := s.store.Update(func(cfg *config.Config) error {
		cfg.Weather.ForecastURLTemplate = forecastURLTemplate
		cfg.Weather.Place = place
		cfg.Weather.LookaheadHours = lookaheadHours
		cfg.Weather.RefreshIntervalHours = refreshIntervalHours
		cfg.Weather.Conditions = conditions
		return nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.redirectToConfig(w, r, "saved")
}
