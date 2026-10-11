# calendar

A standalone Go web application that replaces a Google Sheet used to track
a housing company's rotating handyman/caretaker duty ("talonmiesvuorot")
among apartment pairs. It serves a multilingual public info view plus a
protected configuration UI from a single self-contained binary, with no
external database — everything lives in one YAML configuration file.

## Documentation

- [`cmd/handyman-duty/README.md`](cmd/handyman-duty/README.md) — what the
  application does, building/running it, all CLI flags, Docker usage, and
  the configuration file format. **Start here.**
- [`docs/handyman-duty/README.md`](docs/handyman-duty/README.md) — design
  notes: package layout and the reasoning behind how the application is
  built.

## Quick start

```sh
go build -o calendar ./cmd/handyman-duty
./calendar -config config.yaml
```

See `cmd/handyman-duty/README.md` for the full set of CLI flags, Docker
instructions, and configuration details.

