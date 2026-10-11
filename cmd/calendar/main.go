// Command calendar runs the caretaker duty schedule web application:
// a built-in HTTP server that shows the current apartment-pair duty and
// nearby flag days, offers an iCal export, and (for configured
// administrators) a settings UI -- all driven by a single YAML config file.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Olarin-Aurinkovuori/calendar/internal/config"
	"github.com/Olarin-Aurinkovuori/calendar/internal/flagdays"
	"github.com/Olarin-Aurinkovuori/calendar/internal/webapp"
)

func main() {
	defaultPath := os.Getenv("CONFIG_PATH")
	if defaultPath == "" {
		defaultPath = "config.yaml"
	}
	configPath := flag.String("config", defaultPath, "path to the YAML configuration file (env CONFIG_PATH)")
	listenAddr := flag.String("listen", "", "override the server listen address (host:port, e.g. :8080 or 0.0.0.0:9090); overrides server.listen_addr in the config file")
	basePath := flag.String("base-path", "", "override the URL path prefix the application is served under (e.g. /calendar), for reverse-proxy deployments that share a host with other applications; overrides server.base_path in the config file. Leave empty (the default) to serve from the domain root")
	dumpDefaultConfig := flag.Bool("dump-default-config", false, "print the built-in default configuration as YAML to stdout and exit, without reading or writing any configuration file")
	dumpFlagDays := flag.Bool("dump-flag-days", false, "print the known flagging days from the configuration file as YAML to stdout and exit")
	dumpYear := flag.String("year", "", "with -dump-flag-days, restrict the dump to this single calendar year (e.g. 2026), fetching it first if not already known; if omitted, all known years are dumped")
	dumpEmbeddedAssets := flag.String("dump-embedded-assets", "", "write every built-in template/static asset file, exactly as compiled into the binary, under this directory (as \"templates/...\" and \"static/...\") and exit; the directory must not already exist. Useful as a starting point for writing override templates/static assets")
	dumpEffectiveAssets := flag.String("dump-effective-assets", "", "write every template/static asset file as currently served -- on-disk overrides under the working directory where present, otherwise the built-in default -- under this directory and exit; the directory must not already exist")
	trustProxyUserParam := flag.String("trust-proxy-user-param", "", "name of a URL query parameter (e.g. \"user\") that a trusted reverse proxy sets to the already-authenticated username, to offload /config authentication to the proxy; leave empty (the default) to require HTTP Basic Auth only. Only enable this if the proxy is the sole path to this server and controls/strips this parameter itself")
	flag.Parse()

	if *dumpEmbeddedAssets != "" {
		if err := webapp.DumpEmbeddedAssets(*dumpEmbeddedAssets); err != nil {
			log.Fatalf("dumping embedded assets: %v", err)
		}
		return
	}
	if *dumpEffectiveAssets != "" {
		if err := webapp.DumpEffectiveAssets(*dumpEffectiveAssets); err != nil {
			log.Fatalf("dumping effective assets: %v", err)
		}
		return
	}

	if *dumpDefaultConfig {
		if err := dumpYAML(os.Stdout, config.Default()); err != nil {
			log.Fatalf("dumping default config: %v", err)
		}
		return
	}

	store, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("loading config %s: %v", *configPath, err)
	}
	log.Printf("using configuration file %s", store.Path())

	if *dumpFlagDays {
		if *dumpYear != "" {
			year, perr := strconv.Atoi(*dumpYear)
			if perr != nil {
				log.Fatalf("invalid -year %q: must be a 4-digit calendar year", *dumpYear)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := flagdays.EnsureYear(ctx, store, year)
			cancel()
			if err != nil {
				log.Fatalf("fetching flag days for %d: %v", year, err)
			}
		} else {
			ensureFlagDays(store)
		}

		flagging, ffErr := store.EffectiveFlaggingDays()
		if ffErr != nil {
			log.Fatalf("reading flag days: %v", ffErr)
		}
		if *dumpYear != "" {
			filtered := config.FlaggingDaysConfig{
				SourceURLTemplate: flagging.SourceURLTemplate,
				YearFileTemplate:  flagging.YearFileTemplate,
				Years:             map[string]*config.FlagYear{},
			}
			if fy, ok := flagging.Years[*dumpYear]; ok {
				filtered.Years[*dumpYear] = fy
			}
			flagging = filtered
		}
		if err := dumpYAML(os.Stdout, map[string]config.FlaggingDaysConfig{"flagging_days": flagging}); err != nil {
			log.Fatalf("dumping flag days: %v", err)
		}
		return
	}

	ensureFlagDays(store)

	var effectiveBasePath string
	store.View(func(cfg *config.Config) { effectiveBasePath = cfg.Server.BasePath })
	if *basePath != "" {
		effectiveBasePath = config.NormalizeBasePath(*basePath)
	}

	srv := webapp.New(store, *trustProxyUserParam, effectiveBasePath)

	var addr string
	store.View(func(cfg *config.Config) { addr = cfg.Server.ListenAddr })
	if *listenAddr != "" {
		addr = *listenAddr
	}
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}

// dumpYAML marshals v as YAML to w.
func dumpYAML(w io.Writer, v any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(4)
	defer enc.Close()
	return enc.Encode(v)
}

// ensureFlagDays makes sure the current and next calendar year both have
// flag days recorded, fetching them from the configured online source if
// they are missing, and persisting the result to the config file.
func ensureFlagDays(store *config.Store) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	thisYear := time.Now().Year()
	for _, year := range []int{thisYear, thisYear + 1} {
		changed, err := flagdays.EnsureYear(ctx, store, year)
		switch {
		case err != nil:
			log.Printf("could not fetch flag days for %d: %v", year, err)
		case changed:
			log.Printf("fetched flag days for %d", year)
		default:
			// already had data for this year; nothing to do.
		}
	}
}
