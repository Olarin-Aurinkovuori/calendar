# Design notes: handyman-duty

This document records the design decisions behind the `calendar`
("handyman-duty") application: why it is shaped the way it is, not just
what it does. For build/run/CLI instructions see
`cmd/handyman-duty/README.md`.

## Origin and goal

The application replaces a Google Sheet a housing company used to work out
whose turn it was to do caretaker/handyman duty ("talonmiesvuorot"), which
apartment pair is up next, and whether an upcoming day is a Finnish
flag-flying day ("liputuspäivä"). It is a single self-contained Go binary
with a built-in HTTP server and no external database: everything lives in
one YAML configuration file (plus optional external files it can reference
— see below).

## Package layout

- `cmd/handyman-duty` — the CLI entry point and flag handling.
- `internal/config` — the YAML schema, a mutex-guarded `Store` for safe
  concurrent load/save, and the external-file resolution logic described
  below.
- `internal/rotation` — pure date arithmetic: which duty group is on duty
  on a given date, and the summer/winter season classification used by the
  week list.
- `internal/i18n` — `Accept-Language` negotiation against the languages
  configured in YAML, with fallback to a configured default and finally to
  the raw key (so a missing translation is visible, not a crash).
- `internal/ical` — minimal RFC 5545 VCALENDAR rendering for the duty/flag
  day export, with no third-party dependency.
- `internal/flagdays` — fetches and HTML-parses Finnish flag days from an
  online source for a given year, and only touches the configuration when
  a year is actually missing.
- `internal/webapp` — HTTP handlers, templates, and static assets for both
  the public info view and the authenticated config UI.

## Configuration model

Everything is one `Config` struct serialized as YAML:
`server`, `default_language`, `languages`, `duty`, `schedule`,
`flagging_days`, `auth`. The in-memory `config.Store` wraps it behind a
`sync.RWMutex` with `View`/`Update` closures, so handlers never see a
half-written config and concurrent requests don't race.

Two parts of the configuration can optionally live *outside* the main
file, each toggled the same way:

- `auth.users_file`: admin accounts, instead of inline `auth.users`.
- `flagging_days.year_file_template`: a path template containing the
  literal placeholder `{year}` (e.g. `flagdays-{year}.yaml`); when set,
  every year's flag day data is read from/written to its own external file
  instead of the inline `flagging_days.years` map.

Both are **global on/off switches** — setting the template/file path moves
*all* data of that kind out to disk; clearing it merges everything back
in. Switching is migration-safe in both directions (`Store.SetUsersFile`,
`Store.SetYearFileTemplate`): existing data is copied out or merged back,
never silently dropped. This mirrors the common pattern of letting an
operator split out secrets/large data without a separate migration tool.

Any relative path referenced from the main config (an external users file,
an external flag-year file) resolves relative to the *directory containing
the main config file*, not the process's current working directory —
important once config can live in a different place than the binary (e.g.
a Docker volume).

**Locking discipline**: resolving external files requires I/O, which must
happen *outside* the `Store`'s read/write lock (Go's `sync.RWMutex` can
deadlock on recursive `RLock` if a writer is waiting in between). The rule
followed throughout: any code path that needs both external-file data and
a `store.View`/`store.Update` closure resolves the external data first,
then passes the already-resolved plain data into the closure.

## Rotation and season logic

`rotation.Schedule` turns `duty.start_date` + `duty.period_days` +
`duty.groups` into period lookups via floor-division day arithmetic (so
dates before the start date still resolve correctly). All date arithmetic
normalizes to UTC midnight first (`dateOnly`) so DST transitions can't
shift a 24-hour day into 23 or 25 hours and corrupt period boundaries.

`rotation.Season` classifies a week (by its Monday) as summer or winter
using two configurable `MM-DD` anchors: summer starts on the first Monday
on/after `summer_start_anchor` (default `05-01`, the first full week of
May) and ends on the Sunday of the week containing `summer_end_anchor`
(default `09-30`, the week including the last day of September).

## Public info view

Shows today's duty pair, the next flag day (official/"virallinen
liputuspäivä" or recommended/"suositeltu liputuspäivä"), the number of
calendar weeks until that next flag day, and a week-by-week
schedule table for a configurable window (default: one month back to three
months ahead). The today-summary card (duty pair, flag-day status) uses
the same green background as the schedule table's current-week row, so
both "today" indicators match visually. The card also shows today's
season extra-info text (`today_season_summer_info`/`today_season_winter_info`),
a translation key dedicated to the summary card and independent from the
week-row tooltip's text below, so the two can be customized separately.
Any currently-active special dates (see below) are shown in their own
`.special-box` block beneath the flag-day summary.
When today itself is a flag day, a
larger waving flag is
shown alongside the mandatory/optional badge (`static/img/finland-flag-waving.gif`,
a free-to-embed animated GIF from flaggifs.org, downloaded once rather
than hot-linked so the page keeps working offline; it follows the same
embedded-with-filesystem-override rule as other static assets, and is
listed in the References section like any other asset). Each row shows
the Finnish-style week number, duty pair, date range, a flag marker plus
any flag days that week (name plus exact date in brackets, with a 🇫🇮
marker). Special dates (see below) share these same two columns with
official flag days — sorted together by date — using their own
configured icon instead of 🇫🇮 and no mandatory/optional badge. Season is shown only as the row's background (orange for summer,
cool light-blue for winter) — there is no dedicated season column. Hovering
a row reveals a CSS-only tooltip (anchored in the week-number cell, shown
via `tr:hover .season-tooltip`, since a native `title` attribute can't hold
a link) with the season description (`schedule_season_summer_tooltip`/
`schedule_season_winter_tooltip`, its own dedicated translation keys,
separate from the summary card's) and an "add to calendar" link to
`/calendar.ics` for that row's duty group, sized to cover the rest of the
current year (or into next year too, if less than a month of the current
year remains) via a shared `YearEndMonthsAhead` months-ahead value computed
once per request. There is no separate, dedicated iCal-download form on
this page any more — those per-row links (and `/calendar.ics` queried
directly) are the only way to get a calendar file from the UI. The flag
marker cell has its own hover tooltip (`.flag-marker-cell:hover
.flag-tooltip`) confirming the week includes a flag day; for the current
week, if today itself is a flag day, the tooltip also embeds the same
fluttering-flag GIF plus the mandatory/optional label, so the "today is a
flag day" indicator is reachable from the schedule table too, not just the
top summary. The row
containing today is highlighted in green.
Unlike the rest of the page, this table's
card breaks out of the normal content width (a viewport-relative width
centered via `transform: translateX(-50%)`) so each row's columns can sit
on one line whenever the browser window is wide enough, instead of being
capped at the page's narrower reading-width column.

A **References** section at the bottom of the page reports, for every year
covered by the displayed window, where its flag day data came from (the
source URL, and the external file path if `year_file_template` is in use),
and whether each UI template/static asset is currently served from the
embedded build or from an on-disk override (see below) — so an operator
looking at the page can tell exactly what's feeding it.

## UI assets: embedded with filesystem override

Templates and static files are embedded via `//go:embed` so the binary is
fully self-contained, but an `overlayFS` checks the process's current
working directory first (`os.Open("templates/index.html")`, etc.) and
falls back to the embedded copy. This lets a deployment override a single
template or stylesheet by dropping a same-named file next to where the
binary runs — no rebuild needed — which is also why the Docker image's
working directory is the volume mount point (see below).

## Config (admin) UI and authentication

`/config` and everything under it is protected by `requireAuth`, which
checks a `?user=` query parameter against configured users (**not** a
password check) if `-trust-proxy-user-param` is set — meant for offloading
authentication to a reverse proxy that already authenticated the caller
and rewrites the request to append `user=$authenticated_user` — falling
back to normal HTTP Basic Auth otherwise. Bcrypt hashes are used for
Basic Auth passwords. Since `requireAuth` reads only the first `user`
value via `r.URL.Query().Get`, a proxy only needs to place its trusted
value ahead of any client-supplied one, not strip the latter outright;
see `cmd/handyman-duty/README.md` for worked Caddy and nginx examples.

The config UI edits the duty roster, schedule window/season anchors,
languages/translations, flag days per year (including the source and a
manual re-fetch), the external-file toggles described above, and admin
users — all persisted back through the same `Store`. Each flag day row has
a mandatory checkbox; unchecked rows additionally show a
"(Suositeltu liputuspäivä)"/"(Recommended Flag Day)" hint next to the
checkbox so editors can tell at a glance which days are optional.

## iCal export

`internal/ical` renders all-day VEVENTs (duty periods for a selected group,
with flag days inside the period as separate reminder events) as a
dependency-free RFC 5545 document, including correct line folding and TEXT
escaping.

## Flag day auto-fetch

If a requested year has no flag day data, `flagdays.EnsureYear` fetches
`flagging_days.source_url_template` (with `{year}` substituted) and parses
it. The parser (`internal/flagdays`) is written against the HTML structure
used by intermin.fi (`<h2>` month headings followed by day-entry lines) and
has been validated against both intermin.fi/liputuspaivat-2026 and
almanakka.helsinki.fi/liputuspaivat-2027. A day is "official"
(`mandatory: true`) unless its text marks it as recommended
("suositeltu"). This only ever fetches a year that's actually missing —
it never silently overwrites configured data.

## Special dates

`config.SpecialDate` entries (`special_dates` in the YAML file) are
admin-configured, one-off (`YYYY-MM-DD`, not recurring) custom flagged
dates, edited via a dedicated `/config/special-dates` form (one labeled
fieldset per entry, following the existing indexed-row/blank-extra-rows
save convention, plus a live JavaScript preview —
`static/special-dates.js`, the project's first dedicated JS asset — that
re-renders each entry's flag-marker/list/summary/long-description preview
as the fields are edited, without a server round-trip). Each entry has:

- `icon` + `short_description`: shown in the schedule table's flag-marker
  and flag-day-list columns alongside official flag days (see above).
- `summary_description` + `summary_icon` (+ `summary_icon_scale`) +
  `summary_lead_days`: shown in today's-summary once the date is within
  `summary_lead_days` days away.
- `long_description` + `long_icon` (+ `long_icon_scale`) +
  `long_lead_days`: shown instead of the summary text once the date is
  within `long_lead_days` days away (including on the date itself); this
  takes precedence whenever both windows would otherwise apply.
- If several special dates are active on the same day, all of them are
  shown (stacked), not just the nearest one.

An icon field may be plain text/emoji, an already-local path, or a remote
`http(s)://` URL. A submitted URL is downloaded once at save time
(`internal/icons.Fetch`) and cached under `static/special-icons/<hash>.<ext>`
(hashed from the URL, so resaving the same URL reuses the cached file
instead of re-fetching), following the same CWD-relative-to-the-process
convention as other static overrides (see above) so the cached file is
immediately servable via the existing `/static/` route with no extra
routing. If any icon URL in a save fails to download, the whole save is
rejected (`icon_fetch_failed` flash) rather than partially persisting.
`summary_icon`/`long_icon` are optional — if unset, their description is
shown without an icon; `icon_scale` (default 1.0) only affects image
icons. Whether an icon value is an image (vs. plain text) is decided by
the same heuristic in both Go (`internal/webapp/domain.go`'s
`iconIsImage`) and JavaScript (`special-dates.js`'s `isImageIcon`, kept
manually in sync) — a `http://`/`https://` prefix, a leading `/`, or a
recognized image extension.

## CLI flags

`-config`, `-listen`, `-base-path`, `-dump-default-config`,
`-dump-flag-days` (with `-year` to restrict to one year),
`-dump-embedded-assets`/`-dump-effective-assets`, and
`-trust-proxy-user-param`. See `cmd/handyman-duty/README.md` for details.

## Docker image

Multi-stage build: `golang:1.26-alpine` compiles a fully static binary
(`CGO_ENABLED=0`, `-extldflags=-static`), which then runs on
`gcr.io/distroless/static-debian12:nonroot` — no shell, no package
manager, ~12 MB image, with ca-certificates and tzdata included for the
HTTPS flag-day fetch and `Europe/Helsinki` local time. All runtime state
(`config.yaml`, any external auth/flag-day files it references, optional
`templates/`/`static/` overrides) lives under a single `/data` volume
mount, kept separate from the read-only application layer — directly
reusing the embed-with-filesystem-override design above, since the
container's working directory *is* that volume.

## CI: publishing to ghcr.io

`.github/workflows/docker-publish.yml` builds the image for
`linux/amd64`/`linux/arm64` and pushes to GitHub Container Registry:

- every push to `main` → tagged `unstable`.
- every tag matching `v[0-9]+.[0-9]+.[0-9]+` → tagged with that exact
  version (e.g. `v1.2.3`).
- every push to a branch named `test/image/$subject` → tagged
  `v0.0.0-$subject`, for ad hoc test builds without cutting a release tag.
