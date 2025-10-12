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
