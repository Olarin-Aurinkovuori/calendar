# Determine the version/commit to stamp into built binaries via linker
# flags (see internal/version and config.Config.WrittenBy, which records
# these on every config.yaml save for future migrations). Prefers git
# metadata, falling back to sane defaults when it's unavailable (e.g.
# building from a source tarball with no .git, or no tags yet).
GIT_VERSION := $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
GIT_COMMIT  := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

VERSION_PKG := github.com/Olarin-Aurinkovuori/calendar/internal/version
LDFLAGS     := -s -w -X $(VERSION_PKG).Version=$(GIT_VERSION) -X $(VERSION_PKG).Commit=$(GIT_COMMIT)

.PHONY: build
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o calendar ./cmd/calendar

.PHONY: docker-build
docker-build:
	docker build \
		--build-arg VERSION=$(GIT_VERSION) \
		--build-arg VCS_REF=$(GIT_COMMIT) \
		-f cmd/calendar/Dockerfile -t calendar:latest .

.PHONY: fmt
fmt:
	gofmt -l .

.PHONY: vet
vet:
	go vet ./...

.PHONY: clean
clean:
	rm -f calendar

# Used by other build drivers (e.g. docker-compose.yml) that want the same
# version/commit without duplicating the git commands, e.g.:
#   export VERSION=$$(make -s print-version) VCS_REF=$$(make -s print-commit)
.PHONY: print-version
print-version:
	@echo $(GIT_VERSION)

.PHONY: print-commit
print-commit:
	@echo $(GIT_COMMIT)
