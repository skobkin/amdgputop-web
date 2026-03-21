# Repository Guidelines

## Project Structure & Module Organization

- `cmd/`: Go entrypoints. Primary binary is `cmd/amdgputop-web`.
- `internal/`: Backend implementation (HTTP/WebSocket server, sampler, proc scanner).
- `internal/**/testdata/`: Fixture data for unit/integration tests.
- `web/`: Preact + Vite frontend (`src/`, `public/`, `vite.config.ts`).
- `docs/`: Operational docs (Docker, accessibility, screenshots).
- Build artifacts: frontend bundle is generated into `internal/httpserver/assets/` and embedded at build time (not committed).

## Build, Test, and Development Commands

- `make`: Runs `gofmt`, `go vet`, `go test`, frontend build, and backend build.
- `make build`: Builds the backend binary `amdgputop-web`.
- `make test`: Runs Go tests (`go test ./...`).
- `make race`: Runs race detector (`go test -race ./...`).
- `make frontend-build`: Builds the frontend (`npm --prefix web run build`). Needs to be rebuilt before building Go app if front-end changed were made.
- `cd web && npm run dev`: Local frontend dev server (Vite).

## Coding Style & Naming Conventions

- Go code must be `gofmt`-formatted; CI enforces this.
- Linting: `go vet ./...` (CI/`make lint`).
- Use standard Go naming conventions (exported `CamelCase`, unexported `camelCase`).
- Frontend code is TypeScript/Preact; follow existing `web/src` patterns and keep component/file names aligned with their exported symbols.

## Testing Guidelines

- Tests use Go’s `testing` package; files follow `*_test.go` naming.
- Fixtures live under `internal/**/testdata/`.
- Run all tests with `go test ./...` (or `make test`).
- Frontend has no dedicated test runner; rely on `npm --prefix web run build` plus backend tests.

## Commit & Pull Request Guidelines

- Commit messages follow Conventional Commits: `type(scope): summary`.
  - Examples: `fix(web): correct chart timestamps`, `build(deps): bump preact`.
- PRs should include:
  - A short description of behavior changes.
  - Test evidence (commands run and results).
  - Screenshots for UI-visible changes (`docs/screenshot.webp` if updated).

## Configuration Notes

- Runtime configuration is via `APP_*` environment variables (see `README.md` and `internal/config/config.go`).
- For containerized runs, permissions and GPU access details are in `docs/DOCKER.md`.
