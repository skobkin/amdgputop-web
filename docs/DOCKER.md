# Docker Usage

This application ships with a multi-stage Docker build that embeds the static
frontend into the Go binary and runs it inside a slim Alpine image as a
non-root user.

## Build

```bash
docker build -t amdgputop-web:dev \
  --build-arg VERSION=$(git describe --tags --always --dirty) \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  .
```

## Runtime requirements

- **AMD GPU access**: bind the host DRM/AMDGPU device files into the container.
- **Process telemetry (optional)**: add `--pid=host` if you need per-process
  stats for host workloads. Without it, only processes in the container are
  visible.
- **Permissions**: add the container user to the same groups that can read the
  devices (typically `video` and `render`).

Example run command on a host with one GPU:

```bash
docker run --rm -p 8080:8080 \
  --device=/dev/dri
  --device=/dev/kfd \
  --group-add video \
  --group-add render \
  --pid=host \  # optional; required for host process visibility
  -e APP_ALLOWED_ORIGINS="http://localhost:8080" \
  amdgputop-web:dev
```

If you do not supply `--pid=host`, the process table renders container-local
processes only. The rest of the metrics (busy %, clocks, temps, etc.) continue
to work provided the device nodes are accessible.

## Troubleshooting

- **Permission denied when reading `/dev/dri/renderD*`**: run `ls -l` on the
  device nodes to confirm their group ownership, then add matching
  `--group-add` flags.
- **Missing metrics**: the sampler degrades gracefully when files are absent.
  Use `docker logs` to confirm which counters were skipped.
- **Process table empty**: either no GPU clients are active or `--pid=host` was
  not provided. Note that some distributions mount `/proc` with `hidepid=2`,
  which prevents visibility without additional privileges.
