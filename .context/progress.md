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

## US-002: Add golangci-lint workflow
- Added `.github/workflows/lint.yml`: triggers on `push` and `pull_request`; single `golangci` job on `ubuntu-latest`.
- Steps in order: `actions/checkout@v4`; `actions/setup-go@v5` with `go-version-file: go.mod`; `golangci/golangci-lint-action@v6` with `version: latest`.
- No `.golangci.yml` added — relies on golangci-lint's default linter set (gofmt overlap with CI accepted).
- Files changed: `.github/workflows/lint.yml` (new).
- **Learnings for future iterations:**
  - Spaces only; verified no tabs with `grep -P '\t'` and structure with `ruby -ryaml`.
  - Confirmed absence of `.golangci.yml`/`.golangci.yaml` so defaults are used.

## US-003: Verify workflows locally and validate YAML schema
- Verification-only story (no application code changed). Reproduced every CI step on the current tree and validated both workflow YAML files.
- Results:
  - `gofmt -l .` → empty (clean).
  - `go vet ./...` → ok.
  - `go build -o smartview .` → ok.
  - `go test ./...` → ok (`internal/smart` and `internal/ui` pass; root has no tests).
  - Tabs: `grep -Pn '\t' .github/workflows/*.yml` → none.
  - YAML parse: `ruby -ryaml` loads both `ci.yml` and `lint.yml` cleanly.
  - actionlint: not installed on PATH; ran it via `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/*.yml` → exit 0, no errors.
- Files changed: `.context/progress.md` (notes); `.gitignore` (pre-existing local change adding `.context`).
- **Learnings for future iterations:**
  - actionlint can be run without installing it via `go run github.com/rhysd/actionlint/cmd/actionlint@latest` (needs network for first fetch); `go run pkg@version` does not touch the module's `go.mod`/`go.sum`.
  - actionlint passes clean on both workflows, confirming US-001/US-002 are syntactically valid for GitHub Actions (not just generic YAML).
