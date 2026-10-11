// Package i18n negotiates the UI language from the browser's Accept-Language
// header against the languages defined in the YAML configuration, and looks
// up translated strings with a fallback chain down to the configured
// default language and finally the raw key.
package i18n

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Olarin-Aurinkovuori/calendar/internal/config"
)

// Translator resolves UI strings for configured languages.
type Translator struct {
	languages   map[string]config.Language
	defaultLang string
}

// New builds a Translator from the configured languages and default
// language code. If defaultLang is not itself configured, one of the
// configured languages (chosen deterministically) is used as the fallback.
func New(languages map[string]config.Language, defaultLang string) *Translator {
	t := &Translator{languages: languages, defaultLang: defaultLang}
	if _, ok := languages[defaultLang]; !ok {
		for _, code := range t.SortedCodes() {
			t.defaultLang = code
			break
		}
	}
	return t
}

// SortedCodes returns the configured language codes in a stable order.
func (t *Translator) SortedCodes() []string {
	codes := make([]string, 0, len(t.languages))
	for c := range t.languages {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes
}

// DefaultLang returns the resolved default/fallback language code.
func (t *Translator) DefaultLang() string { return t.defaultLang }

// Languages returns the configured languages keyed by code.
func (t *Translator) Languages() map[string]config.Language { return t.languages }

// IsSupported reports whether code is a configured language.
func (t *Translator) IsSupported(code string) bool {
	_, ok := t.languages[code]
	return ok
}

type weightedTag struct {
	tag    string
	weight float64
}

// NegotiateLanguage parses an Accept-Language header value and returns the
// best matching configured language code, falling back to the default
// language if nothing matches (including when the header is empty/invalid).
func (t *Translator) NegotiateLanguage(acceptLanguage string) string {
	if acceptLanguage == "" {
		return t.defaultLang
	}
	var tags []weightedTag
	for _, part := range strings.Split(acceptLanguage, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		weight := 1.0
		tag := part
		if idx := strings.Index(part, ";"); idx >= 0 {
			tag = strings.TrimSpace(part[:idx])
			params := part[idx+1:]
			for _, p := range strings.Split(params, ";") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "q=") {
					if w, err := strconv.ParseFloat(strings.TrimPrefix(p, "q="), 64); err == nil {
						weight = w
					}
				}
			}
		}
		tags = append(tags, weightedTag{tag: tag, weight: weight})
	}
	sort.SliceStable(tags, func(i, j int) bool { return tags[i].weight > tags[j].weight })
	for _, wt := range tags {
		if wt.tag == "*" {
			continue
		}
		primary := strings.ToLower(strings.SplitN(wt.tag, "-", 2)[0])
		if t.IsSupported(primary) {
			return primary
		}
	}
	return t.defaultLang
}

// T returns the translated string for key in the given language. If the
// language or key is missing, it falls back to the default language, and
// finally to the bare key so missing translations are visible rather than
// crashing the UI. If args are given, the resolved string is treated as a
// fmt format string.
func (t *Translator) T(lang, key string, args ...interface{}) string {
	val := t.lookup(lang, key)
	if val == "" {
		val = key
	}
	if len(args) == 0 {
		return val
	}
	return fmt.Sprintf(val, args...)
}

func (t *Translator) lookup(lang, key string) string {
	if l, ok := t.languages[lang]; ok {
		if v, ok := l.Strings[key]; ok && v != "" {
			return v
		}
	}
	if lang != t.defaultLang {
		if l, ok := t.languages[t.defaultLang]; ok {
			if v, ok := l.Strings[key]; ok && v != "" {
				return v
			}
		}
	}
	return ""
}
