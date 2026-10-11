# syntax=docker/dockerfile:1

# Build this from the repository root (the build context must include
# go.mod, go.sum, internal/, and cmd/calendar/), e.g.:
#   docker build -f cmd/calendar/Dockerfile -t calendar .

# ---------------------------------------------------------------------------
# Build stage: compile a fully static binary (CGO disabled) so it can run
# unmodified on the distroless "static" base below, which has no libc/shell.
# ---------------------------------------------------------------------------
ARG GO_VERSION=1.26

FROM golang:${GO_VERSION}-bookworm AS builder

WORKDIR /src

# Cache module downloads in their own layer, separate from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# VERSION/VCS_REF identify this build in the running binary (and, from
# there, get stamped into config.yaml's written_by section on every save,
# for future config migrations); .git is excluded from the build context
# (see .dockerignore), so these must be passed in from outside -- use
# "make docker-build" (repo root Makefile) to do that automatically from
# git, or pass them by hand:
#   docker build --build-arg VERSION=$(git describe --tags --always --dirty) \
#     --build-arg VCS_REF=$(git rev-parse HEAD) -f cmd/calendar/Dockerfile -t calendar .
ARG VERSION=dev
ARG VCS_REF=unknown
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -extldflags=-static \
    -X github.com/Olarin-Aurinkovuori/calendar/internal/version.Version=${VERSION} \
    -X github.com/Olarin-Aurinkovuori/calendar/internal/version.Commit=${VCS_REF}" \
    -o /out/calendar ./cmd/calendar

# ---------------------------------------------------------------------------
# Runtime stage: distroless "static" -- just ca-certificates, tzdata, and a
# /etc/passwd entry for the built-in "nonroot" (65532:65532) user. No shell,
# no package manager, no HTTP client to exec a HEALTHCHECK with; rely on an
# orchestrator-level TCP/HTTP probe against the published port instead.
#
# Everything the application reads or writes at runtime -- the YAML config
# file, any external auth-users/flag-day-year files it references, and
# optional template/static overrides -- lives under /data, a single volume
# mount point kept entirely separate from the read-only application layer.
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/calendar /app/calendar

ENV TZ=Europe/Helsinki \
    CONFIG_PATH=/data/config.yaml

WORKDIR /data
VOLUME ["/data"]

EXPOSE 8080

ENTRYPOINT ["/app/calendar"]
