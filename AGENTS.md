# Repository Guidelines

## Project Structure & Module Organization
This repository contains standalone skills in `skills/` and supporting tools in `tools/`. The `agentmux` Go module lives in `tools/agentmux/`; its CLI entrypoint is `tools/agentmux/cmd/agentmux/main.go`, core logic is under `tools/agentmux/internal/`, and its install/release helpers are under `tools/agentmux/scripts/`. The directly copyable skills are `skills/agentmux/` and `skills/cookbook-forge/`.

## Build, Test, and Development Commands
Work from `tools/agentmux/` for the Go module unless you are editing repo-level files.

- `go build -o ./bin/agentmux ./cmd/agentmux` builds the local CLI binary.
- `go test ./...` runs all Go tests when present; use it before opening a PR.
- `./scripts/install.sh` builds and installs `agentmux` and the default config.
- `./scripts/release.sh` creates CLI-only release tarballs in `tools/agentmux/dist/`.

If your machine has restricted default Go cache paths, use the repo-local cache pattern already documented in the README:
`GOCACHE=$PWD/.cache/go-build GOPATH=$PWD/.cache/go-path GOMODCACHE=$PWD/.cache/go-mod go build ./cmd/agentmux`

## Coding Style & Naming Conventions
Follow standard Go formatting: tabs for indentation, `gofmt` output, and short package names. Keep internal packages single-purpose and colocate code by concern under `tools/agentmux/internal/<package>/`. Exported identifiers use `CamelCase`; unexported helpers use `camelCase`. Shell scripts should stay POSIX-friendly Bash with `set -euo pipefail`.

## Testing Guidelines
New features should add table-driven Go tests next to the package they cover, with names like `TestSummonReuse` or `TestCaptureTimeout`. Run `go test ./...` locally before submitting. If you change packaging or install behavior, also smoke-test `./scripts/install.sh` or `./scripts/release.sh` as appropriate.

## Commit & Pull Request Guidelines
Recent commits use short, imperative subjects such as `Improve help and packaging workflow`. Keep commit titles concise, sentence case, and focused on one change. PRs should include a clear summary, note affected commands or config files, and paste verification steps or command output. Screenshots are unnecessary unless you are changing rendered documentation or terminal UX in a way text cannot show clearly.

## Security & Configuration Tips
Do not commit local cache directories, built artifacts, or secrets; `.gitignore` already excludes `bin/`, `dist/`, and `.cache/`. Treat `agentmux/examples/config.yaml` as the baseline for config changes, and document any new environment variables in `agentmux/README.md`.
