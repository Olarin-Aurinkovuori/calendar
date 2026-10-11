// Package webapp implements the HTTP server: the public info view, the
// iCal export endpoint, and the HTTP-Basic-Auth-protected configuration UI.
package webapp

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Olarin-Aurinkovuori/calendar/internal/config"
)

//go:embed templates/*.html
var embeddedTemplateFS embed.FS

//go:embed static/*
var embeddedStaticFS embed.FS

// overlayFS serves files from the process's current working directory when
// present, falling back to the compiled-in embed.FS otherwise. This lets a
// deployment override any template or static asset (e.g. "templates/index.html"
// or "static/style.css") by placing a same-named file/directory next to
// where the binary runs, without needing to rebuild it.
type overlayFS struct {
	embedded embed.FS
}

func (o overlayFS) Open(name string) (fs.File, error) {
	if f, err := os.Open(name); err == nil {
		return f, nil
	}
	return o.embedded.Open(name)
}

var (
	templateFS fs.FS = overlayFS{embedded: embeddedTemplateFS}
	staticFS   fs.FS = overlayFS{embedded: embeddedStaticFS}
)

// AssetOrigin reports whether one UI asset file (a template or static file)
// was loaded from an external override on disk or from the built-in
// embedded copy, for display as a reference on the info view.
type AssetOrigin struct {
	Name     string // overlay-relative name, e.g. "templates/index.html"
	External bool
	Dir      string // absolute directory the override was read from, only set when External
}

// assetOrigin inspects name (an overlayFS-relative path) the same way
// overlayFS.Open does, reporting whether it currently resolves to an
// external on-disk file.
func assetOrigin(name string) AssetOrigin {
	o := AssetOrigin{Name: name}
	if info, err := os.Stat(name); err == nil && !info.IsDir() {
		o.External = true
		if abs, err := filepath.Abs(filepath.Dir(name)); err == nil {
			o.Dir = abs
		} else {
			o.Dir = filepath.Dir(name)
		}
	}
	return o
}

// publicAssetOrigins reports the origin of every template/static file used
// to render the public info view, in display order.
func publicAssetOrigins() []AssetOrigin {
	return []AssetOrigin{
		assetOrigin("templates/layout.html"),
		assetOrigin("templates/index.html"),
		assetOrigin("static/style.css"),
		assetOrigin("static/img/finland-flag-waving.gif"),
	}
}

// Server holds everything needed to serve HTTP requests.
type Server struct {
	store               *config.Store
	tmplIndex           *template.Template
	tmplConfig          *template.Template
	mux                 *http.ServeMux
	trustProxyUserParam string
	basePath            string
}

// New builds a Server backed by the given configuration store. Each page is
// parsed as its own template set (layout + that page's content) so that
// the "content" block defined by one page's template cannot shadow another
// page's same-named block.
//
// trustProxyUserParam, if non-empty, names a URL query parameter (e.g.
// "user") that a trusted reverse proxy is expected to set to the
// already-authenticated username (e.g. by rewriting requests to append
// user=$authenticated_user). When present on a request to a protected
// /config route, that username is trusted outright -- without a password
// check -- as long as it matches a configured admin user; this lets
// authentication be offloaded entirely to the proxy. Leave empty to require
// HTTP Basic Auth only (the default, safe behaviour).
//
// basePath, if non-empty (see config.NormalizeBasePath for its expected
// form, e.g. "/calendar"), serves the whole application under that URL
// path prefix instead of the domain root: every route, generated link,
// and static asset reference is prefixed with it. This lets the
// application share a host with other services behind a reverse proxy
// that forwards the prefixed path through unchanged (i.e. no
// prefix-stripping on the proxy side).
func New(store *config.Store, trustProxyUserParam string, basePath string) *Server {
	funcs := template.FuncMap{"url": urlFunc(basePath)}
	tmplIndex := template.Must(template.New("").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/index.html"))
	tmplConfig := template.Must(template.New("").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/config.html"))
	s := &Server{store: store, tmplIndex: tmplIndex, tmplConfig: tmplConfig, mux: http.NewServeMux(), trustProxyUserParam: trustProxyUserParam, basePath: basePath}
	s.routes()
	return s
}

// urlFunc returns the "url" template helper bound to basePath: it prefixes
// a root-relative path (starting with "/") with basePath, leaving already-
// absolute URLs (http://, https://, //) and any other value (plain
// relative paths, emoji/text "icon" values) unchanged. This lets the same
// helper wrap both literal asset paths (e.g. {{url "/static/style.css"}})
// and data-driven paths that might be a locally cached file ("/static/...")
// or a remote URL (e.g. a special-date icon fetched from elsewhere).
func urlFunc(basePath string) func(string) string {
	return func(p string) string {
		if p == "" {
			return basePath
		}
		if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "//") {
			return p
		}
		if strings.HasPrefix(p, "/") {
			return basePath + p
		}
		return p
	}
}

// Handler returns the http.Handler to pass to http.ListenAndServe.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	bp := s.basePath

	s.mux.Handle(bp+"/static/", http.StripPrefix(bp, http.FileServer(http.FS(staticFS))))

	s.mux.HandleFunc("GET "+bp+"/{$}", s.handleIndex)
	s.mux.HandleFunc("GET "+bp+"/set-language", s.handleSetLanguage)
	s.mux.HandleFunc("GET "+bp+"/calendar.ics", s.handleCalendar)
	s.mux.HandleFunc("GET "+bp+"/schedule/more", s.handleScheduleMore)

	s.mux.Handle("GET "+bp+"/config", s.requireAuth(http.HandlerFunc(s.handleConfigGet)))
	s.mux.Handle("POST "+bp+"/config/duty", s.requireAuth(http.HandlerFunc(s.handleConfigDuty)))
	s.mux.Handle("POST "+bp+"/config/schedule", s.requireAuth(http.HandlerFunc(s.handleConfigSchedule)))
	s.mux.Handle("POST "+bp+"/config/flagdays/template", s.requireAuth(http.HandlerFunc(s.handleConfigFlagTemplate)))
	s.mux.Handle("POST "+bp+"/config/flagdays/year-file-template", s.requireAuth(http.HandlerFunc(s.handleConfigFlagYearFileTemplate)))
	s.mux.Handle("POST "+bp+"/config/flagdays/add-year", s.requireAuth(http.HandlerFunc(s.handleConfigFlagAddYear)))
	s.mux.Handle("POST "+bp+"/config/flagdays/{year}", s.requireAuth(http.HandlerFunc(s.handleConfigFlagYear)))
	s.mux.Handle("POST "+bp+"/config/flagdays/{year}/refetch", s.requireAuth(http.HandlerFunc(s.handleConfigFlagRefetch)))
	s.mux.Handle("POST "+bp+"/config/languages/add", s.requireAuth(http.HandlerFunc(s.handleConfigLanguageAdd)))
	s.mux.Handle("POST "+bp+"/config/languages/{code}", s.requireAuth(http.HandlerFunc(s.handleConfigLanguageUpdate)))
	s.mux.Handle("POST "+bp+"/config/users/add", s.requireAuth(http.HandlerFunc(s.handleConfigUserAdd)))
	s.mux.Handle("POST "+bp+"/config/users/{username}", s.requireAuth(http.HandlerFunc(s.handleConfigUserUpdate)))
	s.mux.Handle("POST "+bp+"/config/users-file", s.requireAuth(http.HandlerFunc(s.handleConfigUsersFile)))
	s.mux.Handle("POST "+bp+"/config/special-dates", s.requireAuth(http.HandlerFunc(s.handleConfigSpecialDates)))
	s.mux.Handle("POST "+bp+"/config/special-dates/delete", s.requireAuth(http.HandlerFunc(s.handleConfigSpecialDateDelete)))
	s.mux.Handle("POST "+bp+"/config/special-dates/cleanup", s.requireAuth(http.HandlerFunc(s.handleConfigSpecialDatesCleanup)))

	if bp != "" {
		// A request to the bare prefix without a trailing slash (e.g.
		// "/calendar") would otherwise 404, since every pattern above is
		// anchored at "bp/...". Redirect it to the slash form the "/{$}"
		// pattern expects.
		s.mux.HandleFunc("GET "+bp, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, bp+"/", http.StatusMovedPermanently)
		})
	}
}

func (s *Server) renderIndex(w http.ResponseWriter, data any) {
	if err := s.tmplIndex.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) renderConfig(w http.ResponseWriter, data any) {
	if err := s.tmplConfig.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// renderScheduleMore writes the "Show more" button's fragment response: just
// the "week_rows" named template (a bare sequence of <tr> elements, no
// layout), for the client to append to the existing schedule table.
func (s *Server) renderScheduleMore(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmplIndex.ExecuteTemplate(w, "week_rows", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
