package webapp

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/Olarin-Aurinkovuori/calendar/internal/config"
	"github.com/Olarin-Aurinkovuori/calendar/internal/i18n"
)

const langCookieName = "calendar_lang"

// LangOption is one entry in the language switcher.
type LangOption struct {
	Code string
	Name string
}

// baseData holds the fields every rendered page needs (navigation, language
// switcher, translation function).
type baseData struct {
	Lang      string
	Path      string
	Languages []LangOption
	T         func(key string, args ...any) string
	Flash     string
}

// resolveLanguage determines the UI language for a request: an explicit
// cookie set via /set-language takes precedence, otherwise the browser's
// Accept-Language header is negotiated against the configured languages,
// falling back to the configured default language.
func (s *Server) resolveLanguage(r *http.Request, tr *i18n.Translator) string {
	if c, err := r.Cookie(langCookieName); err == nil && c.Value != "" {
		if tr.IsSupported(c.Value) {
			return c.Value
		}
	}
	return tr.NegotiateLanguage(r.Header.Get("Accept-Language"))
}

func (s *Server) newBaseData(r *http.Request, cfg *config.Config, tr *i18n.Translator, lang string) baseData {
	opts := make([]LangOption, 0, len(cfg.Languages))
	for _, code := range tr.SortedCodes() {
		opts = append(opts, LangOption{Code: code, Name: cfg.Languages[code].Name})
	}
	return baseData{
		Lang:      lang,
		Path:      r.URL.Path,
		Languages: opts,
		T: func(key string, args ...any) string {
			return tr.T(lang, key, args...)
		},
	}
}

// handleSetLanguage stores the chosen language in a cookie and redirects
// back to the page the user came from (restricted to local paths to avoid
// open-redirect abuse).
func (s *Server) handleSetLanguage(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	redirect := r.URL.Query().Get("redirect")
	if redirect == "" || !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") {
		redirect = s.basePath + "/"
	}
	if u, err := url.Parse(redirect); err != nil || u.IsAbs() {
		redirect = s.basePath + "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     langCookieName,
		Value:    lang,
		Path:     "/",
		MaxAge:   int((365 * 24 * 60 * 60)),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}
