# amdgpu_top‑web — Plan & TODO (updated)

A lean, **read‑only** web UI for live AMD GPU telemetry inspired by [`amdgpu_top`](https://github.com/Umio-Yasuno/amdgpu_top).
**Backend:** Go (latest stable), stdlib HTTP + WebSocket push.
**Frontend:** Preact + Pico.css + Zustand (no heavy frameworks).
**Runtime:** Works on host and inside Docker with `/dev/kfd`, `/dev/dri` mounted.
**Extra:** “Process Top” (best‑effort) showing processes that use the GPU.

---

## 0) Ground Rules (hard requirements)

* **Go:** use the **latest stable Go** at project init and keep it updated (pin via `toolchain` in `go.mod`; CI matrix includes “stable”).
* **Routing:** use **stdlib** (`net/http`, `http.ServeMux`, `http.Server`).
* **Transport:** WebSocket for live updates (library: `nhooyr.io/websocket` — small, maintained).
* **Frontend:** read‑only; allow GPU selection if multiple are present.
* **Config:** entirely via **environment variables**.
* **Docker:** must run with AMD device nodes mounted; degrade gracefully if some counters are missing.
* **Separation of concerns:** sampler(s) ↔ cache/bus ↔ HTTP/WS ↔ frontend. No God objects.
* **Git policy:** **disable GPG signing** so the agent can commit:

  ```
  git config commit.gpgSign false
  ```

  (set at repo level; do **not** flip global settings without user consent)

---

## 1) High‑Level Architecture

```
Browser (Preact SPA)
  |  HTTP: GET /, /api/gpus, /api/healthz, /api/version
  |  WS:   /ws  (hello, subscribe{gpu_id}, stats, error)
  v
Go HTTP server (stdlib)
  - static assets (embed.FS)
  - REST endpoints
  - WS upgrade (nhooyr)
  v
WS Hub  <---->  GPU Samplers (one per GPU)
  - per-client subscription         - enumerate DRM cards via /sys
  - cached latest snapshot          - read sysfs/hwmon/debugfs (best-effort)
  - broadcast @interval             - separate process-scanner (best-effort)
```

---

## 2) Data Model & Contracts

**GPU identity**

```json
{
  "id": "card0",
  "pci": "0000:0a:00.0",
  "pci_id": "1002:73df",
  "name": "AMD Radeon RX 6800",
  "render_node": "/dev/dri/renderD128"
}
```

**GPU telemetry snapshot** (values can be `null` if unavailable)

```json
{
  "type": "stats",
  "gpu_id": "card0",
  "ts": "2025-10-12T13:07:05.123Z",
  "metrics": {
    "gpu_busy_pct": 42.3,
    "mem_busy_pct": 18.0,
    "sclk_mhz": 1267,
    "mclk_mhz": 875,
    "temp_c": 64.5,
    "fan_rpm": 1450,
    "power_w": 165.2,
    "vram_used_bytes": 1845493760,
    "vram_total_bytes": 17071734784,
    "gtt_used_bytes": 268435456,
    "gtt_total_bytes": 34359738368
  }
}
```

**Per‑process (best‑effort)**

```json
{
  "type": "procs",
  "gpu_id": "card0",
  "ts": "2025-10-12T13:07:05.123Z",
  "capabilities": {
    "vram_gtt_from_fdinfo": true,
    "engine_time_from_fdinfo": false
  },
  "processes": [
    {
      "pid": 1234,
      "uid": 1000,
      "user": "alice",
      "name": "blender",
      "cmd": "blender --render file.blend",
      "render_node": "renderD128",
      "vram_bytes": 734003200,
      "gtt_bytes": 134217728,
      "gpu_time_ms_per_s": null  // if engine time deltas available, else null
    }
  ]
}
```

**WS messages**

* Client → Server:

    * `{"type":"subscribe","gpu_id":"card0"}`
* Server → Client:

    * `{"type":"hello","interval_ms":250,"gpus":[...],"features":{"procs":true}}`
    * `{"type":"stats",...}` (every tick)
    * `{"type":"procs",...}` (separate cadence, e.g. 1–2s)
    * `{"type":"error","message":"..."}`
    * `{"type":"pong"}`

**HTTP endpoints**

* `GET /` — SPA
* `GET /api/gpus` — list GPUs
* `GET /api/healthz` (liveness), `GET /api/readyz` (after first enumerate)
* `GET /api/version`
* Optional: `GET /metrics` (Prometheus), gated by env

---

## 3) Backend Design (Go)

**Versioning**

* `go.mod`: `go <latest>` + `toolchain go<latest>` (upgrade regularly; CI enforces “stable”).

**Packages**

```
/cmd/amdgpu-top-web     # main wiring
/internal/config         # env parsing (slog-backed)
/internal/httpserver     # http mux, static, health, version
/internal/ws             # hub, client session, JSON framing
/internal/gpu            # enumeration + GPU sampler
/internal/procscan       # process top scanner (best-effort)
/internal/version        # build info
/pkg/model               # shared structs
```

**Routing**

* `http.NewServeMux()`; explicit route registration; no third-party router.

**WebSocket**

* `nhooyr.io/websocket` w/ origin checks + read/write deadlines.
* Backpressure: bounded per‑client channel; drop oldest + emit error flag to client (never block producers).

**GPU discovery & sampling**

* Enumerate via `/sys/class/drm/card*/device`.
* Identity from `uevent` + PCI path; render node via `/dev/dri/renderD*`.
* Metrics (read‑only):

    * Busy%: `/sys/class/drm/cardX/device/gpu_busy_percent` (preferred).
      Fallback: parse `/sys/kernel/debug/dri/X/amdgpu_pm_info` if readable (optional; do not require debugfs).
    * Clocks/power/temp/fan via `hwmon` under the device.
    * VRAM/GTT totals & used via `mem_info_*` files (sysfs).
* One **sampler goroutine per GPU** → update cache → broadcast.

**Process Top (best‑effort, like `amdgpu_top`)**

* Primary data source: `/proc/<pid>/fdinfo/*` for FDs targeting `/dev/dri/renderD*`.

    * Parse `drm-memory` lines (VRAM/GTT/System).
    * If kernel exposes `drm-engine` counters in fdinfo, compute per‑tick deltas to approximate **GPU time ms/s** for that process; otherwise leave `null`.
* PID discovery strategy (performance‑aware):

    * Scan `/proc` at **slower cadence** (e.g., `APP_PROC_SCAN_INTERVAL` default **2s**).
    * For each PID, short‑circuit if no `fd` dir or not readable.
    * Within `fdinfo`, **stop early** after the first renderD* match if only memory totals are needed.
    * Maintain a watchlist of PIDs seen using renderD* in the last N scans to prioritize.
    * Hard limits: `APP_PROC_MAX_PIDS` (default 5000), `APP_PROC_MAX_FDS_PER_PID` (default 64), and time budget per scan.
* Name/owner:

    * `/proc/<pid>/comm` + `/proc/<pid>/cmdline` (truncate long cmdlines).
    * Resolve `uid` to user via `os/user` (best‑effort).
* Security/Privacy: read‑only; respect `hidepid` and permission errors → skip.
  **Note:** Inside Docker without `--pid=host`, you will **not** see host processes; process top will show container processes only. Document this clearly.

**Configuration (env)**

* `APP_LISTEN_ADDR` = `:8080`
* `APP_SAMPLE_INTERVAL` = `250ms` (100ms–2s bounds)
* `APP_ALLOWED_ORIGINS` = `*` (dev); tighten in prod
* `APP_DEFAULT_GPU` = `auto` | `cardN`
* `APP_ENABLE_PROMETHEUS` = `false`
* `APP_ENABLE_PPROF` = `false`
* `APP_LOG_LEVEL` = `INFO`
* `APP_SYSFS_ROOT` = `/sys` (for tests)
* `APP_DEBUGFS_ROOT` = `/sys/kernel/debug` (optional, RO)
* `APP_WS_MAX_CLIENTS` = `1024`
* `APP_WS_WRITE_TIMEOUT` = `3s`
* `APP_WS_READ_TIMEOUT` = `30s`
* **Process Top:**

    * `APP_PROC_ENABLE` = `true`
    * `APP_PROC_SCAN_INTERVAL` = `2s`
    * `APP_PROC_MAX_PIDS` = `5000`
    * `APP_PROC_MAX_FDS_PER_PID` = `64`

**Observability**

* Optional Prometheus export: latest metrics per GPU (labels: `gpu_id`) and counters for `proc_count`, `scan_duration_ms`, `scan_errors_total`.

**Security**

* Run as **non‑root**; RO reads from sysfs/hwmon/proc.
* Whitelist known paths; never echo arbitrary file content.
* CORS allowlist in prod.
* No browser‑exposed secrets; no command endpoints.

---

## 4) Frontend Design (Preact + Pico.css + Zustand)

**Build**

* Vite + `@preact/preset-vite`; embed assets into Go binary via `go:embed`.

**State (Zustand)**

```ts
{
  gpus: GPUInfo[],
  selectedGpuId: string,
  connection: 'connecting'|'open'|'closed'|'error',
  statsByGpu: Record<string, StatsSnapshot>,
  procsByGpu: Record<string, ProcSnapshot>,
  lastUpdatedTs: number
}
```

**Components**

* `<App>`: WS lifecycle; error/stale banners.
* `<GpuSelector>`: dropdown to switch GPU (read from `hello`/`/api/gpus`).
* `<StatsTiles>`: GPU busy, temp, power, clocks.
* `<MemoryBars>`: VRAM/GTT progress bars.
* `<ProcTable>`: **read‑only** table of processes using GPU
  Columns: PID, User, Name, VRAM, GTT, Total, (optional) GPU Time ms/s. Sortable by Total desc.
  If capability missing, column shows `—`.
* `<Footer>`: interval, version, link to repo.

**UX**

* Read‑only, keyboard accessible, responsive.
* Show “stale” indicator if no update > 2× interval.

---

## 5) Containerization & Deployment

**Dockerfile (multi‑stage)**

1. Frontend build (node) → `/web/dist`
2. Go build (latest toolchain, `CGO_ENABLED=0`) → static binary with embed.
3. Final image: **distroless** or `alpine` (if you want shell for debug). Run as non‑root.

**Runtime**

```bash
docker run --rm -p 8080:8080 \
  --device=/dev/kfd --device=/dev/dri \
  --group-add video --group-add render \
  -e APP_ALLOWED_ORIGINS="http://localhost:8080" \
  ghcr.io/your-org/amdgpu-top-web:latest
```

**Process Top caveat in Docker**

* To see **host** processes, you must run with `--pid=host` (and accept the security trade‑off). Without it, process top reflects **container** processes only:

```bash
docker run --rm -p 8080:8080 \
  --pid=host \
  --device=/dev/kfd --device=/dev/dri \
  --group-add video --group-add render \
  ghcr.io/your-org/amdgpu-top-web:latest
```

---

## 6) Testing Plan (host‑first, then Docker)

**Host‑first (agent machine has AMD GPU)**

1. Build & run **on host** (no Docker).
2. Confirm:

    * `/api/gpus` lists cards.
    * WS `hello` + `stats` stream updates at `APP_SAMPLE_INTERVAL`.
    * Process Top: at least one GPU‑using process shows up when a workload is active (e.g., `glxgears`, `vulkaninfo`, a game).
3. Record all issues in `DEVLOG.md` and `TROUBLESHOOTING.md` (see templates below).

**Then Docker**

1. Run container with devices; verify GPU metrics.
2. Verify **process top** behavior with/without `--pid=host`; document the difference.
3. Test with/without debugfs bind‑mount (optional):
   `-v /sys/kernel/debug:/sys/kernel/debug:ro` (only if needed; do not require).

**Automated**

* Unit tests: parsers for sysfs/hwmon/fdinfo (fixtures under `/testdata`).
* Integration: point `APP_SYSFS_ROOT`/`APP_DEBUGFS_ROOT` to fixture trees; spin WS and assert frames.
* Race detector in CI; `pprof` locally when `APP_ENABLE_PPROF=true`.

---

## 7) Milestones & TODO (commit at each ✓)

> **Git policy (pre‑req for agent):**
>
> ```
> git config commit.gpgSign false
> ```
>
> Make frequent, small commits with clear messages. Tag milestones.

### Phase 0 — Repo & Scaffolding

* [ ] ✓ Create repo, MIT license, `README.md`.
* [ ] ✓ Init Go module (**latest Go**, add `toolchain`), basic `Makefile`.
* [ ] ✓ Frontend scaffold (Vite + Preact + Pico + Zustand).
* [ ] ✓ CI: build + test + race; dependabot/renovate for Go/Node.

### Phase 1 — Backend skeleton (stdlib only)

* [x] `internal/config` (env + defaults).
* [x] `internal/httpserver` with `http.ServeMux`, `/healthz`, `/readyz`, `/version`, static. _(Handlers implemented; integration tests cover health/ready/version/static/WS)_
* [x] WS endpoint `/ws` (hello, subscribe, ping/pong). _(Integration test exercises hello+stats flow)_
* [x] Embed placeholder SPA; end‑to‑end echo test. _(Placeholder page documents HTTP/WS; tests cover WS flow)_

### Phase 2 — GPU enumeration & metrics

* [x] DRM card discovery via sysfs; render node detection.
* [x] Implement metrics readers (sysfs/hwmon; optional debugfs fallback).
* [x] Sampler per GPU; cache + broadcast `stats`.
* [x] `/api/gpus` wired; frontend selector works. _(Frontend selector pending)_
* [x] Unit tests for discovery & sampler (sysfs/debugfs fixtures).
* [x] `/api/gpus/{id}/metrics` REST endpoint for direct polling.

### Phase 3 — Process Top (best‑effort)

* [x] Implement `/proc` scanner with budgets/limits; parse `fdinfo` for renderD* FDs.
* [x] Extract VRAM/GTT totals from `fdinfo: drm-memory`.
* [x] Optional engine time deltas if `drm-engine` present; otherwise null.
* [x] Separate cadence and payload (`procs` frames); capability flag to client.
* [x] REST endpoint `/api/gpus/{id}/procs` for direct polling.
* [ ] Frontend `<ProcTable>` with sorting & null‑safe UI.

### Phase 4 — Robustness & Observability

* [ ] Backpressure/drop policy in WS hub; client caps.
* [ ] Prometheus (optional) + minimal counters.
* [ ] Logs: structured (`slog`), per‑request IDs.
* [ ] Graceful shutdown; leak checks (`-race` clean).

### Phase 5 — Docker & Docs

* [ ] Multi‑stage Dockerfile; non‑root runtime.
* [ ] Doc device mounts; **note** process top requires `--pid=host` to see host PIDs.
* [ ] Troubleshooting guide; permissions matrix.

### Phase 6 — Polish

* [ ] Accessibility pass; Lighthouse baseline.
* [ ] README with quickstart, env var table, screenshots.
* [ ] Release v0.1.0 (host‑first); v0.2.0 (Docker validated).

---

## 8) Implementation Notes (pragmatic / “don’t shoot yourself”)

* **Never** block sampling on slow clients—**broadcast from cache**.
* If a metric file is missing or unreadable, set `null` and move on; don’t spam logs.
* Process scanning can be expensive. Keep it **budgeted** and **coarse** (≥ 2s).
  Prioritize known GPU users; cap FDs inspected per PID.
* **Truth bomb:** Inside a container without `--pid=host`, process top for host apps is **not possible**; don’t over‑engineer around this.
* Gorilla/websocket is archived; stick with `nhooyr`.
* Don’t rely on debugfs; use it only when present and readable.

---

## 9) Pseudocode snippets

**Main**

```go
func main() {
  cfg := config.FromEnv()
  ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
  defer stop()

  gpus := gpu.Discover(cfg.SysfsRoot)
  bus  := ws.NewHub(cfg)

  // GPU samplers
  for _, g := range gpus {
    go gpu.RunSampler(ctx, g, cfg.SampleInterval, bus.UpdateStats)
  }

  // Proc scanner (optional)
  if cfg.ProcEnable {
    go procscan.Run(ctx, cfg, gpus, bus.UpdateProcs)
  }

  srv := httpserver.New(cfg, bus, gpus)
  go func() { _ = srv.ListenAndServe() }()
  <-ctx.Done()
  _ = srv.Shutdown(context.Background())
}
```

**Process scan (fdinfo path check)**

```go
func scanPid(pid int, limits Limits) (ProcUsage, bool) {
  fdinfo := fmt.Sprintf("/proc/%d/fdinfo", pid)
  entries, err := os.ReadDir(fdinfo)
  if err != nil { return ProcUsage{}, false }

  var vram, gtt uint64
  var sawRender bool
  n := 0
  for _, e := range entries {
    if n >= limits.MaxFDs { break }
    p := filepath.Join(fdinfo, e.Name())
    b, err := os.ReadFile(p)
    if err != nil { continue }
    if !bytes.Contains(b, []byte("drm")) { continue }

    // quick path: is this fd bound to a render node?
    // (optionally readlink /proc/pid/fd/<n> to check 'renderD')
    link, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", e.Name()))
    if !strings.Contains(link, "renderD") { continue }
    sawRender = true
    n++

    // parse drm-memory lines
    // VRAM: <bytes>, GTT: <bytes> (kernels vary; be tolerant)
    vram += parseBytes(b, "drm-memory", "VRAM")
    gtt  += parseBytes(b, "drm-memory", "GTT")

    if limits.StopAfterFirst && vram > 0 { break }
  }
  if !sawRender { return ProcUsage{}, false }

  name := readFirstLine(fmt.Sprintf("/proc/%d/comm", pid))
  cmd  := readCmdline(pid)
  uid  := statUID(pid)

  return ProcUsage{PID: pid, UID: uid, Name: name, Cmd: cmd, VRAM: vram, GTT: gtt}, true
}
```

---

## 10) Environment Variables (quick reference)

| Var                        | Default             | Notes                  |
| -------------------------- | ------------------- | ---------------------- |
| `APP_LISTEN_ADDR`          | `:8080`             | HTTP/WS listen         |
| `APP_SAMPLE_INTERVAL`      | `250ms`             | GPU sampling cadence   |
| `APP_ALLOWED_ORIGINS`      | `*`                 | CORS; restrict in prod |
| `APP_DEFAULT_GPU`          | `auto`              | Initial selection      |
| `APP_ENABLE_PROMETHEUS`    | `false`             | `/metrics`             |
| `APP_ENABLE_PPROF`         | `false`             | Enable pprof           |
| `APP_LOG_LEVEL`            | `INFO`              | slog level             |
| `APP_SYSFS_ROOT`           | `/sys`              | testing override       |
| `APP_DEBUGFS_ROOT`         | `/sys/kernel/debug` | optional fallback      |
| `APP_WS_MAX_CLIENTS`       | `1024`              | cap                    |
| `APP_WS_WRITE_TIMEOUT`     | `3s`                | write deadline         |
| `APP_WS_READ_TIMEOUT`      | `30s`               | read/heartbeat         |
| `APP_PROC_ENABLE`          | `true`              | enable Process Top     |
| `APP_PROC_SCAN_INTERVAL`   | `2s`                | proc cadence           |
| `APP_PROC_MAX_PIDS`        | `5000`              | guardrail              |
| `APP_PROC_MAX_FDS_PER_PID` | `64`                | guardrail              |

---

## 11) Agent Execution Checklist (host‑first)

1. **Git init** (done)

    * `git init && git config commit.gpgSign false`
    * First commit: scaffolding.
2. **Backend skeleton** (serve `/hello` via WS, `/healthz`). (done)

    * Commit.
3. **GPU discovery** (host test: list GPUs). (done)

    * Commit with sample output in `DEVLOG.md`.
4. **Metrics sampler** (busy/temp/power/memory).

    * Host test with a GPU workload; screenshot/notes → `DEVLOG.md`.
    * Commit.
5. **Frontend minimal** (tiles + bars; live WS).

    * Commit.
6. **Process Top** (host test):

    * Verify VRAM/GTT appears; note kernel and permissions.
    * If `drm-engine` counters present, compute ms/s; else set `null`.
    * Commit and document findings.
7. **Dockerization** (metrics first):

    * Run with `/dev/kfd` + `/dev/dri`; confirm metrics.
    * Commit.
8. **Process Top in Docker**:

    * Test with default PID namespace (expect container‑only).
    * Test with `--pid=host` (expect host processes).
    * Document both clearly in `TROUBLESHOOTING.md`.
    * Commit.
9. **Hardening (caps/timeouts) & docs**.

    * Commit and tag `v0.1.0`.

---

## 12) Troubleshooting & Devlog Templates

**`DEVLOG.md`**

```
# Dev Log
Date: YYYY-MM-DD
Host: <distro>, Kernel: <version>, Go: <version>, GPU: <model>
- Implemented <feature>. Observations:
  - gpu_busy_percent: present/absent
  - hwmon: power/temp/fan present/absent
  - debugfs amdgpu_pm_info: readable? yes/no
  - fdinfo drm-memory: present? yes/no; sample lines:
    ...
  - fdinfo drm-engine: present? yes/no; fields:
    ...
Issues:
- <short description> (linked to TROUBLESHOOTING.md)
```

**`TROUBLESHOOTING.md`**

```
# Troubleshooting

## Metrics missing (gpu_busy_percent)
- Some kernels lack this sysfs file. Fallback: debugfs amdgpu_pm_info (requires readable debugfs).
- If both absent, GPU busy% shows "—".

## Process Top empty
- Running inside Docker without `--pid=host` → container cannot see host PIDs.
- Host /proc mounted with hidepid=2 → cannot read other users' fdinfo; run as a user with permission or adjust mount options.

## No temp/power/fan
- hwmon not exposed for this device; show "—".

## Permissions
- Ensure container user is in `video` and `render` groups; device nodes exist.
```

---

## 13) Definition of Done

* Works on host and in Docker (with AMD devices mounted).
* Frontend is **read‑only**; GPU selector works.
* Live updates via WebSocket at configured interval.
* **Process Top** shows GPU‑using processes on host; documented behavior in Docker.
* Clean shutdown; no goroutine leaks (`-race` clean).
* CI green; docs & env table present.

---

Keep it small, predictable, and brutally honest about the limits: **process‑level visibility depends on what `/proc` and the kernel expose** and whether you’re in the host PID namespace. Don’t guess; detect capabilities, surface them, and document the gaps.
