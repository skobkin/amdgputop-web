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
VID_GID=$(getent group video | cut -d: -f3)
RENDER_GID=$(getent group render | cut -d: -f3)

docker run --rm -p 8080:8080 \
  --device=/dev/dri \
  --device=/dev/kfd \
  --group-add "${VID_GID}" \
  --group-add "${RENDER_GID}" \
  --pid=host \  # required for host process visibility
  --cap-add SYS_PTRACE \  # required to read host /proc entries
  --user root \
  -e APP_ALLOWED_ORIGINS="http://localhost:8080" \
  amdgputop-web:dev
```

If you omit `--pid=host`, the process table renders container-local processes
only. The rest of the metrics (busy %, clocks, temps, etc.) continue to work
provided the device nodes are accessible.

## Troubleshooting

- **Permission denied when reading `/dev/dri/renderD*`**: run `ls -l` on the
  device nodes to confirm their group ownership, then add matching
  `--group-add` flags.
- **Missing metrics**: the sampler degrades gracefully when files are absent.
  Use `docker logs` to confirm which counters were skipped.
- **Process table empty**: either no GPU clients are active or the container
  cannot read their `/proc/<pid>/fdinfo`. Host visibility requires `--pid=host`
  **and** elevated privileges—run as `root` and add `--cap-add SYS_PTRACE`
  (or an equivalent capability set). Some distributions also mount `/proc` with
  `hidepid=2`, which prevents observation without additional permissions.
