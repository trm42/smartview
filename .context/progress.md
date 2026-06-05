# Autopilot Progress

## Codebase Patterns
- Go module `smartview`, Go 1.26.4 (declared in `go.mod`).
- Local quality gate (from CLAUDE.md): `gofmt -l .` (must be empty), `go vet ./...`, `go build -o smartview .`, `go test ./...`. The `smartview` binary is gitignored (`/smartview`).
- Tests parse JSON fixtures in `internal/smart/testdata/`; no smartctl binary or runtime hardware needed in CI.

## US-001: Add CI workflow (build/test/vet/fmt) with Go version matrix
- Added `.github/workflows/ci.yml`: triggers on `push` and `pull_request`; single `build-test` job on `ubuntu-latest` with `strategy.fail-fast: false` and an `include` matrix of two legs — `go.mod` (`go-version-file: go.mod`) and `stable` (`go-version: stable`).
- Steps in order: `actions/checkout@v4`; `actions/setup-go@v5` (both `go-version-file` and `go-version` from matrix + `cache: true`); gofmt check that fails printing offending files; `go vet ./...`; `go build -o smartview .`; `go test ./...`.
- Files changed: `.github/workflows/ci.yml` (new).
- **Learnings for future iterations:**
  - `setup-go@v5` ignores empty inputs, so passing both `go-version-file` and `go-version` from a matrix where each leg sets only one is the clean way to get a single-source-of-truth leg plus a `stable` leg.
  - YAML forbids tabs — verified with `grep -P '\t'`; validated structure with `ruby -ryaml`. `python3` here has no `pyyaml`.
  - All local checks pass clean (gofmt clean, vet ok, build ok, tests ok) so CI should be green.
