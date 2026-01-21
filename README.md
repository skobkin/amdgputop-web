# amdgpu_top-web

[![CI](https://github.com/skobkin/amdgputop-web/actions/workflows/ci.yml/badge.svg)](https://github.com/skobkin/amdgputop-web/actions/workflows/ci.yml)

Read-only web UI for live AMD GPU telemetry inspired by the `amdgpu_top` CLI.
The backend is pure Go (stdlib HTTP + WebSockets) and the frontend is a compact
Preact single-page app.

![AMD GPU telemetry UI](docs/screenshot.webp "Current UI snapshot")

## Features

- 🖥️ Enumerates DRM GPUs and streams utilization, clocks, temps, VRAM/GTT usage.
- 🧾 Optional “process top” view sourced from `/proc/*/fdinfo` with engine-time
  deltas when exposed by the kernel.
- 📈 Historical charts (uPlot) for the selected GPU with hover tooltips.
- 🌐 REST endpoints for `/api/gpus`, `/api/gpus/<id>/metrics`, and `/api/gpus/<id>/procs`
  alongside a WebSocket feed (`/ws`).
- 📊 Optional Prometheus `/metrics` export with per-GPU telemetry (no per-process data).
- ⚙️ Configuration via environment variables (`APP_*`), including sampler cadence,
  process scanner limits, and allowed origins.

## Quick Start (host build)

```bash
cd web && npm ci && npm run build
go build ./cmd/amdgputop-web
./amdgputop-web            # listens on :8080 by default

# Alternatively, run the default build pipeline:
# make
```

The frontend build output is generated into `internal/httpserver/assets/` and is
embedded at compile time; those files are not committed to the repository.

On AMD hardware you can sanity-check the sampler without the web UI:

```bash
go run ./cmd/sampler-test -sample
```

## Docker

The official image built by Github Actions is available here: [``]().

### Docker compose

Example Docker stack: https://git.skobk.in/skobkin/docker-stacks/src/branch/master/amdgputop-web

### Running manually

An Alpine-based multi-stage image is defined in `Dockerfile`.

```bash
docker build -t amdgputop-web:dev .

VID_GID=$(getent group video | cut -d: -f3)
RENDER_GID=$(getent group render | cut -d: -f3)

docker run --rm -p 8080:8080 \
  --device=/dev/dri \
  --device=/dev/kfd \
  --group-add "${VID_GID}" \
  --group-add "${RENDER_GID}" \
  -v "/usr/share/hwdata/pci.ids:/usr/share/hwdata/pci.ids:ro" \
  --pid=host \
  --cap-add SYS_PTRACE \
  --user root \
  amdgputop-web:dev
```

### Important notes

> **GPU names**: the runtime resolves PCI IDs via `/usr/share/hwdata/pci.ids`.
> If your distribution stores the database elsewhere (e.g. `/usr/share/misc/pci.ids`),
> adjust the bind mount path accordingly.

> **Why root + `SYS_PTRACE`?** Reading `/proc/<pid>/fdinfo` for host workloads
> requires elevated privileges and the `CAP_SYS_PTRACE` capability. Running the
> container as `root` with `--cap-add SYS_PTRACE` is the simplest way to let the
> process scanner observe GPU clients outside the container. If you only need
> device-level metrics, you can omit `--pid=host`, `--user root`, and the extra
> capability and run with the default non-root user.

Refer to `docs/DOCKER.md` for more detail, including why `--pid=host` is needed
to observe host processes.

#### Troubleshooting & permissions

- The [permissions matrix](docs/DOCKER.md#permissions-matrix) explains which
  flags, groups, and capabilities are required for device-only metrics versus
  host process telemetry.
- If the UI shows empty process tables or partial metrics, consult the
  [troubleshooting section](docs/DOCKER.md#troubleshooting) for the most common
  container permission fixes.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `APP_LISTEN_ADDR` | `:8080` | HTTP listen address. |
| `APP_LOG_LEVEL` | `INFO` | Log verbosity (`DEBUG`, `INFO`, `WARN`, `ERROR`). |
| `APP_ALLOWED_ORIGINS` | `*` | Comma-separated origins allowed for WebSocket/HTTP. |
| `APP_DEFAULT_GPU` | `auto` | GPU pre-selected on connect (`auto` = first detected). |
| `APP_ENABLE_PROMETHEUS` | `false` | Enable `/metrics` endpoint with per-GPU telemetry when `true`. |
| `APP_ENABLE_PPROF` | `false` | Expose Go pprof handlers on `/debug/pprof/*`. |
| `APP_CHARTS_ENABLE` | `true` | Toggle historical charts feature. |
| `APP_CHARTS_MAX_POINTS` | `7200` | Maximum data points retained per chart. |
| `APP_SAMPLE_INTERVAL` | `2s` | Metrics sampling cadence. |
| `APP_PROC_ENABLE` | `true` | Toggle process scanner feature. |
| `APP_PROC_SCAN_INTERVAL` | `2s` | Interval between process snapshot scans. |
| `APP_PROC_MAX_PIDS` | `5000` | Upper bound on tracked process count per scan. |
| `APP_PROC_MAX_FDS_PER_PID` | `64` | Max file descriptors per PID to inspect. |
| `APP_WS_MAX_CLIENTS` | `1024` | Maximum concurrent WebSocket clients. |
| `APP_WS_WRITE_TIMEOUT` | `3s` | WebSocket write timeout. |
| `APP_WS_READ_TIMEOUT` | `30s` | WebSocket read timeout. |
| `APP_SYSFS_ROOT` | `/sys` | Override sysfs root (test-only). |
| `APP_DEBUGFS_ROOT` | `/sys/kernel/debug` | Override debugfs root (test-only). |
| `APP_PROC_ROOT` | `/proc` | Override procfs root (test-only). |

See `internal/config/config.go` for the full list, including test-only roots
(`APP_SYSFS_ROOT`, `APP_DEBUGFS_ROOT`, `APP_PROC_ROOT`).

## Prometheus

Set `APP_ENABLE_PROMETHEUS=true` to expose `GET /metrics`. The exporter
publishes WebSocket counters along with the latest per-GPU telemetry pulled from
the sampler. Each gauge is labeled with `gpu_id` and includes:

- Busy percentages for graphics and memory engines.
- Current SCLK/MCLK frequencies, temperature, fan RPM, and power draw.
- VRAM/GTT usage and capacity.
- Timestamps and age for the most recent sample.

Per-process statistics stay out of the Prometheus surface area.

## Development

```bash
# Backend
go test ./...

# Frontend
cd web && npm ci && npm run build
```

CI (see `.github/workflows/ci.yml`) enforces `gofmt`, `go vet`, Go tests,
frontend build, and publishes tagged releases with Linux binaries and Docker
images.
