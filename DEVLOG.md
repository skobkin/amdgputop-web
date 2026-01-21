# Dev Log
Date: 2025-10-12
Host: Unknown (container), Kernel: Unknown, Go: go1.25.1, GPU: n/a
- Implemented GPU discovery scaffolding. Observations:
  - gpu_busy_percent: n/a
  - hwmon: n/a
  - debugfs amdgpu_pm_info: n/a
  - fdinfo drm-memory: n/a
  - fdinfo drm-engine: n/a
    - Discovery run via `APP_LISTEN_ADDR=:0 go run ./cmd/amdgputop-web`
    - Result: `count=0` GPUs discovered (expected on builder without AMD GPU)
- Implemented metrics reader scaffolding (sysfs/hwmon/debugfs fallbacks). Observations:
  - No AMD devices available in builder to validate numerical output.
- Added sampler manager and WebSocket streaming pipeline. Observations:
  - Unable to validate live telemetry due to absence of AMD GPU; verified framework via automated tests only.
Issues:
- None

Date: 2025-10-13
Host: CI builder (Go 1.25.1)
- Added HTTP/WebSocket observability instrumentation and graceful shutdown coverage.
- `go test -race ./...` currently fails with repeated `hole in findfunctab` linker errors (Go 1.25.1 race runtime bug on this toolchain). Pending upstream fix; tracked for re-test after toolchain update.

Date: 2026-01-21
Host: Unknown (local dev), Kernel: Unknown, Go: Unknown, GPU: Unknown
- Added uPlot-based charts.
- Updated frontend dependencies (preact, zustand, vite, @types/node) and Go module dependencies.
- Refreshed runtime base image and healthcheck tooling; updated screenshot with charts.
Issues:
- None
