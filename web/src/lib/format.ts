export function formatPercent(value: number | null, fractionDigits = 0): string {
  if (value == null || Number.isNaN(value)) {
    return '—';
  }
  return `${value.toFixed(fractionDigits)}%`;
}

export function formatMHz(value: number | null): string {
  if (value == null || Number.isNaN(value)) {
    return '—';
  }
  return `${value.toFixed(0)} MHz`;
}

export function formatTemperature(value: number | null): string {
  if (value == null || Number.isNaN(value)) {
    return '—';
  }
  return `${value.toFixed(0)} °C`;
}

export function formatPower(value: number | null): string {
  if (value == null || Number.isNaN(value)) {
    return '—';
  }
  return `${value.toFixed(0)} W`;
}

export function formatRPM(value: number | null): string {
  if (value == null || Number.isNaN(value)) {
    return '—';
  }
  return `${value.toFixed(0)} RPM`;
}

const BYTE_UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];

export function formatBytes(value: number | null, fractionDigits = 1): string {
  if (value == null || Number.isNaN(value)) {
    return '—';
  }
  let index = 0;
  let result = value;
  while (result >= 1024 && index < BYTE_UNITS.length - 1) {
    result /= 1024;
    index += 1;
  }
  return `${result.toFixed(index === 0 ? 0 : fractionDigits)} ${BYTE_UNITS[index]}`;
}

export function formatTimeAgo(timestamp: string | null): string {
  if (!timestamp) {
    return '—';
  }
  const ts = Date.parse(timestamp);
  if (Number.isNaN(ts)) {
    return '—';
  }
  const diff = Date.now() - ts;
  if (diff < 0) {
    return 'just now';
  }
  if (diff < 1000) {
    return `${Math.round(diff)} ms ago`;
  }
  const seconds = Math.round(diff / 1000);
  if (seconds < 60) {
    return `${seconds}s ago`;
  }
  const minutes = Math.round(seconds / 60);
  return `${minutes}m ago`;
}

export function formatGpuTime(value: number | null): string {
  if (value == null || Number.isNaN(value)) {
    return '—';
  }
  return `${value.toFixed(1)} ms/s`;
}
