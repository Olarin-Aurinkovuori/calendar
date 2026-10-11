# Calendar

A standalone Go web application that replaces a Google Sheet used to track
a housing company's rotating handyman/caretaker duty ("talonmiesvuorot")
among apartment pairs. It serves a small built-in HTTP server with:

- A public info view showing today's date, the apartment pair currently on
  duty, and whether today (or an upcoming day) is a Finnish flag day
  ("liputuspäivä"), mandatory or optional.
- A multilingual UI. All UI strings live in the YAML configuration file;
  the language is chosen from the browser's `Accept-Language` header, with
  a configurable default as fallback, and can be overridden via a language
  switcher (stored in a cookie).
- An HTTP-Basic-Auth-protected configuration UI for editing the duty
  roster, flag days (per year, including re-fetching from an online
  source), UI languages/translations, and admin users — all persisted back
  to the YAML file.
- An iCal (`.ics`) export for a selected apartment pair's duty periods,
  including flag-day reminders that fall within those periods, downloadable
  via "add to calendar" links in the schedule table or by querying
  `/calendar.ics?group=<id>&months=<n>` directly.
- Automatic fetching of Finnish flag days for the current/next year from
  an online source (intermin.fi) if they are missing from the
  configuration, with results written back to the config file.

## Building and running

Run these from the repository root (this file lives under
`cmd/calendar/`, alongside `main.go` and the `Dockerfile`):

```sh
go build -o calendar ./cmd/calendar
./calendar -config config.yaml
```

If `config.yaml` does not exist, it is created with sensible defaults on
first run (admin user `admin` / password `change-me`, three example duty
groups, Finnish/English UI strings, and Finnish flag days for 2026/2027).
**Change the default admin password before exposing the server.**

The config path can also be set via the `CONFIG_PATH` environment
variable; the `-config` flag takes precedence. The server listens on the
address configured under `server.listen_addr` (default `:8080`).

### Serving under a URL path prefix

By default the application serves from the domain root. To share a host
with other applications behind a reverse proxy, set `server.base_path`
in the config file (or pass `-base-path`) to a path like `/calendar`:
every route, generated link, and static asset reference is then prefixed
with it automatically. The reverse proxy just needs to forward the
prefixed path through **unchanged** (no prefix-stripping) to the
application -- see the example nginx configuration below.

```nginx
location /calendar/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

(`proxy_pass` has no path of its own here, so nginx forwards the full
incoming request URI, including the `/calendar` prefix, unchanged --
matching what `-base-path /calendar` expects the application to see.)

### Command-line flags

- `-config <path>`: path to the YAML configuration file (default from
  `CONFIG_PATH` env var, or `config.yaml`).
- `-listen <addr>`: override the listen address (e.g. `:8080` or
  `0.0.0.0:9090`), taking precedence over `server.listen_addr`.
- `-base-path <path>`: override the URL path prefix the application is
  served under (e.g. `/calendar`), taking precedence over
  `server.base_path`. Leave empty (the default) to serve from the domain
  root.
- `-dump-default-config`: print the built-in default configuration as YAML
  to stdout and exit, without touching any configuration file.
- `-dump-flag-days`: print the known flagging days (from the config file,
  fetching the current/next year first if missing) as YAML to stdout and
  exit.
- `-year <YYYY>`: with `-dump-flag-days`, restrict the dump to a single
  calendar year, fetching it first if not already known; omit to dump all
  known years.
- `-dump-embedded-assets <dir>`: write every built-in template/static asset
  file, exactly as compiled into the binary, under `<dir>` (as
  `templates/...` and `static/...`) and exit; `<dir>` must not already
  exist. Handy as a starting point for writing override templates/static
  assets.
- `-dump-effective-assets <dir>`: write every template/static asset file as
  currently served -- an on-disk override under the working directory if
  present, otherwise the built-in default -- under `<dir>` and exit;
  `<dir>` must not already exist.
- `-trust-proxy-user-param <name>`: name of a URL query parameter (e.g.
  `user`) that a trusted reverse proxy sets to the already-authenticated
  username, to offload `/config` authentication to the proxy (it rewrites
  requests to append `user=$authenticated_user`). The supplied username is
  trusted without a password check as long as it matches a configured
  admin user. Falls back to HTTP Basic Auth when the parameter is absent.
  Leave unset (the default) to require Basic Auth only; only enable this
  if the proxy is the sole path to the server and fully controls/strips
  this parameter itself.

### Offloading authentication to a reverse proxy

Start the server with `-trust-proxy-user-param=user`, keep it reachable
only from the proxy (e.g. bind it to `127.0.0.1` or a container-internal
network), and have the proxy do its own authentication in front of
`/config`, then set `user=<name>` on the request so calendar trusts
it instead of checking Basic Auth itself. calendar reads only the
*first* `user` value in the query string, so as long as the trusted value
is placed first (as in both examples below), a client-supplied `user`
parameter tacked on afterwards is ignored either way.

**Caddy** (`basic_auth` + `rewrite`, using `{http.auth.user.id}`):

```caddyfile
duty.example.com {
    # Generate hashes with: caddy hash-password
    basic_auth /config* {
        admin $2a$14$Zkx19XLiW6VYouLHR5NmfOFU0z2GTNmpkT/5qqR7hx4IjWJPDhjvG
    }

    # Replace the query string with the authenticated username.
    rewrite /config* ?user={http.auth.user.id}

    reverse_proxy 127.0.0.1:8080
}
```

**nginx** (`auth_basic` + `rewrite`, using `$remote_user`):

```nginx
server {
    listen 80;
    server_name duty.example.com;

    location /config {
        auth_basic "calendar";
        auth_basic_user_file /etc/nginx/htpasswd/calendar;

        # Put the authenticated username first; nginx appends any existing
        # query string (including a client-supplied "user") after it, and
        # calendar only looks at the first "user" value.
        rewrite ^/config(.*)$ /config$1?user=$remote_user break;

        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
    }

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
    }
}
```

Either way, `admin` must also exist as a configured user in `config.yaml`
(the proxy only vouches for the *name*; calendar still checks that
the name is a known user, it just skips the password check).

## Running with Docker

The `Dockerfile` (in this directory) builds a fully static binary and
ships it on `gcr.io/distroless/static-debian12:nonroot` (no shell, no
package manager, ~12 MB image). Everything the application reads or
writes at runtime -- `config.yaml`, any external auth-users/flag-day-year
files it references, and optional `templates/`/`static/` override
directories -- lives entirely under `/data`, a single volume mount point
kept separate from the read-only application layer.

Build with the repository root as context (the Dockerfile needs `go.mod`,
`go.sum`, and `internal/`), run from the repository root:

```sh
docker build -f cmd/calendar/Dockerfile -t calendar .

mkdir -p data && chown 65532:65532 data   # match the image's nonroot user
docker run -d --name calendar \
  -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  calendar
```

On first run, `config.yaml` (with sensible defaults) is created directly
inside the mounted `./data` directory, so it -- and any files it
references via relative paths (external `users_file`, per-year flag-day
files, `year_file_template` entries) -- persists across container
restarts/upgrades and is editable from the host. To override a UI
template or static asset without rebuilding the image, drop a same-named
file under `./data/templates/` or `./data/static/` (e.g.
`./data/static/style.css`); the public info view's References section
reports whether each asset is currently served built-in or from that
override. Running the container with `-dump-embedded-assets /data/seed`
(or `-dump-effective-assets`) as its command seeds `./data/seed/templates/`
and `./data/seed/static/` with a starting point to copy and edit files
from into `./data/templates/`/`./data/static/`.

A `docker-compose.yml` at the repository root has the same layout;
`docker compose up -d` builds and starts it the same way. Since the base image has no
shell or HTTP client, container-level `HEALTHCHECK`/`docker exec` are not
available -- use an orchestrator-level HTTP probe against `/` instead.

### Published images

A GitHub Actions workflow (`.github/workflows/docker-publish.yml`) builds
this Dockerfile (for `linux/amd64` and `linux/arm64`) and pushes it to
GitHub Container Registry:

- every push to `main` is published as `ghcr.io/<owner>/<repo>:unstable`.
- every tag matching `v$MAJOR.$MINOR.$PATCH` (e.g. `v1.2.3`) is published
  as `ghcr.io/<owner>/<repo>:v1.2.3`.
- every push to a branch named `test/image/$subject` (e.g.
  `test/image/foo`) is published as `ghcr.io/<owner>/<repo>:v0.0.0-foo`,
  for ad hoc test builds without cutting a real release tag.

```sh
docker pull ghcr.io/<owner>/<repo>:unstable
```

## Configuration

See `internal/config/default-config.yaml` for the full default
configuration structure, including the duty roster format (ordered list
of groups, a rotation start date, and a period length in days) and the
flag-day layout (one entry per year with a source URL template and a list
of date/name/mandatory entries).

An optional `weather` section can show admin-configured hint text on the
today's-summary view based on a short-term temperature/rainfall/snow
forecast (e.g. from the FMI open data API) and simple threshold
conditions (e.g. `"$MIN_TEMPERATURE <= 2"`); see
[`docs/calendar/README.md`](../../docs/calendar/README.md#weather-forecast-notices)
for the full variable/operator reference. Leave it unset to disable the
feature.

For the reasoning behind these design choices, see
[`docs/calendar/README.md`](../../docs/calendar/README.md).

## Development

```sh
go build ./...
go vet ./...
gofmt -l .
```

No external services are required to run or build the application beyond
network access for the optional flag-day auto-fetch feature.
