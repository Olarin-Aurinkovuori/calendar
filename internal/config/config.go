// Package config defines the YAML-backed configuration for the calendar
// application and provides thread-safe, atomic load/save access to it.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/Olarin-Aurinkovuori/calendar/internal/version"
)

// Config is the root of the YAML configuration file.
type Config struct {
	Server          ServerConfig        `yaml:"server"`
	DefaultLanguage string              `yaml:"default_language"`
	Languages       map[string]Language `yaml:"languages"`
	Duty            DutyConfig          `yaml:"duty"`
	Schedule        ScheduleConfig      `yaml:"schedule"`
	FlaggingDays    FlaggingDaysConfig  `yaml:"flagging_days"`
	SpecialDates    []SpecialDate       `yaml:"special_dates"`
	Weather         WeatherConfig       `yaml:"weather"`
	Auth            AuthConfig          `yaml:"auth"`
	// WrittenBy records which build of this application last wrote this
	// file (see save()). It is stamped automatically on every save and
	// is not meant to be hand-edited; it exists so a future config
	// migration can tell which version/commit a given file was last
	// written by.
	WrittenBy WrittenByInfo `yaml:"written_by"`
}

// WrittenByInfo identifies the build that wrote a configuration file.
type WrittenByInfo struct {
	Version string `yaml:"version"`
	Commit  string `yaml:"commit"`
}

// ServerConfig controls how the built-in HTTP server listens.
type ServerConfig struct {
	ListenAddr string `yaml:"listen_addr"`
	// BasePath lets the whole application be served under a URL path
	// prefix (e.g. "/calendar") instead of the domain root, for
	// reverse-proxy deployments that share a host with other
	// applications. Empty (the default) serves from the root. See
	// NormalizeBasePath for the accepted/normalized form.
	BasePath string `yaml:"base_path"`
}

// NormalizeBasePath cleans a configured/CLI-supplied base path into the
// form the server expects: empty (root) or a leading slash with no
// trailing slash (e.g. "/calendar"). "/" and "" both normalize to "".
func NormalizeBasePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}

// Language holds the display name and translated UI strings for one locale.
type Language struct {
	Name    string            `yaml:"name"`
	Strings map[string]string `yaml:"strings"`
}

// DutyGroup is one apartment pair (or group) that takes turns on duty.
type DutyGroup struct {
	ID         string   `yaml:"id"`
	Label      string   `yaml:"label"`
	Apartments []string `yaml:"apartments"`
}

// DutyConfig describes the rotation: an ordered list of groups, the date the
// first group's period begins, and how many days each period lasts.
type DutyConfig struct {
	PeriodDays int         `yaml:"period_days"`
	StartDate  string      `yaml:"start_date"` // YYYY-MM-DD
	Groups     []DutyGroup `yaml:"groups"`
}

// ScheduleConfig controls the "upcoming schedule" week list shown on the
// info view: how far back and ahead it spans, and the summer/winter season
// rule used to label each week.
type ScheduleConfig struct {
	// MonthsBefore/MonthsAfter define the default window shown, relative to
	// today: from MonthsBefore months ago through MonthsAfter months ahead.
	MonthsBefore int `yaml:"months_before"`
	MonthsAfter  int `yaml:"months_after"`
	// SummerStartAnchor is a "MM-DD" date; summer begins on the first Monday
	// on or after this date each year (default "05-01": first full week of
	// May).
	SummerStartAnchor string `yaml:"summer_start_anchor"`
	// SummerEndAnchor is a "MM-DD" date; summer ends at the end (Sunday) of
	// the week containing this date each year (default "09-30": the week
	// including the last day of September).
	SummerEndAnchor string `yaml:"summer_end_anchor"`
	// ShowMoreWeeks is how many additional weeks the info view's "show
	// more" button loads per click (default 12).
	ShowMoreWeeks int `yaml:"show_more_weeks"`
}

// SpecialDate is an admin-configured one-off date (e.g. a local event) that
// is flagged in the schedule table -- sharing the flag-marker and flag-day
// list columns with official/recommended flag days -- and, independently,
// in the today's-summary card as it approaches and arrives.
type SpecialDate struct {
	Date string `yaml:"date"` // YYYY-MM-DD, an exact one-off date
	// Icon is shown in the schedule table's flag-marker and flag-day-list
	// columns for this date: either plain text/emoji, or a local image path
	// (a remote URL submitted via the config UI is downloaded once and
	// replaced with the resulting local path).
	Icon string `yaml:"icon"`
	// Title is shown next to Icon in the schedule table's flag-day-list
	// column, and again (together with Summary and Description) in
	// today's-summary once within DescriptionLeadDays.
	Title string `yaml:"title"`
	// Summary is shown, on its own, in today's-summary once Date is within
	// SummaryLeadDays days away but not yet within DescriptionLeadDays.
	Summary string `yaml:"summary"`
	// SummaryIcon is shown alongside Summary, both inline (small, next to
	// other upcoming special dates on the same row) and, larger, in a
	// hover tooltip over that inline entry.
	SummaryIcon string `yaml:"summary_icon"`
	// SummaryIconScale scales SummaryIcon when it is an image (1.0 = normal
	// size; e.g. 2.0 renders it twice as large). Ignored for text/emoji
	// icons. Defaults to 1.0.
	SummaryIconScale float64 `yaml:"summary_icon_scale"`
	SummaryLeadDays  int     `yaml:"summary_lead_days"`
	// Description is shown in today's-summary, together with Title and
	// Summary, once Date is within DescriptionLeadDays days away (including
	// on Date itself), taking precedence over the Summary-only view.
	Description string `yaml:"description"`
	// DescriptionIcon is shown, scaled by DescriptionIconScale, alongside
	// Title/Summary/Description in that close-up view.
	DescriptionIcon string `yaml:"description_icon"`
	// DescriptionIconScale scales DescriptionIcon when it is an image, same
	// semantics as SummaryIconScale.
	DescriptionIconScale float64 `yaml:"description_icon_scale"`
	DescriptionLeadDays  int     `yaml:"description_lead_days"`
}

// FlagDay is a single Finnish flag-flying day.
type FlagDay struct {
	Date      string `yaml:"date"` // YYYY-MM-DD
	Name      string `yaml:"name"`
	Mandatory bool   `yaml:"mandatory"`
}

// FlagYear holds the resolved flag days for one calendar year plus
// bookkeeping about where they came from.
type FlagYear struct {
	Source    string    `yaml:"source"`
	FetchedAt string    `yaml:"fetched_at,omitempty"`
	Days      []FlagDay `yaml:"days"`
}

// FlaggingDaysConfig configures the per-year flag day source and cache.
type FlaggingDaysConfig struct {
	SourceURLTemplate string `yaml:"source_url_template"`
	// YearFileTemplate, if set, is a path template (containing the literal
	// placeholder "{year}") for an external YAML file that holds one
	// calendar year's flag day data, used instead of inlining it in Years
	// below. Relative paths are resolved against the directory containing
	// the main configuration file. Example: "flagdays-{year}.yaml".
	YearFileTemplate string               `yaml:"year_file_template,omitempty"`
	Years            map[string]*FlagYear `yaml:"years"`
}

// yearPlaceholder is the substring replaced by a 4-digit calendar year in
// FlaggingDaysConfig.YearFileTemplate (and SourceURLTemplate).
const yearPlaceholder = "{year}"

// yearPattern matches a bare 4-digit calendar year extracted from a
// resolved YearFileTemplate match.
var yearPattern = regexp.MustCompile(`^\d{4}$`)

// splitYearTemplate splits a template containing "{year}" into the literal
// text before and after the placeholder.
func splitYearTemplate(tmpl string) (prefix, suffix string, ok bool) {
	idx := strings.Index(tmpl, yearPlaceholder)
	if idx < 0 {
		return "", "", false
	}
	return tmpl[:idx], tmpl[idx+len(yearPlaceholder):], true
}

// WeatherConfig configures an optional short-term forecast feed (from the
// Finnish Meteorological Institute's open data WFS API, or anything
// returning the same "multipointcoverage" response shape) and a list of
// simple threshold conditions evaluated against it; every condition that
// evaluates true has its Text shown on the today's-summary view.
//
// Conditions reference "$MIN_"/"$MAX_"/"$MEAN_"/"$AVG_" ("MEAN"/"AVG" are
// synonyms) prefixed variables -- "TEMPERATURE" (°C), "RAINFALL" (mm,
// FMI's hourly "Precipitation1h"), and "SNOW" (cm, FMI's cumulative
// "SnowAccumulation" -- so MAX_SNOW is the total accumulated over the
// window) -- combined with <, <=, >, >=, &&, ||, and parentheses, e.g.
// "($MIN_TEMPERATURE < 0 && $MAX_TEMPERATURE < 5) || $MAX_SNOW > 5cm".
// Leave ForecastURLTemplate empty to disable the feature entirely.
type WeatherConfig struct {
	// ForecastURLTemplate is the forecast API request URL, with the
	// literal placeholders "{place}", "{start}", and "{end}" substituted
	// at fetch time ({start}/{end} are UTC RFC3339 timestamps spanning
	// LookaheadHours from now). Example (FMI, Harmonie surface model):
	// "https://opendata.fmi.fi/wfs?service=WFS&version=2.0.0&request=getFeature&storedquery_id=fmi::forecast::harmonie::surface::point::multipointcoverage&place={place}&parameters=Temperature,Precipitation1h,SnowAccumulation&starttime={start}&endtime={end}&timestep=60"
	ForecastURLTemplate string `yaml:"forecast_url_template,omitempty"`
	// Place is substituted for the "{place}" placeholder (e.g. an FMI
	// place name like "espoo").
	Place string `yaml:"place,omitempty"`
	// LookaheadHours is how far ahead of now the forecast window extends
	// (default 24).
	LookaheadHours int `yaml:"lookahead_hours,omitempty"`
	// RefreshIntervalHours is how long a fetched forecast is reused
	// before refetching, so the upstream API isn't hit on every page
	// load (default 4).
	RefreshIntervalHours int                `yaml:"refresh_interval_hours,omitempty"`
	Conditions           []WeatherCondition `yaml:"conditions,omitempty"`
}

// WeatherCondition is one threshold expression (see WeatherConfig) paired
// with the hint text shown on the today's-summary view when it is true.
// Every condition that evaluates true has its Text shown (not just the
// first match), in Conditions' order.
type WeatherCondition struct {
	Condition string `yaml:"condition"`
	Text      string `yaml:"text"`
	// Icon, if set, additionally evaluates Condition separately for each
	// day across the upcoming-schedule view's displayed weeks (as far
	// ahead as the forecast source actually provides data for); any week
	// with at least one matching day shows Icon in that week's
	// flag-marker column, with a tooltip listing the matching day(s) and
	// the value of every variable Condition referenced that day.
	Icon string `yaml:"icon,omitempty"`
}

// User is a configuration-UI administrator account.
type User struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"`
}

// AuthConfig lists the users allowed to edit the configuration via the UI.
// Users can be listed inline (Users) or, to keep credentials out of the
// main configuration file, in a separate YAML file (UsersFile) with the
// same "users: [...]" shape. When UsersFile is set it takes precedence:
// it is the authoritative source for authentication, and edits made via
// the config UI are written there instead of to Users.
type AuthConfig struct {
	Users     []User `yaml:"users,omitempty"`
	UsersFile string `yaml:"users_file,omitempty"`
}

// usersFileDoc is the schema of an external auth users file.
type usersFileDoc struct {
	Users []User `yaml:"users"`
}

// YearFilePath returns the resolved external file path where year's flag
// day data would be stored, given the main configuration file's path, if
// YearFileTemplate is configured. The second return value is false when
// YearFileTemplate is empty (inline storage is used instead).
func (f FlaggingDaysConfig) YearFilePath(configPath, year string) (string, bool) {
	if f.YearFileTemplate == "" {
		return "", false
	}
	return flagYearFilePath(configPath, f.YearFileTemplate, year), true
}

// SortedYears returns the configured flag-day years in ascending order.
func (f FlaggingDaysConfig) SortedYears() []string {
	years := make([]string, 0, len(f.Years))
	for y := range f.Years {
		years = append(years, y)
	}
	sort.Strings(years)
	return years
}

// Store wraps a Config with a mutex and the on-disk path it was loaded from,
// providing atomic, race-free read/modify/save access.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  *Config
}

// Load reads the configuration from path, creating a default file there if
// it does not yet exist.
func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			s := &Store{path: path, cfg: cfg}
			if err := s.save(); err != nil {
				return nil, fmt.Errorf("writing default config: %w", err)
			}
			return s, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	normalize(cfg)
	s := &Store{path: path, cfg: cfg}
	if backfillTranslations(cfg) {
		if err := s.save(); err != nil {
			return nil, fmt.Errorf("writing backfilled config: %w", err)
		}
	}
	return s, nil
}

// backfillTranslations fills in any translation keys that are missing from
// an already-configured language by copying them from the embedded default
// configuration's matching language. This lets existing config.yaml files
// (written before new UI strings were introduced) automatically pick up the
// new keys instead of showing the raw key as a fallback. It never adds a
// language that isn't already configured, and never overwrites an existing
// (possibly customized) translation. Returns whether anything changed.
func backfillTranslations(cfg *Config) bool {
	changed := false
	def := Default()
	for code, defLang := range def.Languages {
		lang, ok := cfg.Languages[code]
		if !ok {
			continue
		}
		if lang.Strings == nil {
			lang.Strings = map[string]string{}
		}
		for k, v := range defLang.Strings {
			if _, exists := lang.Strings[k]; !exists {
				lang.Strings[k] = v
				changed = true
			}
		}
		cfg.Languages[code] = lang
	}
	return changed
}

func normalize(cfg *Config) {
	cfg.Server.BasePath = NormalizeBasePath(cfg.Server.BasePath)
	if cfg.Languages == nil {
		cfg.Languages = map[string]Language{}
	}
	if cfg.FlaggingDays.Years == nil {
		cfg.FlaggingDays.Years = map[string]*FlagYear{}
	}
	if cfg.Duty.PeriodDays <= 0 {
		cfg.Duty.PeriodDays = 7
	}
	if cfg.Schedule.MonthsBefore <= 0 {
		cfg.Schedule.MonthsBefore = 1
	}
	if cfg.Schedule.MonthsAfter <= 0 {
		cfg.Schedule.MonthsAfter = 3
	}
	if cfg.Schedule.SummerStartAnchor == "" {
		cfg.Schedule.SummerStartAnchor = "05-01"
	}
	if cfg.Schedule.SummerEndAnchor == "" {
		cfg.Schedule.SummerEndAnchor = "09-30"
	}
	if cfg.Schedule.ShowMoreWeeks <= 0 {
		cfg.Schedule.ShowMoreWeeks = 12
	}
	if cfg.Weather.ForecastURLTemplate != "" {
		if cfg.Weather.LookaheadHours <= 0 {
			cfg.Weather.LookaheadHours = 24
		}
		if cfg.Weather.RefreshIntervalHours <= 0 {
			cfg.Weather.RefreshIntervalHours = 4
		}
	}
	for i := range cfg.SpecialDates {
		if cfg.SpecialDates[i].SummaryLeadDays < 0 {
			cfg.SpecialDates[i].SummaryLeadDays = 0
		}
		if cfg.SpecialDates[i].DescriptionLeadDays < 0 {
			cfg.SpecialDates[i].DescriptionLeadDays = 0
		}
		if cfg.SpecialDates[i].SummaryIconScale <= 0 {
			cfg.SpecialDates[i].SummaryIconScale = 1.0
		}
		if cfg.SpecialDates[i].DescriptionIconScale <= 0 {
			cfg.SpecialDates[i].DescriptionIconScale = 1.0
		}
	}
}

// Path returns the file path this store persists to.
func (s *Store) Path() string { return s.path }

// View runs fn with a read lock held, giving safe read-only access to the
// current configuration.
func (s *Store) View(fn func(cfg *Config)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.cfg)
}

// Update runs fn with a write lock held and persists the configuration
// afterwards unless fn returns an error.
func (s *Store) Update(fn func(cfg *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(s.cfg); err != nil {
		return err
	}
	return s.save()
}

// save marshals the config to YAML and writes it atomically (temp file +
// rename) so readers never observe a partially written file. It first
// stamps WrittenBy with this running binary's version/commit (see
// internal/version), so the file always reflects whichever build last
// wrote it.
func (s *Store) save() error {
	s.cfg.WrittenBy = WrittenByInfo{Version: version.Version, Commit: version.Commit}
	data, err := yaml.Marshal(s.cfg)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	return atomicWriteFile(s.path, data)
}

// atomicWriteFile writes data to path via a temp file in the same directory
// followed by a rename, so readers never observe a partially written file.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// resolvePath resolves a possibly-relative path against the directory
// containing the main configuration file.
func (s *Store) resolvePath(p string) string {
	return resolveRelative(s.path, p)
}

// resolveRelative resolves a possibly-relative path p against the
// directory containing configPath. All external files referenced from the
// main configuration file (auth users file, per-year flag day files, etc.)
// use this so relative paths are always anchored to the config file's own
// location rather than the process's current working directory.
func resolveRelative(configPath, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(filepath.Dir(configPath), p)
}

// EffectiveUsers returns the current administrator accounts: the inline
// Auth.Users list, or -- if Auth.UsersFile is configured -- the users
// loaded fresh from that external YAML file (so edits made directly to the
// file, outside the config UI, are always picked up).
func (s *Store) EffectiveUsers() ([]User, error) {
	s.mu.RLock()
	usersFile := s.cfg.Auth.UsersFile
	inline := append([]User(nil), s.cfg.Auth.Users...)
	var fullPath string
	if usersFile != "" {
		fullPath = s.resolvePath(usersFile)
	}
	s.mu.RUnlock()

	if usersFile == "" {
		return inline, nil
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading auth users file %q: %w", fullPath, err)
	}
	var doc usersFileDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing auth users file %q: %w", fullPath, err)
	}
	return doc.Users, nil
}

// UpdateUsers replaces the administrator account list via fn, writing the
// result to the external users file if Auth.UsersFile is configured, or to
// the inline Auth.Users field (persisting config.yaml) otherwise.
func (s *Store) UpdateUsers(fn func(users []User) []User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	usersFile := s.cfg.Auth.UsersFile
	if usersFile == "" {
		s.cfg.Auth.Users = fn(s.cfg.Auth.Users)
		return s.save()
	}

	fullPath := s.resolvePath(usersFile)
	var doc usersFileDoc
	if data, err := os.ReadFile(fullPath); err == nil {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parsing auth users file %q: %w", fullPath, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading auth users file %q: %w", fullPath, err)
	}
	doc.Users = fn(doc.Users)
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshalling auth users file: %w", err)
	}
	return atomicWriteFile(fullPath, out)
}

// SetUsersFile changes where administrator accounts are stored. Switching
// to a not-yet-existing external file seeds it with the current inline
// users; switching back to inline storage pulls in whatever is currently in
// the external file. Either way, this ensures accounts that are already
// valid keep working instead of being silently lost (which would lock
// everyone out of the configuration UI).
func (s *Store) SetUsersFile(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.cfg.Auth.UsersFile
	if path == old {
		return nil
	}
	if path != "" {
		fullPath := s.resolvePath(path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			out, merr := yaml.Marshal(usersFileDoc{Users: s.cfg.Auth.Users})
			if merr != nil {
				return fmt.Errorf("marshalling auth users file: %w", merr)
			}
			if werr := atomicWriteFile(fullPath, out); werr != nil {
				return werr
			}
		} else if err != nil {
			return fmt.Errorf("checking auth users file %q: %w", fullPath, err)
		}
	} else if old != "" {
		oldFullPath := s.resolvePath(old)
		data, err := os.ReadFile(oldFullPath)
		if err == nil {
			var doc usersFileDoc
			if err := yaml.Unmarshal(data, &doc); err != nil {
				return fmt.Errorf("parsing auth users file %q: %w", oldFullPath, err)
			}
			s.cfg.Auth.Users = doc.Users
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("reading auth users file %q: %w", oldFullPath, err)
		}
	}
	s.cfg.Auth.UsersFile = path
	return s.save()
}

// flagYearFilePath resolves the external file path for one calendar year
// given a YearFileTemplate, anchored to the directory containing the main
// configuration file.
func flagYearFilePath(configPath, tmpl, year string) string {
	return resolveRelative(configPath, strings.ReplaceAll(tmpl, yearPlaceholder, year))
}

// loadExternalFlagYears discovers and loads every external flag-year file
// matching tmpl (by globbing the literal text surrounding "{year}"),
// anchored to the directory containing configPath.
func loadExternalFlagYears(configPath, tmpl string) (map[string]*FlagYear, error) {
	resolved := resolveRelative(configPath, tmpl)
	prefix, suffix, ok := splitYearTemplate(resolved)
	if !ok {
		return nil, fmt.Errorf("flagging_days.year_file_template %q must contain %q", tmpl, yearPlaceholder)
	}
	matches, err := filepath.Glob(prefix + "*" + suffix)
	if err != nil {
		return nil, fmt.Errorf("listing flag year files: %w", err)
	}
	years := map[string]*FlagYear{}
	for _, m := range matches {
		yearStr := strings.TrimSuffix(strings.TrimPrefix(m, prefix), suffix)
		if !yearPattern.MatchString(yearStr) {
			continue
		}
		data, err := os.ReadFile(m)
		if err != nil {
			return nil, fmt.Errorf("reading flag year file %q: %w", m, err)
		}
		var fy FlagYear
		if err := yaml.Unmarshal(data, &fy); err != nil {
			return nil, fmt.Errorf("parsing flag year file %q: %w", m, err)
		}
		years[yearStr] = &fy
	}
	return years, nil
}

// EffectiveFlagYears returns the current year -> FlagYear map: the inline
// FlaggingDays.Years map, or -- if FlaggingDays.YearFileTemplate is
// configured -- the years discovered and loaded fresh from the matching
// external files (so edits made directly to those files are always picked
// up).
func (s *Store) EffectiveFlagYears() (map[string]*FlagYear, error) {
	s.mu.RLock()
	tmpl := s.cfg.FlaggingDays.YearFileTemplate
	inline := make(map[string]*FlagYear, len(s.cfg.FlaggingDays.Years))
	for y, fy := range s.cfg.FlaggingDays.Years {
		inline[y] = fy
	}
	path := s.path
	s.mu.RUnlock()

	if tmpl == "" {
		return inline, nil
	}
	return loadExternalFlagYears(path, tmpl)
}

// EffectiveFlaggingDays returns the current flagging-days configuration
// with Years resolved via EffectiveFlagYears (external files if
// configured, inline otherwise).
func (s *Store) EffectiveFlaggingDays() (FlaggingDaysConfig, error) {
	s.mu.RLock()
	fd := s.cfg.FlaggingDays
	s.mu.RUnlock()
	years, err := s.EffectiveFlagYears()
	if err != nil {
		return fd, err
	}
	fd.Years = years
	return fd, nil
}

// UpdateFlagYear replaces one calendar year's flag day data via fn (which
// receives the existing entry, or nil if there is none), writing the
// result to that year's external file if FlaggingDays.YearFileTemplate is
// configured, or to the inline FlaggingDays.Years map (persisting
// config.yaml) otherwise.
func (s *Store) UpdateFlagYear(year string, fn func(existing *FlagYear) *FlagYear) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tmpl := s.cfg.FlaggingDays.YearFileTemplate
	if tmpl == "" {
		if s.cfg.FlaggingDays.Years == nil {
			s.cfg.FlaggingDays.Years = map[string]*FlagYear{}
		}
		s.cfg.FlaggingDays.Years[year] = fn(s.cfg.FlaggingDays.Years[year])
		return s.save()
	}

	fullPath := flagYearFilePath(s.path, tmpl, year)
	var existing *FlagYear
	if data, err := os.ReadFile(fullPath); err == nil {
		var fy FlagYear
		if err := yaml.Unmarshal(data, &fy); err != nil {
			return fmt.Errorf("parsing flag year file %q: %w", fullPath, err)
		}
		existing = &fy
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading flag year file %q: %w", fullPath, err)
	}
	updated := fn(existing)
	out, err := yaml.Marshal(updated)
	if err != nil {
		return fmt.Errorf("marshalling flag year file: %w", err)
	}
	return atomicWriteFile(fullPath, out)
}

// SetYearFileTemplate changes where per-year flag day data is stored.
// Switching to external-file storage copies any inline years that don't
// already have a corresponding external file, so existing data is not
// orphaned. Switching back to inline storage pulls in whatever the
// matching external files currently contain. Either way, data that was
// already valid keeps being found afterwards.
func (s *Store) SetYearFileTemplate(tmpl string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.cfg.FlaggingDays.YearFileTemplate
	if tmpl == old {
		return nil
	}
	if tmpl != "" {
		if _, _, ok := splitYearTemplate(tmpl); !ok {
			return fmt.Errorf("flagging_days.year_file_template %q must contain %q", tmpl, yearPlaceholder)
		}
		for year, fy := range s.cfg.FlaggingDays.Years {
			if fy == nil {
				continue
			}
			fullPath := flagYearFilePath(s.path, tmpl, year)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				out, merr := yaml.Marshal(fy)
				if merr != nil {
					return fmt.Errorf("marshalling flag year file: %w", merr)
				}
				if werr := atomicWriteFile(fullPath, out); werr != nil {
					return werr
				}
			} else if err != nil {
				return fmt.Errorf("checking flag year file %q: %w", fullPath, err)
			}
		}
	} else if old != "" {
		years, err := loadExternalFlagYears(s.path, old)
		if err != nil {
			return err
		}
		if s.cfg.FlaggingDays.Years == nil {
			s.cfg.FlaggingDays.Years = map[string]*FlagYear{}
		}
		for year, fy := range years {
			s.cfg.FlaggingDays.Years[year] = fy
		}
	}
	s.cfg.FlaggingDays.YearFileTemplate = tmpl
	return s.save()
}
